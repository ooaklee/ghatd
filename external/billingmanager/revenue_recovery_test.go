package billingmanager

import (
	"context"
	"errors"
	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/paymentprovider"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

// Audit disposition: new scoped recovery cases. Current permission is required
// before replay, and provider failures cannot become a no-revenue resolution.
type recoveryAuthority struct {
	calls int
	err   error
}

func (a *recoveryAuthority) AuthorizeRevenueReconciliation(context.Context, string) error {
	a.calls++
	return a.err
}

type recoveryRevenueProvider struct {
	*revenueBoundaryProvider
	calls int
}

func (p *recoveryRevenueProvider) ReconcileRevenueEvent(_ context.Context, scope paymentprovider.RevenueScope, id string) (*paymentprovider.RevenueEvidence, error) {
	p.calls++
	if p.err != nil {
		return nil, p.err
	}
	return p.e, nil
}

type recoveryRevenueFeed struct {
	*revenueBoundaryFeed
	source, resolution billing.RevenueObservation
	resolutionErr      error
	resolved           []billing.ResolveRevenueRequest
}

func (f *recoveryRevenueFeed) GetRevenueObservation(context.Context, string) (billing.RevenueObservation, error) {
	return f.source, nil
}
func (f *recoveryRevenueFeed) GetRevenueSourceResolution(context.Context, string) (billing.RevenueObservation, error) {
	if f.resolutionErr != nil {
		return billing.RevenueObservation{}, f.resolutionErr
	}
	if f.resolution.ID == "" {
		return billing.RevenueObservation{}, billing.ErrRevenueNotFound
	}
	return f.resolution, nil
}
func (f *recoveryRevenueFeed) ResolveQuarantinedRevenue(_ context.Context, r billing.ResolveRevenueRequest) (billing.RevenueObservation, error) {
	f.resolved = append(f.resolved, r)
	return billing.RevenueObservation{ID: "resolution", ResolutionOf: r.ObservationID, ResolutionBy: r.ActorID, ResolutionReason: r.Reason}, nil
}
func TestRevenueSourceScopedRecovery(t *testing.T) {
	revoked := errors.New("permission revoked")
	type testCase struct {
		name               string
		existing           bool
		permission         error
		providerError      error
		sourceError        error
		associationError   error
		wrongScope         bool
		changedFingerprint bool
		want               error
	}
	cases := []testCase{
		{name: "authenticated_source_recovery_accepts_net_facts"},
		{name: "committed_resolution_replays_before_provider_lookup", existing: true, providerError: paymentprovider.ErrPaymentProviderAPIRequestFailed},
		{name: "revoked_permission_denies_replay", existing: true, permission: revoked, want: revoked},
		{name: "authority_dependency_outage_is_not_absence", permission: billing.ErrRevenueUnavailable, want: billing.ErrRevenueUnavailable},
		{name: "provider_outage_keeps_source_pending", providerError: paymentprovider.ErrPaymentProviderAPIRequestFailed, want: paymentprovider.ErrPaymentProviderAPIRequestFailed},
		{name: "source_receipt_lookup_outage_keeps_pending", sourceError: billing.ErrRevenueUnavailable, want: billing.ErrRevenueUnavailable},
		{name: "joined_absence_and_outage_cannot_create_resolution", sourceError: errors.Join(billing.ErrRevenueNotFound, billing.ErrRevenueUnavailable), want: billing.ErrRevenueUnavailable},
		{name: "unresolved_historical_payer_has_no_false_completion", associationError: billing.ErrRevenueNotFound, want: billing.ErrRevenueUnassessable},
		{name: "refetched_scope_must_match_persisted_source", wrongScope: true, want: billing.ErrRevenueInvalid},
		{name: "stale_original_fingerprint_is_denied", changedFingerprint: true, want: billing.ErrRevenueConflict},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			scope := paymentprovider.RevenueScope{Provider: "stripe", AccountID: "acct_primary"}
			at := time.Now().UTC()
			invoice := &paymentprovider.RevenueInvoiceEvidence{Scope: scope, InvoiceID: "in_paid", PaymentID: "pi_paid", CustomerID: "cus_payer", SubscriptionID: "sub_recurring", Currency: "GBP", CurrencyExponent: 2, PaidAt: at, GrossPaidMinor: 1200, Lines: []paymentprovider.RevenueLineEvidence{{ID: "il_one", SubscriptionID: "sub_recurring", PriceID: "price_snapshot", NetPaidMinor: 1000}}}
			p := &recoveryRevenueProvider{revenueBoundaryProvider: &revenueBoundaryProvider{e: &paymentprovider.RevenueEvidence{Scope: scope, EnvelopeID: "evt_pending", Kind: billing.RevenuePayment, PaymentID: "pi_paid", Invoice: invoice}, err: tc.providerError}}
			if tc.wrongScope {
				p.e.Scope.AccountID = "acct_other"
			}
			feed := &recoveryRevenueFeed{revenueBoundaryFeed: &revenueBoundaryFeed{}, source: billing.RevenueObservation{ID: "original", Scope: billingRevenueScope(scope), EnvelopeID: "evt_pending", Fingerprint: "original-fingerprint", QuarantineReason: "historical_payer_plan_pending"}, resolutionErr: tc.sourceError}
			req := ReconcileRevenueSourceRequest{ObservationID: "original", ExpectedFingerprint: "original-fingerprint", Reason: "verified-source-recovered", ActorID: "current-worker"}
			if tc.changedFingerprint {
				req.ExpectedFingerprint = "stale"
			}
			if tc.existing {
				feed.resolution = billing.RevenueObservation{ID: "already-committed", ResolutionOf: "original", ResolutionBy: req.ActorID, ResolutionReason: req.Reason}
			}
			auth := &recoveryAuthority{err: tc.permission}
			s, err := (&Service{}).WithRevenueServices(revenueBoundaryRegistry{p: p}, feed, &revenueBoundaryAssociation{err: tc.associationError})
			require.NoError(t, err)
			_, err = s.WithRevenueReconciliationAuthority(auth)
			require.NoError(t, err)
			result, err := s.ReconcileRevenueSource(context.Background(), req)
			require.Equal(t, 1, auth.calls)
			if tc.want != nil {
				require.ErrorIs(t, err, tc.want)
				require.Empty(t, result.ID)
				require.Empty(t, feed.resolved)
				return
			}
			require.NoError(t, err)
			if tc.existing {
				require.Equal(t, "already-committed", result.ID)
				require.Zero(t, p.calls)
				require.Empty(t, feed.resolved)
			} else {
				require.Equal(t, "resolution", result.ID)
				require.Equal(t, 1, p.calls)
				require.Len(t, feed.resolved, 1)
				require.Len(t, feed.resolved[0].Facts, 1)
				require.EqualValues(t, 1000, feed.resolved[0].Facts[0].PaidMinor)
				require.Equal(t, req.ActorID, feed.resolved[0].ActorID)
			}
		})
	}
}
