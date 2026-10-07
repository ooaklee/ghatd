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

func splitPaymentReportFixture(t *testing.T) (*Service, *fakeRepo, *fixedClock, Claim) {
	t.Helper()
	s, repo, clock := newTestService(t)
	ctx := context.Background()
	for i, name := range []string{"one", "two"} {
		req := accrualReq("partner", "payment-"+name, 5000, 2000)
		req.ReferralID, req.TermsVersion, req.PolicyID = "referral-"+name, "fixture-terms", "fixture-policy"
		req.OccurredAt = req.OccurredAt.Add(time.Duration(i) * 24 * time.Hour)
		_, err := s.Accrue(ctx, req)
		require.NoError(t, err)
	}
	c, err := s.RequestClaim(ctx, ClaimRequest{PartnerID: "partner", ActorID: "owner", AmountMinor: 1500, Currency: testCurrency, DestinationID: "destination", IdempotencyKey: "first"})
	require.NoError(t, err)
	c, err = s.DecideClaim(ctx, ClaimDecision{ClaimID: c.ID, ActorID: "operator", ExpectedRevision: c.Revision, NewState: ClaimProcessing})
	require.NoError(t, err)
	c, err = s.RecordPayment(ctx, RecordPaymentRequest{ClaimID: c.ID, ActorID: "operator", ExpectedRevision: c.Revision, AmountMinor: c.AmountMinor, Currency: testCurrency, State: PaymentStateFull, Method: "bank", Reference: "original-transfer", PaidAt: clock.Now(), IdempotencyKey: "record"})
	require.NoError(t, err)
	return s, repo, clock, c
}

