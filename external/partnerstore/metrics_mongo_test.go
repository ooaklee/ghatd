package partnerstore

import (
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/partnerearnings"
	"github.com/stretchr/testify/require"
)

// Audit disposition: named real Mongo lifecycles verify encrypted immutable
// billing-plan provenance, split transfers/returns, debt and pending separation.
func TestMongoFinancialMetricsFrozenPlanEconomics(t *testing.T) {
	for _, tc := range []struct {
		name                           string
		returned, refunded, pending    bool
		remaining, debt, returnedMinor int64
	}{
		{name: "split_claim_uses_each_frozen_plan_portion", remaining: 500},
		{name: "return_restores_only_original_plan_backing", returned: true, remaining: 1200, returnedMinor: 700},
		{name: "refunded_paid_credit_and_pending_obligation_stay_separate", refunded: true, pending: true, remaining: 1000, debt: 500},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _, _, _, clock, ctx := mongoEarnings(t)
			for i, plan := range []string{"alpha", "beta"} {
				req := partnerearnings.AccrualRequest{PartnerID: "partner", PaymentID: "allocation-" + plan, PaymentMinor: 5000, RateBasisPoints: 2000, Currency: "EUR", OccurredAt: clock.Now().Add(-time.Duration(2-i) * time.Hour), ReferralID: "referral-" + plan, PlanID: plan, TermsVersion: "fixture-terms", PolicyID: "fixture-policy"}
				first, err := s.Accrue(ctx, req)
				require.NoError(t, err)
				stored, err := s.AcceptedAccrual(ctx, "partner", req.PaymentID)
				require.NoError(t, err)
				require.Equal(t, first, stored)
				require.Equal(t, plan, stored.PlanID)
				_, err = s.Accrue(ctx, req)
				require.NoError(t, err)
				req.PlanID = "current-catalogue-plan"
				_, err = s.Accrue(ctx, req)
				require.ErrorIs(t, err, partnerearnings.ErrConflict)
			}
			c, err := s.RequestClaim(ctx, claimRequest(1500, "claim"))
			require.NoError(t, err)
			process(t, s, ctx, c)
			recorded := paymentRequest(c, clock, "record")
			recorded.PaidAt = clock.Now().Add(-time.Hour / 2)
			c, err = s.RecordPayment(ctx, recorded)
			require.NoError(t, err)
			if tc.returned {
				_, err = s.RecordReturnedTransfer(ctx, partnerearnings.ReturnRequest{ClaimID: c.ID, ActorID: "operator", ExpectedRevision: c.Revision, AmountMinor: 700, Currency: "EUR", ReturnedAt: clock.Now().Add(-time.Minute), Reference: "controlled-return", Reason: "verified return", IdempotencyKey: "return"})
				require.NoError(t, err)
			}
			if tc.refunded {
				_, err = s.Reverse(ctx, partnerearnings.ReversalRequest{PartnerID: "partner", PaymentID: "allocation-alpha", RefundID: "refund", Currency: "EUR", CumulativeRefundedMinor: 5000, OccurredAt: clock.Now()})
				require.NoError(t, err)
			}
			if tc.pending {
				_, err = s.Accrue(ctx, partnerearnings.AccrualRequest{PartnerID: "partner", PaymentID: "pending", PaymentMinor: 5000, RateBasisPoints: 2000, HoldDuration: 7 * 24 * time.Hour, Currency: "EUR", OccurredAt: clock.Now(), ReferralID: "referral-pending", TermsVersion: "fixture-terms", PolicyID: "fixture-policy"})
				require.NoError(t, err)
			}
			out, err := s.GetFinancialMetrics(ctx, "partner", partnerearnings.FinancialMetricsQuery{Limit: 1})
			require.NoError(t, err)
			require.True(t, out.HasMore)
			require.EqualValues(t, 1500, out.PeriodMovements.PaidMinor)
			require.EqualValues(t, tc.returnedMinor, out.PeriodMovements.ReturnedMinor)
			require.EqualValues(t, tc.remaining, out.RemainingCommissionMinor)
			require.EqualValues(t, tc.debt, out.CurrentPartnerBalances.DebtMinor)
			require.Equal(t, 1, out.CurrentClaims.Paid)
			require.Zero(t, out.CurrentClaims.ProcessingExposureMinor)
			alpha, err := s.GetFinancialMetrics(ctx, "partner", partnerearnings.FinancialMetricsQuery{Limit: 1, PlanID: "alpha"})
			require.NoError(t, err)
			require.False(t, alpha.HasMore)
			require.Len(t, alpha.Plans, 1)
			require.Equal(t, "alpha", alpha.Plans[0].PlanID)
			require.EqualValues(t, 1000, alpha.PeriodMovements.PaidMinor)
			require.EqualValues(t, tc.returnedMinor, alpha.PeriodMovements.ReturnedMinor)
			require.Equal(t, out.Revision, alpha.Revision)
			require.Equal(t, out.CurrentPartnerBalances, alpha.CurrentPartnerBalances)
			require.Equal(t, out.CurrentClaims, alpha.CurrentClaims)
			from, to := recorded.PaidAt, recorded.PaidAt.Add(time.Second)
			period, err := s.GetFinancialMetrics(ctx, "partner", partnerearnings.FinancialMetricsQuery{Limit: 100, From: &from, To: &to})
			require.NoError(t, err)
			require.Zero(t, period.AcceptedAllocationRows)
			require.EqualValues(t, 1500, period.PeriodMovements.PaidMinor)
			require.EqualValues(t, 1000, period.Plans[0].PeriodMovements.PaidMinor)
			require.EqualValues(t, 500, period.Plans[1].PeriodMovements.PaidMinor)
		})
	}
}
