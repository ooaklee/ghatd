package revenuestore

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/repository/recordstore"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// Audit disposition: fresh replica-set cases exercise pre-payment codec,
// compatible legacy upgrade, lost replies and cross-provenance head races.
func TestMongoCheckoutSubscriptionStatus(t *testing.T) {
	cases := []struct {
		name, state string
		failAt      int
		uncertain   bool
	}{{name: "trial_before_payment", state: "trialing"}, {name: "active_is_not_paid", state: "active"}, {name: "canceled_before_payment", state: "canceled"}, {name: "paused_before_payment", state: "paused"}, {name: "capture_rollback", state: "trialing", failAt: 1}, {name: "head_rollback", state: "trialing", failAt: 2}, {name: "uncertain_commit_original_retry", state: "trialing", uncertain: true}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, store, db, ctx := revenueFixture(t)
			provider := &lifecycleEvidenceFixture{}
			checkout := lifecycleService(t, r, provider, time.Unix(1700000000, 0).UTC())
			i := lifecycleIntent(t, checkout, ctx)
			e := checkoutEvidence(i)
			_, err := checkout.CaptureCheckoutLifecycleEvidence(ctx, i, e)
			require.NoError(t, err)
			clock := &subscriptionClock{i.CreatedAt.Add(time.Second)}
			owner, err := billing.NewRevenueService(r, clock)
			require.NoError(t, err)
			p, err := owner.PrepareSubscriptionStatusForCheckout(ctx, "worker", i.Scope, e.SubscriptionID)
			require.NoError(t, err)
			require.Empty(t, p.FactID)
			if tc.failAt != 0 || tc.uncertain {
				broken, err := NewRepository(failingStore{Store: store, at: tc.failAt, uncertain: tc.uncertain})
				require.NoError(t, err)
				attempt, err := billing.NewRevenueService(broken, clock)
				require.NoError(t, err)
				v, err := attempt.CaptureVerifiedSubscriptionStatus(ctx, p, nativeStatusEvidence(p, tc.state))
				require.Error(t, err)
				require.Zero(t, v)
				if tc.uncertain {
					require.ErrorIs(t, err, billing.ErrRevenueUncertain)
				} else {
					n, err := db.Collection("ghatd_owned_records").CountDocuments(ctx, bson.M{"kind": bson.M{"$in": []string{kindSubscriptionStatusHead, kindSubscriptionStatusCapture}}})
					require.NoError(t, err)
					require.Zero(t, n)
				}
			}
			first, err := owner.CaptureVerifiedSubscriptionStatus(ctx, p, nativeStatusEvidence(p, tc.state))
			require.NoError(t, err)
			current, err := owner.GetSubscriptionStatusForCheckout(ctx, i.Scope, e.SubscriptionID, time.Minute)
			require.NoError(t, err)
			require.Equal(t, first, current)
			clock.at = clock.at.Add(time.Second)
			next, err := owner.PrepareSubscriptionStatusForCheckout(ctx, "replacement", i.Scope, e.SubscriptionID)
			require.NoError(t, err)
			_, err = owner.CaptureVerifiedSubscriptionStatus(ctx, next, nativeStatusEvidence(next, "paused"))
			require.NoError(t, err)
			clock.at = i.CreatedAt.Add(-time.Hour)
			original, err := owner.CaptureVerifiedSubscriptionStatus(ctx, p, nativeStatusEvidence(p, tc.state))
			require.NoError(t, err)
			require.Equal(t, first, original)
			n, err := db.Collection("ghatd_owned_records").CountDocuments(ctx, bson.M{"kind": kindFact})
			require.NoError(t, err)
			require.Zero(t, n)
			var raw bson.M
			require.NoError(t, db.Collection("ghatd_owned_records").FindOne(ctx, bson.M{"kind": kindSubscriptionStatusCapture}).Decode(&raw))
			bytes, err := bson.MarshalExtJSON(raw, false, false)
			require.NoError(t, err)
			require.NotContains(t, string(bytes), i.Request.UserID)
			require.NotContains(t, string(bytes), e.CustomerID)
			require.NotContains(t, string(bytes), p.CheckoutIntentID)
		})
	}
}