// Audit disposition: named real financial-owner lifecycles assert conserved
// split-claim/return/reclaim/refund portions and cohort-vs-current semantics.
// These expected values come from commands through the financial service.
func TestPaymentReportConservesSplitPayouts(t *testing.T) {
	cases := []struct {
		name         string
		returns      int
		reclaim      bool
		refund       bool
		gross, net   int64
		returned     int64
		available    int64
		firstGross   int64
		firstNet     int64
		firstReturns int64
	}{
		{name: "one_whole_claim_is_split_1000_and500", gross: 1500, net: 1500, available: 500, firstGross: 1000, firstNet: 1000},
		{name: "first_return_restores700_of_first_payment", returns: 1, gross: 1500, net: 800, returned: 700, available: 1200, firstGross: 1000, firstNet: 300, firstReturns: 700},
		{name: "second_return_crosses_original_payment_boundary", returns: 2, gross: 1500, net: 100, returned: 1400, available: 1900, firstGross: 1000, firstNet: 0, firstReturns: 1000},
		{name: "restored_backing_reclaimed_and_paid_again", returns: 2, reclaim: true, gross: 2700, net: 1300, returned: 1400, available: 700, firstGross: 2000, firstNet: 1000, firstReturns: 1000},
		{name: "post_payout_refund_does_not_reassign_payout", returns: 2, reclaim: true, refund: true, gross: 2700, net: 1300, returned: 1400, available: 200, firstGross: 2000, firstNet: 1000, firstReturns: 1000},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, _, clock, c := splitPaymentReportFixture(t)
			ctx := context.Background()
			for n := 0; n < tc.returns; n++ {
				key := "return-" + string(rune('a'+n))
				result, err := s.RecordReturnedTransfer(ctx, ReturnRequest{ClaimID: c.ID, ActorID: "operator", ExpectedRevision: c.Revision, AmountMinor: 700, Currency: testCurrency, ReturnedAt: clock.Now(), Reference: key, Reason: "verified_return", IdempotencyKey: key})
				require.NoError(t, err)
				c = result.Claim
			}
			if tc.reclaim {
				second, err := s.RequestClaim(ctx, ClaimRequest{PartnerID: "partner", ActorID: "owner", AmountMinor: 1200, Currency: testCurrency, DestinationID: "destination", IdempotencyKey: "second"})
				require.NoError(t, err)
				second, err = s.DecideClaim(ctx, ClaimDecision{ClaimID: second.ID, ActorID: "operator", ExpectedRevision: second.Revision, NewState: ClaimProcessing})
				require.NoError(t, err)
				_, err = s.RecordPayment(ctx, RecordPaymentRequest{ClaimID: second.ID, ActorID: "operator", ExpectedRevision: second.Revision, AmountMinor: second.AmountMinor, Currency: testCurrency, State: PaymentStateFull, Method: "bank", Reference: "second-transfer", PaidAt: clock.Now(), IdempotencyKey: "record-second"})
				require.NoError(t, err)
			}
			if tc.refund {
				for i, cumulative := range []int64{2500, 1000} {
					_, err := s.Reverse(ctx, ReversalRequest{PartnerID: "partner", PaymentID: "payment-one", RefundID: "refund-" + string(rune('a'+i)), Currency: testCurrency, CumulativeRefundedMinor: cumulative, OccurredAt: clock.Now()})
					require.NoError(t, err)
				}
			}
			out, err := s.GetPaymentReport(ctx, "partner", PaymentQuery{Limit: 1})
			require.NoError(t, err)
			require.EqualValues(t, 2, out.CohortPayments)
			require.EqualValues(t, tc.gross, out.CohortAmounts.GrossPaidBackingMinor)
			require.EqualValues(t, tc.returned, out.CohortAmounts.ReturnedBackingMinor)
			require.EqualValues(t, tc.net, out.CohortAmounts.NetPaidBackingMinor)
			require.EqualValues(t, tc.available, out.Balances.AvailableMinor)
			require.EqualValues(t, tc.gross, out.Balances.PaidOutMinor)
			require.Len(t, out.Items, 1)
			require.Equal(t, "payment-two", out.Items[0].PaymentID)
			require.True(t, out.HasMore)
			next, err := s.GetPaymentReport(ctx, "partner", PaymentQuery{Limit: 100, BeforeAccrualSequence: out.NextBeforeAccrualSequence})
			require.NoError(t, err)
			require.Equal(t, out.Revision, next.Revision)
			require.Equal(t, out.CohortAmounts, next.CohortAmounts)
			require.Equal(t, out.Balances, next.Balances)
			require.Len(t, next.Items, 1)
			first := next.Items[0]
			require.Equal(t, "referral-one", first.ReferralID)
			require.EqualValues(t, tc.firstGross, first.Amounts.GrossPaidBackingMinor)
			require.EqualValues(t, tc.firstReturns, first.Amounts.ReturnedBackingMinor)
			require.EqualValues(t, tc.firstNet, first.Amounts.NetPaidBackingMinor)
			if tc.refund {
				require.EqualValues(t, 2500, first.Amounts.RefundedRevenueMinor, "lower late cumulative evidence does not reduce recorded maximum")
				require.EqualValues(t, 500, first.Amounts.MaturedEarnedMinor)
			} else {
				require.EqualValues(t, 1000, first.Amounts.MaturedEarnedMinor)
			}
			filtered, err := s.GetPaymentReport(ctx, "partner", PaymentQuery{Limit: 100, ReferralID: "referral-one"})
			require.NoError(t, err)
			require.EqualValues(t, 1, filtered.CohortPayments)
			require.Equal(t, first.Amounts, filtered.CohortAmounts)
			require.Equal(t, out.Balances, filtered.Balances, "referral filter cannot invent per-referral available funds")
			from, to := out.Items[0].OccurredAt, out.Items[0].OccurredAt.Add(24*time.Hour)
			cohort, err := s.GetPaymentReport(ctx, "partner", PaymentQuery{Limit: 100, From: &from, To: &to})
			require.NoError(t, err)
			require.EqualValues(t, 1, cohort.CohortPayments)
			require.Equal(t, out.Items[0].Amounts, cohort.CohortAmounts, "cohort retains later payout/return status despite those events outside original payment dates")
		})
	}
}

