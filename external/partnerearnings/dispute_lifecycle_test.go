package partnerearnings

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestDisputeLifecycle exercises the dispute lifecycle end to end: hold,
// won/lost resolution and interaction with refunds and maturity. Each case
// asserts derived balances and journal entry counts so replay-safe
// idempotency is verified, not just returned errors.

const testHold = 15 * 24 * time.Hour

func countEntries(t *testing.T, repo *fakeRepo, kind string) int {
	t.Helper()
	entries, err := repo.ListEntries(context.Background(), testProgram, "prt_1")
	require.NoError(t, err)
	n := 0
	for _, e := range entries {
		if e.Kind == kind {
			n++
		}
	}
	return n
}

func disputeReq(action, disputeID, operationID string, at time.Time) DisputeRequest {
	return DisputeRequest{
		PartnerID: "prt_1", PaymentID: "pay_1", DisputeID: disputeID,
		Action: action, OperationID: operationID, Reason: "customer complaint",
		ActorID: "act_1", Currency: testCurrency, OccurredAt: at,
	}
}

// matureTestPartner accrues pay_1 with a pending hold and advances the clock
// past maturity, leaving 2,000 minor units of matured commission.
func matureTestPartner(t *testing.T, svc *Service, clock *fixedClock) {
	t.Helper()
	req := accrualReq("prt_1", "pay_1", 10000, 2000)
	req.HoldDuration = testHold
	_, err := svc.Accrue(context.Background(), req)
	require.NoError(t, err)
	clock.t = clock.t.Add(testHold)
	matured, err := svc.Mature(context.Background(), "prt_1")
	require.NoError(t, err)
	require.Len(t, matured, 1)
	require.EqualValues(t, 2000, matured[0].AmountMinor)
}

