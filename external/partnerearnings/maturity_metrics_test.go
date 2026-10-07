package partnerearnings

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Audit disposition: named owning financial journeys verify maturity selection
// against actual journal transitions, including zero/refunded/disputed credit.
func TestFinancialMetricsCurrentMaturityMatchesJournalWork(t *testing.T) {
	for _, tc := range []struct {
		name, mode       string
		advance          time.Duration
		rows             int
		net, held, gross int64
	}{
		{"before_deadline_is_not_due", "", time.Hour - time.Nanosecond, 0, 0, 0, 1000},
		{"exact_deadline_is_due", "", time.Hour, 1, 1000, 0, 1000},
		{"late_transition_is_still_due", "", 2 * time.Hour, 1, 1000, 0, 1000},
		{"already_matured_allocation_is_excluded", "matured", time.Hour, 0, 0, 0, 1000},
		{"accepted_zero_rate_still_needs_zero_journal_transition", "zero", time.Hour, 1, 0, 0, 0},
		{"full_refund_still_needs_original_gross_transition", "refund", time.Hour, 1, 0, 0, 1000},
		{"partial_refund_reports_net_not_original_gross", "partial", time.Hour, 1, 600, 0, 1000},
		{"held_credit_matures_without_becoming_claimable", "hold", time.Hour, 1, 1000, 1000, 1000},
		{"refunded_held_credit_keeps_only_remaining_hold", "hold_partial", time.Hour, 1, 600, 600, 1000},
		{"dispute_lost_zero_credit_still_needs_transition", "lost", time.Hour, 1, 0, 0, 1000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _, clock := newTestService(t)
			ctx := context.Background()
			start := clock.Now()
			req := accrualReq("partner", "private-payment", 5000, 2000)
			req.OccurredAt = start
			req.HoldDuration = time.Hour
			req.PlanID = "original-plan"
			if tc.mode == "zero" {
				req.RateBasisPoints = 0
			}
			_, err := s.Accrue(ctx, req)
			require.NoError(t, err)
			if tc.mode == "hold" || tc.mode == "hold_partial" || tc.mode == "lost" {
				action := "hold"
				if tc.mode == "lost" {
					action = "lost"
				}
				_, err = s.Dispute(ctx, DisputeRequest{PartnerID: "partner", PaymentID: req.PaymentID, DisputeID: "private-dispute", OperationID: "dispute-operation", Action: action, Reason: "verified fixture", ActorID: "verified-worker", Currency: testCurrency, OccurredAt: start})
				require.NoError(t, err)
			}
			if tc.mode == "refund" || tc.mode == "partial" || tc.mode == "hold_partial" {
				amount := int64(5000)
				if tc.mode != "refund" {
					amount = 2000
				}
				_, err = s.Reverse(ctx, ReversalRequest{PartnerID: "partner", PaymentID: req.PaymentID, RefundID: "private-refund", Currency: testCurrency, CumulativeRefundedMinor: amount, OccurredAt: start})
				require.NoError(t, err)
			}
			clock.t = start.Add(tc.advance)
			if tc.mode == "matured" {
				_, err = s.Mature(ctx, "partner")
				require.NoError(t, err)
			}
			out, err := s.GetFinancialMetrics(ctx, "partner", FinancialMetricsQuery{Limit: 1})
			require.NoError(t, err)
			due := out.CurrentMaturity
			require.Equal(t, tc.rows, due.DueAllocationRows)
			require.Equal(t, tc.net, due.DuePendingMinor)
			require.Equal(t, tc.held, due.DueDisputeHoldMinor)
			require.NoError(t, due.Validate(out.AsOf))
			if tc.rows > 0 {
				require.True(t, start.Add(time.Hour).Equal(*due.OldestAvailableAt))
			} else {
				require.Nil(t, due.OldestAvailableAt)
			}
			require.LessOrEqual(t, due.DuePendingMinor, out.CurrentPartnerBalances.PendingMinor)
			require.LessOrEqual(t, due.DueDisputeHoldMinor, out.CurrentPartnerBalances.PendingDisputeHoldMinor)
			matured, err := s.Mature(ctx, "partner")
			require.NoError(t, err)
			require.Len(t, matured, tc.rows, "report selection must match actual maturity journal work at the same owning clock")
			for _, entry := range matured {
				require.Equal(t, req.PaymentID, entry.SourceEventID)
				require.Equal(t, tc.gross, entry.AmountMinor)
			}
			after, err := s.GetFinancialMetrics(ctx, "partner", FinancialMetricsQuery{Limit: 1})
			require.NoError(t, err)
			require.Empty(t, after.CurrentMaturity)
			if tc.rows > 0 {
				require.NotEqual(t, out.Revision, after.Revision)
			}
			if tc.held > 0 {
				require.Equal(t, tc.held, after.CurrentPartnerBalances.DisputeHoldMinor)
				require.Zero(t, after.CurrentPartnerBalances.AvailableMinor)
				require.Zero(t, after.CurrentPartnerBalances.PendingDisputeHoldMinor)
			}
			again, err := s.Mature(ctx, "partner")
			require.NoError(t, err)
			require.Empty(t, again)
		})
	}
}

