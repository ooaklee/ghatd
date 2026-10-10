package revenuestore

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/paymentprovider"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// Audit disposition: named real-Mongo tables cover pre-payment provenance,
// negative authorization evidence, atomic uncertainty and concurrent receipts.
type lifecycleEvidenceFixture struct{ checkoutEvidenceFixture }

func (p *lifecycleEvidenceFixture) LookupRevenueCheckoutSessionEvidence(_ context.Context, _ paymentprovider.RevenueScope, _ string) (paymentprovider.RevenueCheckoutEvidence, error) {
	p.calls++
	return p.evidence, p.err
}
func lifecycleService(t *testing.T, r billing.CheckoutRepository, p *lifecycleEvidenceFixture, at time.Time) *billing.CheckoutService {
	t.Helper()
	s, err := billing.NewCheckoutService(r, fixtureClock{at}, p)
	require.NoError(t, err)
	return s
}
func lifecycleIntent(t *testing.T, s *billing.CheckoutService, ctx context.Context) billing.CheckoutIntent {
	t.Helper()
	r := historicalCheckoutRequest()
	r.TrialPeriodDays = 14
	i, err := s.PrepareCheckout(ctx, checkoutResolverRequest().Scope, r)
	require.NoError(t, err)
	require.NoError(t, s.AcknowledgeCheckout(ctx, i, "cs_original"))
	i, err = s.FindCheckoutIntent(ctx, i.Scope, r.IdempotencyKey)
	require.NoError(t, err)
	return i
}
func TestMongoCheckoutLifecycleEvidence(t *testing.T) {
	cases := []struct {
		name        string
		change      func(*paymentprovider.RevenueCheckoutEvidence)
		providerErr error
		want        error
	}{
		{name: "trial_before_first_payment"},
		{name: "wrong_scope", change: func(e *paymentprovider.RevenueCheckoutEvidence) { e.Scope.AccountID = "acct_other" }, want: billing.ErrRevenueUnassessable},
		{name: "wrong_mode", change: func(e *paymentprovider.RevenueCheckoutEvidence) { e.Scope.LiveMode = true }, want: billing.ErrRevenueUnassessable},
		{name: "wrong_intent", change: func(e *paymentprovider.RevenueCheckoutEvidence) { e.IntentID = "another_intent" }, want: billing.ErrRevenueUnassessable},
		{name: "wrong_session", change: func(e *paymentprovider.RevenueCheckoutEvidence) { e.SessionID = "cs_other" }, want: billing.ErrRevenueUnassessable},
		{name: "wrong_payer", change: func(e *paymentprovider.RevenueCheckoutEvidence) { e.ClientReferenceID = "other_payer" }, want: billing.ErrRevenueUnassessable},
		{name: "missing_customer", change: func(e *paymentprovider.RevenueCheckoutEvidence) { e.CustomerID = "" }, want: billing.ErrRevenueUnassessable},
		{name: "missing_subscription", change: func(e *paymentprovider.RevenueCheckoutEvidence) { e.SubscriptionID = "" }, want: billing.ErrRevenueUnassessable},
		{name: "wrong_original_price", change: func(e *paymentprovider.RevenueCheckoutEvidence) { e.PriceID = "price_other" }, want: billing.ErrRevenueUnassessable},
		{name: "wrong_original_amount", change: func(e *paymentprovider.RevenueCheckoutEvidence) { e.UnitAmountMinor++ }, want: billing.ErrRevenueUnassessable},
		{name: "wrong_original_currency", change: func(e *paymentprovider.RevenueCheckoutEvidence) { e.Currency = "EUR" }, want: billing.ErrRevenueUnassessable},
		{name: "wrong_original_cadence", change: func(e *paymentprovider.RevenueCheckoutEvidence) { e.BillingCadence = "year" }, want: billing.ErrRevenueUnassessable},
		{name: "multiple_interval", change: func(e *paymentprovider.RevenueCheckoutEvidence) { e.IntervalCount = 2 }, want: billing.ErrRevenueUnassessable},
		{name: "unfinished_checkout", change: func(e *paymentprovider.RevenueCheckoutEvidence) { e.Status = "open" }, want: billing.ErrRevenueUnassessable},
		{name: "payment_mode", change: func(e *paymentprovider.RevenueCheckoutEvidence) { e.Mode = paymentprovider.CheckoutModePayment }, want: billing.ErrRevenueUnassessable},
		{name: "before_authorization", change: func(e *paymentprovider.RevenueCheckoutEvidence) { e.CreatedAt = e.CreatedAt.Add(-time.Second) }, want: billing.ErrRevenueUnassessable},
		{name: "future_evidence", change: func(e *paymentprovider.RevenueCheckoutEvidence) { e.CreatedAt = e.CreatedAt.Add(time.Hour) }, want: billing.ErrRevenueUnassessable},
		{name: "outage", providerErr: paymentprovider.ErrPaymentProviderAPIRequestFailed, want: paymentprovider.ErrPaymentProviderAPIRequestFailed},
		{name: "conclusive_unassessable", providerErr: paymentprovider.ErrRevenueUnassessable, want: billing.ErrRevenueUnassessable},
		{name: "joined_outage_is_preserved", providerErr: errors.Join(paymentprovider.ErrRevenueUnassessable, paymentprovider.ErrPaymentProviderAPIRequestFailed), want: paymentprovider.ErrPaymentProviderAPIRequestFailed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo, _, db, ctx := revenueFixture(t)
			p := &lifecycleEvidenceFixture{}
			s := lifecycleService(t, repo, p, time.Unix(1700000000, 0).UTC())
			i := lifecycleIntent(t, s, ctx)
			p.evidence = checkoutEvidence(i)
			p.err = tc.providerErr
			if tc.change != nil {
				tc.change(&p.evidence)
			}
			e, err := s.LookupCheckoutLifecycleEvidence(ctx, i)
			if tc.want != nil {
				require.ErrorIs(t, err, tc.want)
				require.Empty(t, e.SubscriptionID)
				if tc.change != nil {
					_, captureErr := s.CaptureCheckoutLifecycleEvidence(ctx, i, p.evidence)
					require.Error(t, captureErr)
				}
			} else {
				require.NoError(t, err)
				a, err := s.CaptureCheckoutLifecycleEvidence(ctx, i, e)
				require.NoError(t, err)
				require.NoError(t, a.Validate())
				found, err := s.FindCheckoutLifecycleAnchor(ctx, i.Scope, e.SubscriptionID)
				require.NoError(t, err)
				require.Equal(t, a, found)
				p.err = paymentprovider.ErrPaymentProviderAPIRequestFailed
				later := lifecycleService(t, repo, p, i.CreatedAt.Add(time.Hour))
				again, err := later.CaptureCheckoutLifecycleEvidence(ctx, i, e)
				require.NoError(t, err)
				require.Equal(t, a, again)
				require.Equal(t, 1, p.calls)
				// A recovered original receipt keeps its original timestamp even if
				// the current clock has moved backwards; it is not a new observation.
				again, err = lifecycleService(t, repo, p, i.CreatedAt.Add(-time.Hour)).CaptureCheckoutLifecycleEvidence(ctx, i, e)
				require.NoError(t, err)
				require.Equal(t, a, again)
				wire, err := json.Marshal(a)
				require.NoError(t, err)
				require.JSONEq(t, "{}", string(wire))
				var raw bson.M
				require.NoError(t, db.Collection("ghatd_owned_records").FindOne(ctx, bson.M{"kind": kindCheckoutLifecycleAnchor}).Decode(&raw))
				bytes, err := bson.MarshalExtJSON(raw, false, false)
				require.NoError(t, err)
				require.NotContains(t, string(bytes), i.Request.UserID)
				require.NotContains(t, string(bytes), e.SubscriptionID)
				changed := e
				changed.CustomerID = "cus_changed"
				_, err = later.CaptureCheckoutLifecycleEvidence(ctx, i, changed)
				require.ErrorIs(t, err, billing.ErrRevenueConflict)
			}
			// No financial association/fact or current-status record is fabricated.
			for _, kind := range []string{kindCheckoutAssociation, kindCheckoutPrincipal, kindFact, kindSubscriptionStatusHead, kindSubscriptionStatusCapture} {
				n, err := db.Collection("ghatd_owned_records").CountDocuments(ctx, bson.M{"kind": kind})
				require.NoError(t, err)
				require.Zero(t, n)
			}
		})
	}
}