func TestMongoSubscriptionStatusLegacyUpgrade(t *testing.T) {
	cases := []struct {
		name       string
		omitSource bool
	}{{"original_codec_without_source_fields", true}, {"empty_source_preserves_original_receipts", false}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, store, db, ctx := revenueFixture(t)
			owner, _, id := nativeStatus(t, r, ctx)
			p, err := owner.PrepareSubscriptionStatus(ctx, "legacy", id)
			require.NoError(t, err)
			// Recreate a literal old codec shape; fingerprint uses the original ordered
			// body, not preparationBody from the implementation being tested.
			body := struct {
				Fact, Fingerprint, Actor, Provider, Account, Principal, Customer, Subscription string
				Live                                                                           bool
				ExpectedRevision                                                               int64
				ExpectedFingerprint, RequestedAt                                               string
			}{p.FactID, p.FactFingerprint, p.ActorID, p.Scope.Provider, p.Scope.AccountID, p.PrincipalID, p.ProviderCustomerID, p.SubscriptionID, p.Scope.LiveMode, p.ExpectedRevision, p.ExpectedFingerprint, p.RequestedAt.UTC().Format(time.RFC3339Nano)}
			data, err := json.Marshal([]any{body, "active", false})
			require.NoError(t, err)
			sum := sha256.Sum256(data)
			v := billing.SubscriptionStatus{Preparation: p, Status: "active", ObservedAt: p.RequestedAt, Revision: 1, Fingerprint: hex.EncodeToString(sum[:])}
			require.NoError(t, v.Validate())
			data, err = json.Marshal(persistSubscriptionStatus(v))
			require.NoError(t, err)
			var old map[string]any
			require.NoError(t, json.Unmarshal(data, &old))
			if tc.omitSource {
				delete(old, "Source")
				delete(old, "CheckoutIntentID")
				delete(old, "CheckoutFingerprint")
			}
			identity := billing.SubscriptionStatusIdentity(p.Scope, p.SubscriptionID)
			err = store.Transact(ctx, identity, func(tx recordstore.Tx) error {
				for _, item := range []struct{ kind, id string }{{kindSubscriptionStatusCapture, p.CaptureID}, {kindSubscriptionStatusHead, identity}} {
					row, err := recordstore.NewRecord(item.kind, item.id, identity, 1, old)
					if err != nil {
						return err
					}
					row.State = "active"
					if err := tx.Insert(ctx, row); err != nil {
						return err
					}
				}
				return nil
			})
			require.NoError(t, err)
			found, err := owner.GetSubscriptionStatusForFact(ctx, id, time.Minute)
			require.NoError(t, err)
			require.Equal(t, v, found)
			next, err := owner.PrepareSubscriptionStatus(ctx, "new_worker", id)
			require.NoError(t, err)
			require.EqualValues(t, 1, next.ExpectedRevision)
			n, err := db.Collection("ghatd_owned_records").CountDocuments(ctx, bson.M{"kind": kindSubscriptionStatusCapture})
			require.NoError(t, err)
			require.EqualValues(t, 1, n, "upgrade read cannot rewrite receipts")
		})
	}
}

func TestMongoSubscriptionStatusCrossSourceRace(t *testing.T) {
	cases := []struct {
		name       string
		otherPayer bool
	}{{"same_payer_one_first_revision", false}, {"different_payer_rejected_before_status_head", true}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, _, db, ctx := revenueFixture(t)
			provider := &lifecycleEvidenceFixture{}
			checkout := lifecycleService(t, r, provider, time.Unix(1700000000, 0).UTC())
			i := lifecycleIntent(t, checkout, ctx)
			e := checkoutEvidence(i)
			_, err := checkout.CaptureCheckoutLifecycleEvidence(ctx, i, e)
			require.NoError(t, err)
			clock := &subscriptionClock{i.CreatedAt.Add(time.Second)}
			owner, err := billing.NewRevenueService(r, clock)
			require.NoError(t, err)
			fact := billing.RevenueFact{Scope: i.Scope, Kind: billing.RevenuePayment, PaymentID: "pi_first", InvoiceID: "in_first", AllocationID: "il_first", PrincipalID: i.Request.UserID, ProviderCustomerID: e.CustomerID, SubscriptionID: e.SubscriptionID, PlanID: i.Request.PlanID, CostID: i.Request.CostID, Currency: "GBP", CurrencyExponent: 2, PaidMinor: 1000, EffectiveAt: clock.at}
			if tc.otherPayer {
				fact.PrincipalID = "other_payer"
				// Native source ownership now rejects this contradiction at
				// financial acceptance, before a wrong-payer status is possible.
				rejected, err := owner.AcceptVerified(ctx, billing.VerifiedRevenueRequest{Scope: fact.Scope, EnvelopeID: "payment_first", Facts: []billing.RevenueFact{fact}})
				require.ErrorIs(t, err, billing.ErrRevenueConflict)
				require.Zero(t, rejected)
				for _, kind := range []string{kindFact, kindObservation, kindHead, kindSubscriptionStatusHead} {
					n, err := db.Collection("ghatd_owned_records").CountDocuments(ctx, bson.M{"kind": kind})
					require.NoError(t, err)
					require.Zero(t, n)
				}
				trial, err := owner.PrepareSubscriptionStatusForCheckout(ctx, "trial_worker", i.Scope, e.SubscriptionID)
				require.NoError(t, err)
				_, err = owner.CaptureVerifiedSubscriptionStatus(ctx, trial, nativeStatusEvidence(trial, "trialing"))
				require.NoError(t, err)
				status, err := owner.GetSubscriptionStatusForCheckout(ctx, i.Scope, e.SubscriptionID, time.Minute)
				require.NoError(t, err)
				require.Equal(t, "trialing", status.Status)
				return
			}
			o := accept(t, owner, ctx, "payment_first", fact)
			trial, err := owner.PrepareSubscriptionStatusForCheckout(ctx, "trial_worker", i.Scope, e.SubscriptionID)
			require.NoError(t, err)
			paid, err := owner.PrepareSubscriptionStatus(ctx, "paid_worker", o.FactIDs[0])
			require.NoError(t, err)
			var wg sync.WaitGroup
			errs := make(chan error, 2)
			for _, p := range []billing.SubscriptionStatusPreparation{trial, paid} {
				wg.Add(1)
				go func(p billing.SubscriptionStatusPreparation) {
					defer wg.Done()
					_, err := owner.CaptureVerifiedSubscriptionStatus(ctx, p, nativeStatusEvidence(p, "active"))
					errs <- err
				}(p)
			}
			wg.Wait()
			close(errs)
			wins := 0
			for err := range errs {
				if err == nil {
					wins++
				} else {
					require.ErrorIs(t, err, billing.ErrRevenueConflict)
				}
			}
			require.Equal(t, 1, wins)
			a, err := owner.GetSubscriptionStatusForCheckout(ctx, i.Scope, e.SubscriptionID, time.Minute)
			require.NoError(t, err)
			b, err := owner.GetSubscriptionStatusForFact(ctx, o.FactIDs[0], time.Minute)
			require.NoError(t, err)
			require.Equal(t, a, b)
		})
	}
}
