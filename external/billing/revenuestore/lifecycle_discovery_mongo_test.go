package revenuestore

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/paymentprovider"
	"github.com/ooaklee/ghatd/external/repository/recordstore"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// Fresh named replica-set cases verify atomic source projection, immutable
// owner pointers and financial compatibility. These are not collector E2E.
func TestMongoLifecycleSourceProjection(t *testing.T) {
	cases := []struct {
		name, action string
		failAt       int
		uncertain    bool
	}{
		{name: "acknowledged_subscription_is_indexed"},
		{name: "payment_checkout_is_not_subscription_source", action: "payment-mode"},
		{name: "unacknowledged_checkout_is_not_discoverable", action: "unacknowledged"},
		{name: "anchor_then_payment_joins_same_owner", action: "anchor-first"},
		{name: "payment_then_anchor_joins_same_owner", action: "payment-first"},
		{name: "renewal_keeps_original_payment_pointer", action: "renewal"},
		{name: "contradictory_payment_payer_rolls_back", action: "wrong-payer"},
		{name: "contradictory_payment_customer_rolls_back", action: "wrong-customer"},
		{name: "legacy_payment_without_customer_stays_financial_only", action: "legacy"},
		{name: "retained_anchor_guards_upgrade_without_projection", action: "legacy-anchor-conflict"},
		{name: "second_checkout_keeps_first_anchor_pointer", action: "second-anchor"},
		{name: "upgrade_payment_retains_existing_first_anchor", action: "upgrade-payment"},
		{name: "ack_projection_rollback", action: "ack-failure", failAt: 3},
		{name: "ack_uncertain_commit_recovers_original", action: "ack-failure", uncertain: true},
		{name: "payment_projection_rollback", action: "payment-failure", failAt: 3},
		{name: "payment_uncertain_commit_recovers_original", action: "payment-failure", uncertain: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, store, db, ctx := revenueFixture(t)
			p := &lifecycleEvidenceFixture{}
			checkout := lifecycleService(t, r, p, time.Unix(1700000000, 0).UTC())
			q := historicalCheckoutRequest()
			if tc.action == "payment-mode" {
				q.Mode = paymentprovider.CheckoutModePayment
				q.ExpectedBillingCadence = "one_time"
			}
			intent, err := checkout.PrepareCheckout(ctx, checkoutResolverRequest().Scope, q)
			require.NoError(t, err)
			if tc.action != "unacknowledged" {
				if tc.action == "ack-failure" {
					broken, err := NewRepository(failingStore{Store: store, at: tc.failAt, uncertain: tc.uncertain})
					require.NoError(t, err)
					attempt := lifecycleService(t, broken, p, intent.CreatedAt)
					err = attempt.AcknowledgeCheckout(ctx, intent, "cs_original")
					require.Error(t, err)
					if tc.uncertain {
						require.ErrorIs(t, err, billing.ErrRevenueUncertain)
					} else {
						for _, kind := range []string{kindCheckoutAck, kindCheckoutSession, kindLifecycleCheckoutSource} {
							n, e := db.Collection("ghatd_owned_records").CountDocuments(ctx, bson.M{"kind": kind})
							require.NoError(t, e)
							require.Zero(t, n)
						}
					}
				}
				require.NoError(t, checkout.AcknowledgeCheckout(ctx, intent, "cs_original"))
				require.NoError(t, checkout.AcknowledgeCheckout(ctx, intent, "cs_original"))
				intent, err = checkout.FindCheckoutIntent(ctx, intent.Scope, q.IdempotencyKey)
				require.NoError(t, err)
			}
			n, err := db.Collection("ghatd_owned_records").CountDocuments(ctx, bson.M{"kind": kindLifecycleCheckoutSource})
			require.NoError(t, err)
			if tc.action == "payment-mode" || tc.action == "unacknowledged" {
				require.Zero(t, n)
				return
			}
			require.Equal(t, int64(1), n)
			evidence := checkoutEvidence(intent)
			f := revenueFact()
			f.Scope = intent.Scope
			f.PrincipalID = intent.Request.UserID
			f.ProviderCustomerID = evidence.CustomerID
			f.SubscriptionID = evidence.SubscriptionID
			f.Currency = "GBP"
			revenue := revenueService(t, r)
			var firstFact string
			capturePayment := func() { o := accept(t, revenue, ctx, "original-payment", f); firstFact = o.FactIDs[0] }
			if tc.action == "payment-first" {
				capturePayment()
			}
			if tc.action != "legacy" && tc.action != "payment-failure" {
				if tc.action != "" && tc.action != "ack-failure" {
					_, err = checkout.CaptureCheckoutLifecycleEvidence(ctx, intent, evidence)
					require.NoError(t, err)
				}
			}
			switch tc.action {
			case "anchor-first", "payment-first", "renewal", "second-anchor", "upgrade-payment":
				if tc.action == "upgrade-payment" {
					_, err = db.Collection("ghatd_owned_records").DeleteMany(ctx, bson.M{"kind": kindLifecycleSubscriptionSource})
					require.NoError(t, err)
				}
				if firstFact == "" {
					capturePayment()
				}
				if tc.action == "renewal" {
					f.PaymentID = "next-payment"
					f.InvoiceID = "next-invoice"
					accept(t, revenue, ctx, "next-envelope", f)
				}
				if tc.action == "second-anchor" {
					q.IdempotencyKey = "later-checkout"
					j, e := checkout.PrepareCheckout(ctx, intent.Scope, q)
					require.NoError(t, e)
					require.NoError(t, checkout.AcknowledgeCheckout(ctx, j, "cs_later"))
					j, e = checkout.FindCheckoutIntent(ctx, intent.Scope, q.IdempotencyKey)
					require.NoError(t, e)
					other := checkoutEvidence(j)
					other.SessionID = j.SessionID
					_, e = checkout.CaptureCheckoutLifecycleEvidence(ctx, j, other)
					require.NoError(t, e)
				}
			case "wrong-payer", "wrong-customer", "legacy-anchor-conflict":
				if tc.action == "legacy-anchor-conflict" {
					_, err = db.Collection("ghatd_owned_records").DeleteMany(ctx, bson.M{"kind": kindLifecycleSubscriptionSource})
					require.NoError(t, err)
				}
				if tc.action == "wrong-customer" {
					f.ProviderCustomerID = "another-customer"
				} else {
					f.PrincipalID = "another-principal"
				}
				_, err = revenue.AcceptVerified(ctx, billing.VerifiedRevenueRequest{Scope: f.Scope, EnvelopeID: "conflicting", Facts: []billing.RevenueFact{f}})
				require.ErrorIs(t, err, billing.ErrRevenueConflict)
				for _, kind := range []string{kindFact, kindObservation} {
					n, e := db.Collection("ghatd_owned_records").CountDocuments(ctx, bson.M{"kind": kind})
					require.NoError(t, e)
					require.Zero(t, n)
				}
			case "legacy":
				f.ProviderCustomerID = ""
				capturePayment()
				n, err := db.Collection("ghatd_owned_records").CountDocuments(ctx, bson.M{"kind": kindLifecycleSubscriptionSource})
				require.NoError(t, err)
				require.Zero(t, n)
			case "payment-failure":
				broken, e := NewRepository(failingStore{Store: store, at: tc.failAt, uncertain: tc.uncertain})
				require.NoError(t, e)
				_, err = revenueService(t, broken).AcceptVerified(ctx, billing.VerifiedRevenueRequest{Scope: f.Scope, EnvelopeID: "original-payment", Facts: []billing.RevenueFact{f}})
				require.Error(t, err)
				if tc.uncertain {
					require.ErrorIs(t, err, billing.ErrRevenueUncertain)
				} else {
					for _, kind := range []string{kindFact, kindObservation, kindHead, kindLifecycleSubscriptionSource} {
						n, e := db.Collection("ghatd_owned_records").CountDocuments(ctx, bson.M{"kind": kind})
						require.NoError(t, e)
						require.Zero(t, n)
					}
				}
				capturePayment()
			}
			if tc.action == "anchor-first" || tc.action == "payment-first" || tc.action == "renewal" || tc.action == "second-anchor" || tc.action == "upgrade-payment" || tc.action == "payment-failure" {
				var source lifecycleSubscriptionSource
				require.NoError(t, store.Read(ctx, func(tx recordstore.Tx) error {
					v, _, e := get[lifecycleSubscriptionSource](ctx, tx, kindLifecycleSubscriptionSource, lifecycleSubscriptionKey(f.Scope, f.SubscriptionID), lifecycleSourcePartition(f.Scope))
					source = v
					return e
				}))
				require.Equal(t, firstFact, source.FactID)
				require.Equal(t, intent.Request.UserID, source.PrincipalID)
				if tc.action != "payment-failure" {
					require.Equal(t, intent.ID, source.AnchorIntentID)
				}
			}
			// Source kinds neither create a status observation nor financial earnings.
			for _, kind := range []string{kindSubscriptionStatusHead, kindSubscriptionStatusCapture} {
				n, e := db.Collection("ghatd_owned_records").CountDocuments(ctx, bson.M{"kind": kind})
				require.NoError(t, e)
				require.Zero(t, n)
			}
			cursor, e := db.Collection("ghatd_owned_records").Find(ctx, bson.M{"kind": bson.M{"$in": []string{kindLifecycleCheckoutSource, kindLifecycleSubscriptionSource}}})
			require.NoError(t, e)
			defer cursor.Close(ctx)
			var raw []bson.M
			require.NoError(t, cursor.All(ctx, &raw))
			body, e := json.Marshal(raw)
			require.NoError(t, e)
			for _, private := range []string{intent.Request.UserID, evidence.CustomerID, evidence.SubscriptionID, intent.ID, "cs_original"} {
				require.NotContains(t, string(body), private)
			}
		})
	}
}

