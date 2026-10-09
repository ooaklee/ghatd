package billingmanager

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/paymentprovider"
	"github.com/stretchr/testify/require"
)

type recoverySnapshotProvider struct {
	*recoveryRevenueProvider
	identity paymentprovider.RevenueSnapshotIdentity
	err      error
	requests []paymentprovider.RevenueSnapshotRequest
}

func (p *recoverySnapshotProvider) VerifyRetainedRevenueSnapshot(_ context.Context, req paymentprovider.RevenueSnapshotRequest) (paymentprovider.RevenueSnapshotIdentity, error) {
	p.requests = append(p.requests, req)
	return p.identity, p.err
}

// Audit disposition: related bridge/replay cases preserve source ownership,
// fresh authority and exact private proof before any recovered-fact write.
func TestRevenueSourceRetainedSnapshotRecovery(t *testing.T) {
	denied := errors.New("current authority denied")
	type testCase struct {
		name                         string
		existing, noSnapshot         bool
		mutate                       func(*recoverySnapshotProvider, *recoveryRevenueFeed)
		permission                   error
		want                         error
		providerCalls, snapshotCalls int
	}
	cases := []testCase{
		{name: "legacy_snapshot_bridges_only_authenticated_stable_identity", providerCalls: 1, snapshotCalls: 1},
		{name: "changed_legacy_representation_requires_original_proof", noSnapshot: true, want: billing.ErrRevenueInvalid, providerCalls: 1},
		{name: "unchanged_legacy_snapshot_recovers_without_manual_proof", noSnapshot: true, mutate: func(p *recoverySnapshotProvider, f *recoveryRevenueFeed) {
			f.source.SourceFingerprint = p.e.LegacySourceFingerprint
		}, providerCalls: 1},
		{name: "new_versioned_source_recovers_without_manual_proof", noSnapshot: true, mutate: func(p *recoverySnapshotProvider, f *recoveryRevenueFeed) {
			f.source.SourceFingerprint = p.e.SourceFingerprint
		}, providerCalls: 1},
		{name: "invalid_original_proof_prevents_remote_read_and_write", mutate: func(p *recoverySnapshotProvider, _ *recoveryRevenueFeed) {
			p.err = paymentprovider.ErrRevenueUnassessable
		}, want: paymentprovider.ErrRevenueUnassessable, snapshotCalls: 1},
		{name: "proof_cannot_change_source_scope", mutate: func(p *recoverySnapshotProvider, _ *recoveryRevenueFeed) { p.identity.Scope.AccountID = "acct_other" }, want: billing.ErrRevenueInvalid, snapshotCalls: 1},
		{name: "proof_cannot_change_source_envelope", mutate: func(p *recoverySnapshotProvider, _ *recoveryRevenueFeed) { p.identity.EnvelopeID = "evt_other" }, want: billing.ErrRevenueInvalid, snapshotCalls: 1},
		{name: "proof_must_match_retained_legacy_hash", mutate: func(p *recoverySnapshotProvider, _ *recoveryRevenueFeed) {
			p.identity.OriginalFingerprint = "different-legacy"
		}, want: billing.ErrRevenueInvalid, snapshotCalls: 1},
		{name: "proof_requires_nonempty_stable_identity", mutate: func(p *recoverySnapshotProvider, _ *recoveryRevenueFeed) { p.identity.CanonicalFingerprint = "" }, want: billing.ErrRevenueInvalid, snapshotCalls: 1},
		{name: "proof_cannot_rescue_changed_remote_economics", mutate: func(p *recoverySnapshotProvider, _ *recoveryRevenueFeed) {
			p.e.SourceFingerprint = "changed-remote"
			p.e.LegacySourceFingerprint = "retained-legacy"
		}, want: billing.ErrRevenueInvalid, providerCalls: 1, snapshotCalls: 1},
		{name: "receipt_replay_without_payload_survives_provider_outage", existing: true, noSnapshot: true, mutate: func(p *recoverySnapshotProvider, _ *recoveryRevenueFeed) {
			p.revenueBoundaryProvider.err = paymentprovider.ErrPaymentProviderAPIRequestFailed
		}},
		{name: "receipt_replay_revalidates_supplied_original_proof_locally", existing: true, snapshotCalls: 1},
		{name: "changed_proof_cannot_hide_behind_receipt", existing: true, mutate: func(p *recoverySnapshotProvider, _ *recoveryRevenueFeed) {
			p.err = paymentprovider.ErrRevenueUnassessable
		}, want: paymentprovider.ErrRevenueUnassessable, snapshotCalls: 1},
		{name: "changed_derived_identity_conflicts_with_receipt", existing: true, mutate: func(p *recoverySnapshotProvider, _ *recoveryRevenueFeed) {
			p.identity.CanonicalFingerprint = "different-stable"
		}, want: billing.ErrRevenueConflict, snapshotCalls: 1},
		{name: "receipt_cannot_belong_to_another_source", existing: true, noSnapshot: true, mutate: func(_ *recoverySnapshotProvider, f *recoveryRevenueFeed) { f.resolution.ResolutionOf = "other-source" }, want: billing.ErrRevenueConflict},
		{name: "receipt_cannot_belong_to_another_scope", existing: true, noSnapshot: true, mutate: func(_ *recoverySnapshotProvider, f *recoveryRevenueFeed) { f.resolution.Scope.AccountID = "acct_other" }, want: billing.ErrRevenueConflict},
		{name: "unaccepted_receipt_cannot_replay", existing: true, noSnapshot: true, mutate: func(_ *recoverySnapshotProvider, f *recoveryRevenueFeed) { f.resolution.AcceptedAt = time.Time{} }, want: billing.ErrRevenueConflict},
		{name: "revoked_authority_denies_proof_and_receipt_replay", existing: true, permission: denied, want: denied},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			scope := paymentprovider.RevenueScope{Provider: "stripe", AccountID: "acct_primary"}
			at := time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC)
			invoice := &paymentprovider.RevenueInvoiceEvidence{Scope: scope, InvoiceID: "in_paid", PaymentID: "pi_paid", CustomerID: "cus_payer", SubscriptionID: "sub_recurring", Currency: "GBP", CurrencyExponent: 2, PaidAt: at, GrossPaidMinor: 1200, Lines: []paymentprovider.RevenueLineEvidence{{ID: "il_one", SubscriptionID: "sub_recurring", PriceID: "price_snapshot", NetPaidMinor: 1000}}}
			p := &recoverySnapshotProvider{recoveryRevenueProvider: &recoveryRevenueProvider{revenueBoundaryProvider: &revenueBoundaryProvider{e: &paymentprovider.RevenueEvidence{Scope: scope, EnvelopeID: "evt_pending", Kind: billing.RevenuePayment, PaymentID: "pi_paid", Invoice: invoice, SourceFingerprint: "stable-v2", LegacySourceFingerprint: "retrieved-legacy"}}}, identity: paymentprovider.RevenueSnapshotIdentity{Scope: scope, EnvelopeID: "evt_pending", OriginalFingerprint: "retained-legacy", CanonicalFingerprint: "stable-v2"}}
			f := &recoveryRevenueFeed{revenueBoundaryFeed: &revenueBoundaryFeed{}, source: billing.RevenueObservation{ID: "original", Scope: billingRevenueScope(scope), EnvelopeID: "evt_pending", Fingerprint: "original-acceptance", SourceFingerprint: "retained-legacy", QuarantineReason: "historical_payer_plan_pending"}}
			req := ReconcileRevenueSourceRequest{ObservationID: "original", ExpectedFingerprint: f.source.Fingerprint, ActorID: "current-worker", Reason: "verified-source-recovered", OriginalSnapshot: []byte("private-original-snapshot")}
			if tc.noSnapshot {
				req.OriginalSnapshot = nil
			}
			if tc.existing {
				f.resolution = billing.RevenueObservation{ID: "resolution", Scope: f.source.Scope, EnvelopeID: f.source.EnvelopeID, Fingerprint: "resolution-acceptance", AcceptedAt: at, ResolutionOf: f.source.ID, SourceFingerprint: f.source.SourceFingerprint, RecoveryFingerprint: "stable-v2", ResolutionBy: req.ActorID, ResolutionReason: req.Reason}
			}
			if tc.mutate != nil {
				tc.mutate(p, f)
			}
			auth := &recoveryAuthority{err: tc.permission}
			s, err := (&Service{}).WithRevenueServices(revenueBoundaryRegistry{p: p}, f, &revenueBoundaryAssociation{})
			require.NoError(t, err)
			_, err = s.WithRevenueReconciliationAuthority(auth)
			require.NoError(t, err)
			original := f.source
			result, err := s.ReconcileRevenueSource(context.Background(), req)
			require.Equal(t, 1, auth.calls)
			require.Equal(t, tc.providerCalls, p.calls)
			require.Len(t, p.requests, tc.snapshotCalls)
			require.Equal(t, original, f.source, "recovery cannot rewrite original quarantine")
			if tc.want != nil {
				require.ErrorIs(t, err, tc.want)
				require.Empty(t, result.ID)
				require.Empty(t, f.resolved)
				return
			}
			require.NoError(t, err)
			require.Equal(t, "resolution", result.ID)
			if tc.existing {
				require.Empty(t, f.resolved)
			} else {
				require.Len(t, f.resolved, 1)
				require.Equal(t, "stable-v2", f.resolved[0].RecoveryFingerprint)
				require.Equal(t, original.Fingerprint, f.resolved[0].ExpectedFingerprint)
				require.Equal(t, original.SourceFingerprint, result.SourceFingerprint)
			}
			if len(p.requests) > 0 {
				require.Equal(t, req.OriginalSnapshot, p.requests[0].OriginalSnapshot)
				require.Equal(t, original.SourceFingerprint, p.requests[0].OriginalFingerprint)
			}
		})
	}
}
