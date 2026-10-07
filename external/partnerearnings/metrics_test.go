package partnerearnings

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func financialMetricFixture(t *testing.T) (*Service, *fakeRepo, *fixedClock, Claim, time.Time, time.Time) {
	t.Helper()
	s, repo, clock := newTestService(t)
	for i, plan := range []string{"alpha", "beta", ""} {
		req := accrualReq("partner", "allocation-"+string(rune('a'+i)), 5000, 2000)
		req.PlanID, req.ReferralID = plan, "referral-"+string(rune('a'+i))
		req.OccurredAt = clock.Now().Add(-time.Duration(72-i*24) * time.Hour)
		if plan == "" {
			req.PaymentMinor = 1500
		}
		_, err := s.Accrue(context.Background(), req)
		require.NoError(t, err)
	}
	claim, err := s.RequestClaim(context.Background(), ClaimRequest{PartnerID: "partner", ActorID: "owner", AmountMinor: 1500, Currency: testCurrency, DestinationID: "destination", IdempotencyKey: "claim"})
	require.NoError(t, err)
	claim, err = s.DecideClaim(context.Background(), ClaimDecision{ClaimID: claim.ID, ActorID: "operator", ExpectedRevision: claim.Revision, NewState: ClaimProcessing})
	require.NoError(t, err)
	paidAt := clock.Now().Add(-12 * time.Hour)
	claim, err = s.RecordPayment(context.Background(), RecordPaymentRequest{ClaimID: claim.ID, ActorID: "operator", ExpectedRevision: claim.Revision, AmountMinor: claim.AmountMinor, Currency: testCurrency, State: PaymentStateFull, Method: "bank", Reference: "fixture-transfer", PaidAt: paidAt, IdempotencyKey: "record"})
	require.NoError(t, err)
	returnedAt := clock.Now().Add(-6 * time.Hour)
	result, err := s.RecordReturnedTransfer(context.Background(), ReturnRequest{ClaimID: claim.ID, ActorID: "operator", ExpectedRevision: claim.Revision, AmountMinor: 700, Currency: testCurrency, Reference: "fixture-return", Reason: "verified fixture return", ReturnedAt: returnedAt, IdempotencyKey: "return"})
	require.NoError(t, err)
	return s, repo, clock, result.Claim, paidAt, returnedAt
}

