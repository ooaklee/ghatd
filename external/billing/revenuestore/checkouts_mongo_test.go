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

// Audit disposition: new named replica-set cases cover immutable checkout
// identity, strict evidence, atomic linkage, lost acknowledgements and races.
type checkoutEvidenceFixture struct {
	evidence paymentprovider.RevenueCheckoutEvidence
	err      error
	calls    int
}

func (p *checkoutEvidenceFixture) LookupRevenueCheckout(context.Context, paymentprovider.RevenueScope, string) (paymentprovider.RevenueCheckoutEvidence, error) {
	p.calls++
	return p.evidence, p.err
}
func historicalCheckoutRequest() paymentprovider.CheckoutSessionRequest {
	return paymentprovider.CheckoutSessionRequest{PriceID: "price_frozen", PlanID: "plan_frozen", PlanSlug: "pro", PlanName: "Pro", CostID: "cost_frozen", UserID: "owning_principal", UserReference: "owning_principal", CustomerEmail: "original@example.test", Mode: paymentprovider.CheckoutModeSubscription, ReturnURL: "https://app.example.test/checkout", ExpectedAmount: 1000, ExpectedCurrency: "GBP", ExpectedBillingCadence: "month", IdempotencyKey: "attempt_original", Metadata: map[string]string{"plan_id": "plan_frozen"}}
}
func checkoutResolverRequest() billing.RevenueAssociationRequest {
	return billing.RevenueAssociationRequest{Scope: billing.RevenueScope{Provider: "stripe", AccountID: "acct_primary"}, InvoiceID: "in_paid", PaymentID: "pi_paid", CustomerID: "cus_payer", SubscriptionID: "sub_recurring", ProviderPriceID: "price_frozen", Currency: "GBP", PaidAt: time.Unix(1700000100, 0).UTC()}
}
func checkoutEvidence(intent billing.CheckoutIntent) paymentprovider.RevenueCheckoutEvidence {
	return paymentprovider.RevenueCheckoutEvidence{Scope: paymentprovider.RevenueScope{Provider: intent.Scope.Provider, AccountID: intent.Scope.AccountID, LiveMode: intent.Scope.LiveMode}, IntentID: intent.ID, SessionID: "cs_original", ClientReferenceID: intent.Request.UserID, CustomerID: "cus_payer", SubscriptionID: "sub_recurring", PriceID: intent.Request.PriceID, Currency: "GBP", Mode: paymentprovider.CheckoutModeSubscription, Status: "complete", CreatedAt: intent.CreatedAt, UnitAmountMinor: 1000, IntervalCount: 1, BillingCadence: "month"}
}
func checkoutServiceFixture(t *testing.T, r billing.CheckoutRepository, p *checkoutEvidenceFixture) *billing.CheckoutService {
	t.Helper()
	s, err := billing.NewCheckoutService(r, fixtureClock{time.Unix(1700000000, 0).UTC()}, p)
	require.NoError(t, err)
	return s
}
func TestMongoCheckoutHistoricalBinding(t *testing.T) {
	type testCase struct {
		name        string
		evidence    func(*paymentprovider.RevenueCheckoutEvidence)
		request     func(*billing.RevenueAssociationRequest)
		providerErr error
		want        error
	}
	cases := []testCase{
		{name: "pre_submission_intent_links_without_post_acknowledgement"},
		{name: "another_paying_principal_is_not_inferred_from_provider_metadata", evidence: func(e *paymentprovider.RevenueCheckoutEvidence) { e.ClientReferenceID = "organization_seat" }, want: billing.ErrRevenueUnassessable},
		{name: "legacy_subscription_without_owning_intent_is_not_backfilled", evidence: func(e *paymentprovider.RevenueCheckoutEvidence) { e.IntentID = "legacy_absent_intent" }, want: billing.ErrRevenueUnassessable},
		{name: "wrong_customer_is_not_joined_by_email", evidence: func(e *paymentprovider.RevenueCheckoutEvidence) { e.CustomerID = "cus_other" }, want: billing.ErrRevenueUnassessable},
		{name: "another_account_is_not_the_same_economics", request: func(r *billing.RevenueAssociationRequest) { r.Scope.AccountID = "acct_other" }, want: billing.ErrRevenueUnassessable},
		{name: "test_live_isolation", request: func(r *billing.RevenueAssociationRequest) { r.Scope.LiveMode = true }, want: billing.ErrRevenueUnassessable},
		{name: "portal_price_change_remains_pending", request: func(r *billing.RevenueAssociationRequest) { r.ProviderPriceID = "price_changed" }, want: billing.ErrRevenueUnassessable},
		{name: "historical_currency_mismatch", request: func(r *billing.RevenueAssociationRequest) { r.Currency = "EUR" }, want: billing.ErrRevenueUnassessable},
		{name: "payment_before_checkout_is_not_owned", request: func(r *billing.RevenueAssociationRequest) { r.PaidAt = time.Unix(1699999999, 0) }, want: billing.ErrRevenueUnassessable},
		{name: "wrong_original_amount_does_not_link", evidence: func(e *paymentprovider.RevenueCheckoutEvidence) { e.UnitAmountMinor++ }, want: billing.ErrRevenueUnassessable},
		{name: "wrong_original_cadence_does_not_link", evidence: func(e *paymentprovider.RevenueCheckoutEvidence) { e.BillingCadence = "year" }, want: billing.ErrRevenueUnassessable},
		{name: "incomplete_provider_session_is_not_linked", evidence: func(e *paymentprovider.RevenueCheckoutEvidence) { e.Status = "open" }, want: billing.ErrRevenueUnassessable},
		{name: "provider_outage_remains_retryable", providerErr: paymentprovider.ErrPaymentProviderAPIRequestFailed, want: paymentprovider.ErrPaymentProviderAPIRequestFailed},
		{name: "conclusive_unassessable_provider_evidence_is_normalized", providerErr: paymentprovider.ErrRevenueUnassessable, want: billing.ErrRevenueUnassessable},
		{name: "joined_evidence_and_outage_remains_retryable", providerErr: errors.Join(paymentprovider.ErrRevenueUnassessable, paymentprovider.ErrPaymentProviderAPIRequestFailed), want: paymentprovider.ErrPaymentProviderAPIRequestFailed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo, _, db, ctx := revenueFixture(t)
			provider := &checkoutEvidenceFixture{}
			svc := checkoutServiceFixture(t, repo, provider)
			r := checkoutResolverRequest()
			intent, err := svc.PrepareCheckout(ctx, r.Scope, historicalCheckoutRequest())
			require.NoError(t, err)
			provider.evidence = checkoutEvidence(intent)
			provider.err = tc.providerErr
			if tc.evidence != nil {
				tc.evidence(&provider.evidence)
			}
			if tc.request != nil {
				tc.request(&r)
			}
			association, err := svc.ResolveRevenueAssociation(ctx, r)
			count, countErr := db.Collection("ghatd_owned_records").CountDocuments(ctx, bson.M{"kind": kindCheckoutAssociation})
			require.NoError(t, countErr)
			if tc.want != nil {
				require.ErrorIs(t, err, tc.want)
				require.Empty(t, association.PrincipalID)
				require.Zero(t, count)
				return
			}
			require.NoError(t, err)
			require.Equal(t, billing.RevenueAssociation{PrincipalID: "owning_principal", PlanID: "plan_frozen", CostID: "cost_frozen"}, association)
			require.Equal(t, int64(1), count)
			acknowledged, err := svc.FindCheckoutIntent(ctx, r.Scope, intent.Request.IdempotencyKey)
			require.NoError(t, err)
			require.Equal(t, "cs_original", acknowledged.SessionID)
			// A renewal recovers owning history even when provider metadata/profile and
			// catalogue availability have changed; it makes no further provider request.
			provider.err = paymentprovider.ErrPaymentProviderAPIRequestFailed
			provider.evidence.ClientReferenceID = "changed_profile"
			r.InvoiceID = "in_renewal"
			r.PaymentID = "pi_renewal"
			r.PaidAt = r.PaidAt.Add(30 * 24 * time.Hour)
			again, err := svc.ResolveRevenueAssociation(ctx, r)
			require.NoError(t, err)
			require.Equal(t, association, again)
			require.Equal(t, 1, provider.calls)
			wire, err := json.Marshal(acknowledged)
			require.NoError(t, err)
			require.NotContains(t, string(wire), "original@example.test")
			require.NotContains(t, string(wire), "attempt_original")
			var raw bson.M
			require.NoError(t, db.Collection("ghatd_owned_records").FindOne(ctx, bson.M{"kind": kindCheckoutIntent}).Decode(&raw))
			rawBytes, err := bson.MarshalExtJSON(raw, false, false)
			require.NoError(t, err)
			require.NotContains(t, string(rawBytes), "original@example.test")
			require.NotContains(t, string(rawBytes), "owning_principal")
		})
	}
}
func TestMongoCheckoutAtomicAssociationRecovery(t *testing.T) {
	cases := []struct {
		name      string
		failAt    int
		uncertain bool
	}{{"acknowledgement_rollback", 1, false}, {"intent_acknowledgement_rollback", 2, false}, {"principal_rollback", 3, false}, {"association_rollback", 4, false}, {"lost_commit_acknowledgement", 0, true}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo, store, db, ctx := revenueFixture(t)
			provider := &checkoutEvidenceFixture{}
			svc := checkoutServiceFixture(t, repo, provider)
			request := checkoutResolverRequest()
			intent, err := svc.PrepareCheckout(ctx, request.Scope, historicalCheckoutRequest())
			require.NoError(t, err)
			provider.evidence = checkoutEvidence(intent)
			broken, err := NewRepository(failingStore{Store: store, at: tc.failAt, uncertain: tc.uncertain})
			require.NoError(t, err)
			faulty := checkoutServiceFixture(t, broken, provider)
			_, err = faulty.ResolveRevenueAssociation(ctx, request)
			require.Error(t, err)
			count, countErr := db.Collection("ghatd_owned_records").CountDocuments(ctx, bson.M{"kind": bson.M{"$in": []string{kindCheckoutAck, kindCheckoutSession, kindCheckoutPrincipal, kindCheckoutAssociation}}})
			require.NoError(t, countErr)
			if tc.uncertain {
				require.ErrorIs(t, err, billing.ErrRevenueUncertain)
				require.Equal(t, int64(4), count)
			} else {
				require.Zero(t, count)
			}
			recovered, err := svc.ResolveRevenueAssociation(ctx, request)
			require.NoError(t, err)
			require.Equal(t, "owning_principal", recovered.PrincipalID)
			count, countErr = db.Collection("ghatd_owned_records").CountDocuments(ctx, bson.M{"kind": bson.M{"$in": []string{kindCheckoutAck, kindCheckoutSession, kindCheckoutPrincipal, kindCheckoutAssociation}}})
			require.NoError(t, countErr)
			require.Equal(t, int64(4), count)
		})
	}
}

