package partnerearnings

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Audit disposition: named financial projection cases prove that bounded/date
// filters do not rewrite running balances or current liabilities. They use the
// financial owner only; Mongo snapshot/paging has separate integration proof.
func statementDebtFixture(t *testing.T) (*Service, Claim) {
	t.Helper()
	s, _, clock := newTestService(t)
	ctx := context.Background()
	_, err := s.Accrue(ctx, accrualReq("partner", "payment", 10000, 2000))
	require.NoError(t, err)
	c, err := s.RequestClaim(ctx, ClaimRequest{ActorID: "owner", PartnerID: "partner", AmountMinor: 1500, Currency: testCurrency, DestinationID: "destination", IdempotencyKey: "request"})
	require.NoError(t, err)
	c, err = s.DecideClaim(ctx, ClaimDecision{ClaimID: c.ID, NewState: ClaimProcessing, ActorID: "operator", ExpectedRevision: c.Revision})
	require.NoError(t, err)
	c, err = s.RecordPayment(ctx, RecordPaymentRequest{ClaimID: c.ID, ActorID: "operator", Method: "paypal", Reference: "customer-safe-reference", PaidAt: clock.Now().Add(-time.Minute), AmountMinor: c.AmountMinor, Currency: c.Currency, State: PaymentStateFull, IdempotencyKey: "payment-record", ExpectedRevision: c.Revision})
	require.NoError(t, err)
	_, err = s.Reverse(ctx, ReversalRequest{PartnerID: "partner", PaymentID: "payment", RefundID: "refund", CumulativeRefundedMinor: 5000, Currency: testCurrency, OccurredAt: clock.Now()})
	require.NoError(t, err)
	_, err = s.Accrue(ctx, accrualReq("partner", "later-payment", 4000, 2000))
	require.NoError(t, err)
	return s, c
}
func TestStatementFiltersPreserveFullRunningBalance(t *testing.T) {
	cases := []struct {
		name     string
		q        StatementQuery
		paidOnly bool
	}{{name: "complete_statement", q: StatementQuery{Limit: 100}}, {name: "paid_filter_keeps_prior_unfiltered_maturity", q: StatementQuery{Limit: 100, Kinds: []string{EntryPaid}}, paidOnly: true}, {name: "bounded_newest_page", q: StatementQuery{Limit: 2}}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, claim := statementDebtFixture(t)
			out, err := s.GetStatement(context.Background(), "partner", tc.q)
			require.NoError(t, err)
			require.EqualValues(t, 300, out.Balances.MatchedMinor)
			require.EqualValues(t, 300, out.Balances.AvailableMinor)
			require.EqualValues(t, 1500, out.Balances.PaidOutMinor)
			require.NotEmpty(t, out.Revision)
			require.False(t, out.AsOf.IsZero())
			require.Equal(t, "partner", out.PartnerID)
			if tc.paidOnly {
				require.Len(t, out.Lines, 1)
				line := out.Lines[0]
				require.EqualValues(t, 500, line.RunningMaturedMinor)
				require.NotNil(t, line.Payment)
				require.Equal(t, claim.Payment, line.Payment)
				return
			}
			require.EqualValues(t, 300, out.Lines[0].RunningMaturedMinor)
			for i := 1; i < len(out.Lines); i++ {
				require.Less(t, out.Lines[i].Entry.Sequence, out.Lines[i-1].Entry.Sequence)
			}
			if tc.q.Limit == 2 {
				require.True(t, out.HasMore)
				require.Equal(t, out.Lines[1].Entry.Sequence, out.NextBeforeSequence)
				next, err := s.GetStatement(context.Background(), "partner", StatementQuery{Limit: 100, BeforeSequence: out.NextBeforeSequence})
				require.NoError(t, err)
				require.Less(t, next.Lines[0].Entry.Sequence, out.NextBeforeSequence)
				require.Equal(t, out.Balances, next.Balances)
			}
		})
	}
}