// Audit disposition: named actual owning fiscal lifecycles distinguish cohort
// status from original economic-date movements and conserve split plan backing.
func TestFinancialMetricsCohortsAndMovements(t *testing.T) {
	cases := []struct {
		name, mode                                            string
		rows, plans                                           int
		accrued, paid, returned, periodAccrued, periodMatured int64
	}{
		{name: "complete_cohort_and_split_manual_payments", rows: 3, plans: 3, accrued: 2300, paid: 1500, returned: 700, periodAccrued: 2300, periodMatured: 2300},
		{name: "first_original_payment_cohort_keeps_later_return_status", mode: "first", rows: 1, plans: 1, accrued: 1000, periodAccrued: 1000},
		{name: "paid_date_uses_original_debit_not_settlement_recording_time", mode: "paid", plans: 2, paid: 1500},
		{name: "return_date_restores_only_original_alpha_backing", mode: "returned", plans: 1, returned: 700},
		{name: "recording_date_does_not_rebook_backdated_transfer", mode: "recorded", plans: 3, periodMatured: 2300},
		{name: "selected_alpha_plan_keeps_global_balances", mode: "alpha", rows: 1, plans: 1, accrued: 1000, paid: 1000, returned: 700, periodAccrued: 1000, periodMatured: 1000},
		{name: "unspecified_plan_is_explicit_not_current_catalogue", mode: "unspecified", rows: 1, plans: 1, accrued: 300, periodAccrued: 300, periodMatured: 300},
		{name: "amended_paid_date_does_not_rebook_original_movement", mode: "amended", plans: 2, paid: 1500},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, _, clock, claim, paidAt, returnedAt := financialMetricFixture(t)
			q := FinancialMetricsQuery{Limit: 100}
			var from time.Time
			switch tc.mode {
			case "first":
				from = clock.Now().Add(-72 * time.Hour)
			case "paid", "amended":
				from = paidAt
			case "returned":
				from = returnedAt
			case "recorded":
				from = clock.Now()
			case "alpha":
				q.PlanID = "alpha"
			case "unspecified":
				q.UnspecifiedPlanOnly = true
			}
			if !from.IsZero() {
				to := from.Add(time.Hour)
				q.From, q.To = &from, &to
			}
			if tc.mode == "amended" {
				_, err := s.AmendPayment(context.Background(), AmendPaymentRequest{ClaimID: claim.ID, ActorID: "operator", ExpectedRevision: claim.Revision, Method: "bank", Reference: "corrected-reference", PaidAt: clock.Now().Add(-2 * time.Hour), Reason: "correct presentation date", IdempotencyKey: "amend"})
				require.NoError(t, err)
			}
			out, err := s.GetFinancialMetrics(context.Background(), "partner", q)
			require.NoError(t, err)
			require.Equal(t, tc.rows, out.AcceptedAllocationRows)
			require.EqualValues(t, tc.accrued, out.CohortAmounts.AccruedMinor)
			require.Len(t, out.Plans, tc.plans)
			require.EqualValues(t, tc.paid, out.PeriodMovements.PaidMinor)
			require.EqualValues(t, tc.returned, out.PeriodMovements.ReturnedMinor)
			require.EqualValues(t, tc.periodAccrued, out.PeriodMovements.AccruedMinor)
			require.EqualValues(t, tc.periodMatured, out.PeriodMovements.MaturedGrossMinor)
			require.EqualValues(t, 1500, out.CurrentPartnerBalances.AvailableMinor)
			require.EqualValues(t, 1500, out.RemainingCommissionMinor)
			require.Equal(t, 1, out.CurrentClaims.Paid)
			require.Zero(t, out.CurrentClaims.ProcessingExposureMinor)
			if tc.mode == "first" {
				require.EqualValues(t, 300, out.CohortAmounts.NetPaidBackingMinor)
				require.EqualValues(t, 700, out.CohortAmounts.ReturnedBackingMinor)
			}
			if tc.mode == "paid" || tc.mode == "amended" {
				require.EqualValues(t, 1000, out.Plans[0].PeriodMovements.PaidMinor)
				require.EqualValues(t, 500, out.Plans[1].PeriodMovements.PaidMinor)
			}
			if tc.mode == "unspecified" {
				require.Equal(t, "unspecified", out.Plans[0].Key)
				require.Empty(t, out.Plans[0].PlanID)
			}
		})
	}
}

func TestFinancialMetricsPlanPagesAndIdentity(t *testing.T) {
	for _, tc := range []struct {
		name  string
		retry bool
	}{{name: "single_snapshot_cursor_independent_totals"}, {name: "callback_retry_does_not_double_metrics", retry: true}} {
		t.Run(tc.name, func(t *testing.T) {
			s, repo, clock, _, _, _ := financialMetricFixture(t)
			for _, plan := range []string{"unspecified", "plan:alpha"} {
				req := accrualReq("partner", "special-"+plan, 5, 0)
				req.PlanID = plan
				_, err := s.Accrue(context.Background(), req)
				require.NoError(t, err)
			}
			if tc.retry {
				var err error
				s, err = NewService(&retryOnceRepo{repo}, clock, &seqIDs{}, s.Config())
				require.NoError(t, err)
			}
			baseline, err := s.GetFinancialMetrics(context.Background(), "partner", FinancialMetricsQuery{Limit: 100})
			require.NoError(t, err)
			require.Len(t, baseline.Plans, 5)
			require.False(t, baseline.HasMore)
			require.Empty(t, baseline.NextAfterPlanKey)
			cursor := ""
			seen := map[string]bool{}
			for i := 0; i < 5; i++ {
				page, err := s.GetFinancialMetrics(context.Background(), "partner", FinancialMetricsQuery{Limit: 1, AfterPlanKey: cursor})
				require.NoError(t, err)
				require.Equal(t, baseline.Revision, page.Revision)
				require.Equal(t, baseline.CohortAmounts, page.CohortAmounts)
				require.Equal(t, baseline.PeriodMovements, page.PeriodMovements)
				require.Equal(t, baseline.CurrentClaims, page.CurrentClaims)
				require.Equal(t, baseline.CurrentPartnerBalances, page.CurrentPartnerBalances)
				require.Equal(t, 5, page.AcceptedAllocationRows)
				require.Len(t, page.Plans, 1)
				require.False(t, seen[page.Plans[0].Key])
				seen[page.Plans[0].Key] = true
				require.Equal(t, i < 4, page.HasMore)
				cursor = page.NextAfterPlanKey
			}
			for _, key := range []string{"unspecified", "plan:unspecified", "plan:plan:alpha"} {
				require.True(t, seen[key], "real plan IDs must not collide with unspecified/prefix grouping")
			}
		})
	}
}