// One shared revision race is the behavior under test; separate table cases
// would not exercise simultaneous creation and acknowledgement of one intent.
func TestMongoCheckoutConcurrentIntentAndAcknowledgement(t *testing.T) {
	repo, _, db, ctx := revenueFixture(t)
	provider := &checkoutEvidenceFixture{}
	svc := checkoutServiceFixture(t, repo, provider)
	r := checkoutResolverRequest()
	var wg sync.WaitGroup
	results := make(chan billing.CheckoutIntent, 8)
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			intent, err := svc.PrepareCheckout(ctx, r.Scope, historicalCheckoutRequest())
			results <- intent
			errs <- err
		}()
	}
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	var first billing.CheckoutIntent
	for intent := range results {
		if first.ID == "" {
			first = intent
		}
		require.Equal(t, first.ID, intent.ID)
	}
	count, err := db.Collection("ghatd_owned_records").CountDocuments(ctx, bson.M{"kind": kindCheckoutIntent})
	require.NoError(t, err)
	require.Equal(t, int64(1), count)
	errs = make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); errs <- svc.AcknowledgeCheckout(ctx, first, "cs_original") }()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	require.ErrorIs(t, svc.AcknowledgeCheckout(ctx, first, "cs_other"), billing.ErrRevenueConflict)
	// Changed frozen terms cannot consume the old browser attempt key.
	changed := historicalCheckoutRequest()
	changed.CustomerEmail = "changed@example.test"
	_, err = svc.PrepareCheckout(ctx, r.Scope, changed)
	require.ErrorIs(t, err, billing.ErrRevenueConflict)
}