func TestMongoCheckoutLifecycleRetainedHistory(t *testing.T) {
	cases := []struct {
		name                   string
		removeKind             string
		secondPayer            bool
		removeFirstAfterSecond bool
	}{
		{name: "same_payer_second_checkout_retains_first_anchor"},
		{name: "later_receipt_cannot_hide_missing_first_receipt", removeFirstAfterSecond: true},
		{name: "different_payer_second_checkout_conflicts", secondPayer: true},
		{name: "missing_original_anchor_is_not_success", removeKind: kindCheckoutLifecycleAnchor},
		{name: "missing_original_receipt_is_not_success", removeKind: kindCheckoutLifecycleReceipt},
		{name: "missing_original_intent_is_not_success", removeKind: kindCheckoutIntent},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo, _, db, ctx := revenueFixture(t)
			p := &lifecycleEvidenceFixture{}
			s := lifecycleService(t, repo, p, time.Unix(1700000000, 0).UTC())
			i := lifecycleIntent(t, s, ctx)
			e := checkoutEvidence(i)
			first, err := s.CaptureCheckoutLifecycleEvidence(ctx, i, e)
			require.NoError(t, err)
			if tc.removeKind != "" {
				_, err = db.Collection("ghatd_owned_records").DeleteMany(ctx, bson.M{"kind": tc.removeKind})
				require.NoError(t, err)
				_, err = s.FindCheckoutLifecycleAnchor(ctx, i.Scope, e.SubscriptionID)
				require.Error(t, err)
				if tc.removeKind == kindCheckoutLifecycleReceipt || tc.removeKind == kindCheckoutIntent {
					require.ErrorIs(t, err, billing.ErrRevenueUnavailable)
				}
				_, err = s.CaptureCheckoutLifecycleEvidence(ctx, i, e)
				require.ErrorIs(t, err, billing.ErrRevenueUnavailable)
				return
			}
			q := historicalCheckoutRequest()
			q.IdempotencyKey = "second_intent"
			if tc.secondPayer {
				q.UserID = "other_payer"
				q.UserReference = q.UserID
			}
			j, err := s.PrepareCheckout(ctx, i.Scope, q)
			require.NoError(t, err)
			require.NoError(t, s.AcknowledgeCheckout(ctx, j, "cs_second"))
			j, err = s.FindCheckoutIntent(ctx, i.Scope, q.IdempotencyKey)
			require.NoError(t, err)
			other := checkoutEvidence(j)
			other.SessionID = "cs_second"
			second, err := s.CaptureCheckoutLifecycleEvidence(ctx, j, other)
			if tc.secondPayer {
				require.ErrorIs(t, err, billing.ErrRevenueConflict)
			} else {
				require.NoError(t, err)
				require.NotEqual(t, first.Fingerprint, second.Fingerprint)
				again, err := s.CaptureCheckoutLifecycleEvidence(ctx, j, other)
				require.NoError(t, err)
				require.Equal(t, second, again)
			}
			found, err := s.FindCheckoutLifecycleAnchor(ctx, i.Scope, e.SubscriptionID)
			require.NoError(t, err)
			require.Equal(t, first, found)
			if tc.removeFirstAfterSecond {
				deleted, err := db.Collection("ghatd_owned_records").DeleteOne(ctx, bson.M{"kind": kindCheckoutLifecycleReceipt, "id": i.ID})
				require.NoError(t, err)
				require.Equal(t, int64(1), deleted.DeletedCount)
				_, err = s.CaptureCheckoutLifecycleEvidence(ctx, j, other)
				require.ErrorIs(t, err, billing.ErrRevenueUnavailable)
			}
		})
	}
}