// Audit disposition: named reservation endings and debt/pending lifecycles
// distinguish unpaid releases, original economics and remaining obligations.
func TestFinancialMetricsReservationEndingsAndDebt(t *testing.T) {
	for _, tc := range []struct {
		name, state           string
		refund, pending       bool
		remaining, debt, paid int64
	}{
		{name: "cancelled_unpaid_reservation_is_not_a_return", state: ClaimCancelled, remaining: 1000},
		{name: "rejected_unpaid_reservation_is_not_a_return", state: ClaimRejected, remaining: 1000},
		{name: "paid_and_refunded_credit_is_debt_not_remaining_commission", state: ClaimPaid, refund: true, debt: 600, paid: 600},
		{name: "positive_pending_and_negative_matched_remain_separate", state: ClaimPaid, refund: true, pending: true, debt: 600, remaining: 1000, paid: 600},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _, clock := newTestService(t)
			ctx := context.Background()
			_, err := s.Accrue(ctx, accrualReq("partner", "allocation", 5000, 2000))
			require.NoError(t, err)
			c, err := s.RequestClaim(ctx, ClaimRequest{PartnerID: "partner", ActorID: "owner", AmountMinor: 600, Currency: testCurrency, DestinationID: "destination", IdempotencyKey: "claim"})
			require.NoError(t, err)
			state := tc.state
			if state == ClaimPaid {
				state = ClaimProcessing
			}
			c, err = s.DecideClaim(ctx, ClaimDecision{ClaimID: c.ID, ActorID: "operator", ExpectedRevision: c.Revision, NewState: state, Reason: "verified fixture decision"})
			require.NoError(t, err)
			if tc.state == ClaimPaid {
				_, err = s.RecordPayment(ctx, RecordPaymentRequest{ClaimID: c.ID, ActorID: "operator", ExpectedRevision: c.Revision, AmountMinor: c.AmountMinor, Currency: testCurrency, State: PaymentStateFull, Method: "bank", Reference: "transfer", PaidAt: clock.Now(), IdempotencyKey: "record"})
				require.NoError(t, err)
			}
			if tc.refund {
				_, err = s.Reverse(ctx, ReversalRequest{PartnerID: "partner", PaymentID: "allocation", RefundID: "refund", Currency: testCurrency, CumulativeRefundedMinor: 5000, OccurredAt: clock.Now()})
				require.NoError(t, err)
			}
			if tc.pending {
				req := accrualReq("partner", "pending", 5000, 2000)
				req.HoldDuration, req.OccurredAt = 7*24*time.Hour, clock.Now()
				_, err = s.Accrue(ctx, req)
				require.NoError(t, err)
			}
			out, err := s.GetFinancialMetrics(ctx, "partner", FinancialMetricsQuery{Limit: 1})
			require.NoError(t, err)
			require.EqualValues(t, tc.remaining, out.RemainingCommissionMinor)
			require.EqualValues(t, tc.debt, out.CurrentPartnerBalances.DebtMinor)
			require.EqualValues(t, tc.paid, out.PeriodMovements.PaidMinor)
			require.Zero(t, out.PeriodMovements.ReturnedMinor)
			require.Zero(t, out.CurrentClaims.ProcessingExposureMinor)
			require.Nil(t, out.CurrentClaims.OldestOpenRequestedAt)
			if tc.pending {
				require.EqualValues(t, -600, out.CurrentPartnerBalances.MatchedMinor)
				require.EqualValues(t, 1000, out.CurrentPartnerBalances.PendingMinor)
			}
		})
	}
}

func TestFinancialMetricsDateAndContextBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name, mode string
		rows       int
		want       error
	}{
		{name: "from_is_inclusive", mode: "from", rows: 1},
		{name: "to_is_exclusive", mode: "to"},
		{name: "empty_financial_snapshot_is_not_source_coverage", mode: "empty"},
		{name: "nil_context", mode: "nil", want: ErrUnavailable},
		{name: "cancelled_context", mode: "cancelled", want: context.Canceled},
		{name: "zero_date", mode: "zero", want: ErrInvalid},
		{name: "equal_dates", mode: "equal", want: ErrInvalid},
		{name: "reversed_dates", mode: "reversed", want: ErrInvalid},
		{name: "empty_plan_cursor", mode: "cursor", want: ErrInvalid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _, clock := newTestService(t)
			ctx := context.Background()
			q := FinancialMetricsQuery{Limit: 100}
			at := clock.Now().Add(-time.Hour)
			if tc.mode != "empty" {
				req := accrualReq("partner", "allocation", 5000, 2000)
				req.OccurredAt = at
				_, err := s.Accrue(ctx, req)
				require.NoError(t, err)
			}
			switch tc.mode {
			case "from":
				q.From = &at
			case "to":
				q.To = &at
			case "nil":
				ctx = nil
			case "cancelled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			case "zero":
				at = time.Time{}
				q.From = &at
			case "equal":
				q.From, q.To = &at, &at
			case "reversed":
				before := at.Add(-time.Hour)
				q.From, q.To = &at, &before
			case "cursor":
				q.AfterPlanKey = "plan:"
			}
			out, err := s.GetFinancialMetrics(ctx, "partner", q)
			require.ErrorIs(t, err, tc.want)
			if tc.want != nil {
				require.Empty(t, out)
			} else {
				require.Equal(t, tc.rows, out.AcceptedAllocationRows)
				require.NotEmpty(t, out.Revision)
				if tc.mode == "empty" {
					require.Empty(t, out.Plans)
					require.Zero(t, out.LedgerSequence)
				}
			}
		})
	}
}

func TestFrozenBillingPlanEvidence(t *testing.T) {
	for _, tc := range []struct {
		name, plan, replay string
		want               error
	}{
		{name: "exact_frozen_plan_replays", plan: "original", replay: "original"},
		{name: "changed_plan_cannot_reprice_existing_payment", plan: "original", replay: "new", want: ErrConflict},
		{name: "unspecified_standalone_provenance_replays", plan: "", replay: ""},
		{name: "known_plan_cannot_be_invented_for_an_accepted_unspecified_row", plan: "", replay: "new", want: ErrConflict},
		{name: "padded_plan_identity_is_invalid", plan: " padded ", want: ErrInvalid},
		{name: "overlong_plan_identity_is_invalid", plan: strings.Repeat("x", 257), want: ErrInvalid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _, _ := newTestService(t)
			req := accrualReq("partner", "allocation", 5000, 2000)
			req.PlanID = tc.plan
			first, err := s.Accrue(context.Background(), req)
			if tc.want == ErrInvalid {
				require.ErrorIs(t, err, tc.want)
				require.Empty(t, first)
				return
			}
			require.NoError(t, err)
			req.PlanID = tc.replay
			again, err := s.Accrue(context.Background(), req)
			require.ErrorIs(t, err, tc.want)
			if tc.want != nil {
				require.Empty(t, again)
			} else {
				require.Equal(t, first, again)
			}
		})
	}
}