func TestStatementRevisionIncludesClaimChanges(t *testing.T) {
	cases := []struct {
		name, state string
		review      bool
	}{{name: "processing_changes_revision_without_adding_journal", state: ClaimProcessing}, {name: "manual_review_changes_current_hold", state: ClaimNeedsReview, review: true}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, _, _ := newTestService(t)
			ctx := context.Background()
			_, err := s.Accrue(ctx, accrualReq("partner", "payment", 10000, 2000))
			require.NoError(t, err)
			c, err := s.RequestClaim(ctx, ClaimRequest{ActorID: "owner", PartnerID: "partner", AmountMinor: 1000, Currency: testCurrency, DestinationID: "destination", IdempotencyKey: "request"})
			require.NoError(t, err)
			if tc.review {
				c, err = s.DecideClaim(ctx, ClaimDecision{ClaimID: c.ID, NewState: ClaimProcessing, ActorID: "operator", ExpectedRevision: c.Revision})
				require.NoError(t, err)
			}
			before, err := s.GetStatement(ctx, "partner", StatementQuery{Limit: 100})
			require.NoError(t, err)
			_, err = s.DecideClaim(ctx, ClaimDecision{ClaimID: c.ID, NewState: tc.state, ActorID: "operator", Reason: "manual review", ExpectedRevision: c.Revision})
			require.NoError(t, err)
			after, err := s.GetStatement(ctx, "partner", StatementQuery{Limit: 100})
			require.NoError(t, err)
			require.Equal(t, before.LedgerSequence, after.LedgerSequence)
			require.NotEqual(t, before.Revision, after.Revision)
			if tc.review {
				require.Zero(t, after.Balances.ReservedMinor)
				require.EqualValues(t, 1000, after.Balances.ReviewHoldMinor)
			} else {
				require.Equal(t, before.Balances, after.Balances)
			}
		})
	}
}

func TestStatementInputAndDateBoundaries(t *testing.T) {
	from := time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC)
	to := from.Add(24 * time.Hour)
	cases := []struct {
		name                  string
		q                     StatementQuery
		want                  error
		nilContext, cancelled bool
		valid                 bool
	}{
		{name: "inclusive_from_exclusive_to", q: StatementQuery{Limit: 100, From: &from, To: &to}, valid: true},
		{name: "zero_limit", q: StatementQuery{}, want: ErrInvalid},
		{name: "oversized_limit", q: StatementQuery{Limit: 101}, want: ErrInvalid},
		{name: "unknown_kind", q: StatementQuery{Limit: 100, Kinds: []string{"guessed-money"}}, want: ErrInvalid},
		{name: "duplicate_kind", q: StatementQuery{Limit: 100, Kinds: []string{EntryPaid, EntryPaid}}, want: ErrInvalid},
		{name: "reversed_dates", q: StatementQuery{Limit: 100, From: &to, To: &from}, want: ErrInvalid},
		{name: "negative_cursor", q: StatementQuery{Limit: 100, BeforeSequence: -1}, want: ErrInvalid},
		{name: "nil_context", q: StatementQuery{Limit: 100}, nilContext: true, want: ErrUnavailable},
		{name: "cancelled_context", q: StatementQuery{Limit: 100}, cancelled: true, want: context.Canceled},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := statementDebtFixture(t)
			ctx := context.Background()
			if tc.nilContext {
				ctx = nil
			}
			if tc.cancelled {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			out, err := s.GetStatement(ctx, "partner", tc.q)
			require.ErrorIs(t, err, tc.want)
			if tc.valid {
				require.NotEmpty(t, out.Lines)
				for _, line := range out.Lines {
					require.False(t, line.Entry.OccurredAt.Before(from))
					require.True(t, line.Entry.OccurredAt.Before(to))
				}
				require.EqualValues(t, 300, out.Balances.AvailableMinor)
			}
		})
	}
}
