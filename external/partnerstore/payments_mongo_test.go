package partnerstore

import (
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/partnerearnings"
	"github.com/stretchr/testify/require"
)

// Audit disposition: named actual owning Mongo fiscal lifecycles exercise
// split claims, retained return portions, reclaims and cross-referral debt.
// Every case uses its own disposable database and real prepared encrypted store.
func TestMongoPaymentReportConservedEconomics(t *testing.T) {
	cases := []struct {
		name                     string
		refund, offset, returned bool
		reclaim                  bool
		gross, net, mature, debt int64
		available                int64
		payments                 int
	}{
		{name: "split_paid_claim", gross: 1500, net: 1500, mature: 2000, available: 500, payments: 2},
		{name: "refunded_paid_referral_creates_partner_debt", refund: true, gross: 1500, net: 1500, mature: 1000, debt: 500, payments: 2},
		{name: "another_referral_credit_offsets_partner_debt", refund: true, offset: true, gross: 1500, net: 1500, mature: 1800, available: 300, payments: 3},
		{name: "partial_return_restores_original_backing", returned: true, gross: 1500, net: 800, mature: 2000, available: 1200, payments: 2},
		{name: "return_reclaimed_then_original_revenue_refunded", returned: true, reclaim: true, refund: true, gross: 2500, net: 1800, mature: 1000, debt: 800, payments: 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, _, _, _, clock, ctx := mongoEarnings(t)
			for i, payment := range []string{"one", "two"} {
				_, err := s.Accrue(ctx, partnerearnings.AccrualRequest{PartnerID: "partner", PaymentID: payment, PaymentMinor: 5000, RateBasisPoints: 2000, Currency: "EUR", OccurredAt: clock.Now().Add(-time.Duration(2-i) * time.Hour), ReferralID: "referral-" + payment, TermsVersion: "fixture-terms", PolicyID: "fixture-policy"})
				require.NoError(t, err)
			}
			claim, err := s.RequestClaim(ctx, claimRequest(1500, "first"))
			require.NoError(t, err)
			process(t, s, ctx, claim)
			paid, err := s.RecordPayment(ctx, paymentRequest(claim, clock, "record"))
			require.NoError(t, err)
			if tc.returned {
				_, err = s.RecordReturnedTransfer(ctx, partnerearnings.ReturnRequest{ClaimID: paid.ID, ActorID: "operator", ExpectedRevision: paid.Revision, AmountMinor: 700, Currency: "EUR", ReturnedAt: clock.Now(), Reference: "return-one", Reason: "verified_return", IdempotencyKey: "return"})
				require.NoError(t, err)
			}
			if tc.reclaim {
				second, err := s.RequestClaim(ctx, claimRequest(1000, "second"))
				require.NoError(t, err)
				process(t, s, ctx, second)
				_, err = s.RecordPayment(ctx, paymentRequest(second, clock, "record-second"))
				require.NoError(t, err)
			}
			if tc.refund {
				_, err = s.Reverse(ctx, partnerearnings.ReversalRequest{PartnerID: "partner", PaymentID: "one", RefundID: "refund", Currency: "EUR", CumulativeRefundedMinor: 5000, OccurredAt: clock.Now()})
				require.NoError(t, err)
			}
			if tc.offset {
				_, err = s.Accrue(ctx, partnerearnings.AccrualRequest{PartnerID: "partner", PaymentID: "offset", PaymentMinor: 4000, RateBasisPoints: 2000, Currency: "EUR", OccurredAt: clock.Now(), ReferralID: "referral-offset", TermsVersion: "fixture-terms", PolicyID: "fixture-policy"})
				require.NoError(t, err)
			}
			out, err := s.GetPaymentReport(ctx, "partner", partnerearnings.PaymentQuery{Limit: 1})
			require.NoError(t, err)
			require.Equal(t, "fixture-program", out.ProgramID)
			require.Equal(t, tc.payments, out.CohortPayments)
			require.EqualValues(t, tc.gross, out.CohortAmounts.GrossPaidBackingMinor)
			require.EqualValues(t, tc.net, out.CohortAmounts.NetPaidBackingMinor)
			require.EqualValues(t, tc.mature, out.CohortAmounts.MaturedEarnedMinor)
			require.EqualValues(t, tc.available, out.Balances.AvailableMinor)
			require.EqualValues(t, tc.debt, out.Balances.DebtMinor)
			require.True(t, out.HasMore)
			first, err := s.GetPaymentReport(ctx, "partner", partnerearnings.PaymentQuery{Limit: 100, ReferralID: "referral-one"})
			require.NoError(t, err)
			require.Len(t, first.Items, 1)
			require.Equal(t, "one", first.Items[0].PaymentID)
			require.Equal(t, out.Balances, first.Balances, "filtered referral report retains global debt/availability")
			require.Equal(t, out.Revision, first.Revision)
			firstNet := int64(1000)
			if tc.returned && !tc.reclaim {
				firstNet = 300
			}
			require.EqualValues(t, firstNet, first.Items[0].Amounts.NetPaidBackingMinor)

		})
	}
}