func TestDisputeLifecycle(t *testing.T) {
	tests := []struct {
		name string
		run  func(t *testing.T, svc *Service, repo *fakeRepo, clock *fixedClock)
	}{
		{
			name: "hold before maturity does not reduce unrelated matured funds",
			run: func(t *testing.T, svc *Service, repo *fakeRepo, clock *fixedClock) {
				ctx := context.Background()
				// Disputed payment stays pending; its hold freezes the
				// accrued commission inside the pending balance.
				req := accrualReq("prt_1", "pay_1", 10000, 2000)
				req.HoldDuration = testHold
				_, err := svc.Accrue(ctx, req)
				require.NoError(t, err)
				_, err = svc.Dispute(ctx, disputeReq("hold", "dsp_1", "op_hold_1", clock.Now()))
				require.NoError(t, err)

				b, err := svc.Balances(ctx, "prt_1")
				require.NoError(t, err)
				require.EqualValues(t, 2000, b.PendingMinor)
				require.EqualValues(t, 2000, b.PendingDisputeHoldMinor)
				require.EqualValues(t, 0, b.DisputeHoldMinor)
				require.EqualValues(t, 0, b.MatchedMinor)
				require.EqualValues(t, 0, b.AvailableMinor)

				// Unrelated payment matures fully; the pending hold must not
				// reduce it.
				req2 := accrualReq("prt_1", "pay_2", 10000, 2000)
				req2.HoldDuration = testHold
				_, err = svc.Accrue(ctx, req2)
				require.NoError(t, err)
				clock.t = clock.t.Add(testHold)
				matured, err := svc.Mature(ctx, "prt_1")
				require.NoError(t, err)
				require.Len(t, matured, 2)

				// pay_1's hold carries into the matured bucket and only
				// freezes pay_1; pay_2's matured funds stay fully available.
				b, err = svc.Balances(ctx, "prt_1")
				require.NoError(t, err)
				require.EqualValues(t, 4000, b.MatchedMinor)
				require.EqualValues(t, 2000, b.AvailableMinor)
				require.EqualValues(t, 0, b.PendingMinor)
				require.EqualValues(t, 0, b.PendingDisputeHoldMinor)
				require.EqualValues(t, 2000, b.DisputeHoldMinor)
				require.Equal(t, 1, countEntries(t, repo, EntryDisputeHold))
				require.Equal(t, 2, countEntries(t, repo, EntryMatured))
			},
		},
		{
			name: "full refund while held then won does not restore refunded money",
			run: func(t *testing.T, svc *Service, repo *fakeRepo, clock *fixedClock) {
				ctx := context.Background()
				matureTestPartner(t, svc, clock)

				_, err := svc.Dispute(ctx, disputeReq("hold", "dsp_1", "op_hold_1", clock.Now()))
				require.NoError(t, err)
				b, err := svc.Balances(ctx, "prt_1")
				require.NoError(t, err)
				require.EqualValues(t, 2000, b.DisputeHoldMinor)
				require.EqualValues(t, 0, b.AvailableMinor)
				require.EqualValues(t, 2000, b.MatchedMinor)

				// Full refund while the dispute is held releases the hold and
				// reverses the matured commission.
				revs, err := svc.Reverse(ctx, ReversalRequest{
					PartnerID: "prt_1", RefundID: "rfd_1", PaymentID: "pay_1",
					CumulativeRefundedMinor: 10000, Currency: testCurrency, OccurredAt: clock.Now(),
				})
				require.NoError(t, err)
				require.Len(t, revs, 1)
				require.EqualValues(t, -2000, revs[0].AmountMinor)
				b, err = svc.Balances(ctx, "prt_1")
				require.NoError(t, err)
				require.EqualValues(t, 0, b.MatchedMinor)
				require.EqualValues(t, 0, b.DisputeHoldMinor)

				// Winning after the refund must not restore anything.
				res, err := svc.Dispute(ctx, disputeReq("won", "dsp_1", "op_won_1", clock.Now()))
				require.NoError(t, err)
				for _, e := range res.Entries {
					require.NotEqual(t, EntryMatured, e.Kind)
					require.NotEqual(t, EntryAccrued, e.Kind)
				}
				b, err = svc.Balances(ctx, "prt_1")
				require.NoError(t, err)
				require.EqualValues(t, 0, b.MatchedMinor)
				require.EqualValues(t, 0, b.AvailableMinor)
				require.EqualValues(t, 0, b.DisputeHoldMinor)

				// Replay-safe: replaying the won operation adds no entries.
				_, err = svc.Dispute(ctx, disputeReq("won", "dsp_1", "op_won_1", clock.Now()))
				require.NoError(t, err)
				require.Equal(t, 1, countEntries(t, repo, EntryAccrued))
				require.Equal(t, 1, countEntries(t, repo, EntryMatured))
				require.Equal(t, 1, countEntries(t, repo, EntryDisputeHold))
				require.Equal(t, 1, countEntries(t, repo, EntryReversed))
				require.Equal(t, 1, countEntries(t, repo, EntryDisputeReleased))
				require.Equal(t, 1, countEntries(t, repo, EntryDisputeWon))
				require.Equal(t, 2, countEntries(t, repo, EntryDisputeDecision))
			},
		},
		{
			name: "lost followed by full refund does not double reverse",
			run: func(t *testing.T, svc *Service, repo *fakeRepo, clock *fixedClock) {
				ctx := context.Background()
				matureTestPartner(t, svc, clock)

				_, err := svc.Dispute(ctx, disputeReq("lost", "dsp_1", "op_lost_1", clock.Now()))
				require.NoError(t, err)
				b, err := svc.Balances(ctx, "prt_1")
				require.NoError(t, err)
				require.EqualValues(t, 0, b.MatchedMinor)
				require.EqualValues(t, 0, b.AvailableMinor)
				require.EqualValues(t, 0, b.DisputeHoldMinor)
				require.Equal(t, 1, countEntries(t, repo, EntryDisputeLost))

				// The refund after the lost dispute adds no further reversal
				// amount: the lost decision already consumed the liability.
				revs, err := svc.Reverse(ctx, ReversalRequest{
					PartnerID: "prt_1", RefundID: "rfd_1", PaymentID: "pay_1",
					CumulativeRefundedMinor: 10000, Currency: testCurrency, OccurredAt: clock.Now(),
				})
				require.NoError(t, err)
				require.Len(t, revs, 1)
				require.EqualValues(t, 0, revs[0].AmountMinor)

				b, err = svc.Balances(ctx, "prt_1")
				require.NoError(t, err)
				require.EqualValues(t, 0, b.MatchedMinor)
				require.EqualValues(t, 0, b.AvailableMinor)
				require.Equal(t, 1, countEntries(t, repo, EntryReversed))
				require.Equal(t, 1, countEntries(t, repo, EntryDisputeLost))
				require.Equal(t, 1, countEntries(t, repo, EntryDisputeDecision))
			},
		},
		{
			name: "late hold after terminal lost does not revive the dispute",
			run: func(t *testing.T, svc *Service, repo *fakeRepo, clock *fixedClock) {
				ctx := context.Background()
				matureTestPartner(t, svc, clock)

				_, err := svc.Dispute(ctx, disputeReq("lost", "dsp_1", "op_lost_1", clock.Now()))
				require.NoError(t, err)
				b, err := svc.Balances(ctx, "prt_1")
				require.NoError(t, err)
				require.EqualValues(t, 0, b.MatchedMinor)

				// A late hold on the same payment cannot re-freeze any
				// liability: the amount is a zero-value placeholder.
				res, err := svc.Dispute(ctx, disputeReq("hold", "dsp_2", "op_hold_2", clock.Now()))
				require.NoError(t, err)
				for _, e := range res.Entries {
					if e.Kind == EntryDisputeHold {
						require.EqualValues(t, 0, e.AmountMinor,
							"late hold must not freeze amount on a terminal lost dispute")
					}
				}

				b, err = svc.Balances(ctx, "prt_1")
				require.NoError(t, err)
				require.EqualValues(t, 0, b.MatchedMinor)
				require.EqualValues(t, 0, b.AvailableMinor)
				require.EqualValues(t, 0, b.DisputeHoldMinor)
				require.EqualValues(t, 0, b.PendingDisputeHoldMinor)
				// Only the lost flow's journal remains; the dsp_2 hold
				// freezes nothing.
				require.Equal(t, 1, countEntries(t, repo, EntryDisputeLost))
				require.Equal(t, 2, countEntries(t, repo, EntryDisputeDecision))
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			svc, repo, clock := newTestService(t)
			tc.run(t, svc, repo, clock)
		})
	}
}