func TestFinancialMetricsExposureAndFrozenObligations(t *testing.T) {
	for _, tc := range []struct {
		name, state               string
		pending                   int64
		refunded, held            bool
		exposure, remaining, debt int64
	}{
		{name: "requested_is_reserved_but_not_potentially_sent", state: ClaimRequested, remaining: 1000},
		{name: "processing_exposure_is_separate_from_earned_obligation", state: ClaimProcessing, exposure: 600, remaining: 1000},
		{name: "review_keeps_potentially_sent_full_claim", state: ClaimNeedsReview, exposure: 600, remaining: 1000},
		{name: "refund_preserves_processing_exposure_even_without_credit", state: ClaimProcessing, refunded: true, exposure: 600},
		{name: "frozen_pending_is_obligation_but_not_available", state: ClaimRequested, held: true, pending: 1000, remaining: 2000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _, clock := newTestService(t)
			ctx := context.Background()
			_, err := s.Accrue(ctx, accrualReq("partner", "allocation", 5000, 2000))
			require.NoError(t, err)
			c, err := s.RequestClaim(ctx, ClaimRequest{PartnerID: "partner", ActorID: "owner", AmountMinor: 600, Currency: testCurrency, DestinationID: "destination", IdempotencyKey: "claim"})
			require.NoError(t, err)
			if tc.state != ClaimRequested {
				_, err = s.DecideClaim(ctx, ClaimDecision{ClaimID: c.ID, ActorID: "operator", ExpectedRevision: c.Revision, NewState: tc.state, Reason: "review"})
				require.NoError(t, err)
			}
			if tc.refunded {
				_, err = s.Reverse(ctx, ReversalRequest{PartnerID: "partner", PaymentID: "allocation", RefundID: "refund", Currency: testCurrency, CumulativeRefundedMinor: 5000, OccurredAt: clock.Now()})
				require.NoError(t, err)
			}
			if tc.pending > 0 {
				req := accrualReq("partner", "pending", 5000, 2000)
				req.HoldDuration = 7 * 24 * time.Hour
				req.OccurredAt = clock.Now()
				_, err = s.Accrue(ctx, req)
				require.NoError(t, err)
				if tc.held {
					_, err = s.Dispute(ctx, DisputeRequest{PartnerID: "partner", PaymentID: "pending", DisputeID: "dispute", OperationID: "hold", Action: "hold", ActorID: "worker", Currency: testCurrency, OccurredAt: clock.Now(), Reason: "verified"})
					require.NoError(t, err)
				}
			}
			out, err := s.GetFinancialMetrics(ctx, "partner", FinancialMetricsQuery{Limit: 1})
			require.NoError(t, err)
			require.EqualValues(t, tc.exposure, out.CurrentClaims.ProcessingExposureMinor)
			require.EqualValues(t, tc.remaining, out.RemainingCommissionMinor)
			require.EqualValues(t, tc.debt, out.CurrentPartnerBalances.DebtMinor)
			require.NotNil(t, out.CurrentClaims.OldestOpenRequestedAt)
			if tc.held {
				require.EqualValues(t, 1000, out.CurrentPartnerBalances.PendingDisputeHoldMinor)
				require.EqualValues(t, 400, out.CurrentPartnerBalances.AvailableMinor)
			}
		})
	}
}

