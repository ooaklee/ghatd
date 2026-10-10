package partnerstore

import (
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/partnerearnings"
	"github.com/stretchr/testify/require"
)

// Audit disposition: isolated named real-Mongo lifecycles compare unfiltered
// current due credit to actual idempotent maturity journals and retained holds.
func TestMongoCurrentMaturityReflectsActualLedgerTransition(t *testing.T) {
	for _, tc := range []struct {
		name, mode       string
		net, held, gross int64
	}{
		{"due_original_credit", "", 1000, 0, 1000},
		{"fully_refunded_due_row", "refund", 0, 0, 1000},
		{"zero_rate_due_row", "zero", 0, 0, 0},
		{"held_due_credit_stays_unavailable_after_maturity", "hold", 1000, 1000, 1000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _, _, _, clock, ctx := mongoEarnings(t)
			start := clock.Now()
			req := partnerearnings.AccrualRequest{PartnerID: "partner", PaymentID: "due-payment", PaymentMinor: 5000, RateBasisPoints: 2000, HoldDuration: time.Hour, Currency: "EUR", OccurredAt: start, PlanID: "original-plan", ReferralID: "private-referral", TermsVersion: "fixture-terms", PolicyID: "fixture-policy"}
			if tc.mode == "zero" {
				req.RateBasisPoints = 0
			}
			_, err := s.Accrue(ctx, req)
			require.NoError(t, err)
			if tc.mode == "refund" {
				_, err = s.Reverse(ctx, partnerearnings.ReversalRequest{PartnerID: "partner", PaymentID: req.PaymentID, RefundID: "refund", Currency: "EUR", CumulativeRefundedMinor: 5000, OccurredAt: start})
				require.NoError(t, err)
			}
			if tc.mode == "hold" {
				_, err = s.Dispute(ctx, partnerearnings.DisputeRequest{PartnerID: "partner", PaymentID: req.PaymentID, DisputeID: "dispute", OperationID: "hold-operation", Action: "hold", Reason: "verified fixture", ActorID: "worker", Currency: "EUR", OccurredAt: start})
				require.NoError(t, err)
			}
			future := req
			future.PaymentID = "future-payment"
			future.PlanID = "future-plan"
			future.HoldDuration = 2 * time.Hour
			future.RateBasisPoints = 2000
			_, err = s.Accrue(ctx, future)
			require.NoError(t, err)
			before, err := s.GetFinancialMetrics(ctx, "partner", partnerearnings.FinancialMetricsQuery{Limit: 1})
			require.NoError(t, err)
			require.Empty(t, before.CurrentMaturity)
			clock.now = start.Add(time.Hour)
			from, to := start.Add(24*time.Hour), start.Add(25*time.Hour)
			out, err := s.GetFinancialMetrics(ctx, "partner", partnerearnings.FinancialMetricsQuery{Limit: 1, PlanID: "future-plan", From: &from, To: &to})
			require.NoError(t, err)
			require.Zero(t, out.AcceptedAllocationRows)
			require.Empty(t, out.Plans)
			require.Equal(t, before.Revision, out.Revision)
			require.Equal(t, before.LedgerSequence, out.LedgerSequence)
			require.EqualValues(t, 1, out.CurrentMaturity.DueAllocationRows)
			require.Equal(t, tc.net, out.CurrentMaturity.DuePendingMinor)
			require.Equal(t, tc.held, out.CurrentMaturity.DueDisputeHoldMinor)
			require.Equal(t, start.Add(time.Hour), *out.CurrentMaturity.OldestAvailableAt)
			matured, err := s.Mature(ctx, "partner")
			require.NoError(t, err)
			require.Len(t, matured, 1)
			require.Equal(t, req.PaymentID, matured[0].SourceEventID)
			require.Equal(t, tc.gross, matured[0].AmountMinor)
			after, err := s.GetFinancialMetrics(ctx, "partner", partnerearnings.FinancialMetricsQuery{Limit: 1})
			require.NoError(t, err)
			require.Empty(t, after.CurrentMaturity)
			require.NotEqual(t, out.Revision, after.Revision)
			require.EqualValues(t, 1000, after.CurrentPartnerBalances.PendingMinor, "future lot remains pending")
			if tc.held > 0 {
				require.Equal(t, tc.held, after.CurrentPartnerBalances.DisputeHoldMinor)
				require.Zero(t, after.CurrentPartnerBalances.AvailableMinor)
			}
			again, err := s.Mature(ctx, "partner")
			require.NoError(t, err)
			require.Empty(t, again)
		})
	}
}
