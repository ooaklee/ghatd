package partnerstore

import (
	"fmt"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/partnerearnings"
	"github.com/stretchr/testify/require"
)

// Audit disposition: new isolated replica-set lifecycle cases assert money,
// original evidence, immutable replay and oldest restored backing together.
func TestMongoDisputeRefundLifecycle(t *testing.T) {
	type testCase struct {
		name                                                    string
		pending                                                 bool
		steps                                                   []string
		matched, dispute, pendingHold, pendingAmount, available int64
	}
	cases := []testCase{
		{name: "pending_dispute_does_not_freeze_unrelated_matured_credit", pending: true, steps: []string{"hold"}, matched: 1000, pendingHold: 2000, pendingAmount: 2000, available: 1000},
		{name: "won_after_full_refund_cannot_restore_money", steps: []string{"hold", "refund", "won"}},
		{name: "lost_then_refund_does_not_reverse_twice", steps: []string{"hold", "lost", "refund"}},
		{name: "late_hold_after_lost_cannot_revive_terminal_dispute", steps: []string{"hold", "lost", "hold"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, repo, _, _, clock, ctx := mongoEarnings(t)
			if tc.pending {
				_, err := svc.Accrue(ctx, partnerearnings.AccrualRequest{PartnerID: "partner", PaymentID: "payment", PaymentMinor: 10000, RateBasisPoints: 2000, HoldDuration: 14 * 24 * time.Hour, Currency: "EUR", OccurredAt: clock.Now()})
				require.NoError(t, err)
				accrue(t, svc, ctx, clock, "unrelated-payment", 5000)
			} else {
				accrue(t, svc, ctx, clock, "payment", 10000)
			}
			for i, action := range tc.steps {
				if action == "refund" {
					_, err := svc.Reverse(ctx, partnerearnings.ReversalRequest{PartnerID: "partner", PaymentID: "payment", RefundID: "refund", CumulativeRefundedMinor: 10000, Currency: "EUR", OccurredAt: clock.Now()})
					require.NoError(t, err)
					continue
				}
				req := partnerearnings.DisputeRequest{PartnerID: "partner", PaymentID: "payment", DisputeID: "dispute", OperationID: fmt.Sprintf("operation-%d", i), Action: action, ActorID: "worker", Reason: "verified-provider-dispute", Currency: "EUR", OccurredAt: clock.Now()}
				result, err := svc.Dispute(ctx, req)
				require.NoError(t, err)
				before, err := repo.ListEntries(ctx, "fixture-program", "partner")
				require.NoError(t, err)
				replay, err := svc.Dispute(ctx, req)
				require.NoError(t, err)
				require.Equal(t, result, replay)
				after, err := repo.ListEntries(ctx, "fixture-program", "partner")
				require.NoError(t, err)
				require.Equal(t, before, after)
			}
			balance, err := svc.Balances(ctx, "partner")
			require.NoError(t, err)
			require.Equal(t, tc.matched, balance.MatchedMinor)
			require.Equal(t, tc.dispute, balance.DisputeHoldMinor)
			require.Equal(t, tc.pendingHold, balance.PendingDisputeHoldMinor)
			require.Equal(t, tc.pendingAmount, balance.PendingMinor)
			require.Equal(t, tc.available, balance.AvailableMinor)
			require.Zero(t, balance.DebtMinor)
			if tc.pending {
				clock.now = clock.now.Add(15 * 24 * time.Hour)
				_, err := svc.Mature(ctx, "partner")
				require.NoError(t, err)
				balance, err = svc.Balances(ctx, "partner")
				require.NoError(t, err)
				require.EqualValues(t, 3000, balance.MatchedMinor)
				require.EqualValues(t, 2000, balance.DisputeHoldMinor)
				require.Zero(t, balance.PendingDisputeHoldMinor)
				require.Zero(t, balance.PendingMinor)
				require.EqualValues(t, 1000, balance.AvailableMinor)
			}
		})
	}
}
func TestMongoManualReviewPreservesReservation(t *testing.T) {
	type testCase struct {
		name, state, currency string
		observed              int64
		wantState             string
	}
	cases := []testCase{
		{name: "partial", state: partnerearnings.PaymentStatePartial, currency: "EUR", observed: 500, wantState: partnerearnings.PaymentStatePartial},
		{name: "unknown", state: partnerearnings.PaymentStateUnknown, wantState: partnerearnings.PaymentStateUnknown},
		{name: "wrong_amount", state: partnerearnings.PaymentStateFull, currency: "EUR", observed: 900, wantState: partnerearnings.PaymentStateMismatched},
		{name: "wrong_currency", state: partnerearnings.PaymentStateFull, currency: "USD", observed: 1000, wantState: partnerearnings.PaymentStateMismatched},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, repo, _, _, clock, ctx := mongoEarnings(t)
			accrue(t, svc, ctx, clock, "payment", 10000)
			claim, err := svc.RequestClaim(ctx, claimRequest(1000, "claim"))
			require.NoError(t, err)
			process(t, svc, ctx, claim)
			req := paymentRequest(claim, clock, "observed")
			req.AmountMinor = tc.observed
			req.Currency = tc.currency
			req.State = tc.state
			review, err := svc.RecordPayment(ctx, req)
			require.NoError(t, err)
			require.Equal(t, partnerearnings.ClaimNeedsReview, review.State)
			require.Nil(t, review.Payment)
			require.Len(t, review.PaymentObservations, 1)
			require.Equal(t, tc.wantState, review.PaymentObservations[0].State)
			require.Equal(t, tc.observed, review.PaymentObservations[0].AmountMinor)
			require.Equal(t, tc.currency, review.PaymentObservations[0].Currency)
			replay, err := svc.RecordPayment(ctx, req)
			require.NoError(t, err)
			require.Equal(t, review, replay)
			fresh, err := repo.GetClaim(ctx, "fixture-program", claim.ID)
			require.NoError(t, err)
			require.Equal(t, review, fresh)
			balance, err := svc.Balances(ctx, "partner")
			require.NoError(t, err)
			require.EqualValues(t, 1000, balance.ReviewHoldMinor)
			require.EqualValues(t, 1000, balance.AvailableMinor)
			require.Zero(t, balance.PaidOutMinor)
			_, err = svc.DecideClaim(ctx, partnerearnings.ClaimDecision{ClaimID: claim.ID, ActorID: "operator", ExpectedRevision: review.Revision, NewState: partnerearnings.ClaimCancelled, Reason: "not_yet_reconciled"})
			require.ErrorIs(t, err, partnerearnings.ErrDenied)
			full := paymentRequest(claim, clock, "confirmed-full")
			full.ExpectedRevision = review.Revision
			paid, err := svc.RecordPayment(ctx, full)
			require.NoError(t, err)
			require.Equal(t, partnerearnings.ClaimPaid, paid.State)
			require.Len(t, paid.PaymentObservations, 1)
			balance, err = svc.Balances(ctx, "partner")
			require.NoError(t, err)
			require.Zero(t, balance.ReviewHoldMinor)
			require.EqualValues(t, 1000, balance.PaidOutMinor)
			require.EqualValues(t, 1000, balance.AvailableMinor)
		})
	}
}
func TestMongoReturnedTransferRestoresOldestBacking(t *testing.T) {
	// This single ordered returned-payout/reclaim/refund lifecycle demonstrates
	// restored provenance and debt across two original payments; a table would
	// duplicate the same stateful scenario without adding independent cases.
	svc, repo, _, _, clock, ctx := mongoEarnings(t)
	accrue(t, svc, ctx, clock, "oldest", 5000)
	accrue(t, svc, ctx, clock, "newer", 5000)
	claim, err := svc.RequestClaim(ctx, claimRequest(1500, "original-claim"))
	require.NoError(t, err)
	process(t, svc, ctx, claim)
	paid, err := svc.RecordPayment(ctx, paymentRequest(claim, clock, "record"))
	require.NoError(t, err)
	req := partnerearnings.ReturnRequest{ClaimID: claim.ID, ActorID: "operator", ExpectedRevision: paid.Revision, IdempotencyKey: "returned", AmountMinor: 700, Currency: "EUR", ReturnedAt: clock.Now(), Reference: "return-reference", Reason: "verified_return"}
	returned, err := svc.RecordReturnedTransfer(ctx, req)
	require.NoError(t, err)
	require.Equal(t, partnerearnings.ClaimPaid, returned.Claim.State)
	require.Equal(t, paid.Payment, returned.Claim.Payment)
	require.Len(t, returned.Claim.ReturnedAdjustments, 1)
	replay, err := svc.RecordReturnedTransfer(ctx, req)
	require.NoError(t, err)
	require.Equal(t, returned, replay)
	balance, err := svc.Balances(ctx, "partner")
	require.NoError(t, err)
	require.EqualValues(t, 1200, balance.AvailableMinor)
	require.EqualValues(t, 1500, balance.PaidOutMinor)
	second, err := svc.RequestClaim(ctx, claimRequest(1200, "reclaim"))
	require.NoError(t, err)
	entries, err := repo.ListEntries(ctx, "fixture-program", "partner")
	require.NoError(t, err)
	allocations := []partnerearnings.Entry{}
	for _, e := range entries {
		if e.Kind == partnerearnings.EntryAllocated && e.SourceEventID == second.ID {
			allocations = append(allocations, e)
		}
	}
	require.Len(t, allocations, 2)
	require.Equal(t, "oldest", allocations[0].SourceRef)
	require.EqualValues(t, 700, allocations[0].AmountMinor)
	require.Equal(t, "newer", allocations[1].SourceRef)
	require.EqualValues(t, 500, allocations[1].AmountMinor)
	over := req
	over.ExpectedRevision = returned.Claim.Revision
	over.IdempotencyKey = "over-cap"
	over.AmountMinor = 801
	_, err = svc.RecordReturnedTransfer(ctx, over)
	require.ErrorIs(t, err, partnerearnings.ErrInvalid)
	_, err = svc.Reverse(ctx, partnerearnings.ReversalRequest{PartnerID: "partner", PaymentID: "oldest", RefundID: "refund-after-return", CumulativeRefundedMinor: 5000, Currency: "EUR", OccurredAt: clock.Now()})
	require.NoError(t, err)
	fresh, err := svc.GetClaim(ctx, second.ID)
	require.NoError(t, err)
	require.Equal(t, partnerearnings.ClaimCancelled, fresh.State)
	balance, err = svc.Balances(ctx, "partner")
	require.NoError(t, err)
	require.EqualValues(t, 200, balance.AvailableMinor)
	require.Zero(t, balance.DebtMinor)
}