func TestMongoCheckoutLifecycleAtomicRecovery(t *testing.T) {
	cases := []struct {
		name      string
		at        int
		uncertain bool
	}{{"anchor_rollback", 1, false}, {"receipt_rollback", 2, false}, {"lost_commit_reply", 0, true}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo, store, db, ctx := revenueFixture(t)
			p := &lifecycleEvidenceFixture{}
			s := lifecycleService(t, repo, p, time.Unix(1700000000, 0).UTC())
			i := lifecycleIntent(t, s, ctx)
			p.evidence = checkoutEvidence(i)
			e, err := s.LookupCheckoutLifecycleEvidence(ctx, i)
			require.NoError(t, err)
			broken, err := NewRepository(failingStore{Store: store, at: tc.at, uncertain: tc.uncertain})
			require.NoError(t, err)
			_, err = lifecycleService(t, broken, p, i.CreatedAt).CaptureCheckoutLifecycleEvidence(ctx, i, e)
			require.Error(t, err)
			n, countErr := db.Collection("ghatd_owned_records").CountDocuments(ctx, bson.M{"kind": bson.M{"$in": []string{kindCheckoutLifecycleAnchor, kindCheckoutLifecycleReceipt}}})
			require.NoError(t, countErr)
			if tc.uncertain {
				require.ErrorIs(t, err, billing.ErrRevenueUncertain)
				require.Equal(t, int64(2), n)
			} else {
				require.Zero(t, n)
			}
			a, err := s.CaptureCheckoutLifecycleEvidence(ctx, i, e)
			require.NoError(t, err)
			found, err := s.FindCheckoutLifecycleAnchor(ctx, i.Scope, e.SubscriptionID)
			require.NoError(t, err)
			require.Equal(t, a, found)
			require.Equal(t, 1, p.calls)
		})
	}
}