func TestFinancialMetricsCurrentMaturityUnfilteredAndClockSensitive(t *testing.T) {
	for _, tc := range []struct {
		name, scope string
		retry       bool
	}{
		{"full_financial_snapshot", "", false},
		{"future_plan_filter_does_not_hide_due_work", "plan", false},
		{"date_window_without_acceptance_does_not_hide_due_work", "date", false},
		{"second_plan_page_does_not_hide_due_work", "cursor", false},
		{"retried_callback_does_not_double_due_totals", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, repo, clock := newTestService(t)
			ctx := context.Background()
			start := clock.Now()
			for n, plan := range []string{"alpha", "beta"} {
				req := accrualReq("partner", plan, 5000, 2000)
				req.OccurredAt = start.Add(time.Duration(n) * time.Minute)
				req.HoldDuration = time.Hour
				req.PlanID = plan
				_, err := s.Accrue(ctx, req)
				require.NoError(t, err)
			}
			before, err := s.GetFinancialMetrics(ctx, "partner", FinancialMetricsQuery{Limit: 1})
			require.NoError(t, err)
			require.Empty(t, before.CurrentMaturity)
			clock.t = start.Add(time.Hour)
			if tc.retry {
				s, err = NewService(&retryOnceRepo{repo}, clock, &seqIDs{}, s.Config())
				require.NoError(t, err)
			}
			q := FinancialMetricsQuery{Limit: 1}
			switch tc.scope {
			case "plan":
				q.PlanID = "beta"
			case "date":
				from, to := start.Add(24*time.Hour), start.Add(25*time.Hour)
				q.From, q.To = &from, &to
			case "cursor":
				q.AfterPlanKey = "plan:alpha"
			}
			out, err := s.GetFinancialMetrics(ctx, "partner", q)
			require.NoError(t, err)
			require.Equal(t, before.Revision, out.Revision, "deadline passage is not a journal mutation")
			require.Equal(t, before.LedgerSequence, out.LedgerSequence)
			require.EqualValues(t, 1, out.CurrentMaturity.DueAllocationRows)
			require.EqualValues(t, 1000, out.CurrentMaturity.DuePendingMinor)
			require.EqualValues(t, 2000, out.CurrentPartnerBalances.PendingMinor)
			require.Equal(t, start.Add(time.Hour), *out.CurrentMaturity.OldestAvailableAt)
			if tc.scope == "date" {
				require.Zero(t, out.AcceptedAllocationRows)
			}
			if tc.scope == "cursor" || tc.scope == "plan" {
				require.Len(t, out.Plans, 1)
				require.Equal(t, "beta", out.Plans[0].PlanID)
			}
			clock.t = clock.t.Add(time.Minute)
			both, err := s.GetFinancialMetrics(ctx, "partner", FinancialMetricsQuery{Limit: 1})
			require.NoError(t, err)
			require.Equal(t, out.Revision, both.Revision)
			require.EqualValues(t, 2, both.CurrentMaturity.DueAllocationRows)
			require.EqualValues(t, 2000, both.CurrentMaturity.DuePendingMinor)
			require.Equal(t, *out.CurrentMaturity.OldestAvailableAt, *both.CurrentMaturity.OldestAvailableAt)
		})
	}
}

