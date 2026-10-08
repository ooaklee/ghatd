package billingmanager

import (
	"context"
	"errors"
	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/billing/revenuestore"
	"github.com/ooaklee/ghatd/external/paymentprovider"
	"github.com/ooaklee/ghatd/external/repository/recordstore"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

// Audit disposition: named actual-owner orchestration cases cover every
// current-authority stage for checkout provenance, with fresh records per case.
type checkoutStatusProviderStub struct{}

func (*checkoutStatusProviderStub) LookupRevenueCheckout(context.Context, paymentprovider.RevenueScope, string) (paymentprovider.RevenueCheckoutEvidence, error) {
	panic("anchor capture cannot call provider")
}
func checkoutStatusManagerFixture(t *testing.T) (*Service, *billing.RevenueService, *statusManagerStore, *statusManagerAuthority, *statusManagerProvider, billing.RevenueScope) {
	t.Helper()
	ctx := context.Background()
	records := &statusManagerStore{records: map[string]recordstore.Record{}}
	repo, err := revenuestore.NewRepository(records)
	require.NoError(t, err)
	at := time.Date(2026, 10, 7, 14, 0, 0, 9, time.UTC)
	clock := fixtureBillingStatusClock{at}
	owner, err := billing.NewRevenueService(repo, clock)
	require.NoError(t, err)
	scope := billing.RevenueScope{Provider: "stripe", AccountID: "acct_primary"}
	checkout, err := billing.NewCheckoutService(repo, clock, &checkoutStatusProviderStub{})
	require.NoError(t, err)
	q := paymentprovider.CheckoutSessionRequest{IdempotencyKey: "trial_original", PriceID: "price_original", PlanID: "plan", CostID: "cost", UserID: "paying-principal", UserReference: "paying-principal", CustomerEmail: "payer@example.test", ReturnURL: "https://example.test/checkout", Mode: paymentprovider.CheckoutModeSubscription, ExpectedCurrency: "GBP", ExpectedAmount: 1000, ExpectedBillingCadence: "month", TrialPeriodDays: 14}
	i, err := checkout.PrepareCheckout(ctx, scope, q)
	require.NoError(t, err)
	require.NoError(t, checkout.AcknowledgeCheckout(ctx, i, "cs_original"))
	i, err = checkout.FindCheckoutIntent(ctx, scope, q.IdempotencyKey)
	require.NoError(t, err)
	_, err = checkout.CaptureCheckoutLifecycleEvidence(ctx, i, paymentprovider.RevenueCheckoutEvidence{Scope: paymentprovider.RevenueScope{Provider: scope.Provider, AccountID: scope.AccountID}, SessionID: i.SessionID, IntentID: i.ID, ClientReferenceID: q.UserID, CustomerID: "cus_payer", SubscriptionID: "sub_original", PriceID: q.PriceID, Currency: q.ExpectedCurrency, Mode: q.Mode, Status: "complete", UnitAmountMinor: q.ExpectedAmount, IntervalCount: 1, BillingCadence: "month", CreatedAt: at})
	require.NoError(t, err)
	provider := &statusManagerProvider{revenueBoundaryProvider: &revenueBoundaryProvider{}, evidence: paymentprovider.RevenueSubscriptionEvidence{Scope: paymentprovider.RevenueScope{Provider: scope.Provider, AccountID: scope.AccountID}, SubscriptionID: "sub_original", CustomerID: "cus_payer", Status: "active"}}
	s, err := (&Service{}).WithRevenueServices(revenueBoundaryRegistry{p: provider}, owner, &revenueBoundaryAssociation{})
	require.NoError(t, err)
	auth := &statusManagerAuthority{}
	s, err = s.WithSubscriptionStatusAuthority(auth)
	require.NoError(t, err)
	return s, owner, records, auth, provider, scope
}

func TestCheckoutSubscriptionStatusManagerStages(t *testing.T) {
	denied := errors.New("status permission revoked")
	type testCase struct {
		name                                                                                             string
		denyAt                                                                                           int
		wrongCustomer, wrongAccount, wrongMode, unknownStatus, providerOutage, replacementActor, lostAck bool
		phase                                                                                            string
		want                                                                                             error
	}
	cases := []testCase{
		{name: "prepared_lookup_capture_and_retained_read"},
		{name: "replacement_operator_recovers_original_author", replacementActor: true},
		{name: "lost_ack_replays_without_provider_lookup", lostAck: true},
		{name: "program_permission_precedes_source_lookup", denyAt: 1, phase: "prepare", want: denied},
		{name: "owning_scope_permission_precedes_preparation_return", denyAt: 2, phase: "prepare", want: denied},
		{name: "lookup_program_permission_precedes_source_validation", denyAt: 3, phase: "lookup", want: denied},
		{name: "scoped_permission_precedes_provider_io", denyAt: 4, phase: "lookup", want: denied},
		{name: "revocation_during_lookup_withholds_evidence", denyAt: 5, phase: "lookup", want: denied},
		{name: "capture_program_permission_precedes_replay", denyAt: 6, phase: "capture", want: denied},
		{name: "capture_scoped_permission_precedes_mutation", denyAt: 7, phase: "capture", want: denied},
		{name: "post_commit_revocation_withholds_response_retains_truth", denyAt: 8, phase: "capture", want: denied},
		{name: "read_program_permission_precedes_status_lookup", denyAt: 9, phase: "read", want: denied},
		{name: "read_scoped_permission_withholds_status", denyAt: 10, phase: "read", want: denied},
		{name: "provider_customer_cannot_change", wrongCustomer: true, phase: "lookup", want: billing.ErrRevenueConflict},
		{name: "provider_account_cannot_change", wrongAccount: true, phase: "lookup", want: billing.ErrRevenueConflict},
		{name: "provider_mode_cannot_change", wrongMode: true, phase: "lookup", want: billing.ErrRevenueConflict},
		{name: "unknown_provider_status_is_not_inactive", unknownStatus: true, phase: "lookup", want: billing.ErrRevenueConflict},
		{name: "provider_outage_does_not_capture", providerOutage: true, phase: "lookup", want: paymentprovider.ErrPaymentProviderAPIRequestFailed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, owner, records, auth, provider, scope := checkoutStatusManagerFixture(t)
			ctx := context.Background()
			auth.denyAt = tc.denyAt
			auth.err = denied
			if tc.wrongCustomer {
				provider.evidence.CustomerID = "cus_other"
			}
			if tc.wrongAccount {
				provider.evidence.Scope.AccountID = "acct_other"
			}
			if tc.wrongMode {
				provider.evidence.Scope.LiveMode = true
			}
			if tc.unknownStatus {
				provider.evidence.Status = "unmapped"
			}
			if tc.providerOutage {
				provider.failure = paymentprovider.ErrPaymentProviderAPIRequestFailed
			}
			p, err := s.PrepareSubscriptionStatusForCheckout(ctx, "original-author", scope, "sub_original")
			if tc.phase == "prepare" {
				require.ErrorIs(t, err, tc.want)
				require.Zero(t, p)
				require.Zero(t, provider.calls)
				return
			}
			require.NoError(t, err)
			actor := "original-author"
			if tc.replacementActor {
				actor = "current-recovery-operator"
			}
			e, err := s.LookupSubscriptionStatus(ctx, actor, p)
			if tc.phase == "lookup" {
				require.ErrorIs(t, err, tc.want)
				require.Zero(t, e)
				if tc.denyAt == 3 || tc.denyAt == 4 {
					require.Zero(t, provider.calls)
				}
				return
			}
			require.NoError(t, err)
			if tc.lostAck {
				records.uncertain = true
				v, err := s.CaptureSubscriptionStatus(ctx, actor, p, e)
				require.ErrorIs(t, err, billing.ErrRevenueUncertain)
				require.Zero(t, v)
				records.uncertain = false
			}
			v, err := s.CaptureSubscriptionStatus(ctx, actor, p, e)
			if tc.phase == "capture" {
				require.ErrorIs(t, err, tc.want)
				require.Zero(t, v)
				stored, err := owner.GetSubscriptionStatusForCheckout(ctx, scope, "sub_original", time.Minute)
				if tc.denyAt == 8 {
					require.NoError(t, err)
					require.Equal(t, "active", stored.Status)
				} else {
					require.ErrorIs(t, err, billing.ErrRevenueNotFound)
					require.Zero(t, stored)
				}
				return
			}
			require.NoError(t, err)
			require.Equal(t, "original-author", v.Preparation.ActorID)
			current, err := s.GetSubscriptionStatusForCheckout(ctx, actor, scope, "sub_original", time.Minute)
			if tc.phase == "read" {
				require.ErrorIs(t, err, tc.want)
				require.Zero(t, current)
				return
			}
			require.NoError(t, err)
			require.Equal(t, v, current)
			for _, record := range records.records {
				require.NotEqual(t, "billing_revenue_fact", record.Kind, "checkout status must not manufacture payment")
			}
			require.Equal(t, 1, provider.calls, "capture/replay/read must not refetch")
			for i, target := range auth.targets {
				if target.PrincipalID != "" {
					require.Equal(t, "paying-principal", target.PrincipalID)
					require.Equal(t, "sub_original", target.SubscriptionID)
					require.Equal(t, p.Scope, target.Scope)
				}
				wantAction := SubscriptionStatusRefresh
				if i >= len(auth.actions)-2 {
					wantAction = SubscriptionStatusRead
				}
				require.Equal(t, wantAction, auth.actions[i])
			}
		})
	}
}
