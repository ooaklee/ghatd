package partnerearnings

import (
	"context"
	"errors"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Audit disposition: related named financial lifecycles join original frozen
// revisions, including future old-owner payments, using fresh owning fixtures.
func TestReferralAmountsRetainOriginalPaymentGroups(t *testing.T) {
	cases := []struct {
		name, mode              string
		payments                int
		accrued, paid, returned int64
	}{
		{name: "split_claim_is_not_duplicated_across_owned_periods", payments: 2, accrued: 2000, paid: 1500},
		{name: "return_restores_only_original_portions", mode: "returned", payments: 2, accrued: 2000, paid: 800, returned: 700},
		{name: "future_payment_still_uses_old_frozen_revision", mode: "future", payments: 3, accrued: 2600, paid: 1500},
		{name: "original_payment_date_cohort_not_payout_date", mode: "cohort", payments: 1, accrued: 1000, paid: 500},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, _, clock, claim := splitPaymentReportFixture(t)
			ctx := context.Background()
			if tc.mode == "returned" {
				_, err := s.RecordReturnedTransfer(ctx, ReturnRequest{ClaimID: claim.ID, ActorID: "operator", ExpectedRevision: claim.Revision, AmountMinor: 700, Currency: testCurrency, ReturnedAt: clock.Now(), Reference: "return", Reason: "verified", IdempotencyKey: "return"})
				require.NoError(t, err)
			}
			if tc.mode == "future" {
				req := accrualReq("partner", "future", 3000, 2000)
				req.ReferralID, req.OccurredAt = "referral-one", clock.Now().Add(30*24*time.Hour)
				_, err := s.Accrue(ctx, req)
				require.NoError(t, err)
			}
			q := ReferralAmountQuery{Groups: []ReferralAmountGroup{{ID: "retained", ReferralIDs: []string{"referral-one", "referral-two"}}, {ID: "unaccepted", ReferralIDs: []string{"not-yet-accrued"}}}}
			if tc.mode == "cohort" {
				from, to := accrualReq("partner", "ignored", 1, 0).OccurredAt.Add(24*time.Hour), accrualReq("partner", "ignored", 1, 0).OccurredAt.Add(48*time.Hour)
				q.From, q.To = &from, &to
			}
			out, err := s.GetReferralAmounts(ctx, "partner", q)
			require.NoError(t, err)
			require.Len(t, out.Items, 2)
			row := out.Items[0]
			require.Equal(t, tc.payments, row.AcceptedPayments)
			require.EqualValues(t, tc.accrued, row.Amounts.AccruedMinor)
			require.EqualValues(t, tc.paid, row.Amounts.NetPaidBackingMinor)
			require.EqualValues(t, tc.returned, row.Amounts.ReturnedBackingMinor)
			require.NotNil(t, row.FirstPaymentAt)
			require.NotNil(t, row.LastPaymentAt)
			require.False(t, row.LastPaymentAt.Before(*row.FirstPaymentAt))
			require.Equal(t, ReferralAmounts{ID: "unaccepted"}, out.Items[1], "absence only means no accepted accrual; no source decision is made")
			payments, err := s.GetPaymentReport(ctx, "partner", PaymentQuery{Limit: 1})
			require.NoError(t, err)
			require.Equal(t, payments.Revision, out.Revision)
			require.Equal(t, payments.Balances, out.Balances, "global balance must not be derived from selected groups/cohort")
		})
	}
}

