package billingmanager

import (
	"context"
	"errors"
	"fmt"
	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/logger"
	"github.com/ooaklee/ghatd/external/paymentprovider"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
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
	f.resolution = billing.RevenueObservation{ID: "resolution", Fingerprint: "receipt-fingerprint", AcceptedAt: time.Now().UTC(), Scope: f.source.Scope, EnvelopeID: f.source.EnvelopeID, SourceFingerprint: f.source.SourceFingerprint, RecoveryFingerprint: r.RecoveryFingerprint, ResolutionOf: r.ObservationID, ResolutionBy: r.ActorID, ResolutionReason: r.Reason}
	return f.resolution, nil
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
		providerReason     string
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
		{name: "current_reason_differs_from_immutable_original", providerReason: "original_payment_pending", want: billing.ErrRevenueUnassessable},
		{name: "private_current_provider_reason_is_not_logged", providerReason: "signed-provider-payload user@example.test", want: billing.ErrRevenueUnassessable},
		{name: "refetched_scope_must_match_persisted_source", wrongScope: true, want: billing.ErrRevenueInvalid},
		{name: "stale_original_fingerprint_is_denied", changedFingerprint: true, want: billing.ErrRevenueConflict},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			scope := paymentprovider.RevenueScope{Provider: "stripe", AccountID: "acct_primary"}
			at := time.Now().UTC()
			invoice := &paymentprovider.RevenueInvoiceEvidence{Scope: scope, InvoiceID: "in_paid", PaymentID: "pi_paid", CustomerID: "cus_payer", SubscriptionID: "sub_recurring", Currency: "GBP", CurrencyExponent: 2, PaidAt: at, GrossPaidMinor: 1200, Lines: []paymentprovider.RevenueLineEvidence{{ID: "il_one", SubscriptionID: "sub_recurring", PriceID: "price_snapshot", NetPaidMinor: 1000}}}
			p := &recoveryRevenueProvider{revenueBoundaryProvider: &revenueBoundaryProvider{e: &paymentprovider.RevenueEvidence{Scope: scope, EnvelopeID: "evt_pending", Kind: billing.RevenuePayment, PaymentID: "pi_paid", Invoice: invoice}, err: tc.providerError}}
			p.e.QuarantineReason = tc.providerReason
			if tc.wrongScope {
				p.e.Scope.AccountID = "acct_other"
			}
			feed := &recoveryRevenueFeed{revenueBoundaryFeed: &revenueBoundaryFeed{}, source: billing.RevenueObservation{ID: "original", Scope: billingRevenueScope(scope), EnvelopeID: "evt_pending", Fingerprint: "original-fingerprint", QuarantineReason: "historical_payer_plan_pending"}, resolutionErr: tc.sourceError}
			req := ReconcileRevenueSourceRequest{ObservationID: "original", ExpectedFingerprint: "original-fingerprint", Reason: "verified-source-recovered", ActorID: "current-worker"}
			if tc.changedFingerprint {
				req.ExpectedFingerprint = "stale"
			}
			if tc.existing {
				feed.resolution = billing.RevenueObservation{ID: "already-committed", Fingerprint: "receipt-fingerprint", AcceptedAt: at, Scope: feed.source.Scope, EnvelopeID: feed.source.EnvelopeID, ResolutionOf: "original", ResolutionBy: req.ActorID, ResolutionReason: req.Reason}
			}
			auth := &recoveryAuthority{err: tc.permission}
			s, err := (&Service{}).WithRevenueServices(revenueBoundaryRegistry{p: p}, feed, &revenueBoundaryAssociation{err: tc.associationError})
			require.NoError(t, err)
			_, err = s.WithRevenueReconciliationAuthority(auth)
			require.NoError(t, err)
			core, logs := observer.New(zap.DebugLevel)
			ctx := logger.TransitWith(context.Background(), zap.New(core))
			result, err := s.ReconcileRevenueSource(ctx, req)
			require.Equal(t, 1, auth.calls)
			if tc.want != nil {
				require.ErrorIs(t, err, tc.want)
				require.Empty(t, result.ID)
				require.Empty(t, feed.resolved)
				entries := logs.FilterMessage("revenue-source-reconciliation-failed").All()
				require.Len(t, entries, 1)
				fields := entries[0].ContextMap()
				require.Equal(t, "billing.revenue_source.reconcile", fields["owner_operation"])
				require.NotContains(t, fields, "error")
				require.NotContains(t, fields, "observation_id")
				if tc.associationError != nil {
					require.Equal(t, "assess_current_evidence", fields["stage"])
					require.Equal(t, "historical_payer_plan_pending", fields["current_quarantine_reason"])
					require.Equal(t, "revenue_unassessable", fields["cause_code"])
				}
				if tc.providerReason != "" {
					require.Equal(t, "historical_payer_plan_pending", fields["original_quarantine_reason"])
					require.Equal(t, diagnosticQuarantineReason(tc.providerReason), fields["current_quarantine_reason"])
					require.Equal(t, "historical_payer_plan_pending", feed.source.QuarantineReason, "diagnostics must preserve original evidence")
					require.NotContains(t, fields, "signed-provider-payload")
					require.NotContains(t, fmt.Sprint(fields), "user@example.test")
				}
				if tc.providerError != nil {
					require.Equal(t, "retrieve_provider_event", fields["stage"])
					require.Equal(t, "not_assessed", fields["current_quarantine_reason"])
				}
				require.NotContains(t, fields, "actor_id")
				return
			}
			require.NoError(t, err)
			require.Empty(t, logs.FilterMessage("revenue-source-reconciliation-failed").All())
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
