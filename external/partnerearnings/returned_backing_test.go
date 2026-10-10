package partnerearnings

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// Audit disposition: related multiple-return lifecycle and corrupted typed
// evidence cases are named. Notes are edited only to prove they are display
// text, while amounts and joins continue to use owning durable journal fields.
func TestReturnedBackingUsesOperationEvidence(t *testing.T) {
	cases := []struct {
		name, mode string
		want       error
	}{
		{name: "edited_note_does_not_reuse_first_payment_backing", mode: "note"},
		{name: "missing_return_release_rejects_new_return", mode: "missing", want: ErrConflict},
		{name: "release_sum_differs_from_return", mode: "amount", want: ErrConflict},
		{name: "release_uses_unknown_payment", mode: "payment", want: ErrConflict},
		{name: "release_currency_mismatch", mode: "currency", want: ErrConflict},
		{name: "release_precedes_operation", mode: "order", want: ErrConflict},
		{name: "duplicate_return_operation", mode: "duplicate", want: ErrConflict},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, repo, clock := newTestService(t)
			ctx := context.Background()
			for _, payment := range []string{"payment-one", "payment-two"} {
				_, err := s.Accrue(ctx, accrualReq("partner", payment, 5000, 2000))
				require.NoError(t, err)
			}
			claim, err := s.RequestClaim(ctx, ClaimRequest{PartnerID: "partner", ActorID: "customer", AmountMinor: 1500, Currency: testCurrency, DestinationID: "destination", IdempotencyKey: "claim"})
			require.NoError(t, err)
			claim, err = s.DecideClaim(ctx, ClaimDecision{ClaimID: claim.ID, ActorID: "operator", ExpectedRevision: claim.Revision, NewState: ClaimProcessing})
			require.NoError(t, err)
			claim, err = s.RecordPayment(ctx, RecordPaymentRequest{ClaimID: claim.ID, ActorID: "operator", ExpectedRevision: claim.Revision, AmountMinor: claim.AmountMinor, Currency: testCurrency, State: PaymentStateFull, Method: "bank", Reference: "original-transfer", PaidAt: clock.Now(), IdempotencyKey: "record"})
			require.NoError(t, err)
			first := ReturnRequest{ClaimID: claim.ID, ActorID: "operator", ExpectedRevision: claim.Revision, AmountMinor: 700, Currency: testCurrency, ReturnedAt: clock.Now(), Reference: "return-one", Reason: "verified_return", IdempotencyKey: "return-one"}
			result, err := s.RecordReturnedTransfer(ctx, first)
			require.NoError(t, err)
			operation := result.Entries[0]
			for i, row := range repo.entries {
				if row.Kind != EntryAllocationReleased || row.SourceEventID != operation.SourceEventID {
					continue
				}
				switch tc.mode {
				case "note":
					repo.entries[i].Note = "edited presentation only"
				case "missing":
					repo.entries = append(repo.entries[:i], repo.entries[i+1:]...)
				case "amount":
					repo.entries[i].AmountMinor = -600
				case "payment":
					repo.entries[i].SourceRef = "unknown-payment"
				case "currency":
					repo.entries[i].Currency = "USD"
				case "order":
					repo.entries[i].Sequence = operation.Sequence
				case "duplicate":
					repo.entries = append(repo.entries, operation)
				}
				break
			}
			before := append([]Entry(nil), repo.entries...)
			second := first
			second.ExpectedRevision, second.IdempotencyKey, second.Reference = result.Claim.Revision, "return-two", "return-two"
			out, err := s.RecordReturnedTransfer(ctx, second)
			require.ErrorIs(t, err, tc.want)
			if tc.want != nil {
				require.Empty(t, out)
				require.Equal(t, before, repo.entries, "invalid prior evidence cannot append another return")
				current, err := s.GetClaim(ctx, claim.ID)
				require.NoError(t, err)
				require.Equal(t, result.Claim, current)
				return
			}
			require.Len(t, out.Claim.ReturnedAdjustments, 2)
			restored, err := returnedAllocationBacking(repo.entries, claim.ID)
			require.NoError(t, err)
			require.Equal(t, map[string]int64{"payment-one": 1000, "payment-two": 400}, restored)
			paid, err := s.GetClaim(ctx, claim.ID)
			require.NoError(t, err)
			_, err = s.DecideClaim(ctx, ClaimDecision{ClaimID: paid.ID, ActorID: "operator", ExpectedRevision: paid.Revision, NewState: ClaimCancelled, Reason: "no cancellation after settlement", ConfirmedUnsent: true})
			require.ErrorIs(t, err, ErrInvalidState)
			_, err = s.Reverse(ctx, ReversalRequest{PartnerID: "partner", PaymentID: "payment-one", RefundID: "refund-after-return", CumulativeRefundedMinor: 5000, Currency: testCurrency, OccurredAt: clock.Now()})
			require.NoError(t, err)
			restored, err = returnedAllocationBacking(repo.entries, claim.ID)
			require.NoError(t, err)
			require.Equal(t, map[string]int64{"payment-one": 1000, "payment-two": 400}, restored, "refund of paid claim cannot release its allocations again")
			beforeReplay := append([]Entry(nil), repo.entries...)
			replay, err := s.RecordReturnedTransfer(ctx, second)
			require.NoError(t, err)
			require.Equal(t, out, replay)
			require.Equal(t, beforeReplay, repo.entries)
		})
	}
}