func TestPaymentReportPendingAndClaimHolds(t *testing.T) {
	cases := []struct {
		name, mode                            string
		pending, matured, reservation, review int64
		dispute, available                    int64
		needsReview                           bool
	}{
		{name: "pending_hold_is_not_matured", mode: "pending", pending: 1000},
		{name: "requested_claim_backing", mode: "requested", matured: 1000, reservation: 600, available: 400},
		{name: "processing_refund_retains_frozen_reservation", mode: "processing_refund", reservation: 600, needsReview: true},
		{name: "needs_review_is_separate_backing", mode: "review", matured: 1000, review: 600, available: 400, needsReview: true},
		{name: "cancelled_claim_releases_all_backing", mode: "cancelled", matured: 1000, available: 1000},
		{name: "dispute_hold_cancels_requested_claim", mode: "hold", matured: 1000, dispute: 1000},
		{name: "won_dispute_restores_credit", mode: "won", matured: 1000, available: 1000},
		{name: "lost_dispute_removes_remaining_credit", mode: "lost"},
		{name: "lost_dispute_arrives_without_prior_hold", mode: "lost_first"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, _, clock := newTestService(t)
			ctx := context.Background()
			req := accrualReq("partner", "payment", 5000, 2000)
			if tc.mode == "pending" {
				req.HoldDuration = 7 * 24 * time.Hour
			}
			_, err := s.Accrue(ctx, req)
			require.NoError(t, err)
			if tc.mode != "pending" {
				claim, err := s.RequestClaim(ctx, ClaimRequest{PartnerID: "partner", ActorID: "owner", AmountMinor: 600, Currency: testCurrency, DestinationID: "destination", IdempotencyKey: "claim"})
				require.NoError(t, err)
				if tc.mode == "review" || tc.mode == "cancelled" || tc.mode == "processing_refund" {
					state := ClaimNeedsReview
					if tc.mode == "cancelled" {
						state = ClaimCancelled
					}
					if tc.mode == "processing_refund" {
						state = ClaimProcessing
					}
					_, err = s.DecideClaim(ctx, ClaimDecision{ClaimID: claim.ID, ActorID: "operator", ExpectedRevision: claim.Revision, NewState: state, Reason: "fixture"})
					require.NoError(t, err)
				}
				if tc.mode == "processing_refund" {
					_, err = s.Reverse(ctx, ReversalRequest{PartnerID: "partner", PaymentID: "payment", RefundID: "refund", Currency: testCurrency, CumulativeRefundedMinor: 5000, OccurredAt: clock.Now()})
					require.NoError(t, err)
				}
				if tc.mode == "hold" || tc.mode == "won" || tc.mode == "lost" {
					for _, action := range []string{"hold", tc.mode} {
						_, err = s.Dispute(ctx, DisputeRequest{PartnerID: "partner", PaymentID: "payment", DisputeID: "dispute", OperationID: "operation-" + action, Action: action, ActorID: "worker", Currency: testCurrency, OccurredAt: clock.Now(), Reason: "verified"})
						require.NoError(t, err)
					}
				}
				if tc.mode == "lost_first" {
					_, err = s.Dispute(ctx, DisputeRequest{PartnerID: "partner", PaymentID: "payment", DisputeID: "dispute", OperationID: "lost-first", Action: DisputeLost, ActorID: "worker", Currency: testCurrency, OccurredAt: clock.Now(), Reason: "verified_terminal_without_prior_hold"})
					require.NoError(t, err)
				}
			}
			out, err := s.GetPaymentReport(ctx, "partner", PaymentQuery{Limit: 100})
			require.NoError(t, err)
			require.Len(t, out.Items, 1)
			require.EqualValues(t, tc.pending, out.Items[0].Amounts.PendingEarnedMinor)
			require.EqualValues(t, tc.matured, out.Items[0].Amounts.MaturedEarnedMinor)
			require.EqualValues(t, tc.reservation, out.Items[0].Amounts.ReservedBackingMinor)
			require.EqualValues(t, tc.review, out.Items[0].Amounts.ReviewBackingMinor)
			require.EqualValues(t, tc.dispute, out.Items[0].Amounts.DisputeHoldMinor)
			require.EqualValues(t, tc.available, out.Balances.AvailableMinor)
			require.Equal(t, tc.needsReview, out.Items[0].ReviewRequired)
		})
	}
}

