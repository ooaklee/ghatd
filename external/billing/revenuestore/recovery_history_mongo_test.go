package revenuestore

import (
	"testing"

	"github.com/ooaklee/ghatd/external/billing"
	"github.com/stretchr/testify/require"
)

// Audit disposition: named native regressions retain encrypted original and
// resolution receipts, reopen the owner and verify exact record-once history.
func TestMongoRecoveredSourcePaymentHistory(t *testing.T) {
	type testCase struct {
		name, recovery string
		noFacts        bool
	}
	cases := []testCase{
		{name: "legacy_refund_resolution"},
		{name: "stable_refund_resolution", recovery: "stable-source-v2"},
		{name: "legacy_no_revenue_resolution", noFacts: true},
		{name: "stable_no_revenue_resolution", recovery: "stable-source-v2", noFacts: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo, _, _, ctx := revenueFixture(t)
			svc := revenueService(t, repo)
			payment := revenueFact()
			accept(t, svc, ctx, "paid-original", payment)
			original, err := svc.AcceptVerified(ctx, billing.VerifiedRevenueRequest{Scope: payment.Scope, EnvelopeID: "retained-refund-source", SourceFingerprint: "legacy-native-source", QuarantineReason: "invoice_economics_unassessable"})
			require.NoError(t, err)
			request := billing.ResolveRevenueRequest{ObservationID: original.ID, ExpectedFingerprint: original.Fingerprint, ActorID: "verified-worker", Reason: "authenticated_source_recovered", RecoveryFingerprint: tc.recovery}
			if !tc.noFacts {
				refund := payment
				refund.Kind = billing.RevenueRefund
				refund.AdjustmentID = "retained-refund"
				refund.CumulativeRefundedMinor = payment.PaidMinor
				request.Facts = []billing.RevenueFact{refund}
			}
			resolution, err := svc.ResolveQuarantinedRevenue(ctx, request)
			require.NoError(t, err)
			restarted, err := NewRepository(repo.store)
			require.NoError(t, err)
			recovered := revenueService(t, restarted)
			replay, err := recovered.ResolveQuarantinedRevenue(ctx, request)
			require.NoError(t, err)
			require.Equal(t, resolution, replay)
			retained, err := recovered.GetRevenueObservation(ctx, original.ID)
			require.NoError(t, err)
			require.Equal(t, original, retained)
			query := billing.RevenueHistoryQuery{Scopes: []billing.RevenueScope{payment.Scope}, Principals: []string{payment.PrincipalID}}
			before, err := repo.ReadRevenueHistory(ctx)
			require.NoError(t, err)
			out, err := recovered.GetPaymentRevenueHistory(ctx, query)
			require.NoError(t, err)
			require.Len(t, out.Items, 1)
			require.Zero(t, out.ScopedUnresolvedSources)
			if tc.noFacts {
				require.Equal(t, payment.PaidMinor, out.Items[0].NetMinor)
				require.EqualValues(t, 1, out.AcceptanceSequence)
			} else {
				require.Zero(t, out.Items[0].NetMinor)
				require.Equal(t, payment.PaidMinor, out.Items[0].RefundedMinor)
				require.EqualValues(t, 2, out.AcceptanceSequence)
			}
			after, err := restarted.ReadRevenueHistory(ctx)
			require.NoError(t, err)
			require.Equal(t, before, after)
		})
	}
}