func TestFinancialMetricsFailuresDiscardPartialResults(t *testing.T) {
	for _, tc := range []struct {
		name, mode string
		want       error
	}{
		{name: "invalid_limit", mode: "limit", want: ErrInvalid},
		{name: "conflicting_plan_filters", mode: "filter", want: ErrInvalid},
		{name: "unknown_cursor", mode: "cursor", want: ErrInvalid},
		{name: "cursor_from_changed_filter_is_not_a_continuation", mode: "excluded_cursor", want: ErrInvalid},
		{name: "cursor_from_excluded_date_is_not_a_continuation", mode: "dated_cursor", want: ErrInvalid},
		{name: "nil_bound_transaction", mode: "bound", want: ErrUnavailable},
		{name: "joined_absence_and_outage", mode: "outage", want: ErrUnavailable},
		{name: "uncertain_snapshot", mode: "uncertain", want: ErrUncertain},
		{name: "invalid_immutable_plan_provenance", mode: "plan", want: ErrConflict},
		{name: "missing_settled_backing", mode: "backing", want: ErrConflict},
		{name: "return_journal_cannot_move_to_another_period", mode: "return_date", want: ErrConflict},
		{name: "recorded_return_date_must_match_its_operation", mode: "adjustment_date", want: ErrConflict},
		{name: "return_adjustment_requires_exact_operation_link", mode: "return_link", want: ErrConflict},
		{name: "return_recording_actor_must_match_journal", mode: "return_actor", want: ErrConflict},
		{name: "return_operation_cannot_alias_a_cancelled_claim", mode: "return_claim_collision", want: ErrConflict},
	} {
		t.Run(tc.name, func(t *testing.T) {
			owner, repo, clock, c, _, _ := financialMetricFixture(t)
			var cancelled Claim
			if tc.mode == "return_claim_collision" {
				var err error
				cancelled, err = owner.RequestClaim(context.Background(), ClaimRequest{PartnerID: "partner", ActorID: "owner", AmountMinor: 250, Currency: testCurrency, DestinationID: "destination", IdempotencyKey: "cancelled"})
				require.NoError(t, err)
				cancelled, err = owner.DecideClaim(context.Background(), ClaimDecision{ClaimID: cancelled.ID, ActorID: "operator", ExpectedRevision: cancelled.Revision, NewState: ClaimCancelled, Reason: "confirmed unsent", ConfirmedUnsent: true})
				require.NoError(t, err)
			}
			r := &paymentReadRepo{entries: repo.entries, claims: []Claim{c}}
			s, err := NewService(r, clock, &seqIDs{}, Config{ProgramID: testProgram, Currency: testCurrency})
			require.NoError(t, err)
			q := FinancialMetricsQuery{Limit: 100}
			switch tc.mode {
			case "limit":
				q.Limit = 101
			case "filter":
				q.PlanID, q.UnspecifiedPlanOnly = "alpha", true
			case "cursor":
				q.AfterPlanKey = "plan:unknown"
			case "excluded_cursor":
				q.PlanID, q.AfterPlanKey = "beta", "plan:alpha"
			case "dated_cursor":
				from := clock.Now().Add(time.Hour)
				q.From, q.AfterPlanKey = &from, "plan:alpha"
			case "bound":
				r.nilBound = true
			case "outage":
				r.fail = errors.Join(ErrNotFound, ErrUnavailable)
			case "uncertain":
				r.commitErr = ErrUncertain
			case "plan":
				r.entries[0].PlanID = " invalid "
			case "backing":
				for n, e := range r.entries {
					if e.Kind == EntryAllocationSettled {
						r.entries[n].AmountMinor++
						break
					}
				}
			case "return_date":
				for n, e := range r.entries {
					if e.Kind == EntryReturned {
						r.entries[n].OccurredAt = e.OccurredAt.Add(time.Hour)
						break
					}
				}
			case "adjustment_date":
				r.claims[0].ReturnedAdjustments[0].ReturnedAt = c.ReturnedAdjustments[0].ReturnedAt.Add(time.Hour)
			case "return_link":
				r.claims[0].ReturnedAdjustments[0].OperationID = "other-operation"
			case "return_actor":
				r.claims[0].ReturnedAdjustments[0].By = "other-operator"
			case "return_claim_collision":
				operation := c.ReturnedAdjustments[0].OperationID
				r.claims = append(r.claims, cancelled)
				r.claims[0].ReturnedAdjustments[0].OperationID = cancelled.ID
				for n, e := range r.entries {
					if (e.Kind == EntryReturned || e.Kind == EntryAllocationReleased) && e.SourceEventID == operation {
						r.entries[n].SourceEventID = cancelled.ID
					}
				}
			}
			out, err := s.GetFinancialMetrics(context.Background(), "partner", q)
			require.ErrorIs(t, err, tc.want)
			require.Empty(t, out)
			if strings.HasPrefix(tc.mode, "return_") || tc.mode == "adjustment_date" {
				payment, err := s.GetPaymentReport(context.Background(), "partner", PaymentQuery{Limit: 100})
				require.ErrorIs(t, err, ErrConflict)
				require.Empty(t, payment)
			}
		})
	}

	for _, tc := range []struct {
		name  string
		value int64
		want  error
	}{{name: "representable_revenue_total", value: math.MaxInt64 / 2}, {name: "overflow_discards_entire_metrics", value: math.MaxInt64, want: ErrInvalid}} {
		t.Run(tc.name, func(t *testing.T) {
			value := tc.value
			s, _, _ := newTestService(t)
			for _, id := range []string{"one", "two"} {
				_, err := s.Accrue(context.Background(), accrualReq("partner", id, value, 0))
				require.NoError(t, err)
			}
			out, err := s.GetFinancialMetrics(context.Background(), "partner", FinancialMetricsQuery{Limit: 100})
			if tc.want != nil {
				require.ErrorIs(t, err, tc.want)
				require.Empty(t, out)
			} else {
				require.NoError(t, err)
				require.EqualValues(t, 2*value, out.CohortAmounts.OriginalRevenueMinor)
			}
		})
	}
}