type paymentReadRepo struct {
	Repository
	entries   []Entry
	claims    []Claim
	fail      error
	commitErr error
	nilBound  bool
	calls     int
}

func (r *paymentReadRepo) WithTransaction(_ context.Context, _, _, _ string, fn func(Repository) error) error {
	r.calls++
	if r.nilBound {
		return fn(nil)
	}
	if err := fn(r); err != nil {
		return err
	}
	return r.commitErr
}
func (r *paymentReadRepo) ListEntries(context.Context, string, string) ([]Entry, error) {
	return r.entries, r.fail
}
func (r *paymentReadRepo) ListClaims(context.Context, string, string, []string, int, string) ([]Claim, error) {
	return r.claims, nil
}

func TestPaymentReportRejectsMalformedEvidence(t *testing.T) {
	cases := []struct{ name, mode string }{
		{name: "wrong_entry_scope", mode: "scope"},
		{name: "wrong_currency", mode: "currency"},
		{name: "missing_journal_prefix", mode: "prefix"},
		{name: "duplicate_accrual_source", mode: "duplicate"},
		{name: "wrong_paid_amount", mode: "paid"},
		{name: "settlement_portion_differs_from_allocation", mode: "portion"},
		{name: "missing_settlement", mode: "missing"},
		{name: "unknown_backing_payment", mode: "payment"},
		{name: "claim_missing_from_current_heads", mode: "claim"},
		{name: "claim_amount_differs_from_total_backing", mode: "amount"},
		{name: "unknown_claim_state", mode: "state"},
		{name: "duplicate_claim_head", mode: "claim_duplicate"},
		{name: "settlement_before_paid_evidence", mode: "settlement_order"},
		{name: "paid_before_complete_allocations", mode: "allocation_order"},
		{name: "joined_absence_and_outage", mode: "outage"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, repo, clock, claim := splitPaymentReportFixture(t)
			r := &paymentReadRepo{entries: append([]Entry(nil), repo.entries...), claims: []Claim{claim}}
			switch tc.mode {
			case "scope":
				r.entries[0].PartnerID = "other"
			case "currency":
				r.entries[0].Currency = "USD"
			case "prefix":
				r.entries = r.entries[1:]
			case "duplicate":
				e := r.entries[0]
				e.ID, e.Sequence = "different-entry", int64(len(r.entries)+1)
				r.entries = append(r.entries, e)
			case "paid", "portion", "payment", "missing":
				for i, e := range r.entries {
					if tc.mode == "paid" && e.Kind == EntryPaid {
						r.entries[i].AmountMinor++
						break
					}
					if e.Kind != EntryAllocationSettled {
						continue
					}
					if tc.mode == "portion" {
						r.entries[i].AmountMinor++
					}
					if tc.mode == "payment" {
						r.entries[i].SourceRef = "unknown"
					}
					if tc.mode == "missing" {
						r.entries = append(r.entries[:i], r.entries[i+1:]...)
						for n := range r.entries {
							r.entries[n].Sequence = int64(n) + 1
						}
					}
					break
				}
			case "claim":
				r.claims = nil
			case "amount":
				r.claims[0].AmountMinor++
			case "state":
				r.claims[0].State = "unknown"
			case "claim_duplicate":
				r.claims = append(r.claims, claim)
			case "settlement_order", "allocation_order":
				paid, backing := -1, -1
				for i, e := range r.entries {
					if e.Kind == EntryPaid {
						paid = i
					}
					kind := EntryAllocationSettled
					if tc.mode == "allocation_order" {
						kind = EntryAllocated
					}
					if e.Kind == kind && backing < 0 {
						backing = i
					}
				}
				require.NotEqual(t, -1, paid)
				require.NotEqual(t, -1, backing)
				r.entries[paid], r.entries[backing] = r.entries[backing], r.entries[paid]
				for i := range r.entries {
					r.entries[i].Sequence = int64(i) + 1
				}
			case "outage":
				r.fail = errors.Join(ErrNotFound, ErrUnavailable)
			}
			s, err := NewService(r, clock, &seqIDs{}, Config{ProgramID: testProgram, Currency: testCurrency})
			require.NoError(t, err)
			out, err := s.GetPaymentReport(context.Background(), "partner", PaymentQuery{Limit: 100})
			want := ErrConflict
			if tc.mode == "outage" {
				want = ErrUnavailable
			}
			require.ErrorIs(t, err, want)
			require.Empty(t, out, "invalid complete financial evidence cannot produce partial totals")
		})
	}
}