// A barrier forces BOTH native guards to observe the owner absent before either
// inserts. Callback repetitions bypass the barrier; provider I/O is absent.
type ownerBarrierStore struct {
	recordstore.Store
	count atomic.Int32
	ready chan struct{}
}
type ownerBarrierTx struct {
	recordstore.Tx
	store *ownerBarrierStore
}

func (s *ownerBarrierStore) Transact(ctx context.Context, guard string, fn func(recordstore.Tx) error) error {
	return s.Store.Transact(ctx, guard, func(tx recordstore.Tx) error { return fn(&ownerBarrierTx{tx, s}) })
}
func (t *ownerBarrierTx) Get(ctx context.Context, kind, id string) (recordstore.Record, error) {
	row, err := t.Tx.Get(ctx, kind, id)
	if kind == kindLifecycleSubscriptionSource && errors.Is(err, recordstore.ErrNotFound) {
		n := t.store.count.Add(1)
		if n == 2 {
			close(t.store.ready)
		}
		if n <= 2 {
			select {
			case <-t.store.ready:
			case <-ctx.Done():
				return recordstore.Record{}, ctx.Err()
			}
		}
	}
	return row, err
}
func TestMongoLifecycleOwnerCrossGuardConcurrency(t *testing.T) {
	for _, tc := range []struct {
		name                      string
		wrongPayer, wrongCustomer bool
	}{{name: "same_owner_joins_after_retry"}, {name: "different_payer_cannot_commit_both", wrongPayer: true}, {name: "different_customer_cannot_commit_both", wrongCustomer: true}} {
		t.Run(tc.name, func(t *testing.T) {
			r, store, db, ctx := revenueFixture(t)
			p := &lifecycleEvidenceFixture{}
			checkout := lifecycleService(t, r, p, time.Unix(1700000000, 0).UTC())
			intent := lifecycleIntent(t, checkout, ctx)
			e := checkoutEvidence(intent)
			f := revenueFact()
			f.Scope = intent.Scope
			f.PrincipalID = intent.Request.UserID
			f.ProviderCustomerID = e.CustomerID
			f.SubscriptionID = e.SubscriptionID
			if tc.wrongPayer {
				f.PrincipalID = "other-payer"
			}
			if tc.wrongCustomer {
				f.ProviderCustomerID = "other-customer"
			}
			barrier := &ownerBarrierStore{Store: store, ready: make(chan struct{})}
			raced, err := NewRepository(barrier)
			require.NoError(t, err)
			trial := lifecycleService(t, raced, p, intent.CreatedAt)
			paid := revenueService(t, raced)
			var wg sync.WaitGroup
			wg.Add(2)
			errs := make([]error, 2)
			go func() { defer wg.Done(); _, errs[0] = trial.CaptureCheckoutLifecycleEvidence(ctx, intent, e) }()
			go func() {
				defer wg.Done()
				_, errs[1] = paid.AcceptVerified(ctx, billing.VerifiedRevenueRequest{Scope: f.Scope, EnvelopeID: "race-paid", Facts: []billing.RevenueFact{f}})
			}()
			wg.Wait()
			require.GreaterOrEqual(t, barrier.count.Load(), int32(2))
			if tc.wrongPayer || tc.wrongCustomer {
				require.False(t, errs[0] == nil && errs[1] == nil)
			}
			// Retry against the winning immutable owner, not another provider lookup.
			_, trialErr := checkout.CaptureCheckoutLifecycleEvidence(ctx, intent, e)
			_, paidErr := revenueService(t, r).AcceptVerified(ctx, billing.VerifiedRevenueRequest{Scope: f.Scope, EnvelopeID: "race-paid", Facts: []billing.RevenueFact{f}})
			if tc.wrongPayer || tc.wrongCustomer {
				require.True(t, (trialErr == nil && errors.Is(paidErr, billing.ErrRevenueConflict)) || (paidErr == nil && errors.Is(trialErr, billing.ErrRevenueConflict)))
			} else {
				require.NoError(t, trialErr)
				require.NoError(t, paidErr)
			}
			n, err := db.Collection("ghatd_owned_records").CountDocuments(ctx, bson.M{"kind": kindLifecycleSubscriptionSource})
			require.NoError(t, err)
			require.Equal(t, int64(1), n)
			require.NoError(t, store.Read(ctx, func(tx recordstore.Tx) error {
				epoch, err := readLifecycleSourceEpoch(ctx, tx, intent.Scope)
				expected := int64(3) // ack + first anchor + accepted payment
				if tc.wrongPayer || tc.wrongCustomer {
					expected = 2
				}
				require.Equal(t, expected, epoch, "cross-guard source increments are conserved; original retries do not advance epoch")
				return err
			}))
		})
	}
}