func TestCurrentMaturityValidation(t *testing.T) {
	for _, tc := range []struct {
		name, mode string
		want       error
	}{
		{"empty_ledger_is_valid", "empty", nil}, {"zero_credit_due_row_is_valid", "zero", nil}, {"held_due_credit_is_valid", "held", nil},
		{"negative_due_rows", "rows", ErrConflict}, {"negative_due_pending", "pending", ErrConflict}, {"hold_exceeds_due_pending", "excess_hold", ErrConflict},
		{"zero_rows_cannot_have_credit", "false_zero", ErrConflict}, {"zero_rows_cannot_have_oldest_time", "empty_time", ErrConflict},
		{"due_rows_need_oldest_time", "missing_time", ErrConflict}, {"oldest_due_time_cannot_be_future", "future", ErrConflict}, {"zero_classification_clock", "clock", ErrConflict},
	} {
		t.Run(tc.name, func(t *testing.T) {
			at := time.Unix(1700000000, 0).UTC()
			oldest := at.Add(-time.Hour)
			out := CurrentMaturity{DueAllocationRows: 1, DuePendingMinor: 1000, OldestAvailableAt: &oldest}
			switch tc.mode {
			case "empty":
				out = CurrentMaturity{}
			case "zero":
				out.DuePendingMinor = 0
			case "held":
				out.DueDisputeHoldMinor = 1000
			case "rows":
				out.DueAllocationRows = -1
			case "pending":
				out.DuePendingMinor = -1
			case "excess_hold":
				out.DueDisputeHoldMinor = 1001
			case "false_zero":
				out.DueAllocationRows = 0
				out.OldestAvailableAt = nil
			case "empty_time":
				out.DueAllocationRows = 0
				out.DuePendingMinor = 0
			case "missing_time":
				out.OldestAvailableAt = nil
			case "future":
				oldest = at.Add(time.Second)
			case "clock":
				at = time.Time{}
			}
			require.ErrorIs(t, out.Validate(at), tc.want)
		})
	}
}

func TestCurrentMaturityProjectionFailureDiscardsResult(t *testing.T) {
	for _, tc := range []struct {
		name, mode string
		want       error
	}{
		{"checked_due_sum_cannot_wrap", "overflow", ErrInvalid},
		{"cancelled_due_projection_is_empty", "cancelled", context.Canceled},
		{"due_credit_cannot_exceed_complete_pending", "excess_pending", ErrConflict},
		{"due_hold_cannot_exceed_complete_pending_hold", "excess_hold", ErrConflict},
	} {
		t.Run(tc.name, func(t *testing.T) {
			at := time.Unix(1700000000, 0).UTC()
			snapshot := paymentSnapshot{asOf: at, balances: Balances{PendingMinor: 1001}, lots: []PaymentEarning{{AvailableAt: at, Amounts: PaymentAmounts{PendingEarnedMinor: 1000}}, {AvailableAt: at, Amounts: PaymentAmounts{PendingEarnedMinor: 1}}}}
			ctx, cancel := context.WithCancel(context.Background())
			t.Cleanup(cancel)
			switch tc.mode {
			case "overflow":
				snapshot.balances.PendingMinor = math.MaxInt64
				snapshot.lots[0].Amounts.PendingEarnedMinor = math.MaxInt64
			case "cancelled":
				cancel()
			case "excess_pending":
				snapshot.balances.PendingMinor = 1000
			case "excess_hold":
				snapshot.balances.PendingDisputeHoldMinor = 9
				snapshot.lots[0].Amounts.DisputeHoldMinor = 10
			}
			out, err := currentMaturityMetrics(ctx, snapshot)
			require.ErrorIs(t, err, tc.want)
			require.Empty(t, out)
		})
	}
}