func TestPaymentReportInputAndAbsence(t *testing.T) {
	from := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	to := from.Add(24 * time.Hour)
	cases := []struct {
		name, mode string
		q          PaymentQuery
		want       error
	}{
		{name: "owning_empty_journal_is_empty_snapshot", q: PaymentQuery{Limit: 100}},
		{name: "zero_limit", q: PaymentQuery{}, want: ErrInvalid},
		{name: "oversized_limit", q: PaymentQuery{Limit: 101}, want: ErrInvalid},
		{name: "negative_cursor", q: PaymentQuery{Limit: 100, BeforeAccrualSequence: -1}, want: ErrInvalid},
		{name: "invalid_referral", q: PaymentQuery{Limit: 100, ReferralID: strings.Repeat("x", 257)}, want: ErrInvalid},
		{name: "reversed_cohort_dates", q: PaymentQuery{Limit: 100, From: &to, To: &from}, want: ErrInvalid},
		{name: "nil_context", mode: "nil", q: PaymentQuery{Limit: 100}, want: ErrUnavailable},
		{name: "cancelled_context", mode: "cancel", q: PaymentQuery{Limit: 100}, want: context.Canceled},
		{name: "nil_transaction_dependency", mode: "bound", q: PaymentQuery{Limit: 100}, want: ErrUnavailable},
		{name: "uncertain_snapshot_commit_discards_data", mode: "uncertain", q: PaymentQuery{Limit: 100}, want: ErrUncertain},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := &paymentReadRepo{}
			r.nilBound = tc.mode == "bound"
			if tc.mode == "uncertain" {
				r.commitErr = ErrUncertain
			}
			s, err := NewService(r, &fixedClock{t: from}, &seqIDs{}, Config{ProgramID: testProgram, Currency: testCurrency})
			require.NoError(t, err)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tc.mode == "nil" {
				ctx = nil
			}
			if tc.mode == "cancel" {
				cancel()
			}
			out, err := s.GetPaymentReport(ctx, "partner", tc.q)
			require.ErrorIs(t, err, tc.want)
			if tc.want != nil {
				require.Empty(t, out)
				if tc.mode == "bound" || tc.mode == "uncertain" {
					require.Equal(t, 1, r.calls)
				} else {
					require.Zero(t, r.calls)
				}
			} else {
				require.Empty(t, out.Items)
				require.Zero(t, out.CohortPayments)
				require.NotEmpty(t, out.Revision)
				require.Equal(t, "partner", out.PartnerID)
			}
		})
	}
}

func TestPaymentReportCohortOverflow(t *testing.T) {
	cases := []struct {
		name  string
		value int64
		want  error
	}{
		{name: "combined_revenue_fits_integer", value: math.MaxInt64 / 2},
		{name: "combined_revenue_overflows_without_money_overflow", value: math.MaxInt64, want: ErrInvalid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, _, _ := newTestService(t)
			for _, payment := range []string{"one", "two"} {
				_, err := s.Accrue(context.Background(), accrualReq("partner", payment, tc.value, 0))
				require.NoError(t, err)
			}
			out, err := s.GetPaymentReport(context.Background(), "partner", PaymentQuery{Limit: 100})
			require.ErrorIs(t, err, tc.want)
			if tc.want != nil {
				require.Empty(t, out)
			} else {
				require.EqualValues(t, 2*tc.value, out.CohortAmounts.OriginalRevenueMinor)
				require.Zero(t, out.CohortAmounts.AccruedMinor)
			}
		})
	}
}