func TestMongoCheckoutLifecyclePaymentOwnership(t *testing.T) {
	cases := []struct {
		name       string
		paidFirst  bool
		otherPayer bool
	}{{"trial_then_first_payment_same_payer", false, false}, {"payment_then_anchor_same_payer", true, false}, {"trial_rejects_payment_other_payer", false, true}, {"payment_rejects_trial_other_payer", true, true}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo, _, _, ctx := revenueFixture(t)
			p := &lifecycleEvidenceFixture{}
			s := lifecycleService(t, repo, p, time.Unix(1700000000, 0).UTC())
			i := lifecycleIntent(t, s, ctx)
			e := checkoutEvidence(i)
			other := historicalCheckoutRequest()
			other.IdempotencyKey = "second_attempt"
			if tc.otherPayer {
				other.UserID = "another_payer"
				other.UserReference = other.UserID
			}
			j, err := s.PrepareCheckout(ctx, i.Scope, other)
			require.NoError(t, err)
			require.NoError(t, s.AcknowledgeCheckout(ctx, j, "cs_second"))
			j, err = s.FindCheckoutIntent(ctx, i.Scope, other.IdempotencyKey)
			require.NoError(t, err)
			second := checkoutEvidence(j)
			second.SessionID = "cs_second"
			if tc.paidFirst {
				p.evidence = e
				_, err = s.ResolveRevenueAssociation(ctx, checkoutResolverRequest())
				require.NoError(t, err)
				_, err = s.CaptureCheckoutLifecycleEvidence(ctx, j, second)
			} else {
				_, err = s.CaptureCheckoutLifecycleEvidence(ctx, i, e)
				require.NoError(t, err)
				// Select a separate original price to force native paid insertion instead
				// of recovering an already stored immutable association.
				// The prior lifecycle identity still guards the same subscription.
				p.evidence = second
				_, err = s.ResolveRevenueAssociation(ctx, checkoutResolverRequest())
			}
			if tc.otherPayer {
				require.ErrorIs(t, err, billing.ErrRevenueConflict)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestMongoCheckoutLifecycleConcurrentCapture(t *testing.T) {
	cases := []struct {
		name    string
		workers int
	}{{"two_original_retries", 2}, {"eight_original_retries", 8}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo, _, db, ctx := revenueFixture(t)
			p := &lifecycleEvidenceFixture{}
			s := lifecycleService(t, repo, p, time.Unix(1700000000, 0).UTC())
			i := lifecycleIntent(t, s, ctx)
			e := checkoutEvidence(i)
			var wg sync.WaitGroup
			results := make(chan billing.CheckoutLifecycleAnchor, tc.workers)
			errs := make(chan error, tc.workers)
			for k := 0; k < tc.workers; k++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					a, err := s.CaptureCheckoutLifecycleEvidence(ctx, i, e)
					results <- a
					errs <- err
				}()
			}
			wg.Wait()
			close(results)
			close(errs)
			for err := range errs {
				require.NoError(t, err)
			}
			var first billing.CheckoutLifecycleAnchor
			for a := range results {
				if first.IntentID == "" {
					first = a
				}
				require.Equal(t, first, a)
			}
			n, err := db.Collection("ghatd_owned_records").CountDocuments(ctx, bson.M{"kind": bson.M{"$in": []string{kindCheckoutLifecycleAnchor, kindCheckoutLifecycleReceipt}}})
			require.NoError(t, err)
			require.Equal(t, int64(2), n)
		})
	}
}
