package billing

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// Audit disposition: named resolution/reporting regressions own independent
// fixtures; native encrypted persistence is covered by the repository suite.
func TestRevenueHistoryRecoveredSourceProvenance(t *testing.T) {
	type testCase struct {
		name, recovery, mutate string
		noFacts                bool
		want                   error
	}
	cases := []testCase{
		{name: "legacy_resolution_keeps_existing_fingerprint"},
		{name: "stable_recovery_identity_joins_refund_history", recovery: "stable-source-v2"},
		{name: "legacy_reasoned_no_revenue", noFacts: true},
		{name: "stable_reasoned_no_revenue", recovery: "stable-source-v2", noFacts: true},
		{name: "changed_recovery_identity_with_old_digest_is_rejected", recovery: "stable-source-v2", mutate: "changed", want: ErrRevenueUnassessable},
		{name: "missing_recovery_identity_with_stable_digest_is_rejected", recovery: "stable-source-v2", mutate: "missing", want: ErrRevenueUnassessable},
		{name: "unexpected_recovery_identity_on_original_is_rejected", mutate: "original", want: ErrRevenueUnassessable},
		{name: "oversized_recovery_identity_is_rejected", recovery: "stable-source-v2", mutate: "oversized", want: ErrRevenueUnassessable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, repo, payment, query := historyFixture(t)
			ctx := context.Background()
			original, err := svc.AcceptVerified(ctx, VerifiedRevenueRequest{Scope: payment.Scope, EnvelopeID: "retained-refund-source", SourceFingerprint: "legacy-source-digest", QuarantineReason: "invoice_economics_unassessable"})
			require.NoError(t, err)
			request := ResolveRevenueRequest{ObservationID: original.ID, ExpectedFingerprint: original.Fingerprint, ActorID: "verified-worker", Reason: "authenticated_source_recovered", RecoveryFingerprint: tc.recovery}
			if !tc.noFacts {
				refund := payment
				refund.Kind = RevenueRefund
				refund.AdjustmentID = "retained-refund"
				refund.CumulativeRefundedMinor = payment.PaidMinor
				request.Facts = []RevenueFact{refund}
			}
			resolution, err := svc.ResolveQuarantinedRevenue(ctx, request)
			require.NoError(t, err)
			replay, err := svc.ResolveQuarantinedRevenue(ctx, request)
			require.NoError(t, err)
			require.Equal(t, resolution, replay)
			require.Equal(t, original, repo.observations[original.ID])
			if tc.mutate != "" {
				tampered := resolution
				switch tc.mutate {
				case "changed":
					tampered.RecoveryFingerprint = "different-source-v2"
				case "missing":
					tampered.RecoveryFingerprint = ""
				case "original":
					v := repo.observations[original.ID]
					v.RecoveryFingerprint = "unexpected-original-recovery"
					repo.observations[original.ID] = v
				case "oversized":
					tampered.RecoveryFingerprint = strings.Repeat("x", 257)
				}
				repo.observations[resolution.ID] = tampered
			}
			out, err := svc.GetPaymentRevenueHistory(ctx, query)
			if tc.want != nil {
				require.ErrorIs(t, err, tc.want)
				require.Zero(t, out)
				return
			}
			require.NoError(t, err)
			require.Len(t, out.Items, 1)
			require.Equal(t, payment, out.Items[0].Original)
			require.Zero(t, out.ScopedUnresolvedSources)
			if tc.noFacts {
				require.Equal(t, payment.PaidMinor, out.Items[0].NetMinor)
				require.EqualValues(t, 1, out.AcceptanceSequence)
			} else {
				require.Zero(t, out.Items[0].NetMinor)
				require.Equal(t, payment.PaidMinor, out.Items[0].RefundedMinor)
				require.EqualValues(t, 2, out.AcceptanceSequence)
			}
		})
	}
}