func TestPaymentReportDisputeProvenance(t *testing.T) {
	cases := []struct {
		name, dispute string
		want          error
	}{
		{name: "own_dispute_releases_its_hold", dispute: "dispute"},
		{name: "another_dispute_cannot_release_that_hold", dispute: "other", want: ErrConflict},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, repo, clock := newTestService(t)
			ctx := context.Background()
			_, err := s.Accrue(ctx, accrualReq("partner", "payment", 5000, 2000))
			require.NoError(t, err)
			for _, action := range []string{"hold", "won"} {
				_, err = s.Dispute(ctx, DisputeRequest{PartnerID: "partner", PaymentID: "payment", DisputeID: "dispute", OperationID: action, Action: action, ActorID: "worker", Currency: testCurrency, OccurredAt: clock.Now(), Reason: "verified"})
				require.NoError(t, err)
			}
			for i, e := range repo.entries {
				if e.Kind == EntryDisputeWon {
					repo.entries[i].DisputeID = tc.dispute
				}
			}
			out, err := s.GetPaymentReport(ctx, "partner", PaymentQuery{Limit: 100})
			require.ErrorIs(t, err, tc.want)
			if tc.want != nil {
				require.Empty(t, out)
			} else {
				require.Zero(t, out.CohortAmounts.DisputeHoldMinor)
				require.EqualValues(t, 1000, out.Balances.AvailableMinor)
			}
		})
	}
}

func TestPaymentReportRevisionTracksClaimHead(t *testing.T) {
	for _, state := range []string{ClaimProcessing, ClaimNeedsReview} {
		t.Run(state, func(t *testing.T) {
			s, _, _ := newTestService(t)
			ctx := context.Background()
			_, err := s.Accrue(ctx, accrualReq("partner", "payment", 5000, 2000))
			require.NoError(t, err)
			c, err := s.RequestClaim(ctx, ClaimRequest{PartnerID: "partner", ActorID: "owner", AmountMinor: 600, Currency: testCurrency, DestinationID: "destination", IdempotencyKey: "claim"})
			require.NoError(t, err)
			before, err := s.GetPaymentReport(ctx, "partner", PaymentQuery{Limit: 100})
			require.NoError(t, err)
			_, err = s.DecideClaim(ctx, ClaimDecision{ClaimID: c.ID, ActorID: "operator", ExpectedRevision: c.Revision, NewState: state, Reason: "review"})
			require.NoError(t, err)
			after, err := s.GetPaymentReport(ctx, "partner", PaymentQuery{Limit: 100})
			require.NoError(t, err)
			require.Equal(t, before.LedgerSequence, after.LedgerSequence)
			require.NotEqual(t, before.Revision, after.Revision)
			if state == ClaimNeedsReview {
				require.EqualValues(t, 600, after.CohortAmounts.ReviewBackingMinor)
				require.Zero(t, after.CohortAmounts.ReservedBackingMinor)
			} else {
				require.Equal(t, before.CohortAmounts, after.CohortAmounts)
			}
		})
	}
}

// Audit disposition: named exact-fit and extra-row boundaries verify the
// exclusive cursor without changing correct pagination on review speculation.
func TestPaymentReportExactPageBoundary(t *testing.T) {
	for _, tc := range []struct {
		name  string
		limit int
		more  bool
	}{{name: "one_extra_matching_row", limit: 1, more: true}, {name: "exactly_two_matching_rows", limit: 2}, {name: "fewer_than_limit", limit: 3}} {
		t.Run(tc.name, func(t *testing.T) {
			s, _, _, _ := splitPaymentReportFixture(t)
			out, err := s.GetPaymentReport(context.Background(), "partner", PaymentQuery{Limit: tc.limit})
			require.NoError(t, err)
			require.Equal(t, tc.more, out.HasMore)
			if !tc.more {
				require.Zero(t, out.NextBeforeAccrualSequence)
				return
			}
			next, err := s.GetPaymentReport(context.Background(), "partner", PaymentQuery{Limit: tc.limit, BeforeAccrualSequence: out.NextBeforeAccrualSequence})
			require.NoError(t, err)
			require.Len(t, next.Items, 1)
			require.False(t, next.HasMore)
		})
	}
}