// Audit disposition: independent owning accrual/maturity/refund lifecycles keep
// current balances representable while testing period movement overflow. The
// period excludes original accrual dates, so cohort sums cannot mask this path.
func TestFinancialMetricsMovementSumsDiscardOverflow(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value int64
		want  error
	}{
		{name: "representable_maturity_and_refund_movements", value: math.MaxInt64 / 2},
		{name: "movement_overflow_with_zero_current_balance", value: math.MaxInt64, want: ErrInvalid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _, clock := newTestService(t)
			ctx := context.Background()
			for _, payment := range []string{"one", "two"} {
				req := accrualReq("partner", payment, tc.value, 10000)
				req.OccurredAt, req.HoldDuration = clock.Now().Add(-24*time.Hour), time.Hour
				_, err := s.Accrue(ctx, req)
				require.NoError(t, err)
				matured, err := s.Mature(ctx, "partner")
				require.NoError(t, err)
				require.Len(t, matured, 1)
				_, err = s.Reverse(ctx, ReversalRequest{PartnerID: "partner", PaymentID: payment, RefundID: "refund-" + payment, CumulativeRefundedMinor: tc.value, Currency: testCurrency, OccurredAt: clock.Now()})
				require.NoError(t, err)
			}
			balances, err := s.Balances(ctx, "partner")
			require.NoError(t, err)
			require.Zero(t, balances.PendingMinor)
			require.Zero(t, balances.MatchedMinor)
			from := clock.Now()
			out, err := s.GetFinancialMetrics(ctx, "partner", FinancialMetricsQuery{Limit: 100, From: &from})
			require.ErrorIs(t, err, tc.want)
			if tc.want != nil {
				require.Empty(t, out)
				return
			}
			require.Zero(t, out.AcceptedAllocationRows)
			require.Equal(t, 2*tc.value, out.PeriodMovements.MaturedGrossMinor)
			require.Equal(t, 2*tc.value, out.PeriodMovements.RefundReversedMinor)
		})
	}
}

func TestFinancialMetricsRemainingSumAndClaimRevision(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value int64
		want  error
	}{
		{name: "representable_pending_plus_matched", value: math.MaxInt64 / 2},
		{name: "remaining_overflow_even_when_selected_cohort_is_empty", value: math.MaxInt64, want: ErrInvalid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _, clock := newTestService(t)
			ctx := context.Background()
			for _, pending := range []bool{false, true} {
				id := "matched"
				if pending {
					id = "pending"
				}
				req := accrualReq("partner", id, tc.value, 10000)
				req.PlanID = "original-plan"
				if pending {
					req.OccurredAt, req.HoldDuration = clock.Now(), 7*24*time.Hour
				}
				_, err := s.Accrue(ctx, req)
				require.NoError(t, err)
			}
			balances, err := s.Balances(ctx, "partner")
			require.NoError(t, err)
			require.Equal(t, tc.value, balances.PendingMinor)
			require.Equal(t, tc.value, balances.MatchedMinor)
			out, err := s.GetFinancialMetrics(ctx, "partner", FinancialMetricsQuery{Limit: 100, PlanID: "outside-selected-cohort"})
			require.ErrorIs(t, err, tc.want)
			if tc.want != nil {
				require.Empty(t, out)
			} else {
				require.Zero(t, out.AcceptedAllocationRows)
				require.Equal(t, 2*tc.value, out.RemainingCommissionMinor)
			}
		})
	}
	for _, state := range []string{ClaimProcessing, ClaimNeedsReview} {
		t.Run("claim_only_revision/"+state, func(t *testing.T) {
			s, _, _ := newTestService(t)
			ctx := context.Background()
			_, err := s.Accrue(ctx, accrualReq("partner", "allocation", 5000, 2000))
			require.NoError(t, err)
			c, err := s.RequestClaim(ctx, ClaimRequest{PartnerID: "partner", ActorID: "owner", AmountMinor: 600, Currency: testCurrency, DestinationID: "destination", IdempotencyKey: "claim"})
			require.NoError(t, err)
			before, err := s.GetFinancialMetrics(ctx, "partner", FinancialMetricsQuery{Limit: 100})
			require.NoError(t, err)
			_, err = s.DecideClaim(ctx, ClaimDecision{ClaimID: c.ID, ActorID: "operator", ExpectedRevision: c.Revision, NewState: state, Reason: "verified review"})
			require.NoError(t, err)
			after, err := s.GetFinancialMetrics(ctx, "partner", FinancialMetricsQuery{Limit: 100})
			require.NoError(t, err)
			require.Equal(t, before.LedgerSequence, after.LedgerSequence)
			require.NotEqual(t, before.Revision, after.Revision)
			require.EqualValues(t, 600, after.CurrentClaims.ProcessingExposureMinor)
			require.Zero(t, before.CurrentClaims.ProcessingExposureMinor)
		})
	}
}