func TestReferralAmountQueryBoundaries(t *testing.T) {
	cases := []struct {
		name, mode string
		want       error
	}{
		{name: "empty_selection_still_reads_global_snapshot"},
		{name: "duplicate_group", mode: "group", want: ErrInvalid},
		{name: "duplicate_revision_in_group", mode: "revision", want: ErrInvalid},
		{name: "overlap_between_groups", mode: "overlap", want: ErrInvalid},
		{name: "missing_revision", mode: "empty", want: ErrInvalid},
		{name: "bounded_group_count", mode: "groups", want: ErrInvalid},
		{name: "exact_total_revision_capacity_is_supported", mode: "capacity"},
		{name: "bounded_total_revision_count", mode: "revisions", want: ErrReportTooLarge},
		{name: "malformed_group_identity", mode: "identity", want: ErrInvalid},
		{name: "malformed_revision_identity", mode: "id", want: ErrInvalid},
		{name: "invalid_date_order", mode: "dates", want: ErrInvalid},
		{name: "zero_date", mode: "zero", want: ErrInvalid},
		{name: "nil_context", mode: "nil", want: ErrUnavailable},
		{name: "canceled_context", mode: "cancel", want: context.Canceled},
		{name: "missing_bound_transaction", mode: "bound", want: ErrUnavailable},
		{name: "unavailable_read_is_not_zero", mode: "outage", want: ErrUnavailable},
		{name: "unknown_commit_discards_report", mode: "uncertain", want: ErrUncertain},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := &paymentReadRepo{}
			s, err := NewService(r, &fixedClock{t: time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)}, &seqIDs{}, Config{ProgramID: testProgram, Currency: testCurrency})
			require.NoError(t, err)
			q := ReferralAmountQuery{}
			ctx := context.Background()
			group := ReferralAmountGroup{ID: "group", ReferralIDs: []string{"revision"}}
			switch tc.mode {
			case "group":
				q.Groups = []ReferralAmountGroup{group, group}
			case "revision":
				group.ReferralIDs = append(group.ReferralIDs, "revision")
				q.Groups = []ReferralAmountGroup{group}
			case "overlap":
				q.Groups = []ReferralAmountGroup{group, {ID: "other", ReferralIDs: []string{"revision"}}}
			case "empty":
				q.Groups = []ReferralAmountGroup{{ID: "empty"}}
			case "groups":
				q.Groups = make([]ReferralAmountGroup, 101)
			case "revisions", "capacity":
				group.ReferralIDs = nil
				limit := MaxReferralAmountRevisions
				if tc.mode == "revisions" {
					limit++
				}
				for n := 0; n < limit; n++ {
					group.ReferralIDs = append(group.ReferralIDs, fmt.Sprintf("revision-%d", n))
				}
				q.Groups = []ReferralAmountGroup{group}
			case "identity":
				group.ID = " malformed "
				q.Groups = []ReferralAmountGroup{group}
			case "id":
				group.ReferralIDs = []string{" invalid "}
				q.Groups = []ReferralAmountGroup{group}
			case "dates":
				from, to := s.clock.Now(), s.clock.Now()
				q.From, q.To = &from, &to
			case "zero":
				q.From = &time.Time{}
			case "nil":
				ctx = nil
			case "cancel":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			case "bound":
				r.nilBound = true
			case "outage":
				r.fail = errors.Join(ErrNotFound, ErrUnavailable)
			case "uncertain":
				r.commitErr = ErrUncertain
			}
			out, err := s.GetReferralAmounts(ctx, "partner", q)
			require.ErrorIs(t, err, tc.want)
			if tc.want != nil {
				require.Empty(t, out)
				if tc.mode == "bound" || tc.mode == "outage" || tc.mode == "uncertain" {
					require.Equal(t, 1, r.calls)
				} else {
					require.Zero(t, r.calls)
				}
			} else {
				require.Len(t, out.Items, len(q.Groups))
				if tc.mode == "capacity" {
					require.Equal(t, ReferralAmounts{ID: "group"}, out.Items[0])
				}
				require.NotEmpty(t, out.Revision)
			}
		})
	}
}

func TestReferralAmountSnapshotConservation(t *testing.T) {
	cases := []struct {
		name  string
		retry bool
		value int64
		want  error
	}{
		{name: "accepted_zero_commission_has_payment_rows", value: 10},
		{name: "callback_retry_resets_group_accumulators", retry: true, value: 10},
		{name: "cohort_revenue_overflow_discards_all_groups", value: math.MaxInt64, want: ErrInvalid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, repo, clock := newTestService(t)
			for _, payment := range []string{"one", "two"} {
				req := accrualReq("partner", payment, tc.value, 0)
				req.ReferralID = "revision-" + payment
				_, err := s.Accrue(context.Background(), req)
				require.NoError(t, err)
			}
			if tc.retry {
				var err error
				s, err = NewService(&retryOnceRepo{repo}, clock, &seqIDs{}, s.Config())
				require.NoError(t, err)
			}
			out, err := s.GetReferralAmounts(context.Background(), "partner", ReferralAmountQuery{Groups: []ReferralAmountGroup{{ID: "group", ReferralIDs: []string{"revision-one", "revision-two"}}}})
			require.ErrorIs(t, err, tc.want)
			if tc.want != nil {
				require.Empty(t, out)
				return
			}
			require.Equal(t, 2, out.Items[0].AcceptedPayments)
			require.EqualValues(t, 2*tc.value, out.Items[0].Amounts.OriginalRevenueMinor)
			require.Zero(t, out.Items[0].Amounts.AccruedMinor)
		})
	}
}