func TestMongoCheckoutSessionCannotAuthorizeTwoIntents(t *testing.T) {
	cases := []struct {
		name         string
		otherAccount bool
	}{{"same_scope_session_owner_conflicts", false}, {"another_merchant_has_distinct_session_identity", true}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo, _, _, ctx := revenueFixture(t)
			provider := &checkoutEvidenceFixture{}
			svc := checkoutServiceFixture(t, repo, provider)
			r := checkoutResolverRequest()
			first, err := svc.PrepareCheckout(ctx, r.Scope, historicalCheckoutRequest())
			require.NoError(t, err)
			require.NoError(t, svc.AcknowledgeCheckout(ctx, first, "cs_original"))
			request := historicalCheckoutRequest()
			request.IdempotencyKey = "different_attempt"
			request.UserID = "different_principal"
			request.UserReference = request.UserID
			scope := r.Scope
			if tc.otherAccount {
				scope.AccountID = "acct_other"
			}
			other, err := svc.PrepareCheckout(ctx, scope, request)
			require.NoError(t, err)
			err = svc.AcknowledgeCheckout(ctx, other, "cs_original")
			if tc.otherAccount {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, billing.ErrRevenueConflict)
				unacked, err := svc.FindCheckoutIntent(ctx, scope, request.IdempotencyKey)
				require.NoError(t, err)
				require.Empty(t, unacked.SessionID)
			}
		})
	}
}
func TestMongoCheckoutSubmissionWindow(t *testing.T) {
	cases := []struct {
		name         string
		age          time.Duration
		acknowledged bool
		want         error
	}{{name: "new_intent_can_be_submitted"}, {name: "retained_idempotency_key_can_be_retried", age: 23*time.Hour - time.Second}, {name: "uncertain_attempt_outside_safe_retention_requires_reconciliation", age: 23 * time.Hour, want: billing.ErrRevenueUnassessable}, {name: "24_hour_key_is_never_recreated", age: 24 * time.Hour, want: billing.ErrRevenueUnassessable}, {name: "acknowledged_session_must_be_retrieved", acknowledged: true, want: billing.ErrRevenueUnassessable}, {name: "clock_before_saved_authorization_refuses_post", age: -time.Second, want: billing.ErrRevenueUnassessable}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo, _, _, ctx := revenueFixture(t)
			provider := &checkoutEvidenceFixture{}
			svc := checkoutServiceFixture(t, repo, provider)
			r := checkoutResolverRequest()
			intent, err := svc.PrepareCheckout(ctx, r.Scope, historicalCheckoutRequest())
			require.NoError(t, err)
			if tc.acknowledged {
				require.NoError(t, svc.AcknowledgeCheckout(ctx, intent, "cs_original"))
				intent, err = svc.FindCheckoutIntent(ctx, r.Scope, intent.Request.IdempotencyKey)
				require.NoError(t, err)
			}
			later, err := billing.NewCheckoutService(repo, fixtureClock{intent.CreatedAt.Add(tc.age)}, provider)
			require.NoError(t, err)
			err = later.CanSubmitCheckout(ctx, intent)
			if tc.want != nil {
				require.ErrorIs(t, err, tc.want)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
