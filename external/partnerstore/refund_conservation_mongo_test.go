package partnerstore

import (
	"errors"
	"fmt"
	"testing"

	"github.com/ooaklee/ghatd/external/partnerearnings"
	"github.com/stretchr/testify/require"
)

// Test audit disposition: appropriate named tables with a fresh encrypted
// database per case. These native financial races and rounding transitions
// complement service-fake coverage; they are not provider/browser evidence.
func TestMongoClaimRefundConservation(t *testing.T) {
	for _, tc := range []struct {
		name       string
		processing bool
		ordering   string
	}{
		{name: "claim_request_races_original_full_refund", ordering: "overlap"},
		{name: "processing_assignment_races_original_full_refund", processing: true, ordering: "overlap"},
		{name: "claim_request_before_original_full_refund", ordering: "claim-first"},
		{name: "original_full_refund_before_claim_request", ordering: "refund-first"},
		{name: "processing_assignment_before_original_full_refund", processing: true, ordering: "claim-first"},
		{name: "original_full_refund_before_processing_assignment", processing: true, ordering: "refund-first"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, repo, _, _, clock, ctx := mongoEarnings(t)
			accrue(t, svc, ctx, clock, "original-payment", 10000)
			var original partnerearnings.Claim
			var err error
			if tc.processing {
				original, err = svc.RequestClaim(ctx, claimRequest(1500, "original-claim"))
				require.NoError(t, err)
			}
			refund := partnerearnings.ReversalRequest{
				PartnerID: "partner", PaymentID: "original-payment", RefundID: "original-refund",
				CumulativeRefundedMinor: 10000, Currency: "EUR", OccurredAt: clock.Now(),
			}
			claimOperation := func() error {
				if tc.processing {
					_, err := svc.DecideClaim(ctx, partnerearnings.ClaimDecision{
						ClaimID: original.ID, NewState: partnerearnings.ClaimProcessing,
						ActorID: "operator", ExpectedRevision: original.Revision,
					})
					return err
				} else {
					_, err := svc.RequestClaim(ctx, claimRequest(1500, "original-claim"))
					return err
				}
			}
			refundOperation := func() error {
				_, err := svc.Reverse(ctx, refund)
				return err
			}
			var claimErr, refundErr error
			switch tc.ordering {
			case "claim-first":
				claimErr = claimOperation()
				refundErr = refundOperation()
			case "refund-first":
				refundErr = refundOperation()
				claimErr = claimOperation()
			case "overlap":
				start := make(chan struct{})
				claimOutcome := make(chan error, 1)
				refundOutcome := make(chan error, 1)
				go func() { <-start; claimOutcome <- claimOperation() }()
				go func() { <-start; refundOutcome <- refundOperation() }()
				close(start)
				claimErr, refundErr = <-claimOutcome, <-refundOutcome
			default:
				t.Fatalf("unsupported fixture ordering %q", tc.ordering)
			}
			require.NoError(t, refundErr)
			if tc.ordering == "claim-first" {
				require.NoError(t, claimErr, "deterministic coverage of a committed claim operation")
			}
			if tc.ordering == "refund-first" {
				require.Error(t, claimErr, "the refund must deny a new reservation or stale assignment")
			}
			claims, err := repo.ListClaims(ctx, svc.ProgramID(), "partner", nil, 0, "")
			require.NoError(t, err)
			balance, err := svc.Balances(ctx, "partner")
			require.NoError(t, err)
			require.Zero(t, balance.AvailableMinor)
			require.Zero(t, balance.MatchedMinor)
			// An unpaid in-flight reservation is an obligation for review, not
			// recorded payout debt. Debt requires a negative settled balance.
			require.Zero(t, balance.DebtMinor)
			if tc.processing {
				require.Len(t, claims, 1)
				if claimErr == nil {
					require.Equal(t, partnerearnings.ClaimProcessing, claims[0].State)
					require.Equal(t, "operator", claims[0].ProcessingActor)
					require.Equal(t, "reversed", claims[0].ReviewReason)
					require.EqualValues(t, 1500, balance.ReservedMinor)
				} else {
					require.ErrorIs(t, claimErr, partnerearnings.ErrStaleWrite)
					require.Equal(t, partnerearnings.ClaimCancelled, claims[0].State)
					require.Equal(t, "reversed", claims[0].Reason)
					require.Zero(t, balance.ReservedMinor)
				}
			} else {
				if claimErr != nil {
					require.True(t, errors.Is(claimErr, partnerearnings.ErrInsufficient), "unexpected claim outcome: %v", claimErr)
					require.Empty(t, claims)
				} else {
					require.Len(t, claims, 1)
					require.Equal(t, partnerearnings.ClaimCancelled, claims[0].State)
					require.Equal(t, "reversed", claims[0].Reason)
				}
				require.Zero(t, balance.ReservedMinor)
			}
			journal, err := svc.ListJournal(ctx, "partner")
			require.NoError(t, err)
			var reversals, releases int
			for _, entry := range journal {
				if entry.Kind == partnerearnings.EntryReversed {
					reversals++
					require.EqualValues(t, -2000, entry.AmountMinor)
				}
				if entry.Kind == partnerearnings.EntryAllocationReleased {
					releases++
					require.Equal(t, "original-payment", entry.SourceRef)
					require.EqualValues(t, -1500, entry.AmountMinor)
				}
			}
			require.Equal(t, 1, reversals)
			if len(claims) == 1 && claims[0].State == partnerearnings.ClaimCancelled {
				require.Equal(t, 1, releases, "cancelled original has exactly one backing release")
			} else {
				require.Zero(t, releases, "no release without a cancelled backing reservation")
			}
			_, err = svc.Reverse(ctx, refund)
			require.NoError(t, err)
			replayed, err := svc.ListJournal(ctx, "partner")
			require.NoError(t, err)
			require.Equal(t, journal, replayed, "original refund replay cannot debit or release again")
			current, err := svc.Balances(ctx, "partner")
			require.NoError(t, err)
			require.Equal(t, balance, current)
			currentClaims, err := repo.ListClaims(ctx, svc.ProgramID(), "partner", nil, 0, "")
			require.NoError(t, err)
			require.Equal(t, claims, currentClaims)
		})
	}
}

func TestMongoCumulativeRefundRoundingConservation(t *testing.T) {
	for _, tc := range []struct {
		name           string
		payment        int64
		rate           int
		commission     int64
		cumulative     []int64
		expectedDebits []int64
	}{
		{name: "split_half_unit_refunds_cannot_exceed_original_commission", payment: 3, rate: 5000, commission: 2, cumulative: []int64{1, 2, 3}, expectedDebits: []int64{1, 0, 1}},
		{name: "zero_rounded_steps_retain_receipts_without_extra_debit", payment: 1000000, rate: 100, commission: 10000, cumulative: []int64{1, 2, 50, 1000000}, expectedDebits: []int64{0, 0, 1, 9999}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, _, _, _, clock, ctx := mongoEarnings(t)
			original, err := svc.Accrue(ctx, partnerearnings.AccrualRequest{
				PartnerID: "partner", PaymentID: "original-payment", PaymentMinor: tc.payment,
				RateBasisPoints: tc.rate, Currency: "EUR", OccurredAt: clock.Now(),
				ReferralID: "referral", TermsVersion: "fixture-terms", PolicyID: "fixture-policy",
			})
			require.NoError(t, err)
			require.Equal(t, tc.commission, original.CommissionMinor)
			var totalDebited int64
			for i, cumulative := range tc.cumulative {
				request := partnerearnings.ReversalRequest{
					PartnerID: "partner", PaymentID: "original-payment", RefundID: fmt.Sprintf("refund-%d", i),
					CumulativeRefundedMinor: cumulative, Currency: "EUR", OccurredAt: clock.Now(),
				}
				reversal, err := svc.Reverse(ctx, request)
				require.NoError(t, err)
				require.Len(t, reversal, 1)
				require.Equal(t, -tc.expectedDebits[i], reversal[0].AmountMinor)
				totalDebited -= reversal[0].AmountMinor
				require.LessOrEqual(t, totalDebited, tc.commission)
				replay, err := svc.Reverse(ctx, request)
				require.NoError(t, err)
				require.Equal(t, reversal, replay)
			}
			require.Equal(t, tc.commission, totalDebited)
			before, err := svc.ListJournal(ctx, "partner")
			require.NoError(t, err)
			var count int
			for _, entry := range before {
				if entry.Kind == partnerearnings.EntryReversed {
					count++
				}
			}
			require.Equal(t, len(tc.cumulative), count, "replays retain one anchor for every original step, including zero debits")
			for _, rejected := range []struct {
				name, refundID string
				cumulative     int64
				want           error
			}{
				{name: "above-original-payment", refundID: "above-original", cumulative: tc.payment + 1, want: partnerearnings.ErrInvalid},
				{name: "changed-original-refund-payload", refundID: "refund-0", cumulative: tc.cumulative[0] + 1, want: partnerearnings.ErrConflict},
			} {
				_, err = svc.Reverse(ctx, partnerearnings.ReversalRequest{
					PartnerID: "partner", PaymentID: "original-payment", RefundID: rejected.refundID,
					CumulativeRefundedMinor: rejected.cumulative, Currency: "EUR", OccurredAt: clock.Now(),
				})
				require.ErrorIs(t, err, rejected.want, rejected.name)
			}
			after, err := svc.ListJournal(ctx, "partner")
			require.NoError(t, err)
			require.Equal(t, before, after)
			balance, err := svc.Balances(ctx, "partner")
			require.NoError(t, err)
			require.Zero(t, balance.AvailableMinor)
			require.Zero(t, balance.MatchedMinor)
			require.Zero(t, balance.DebtMinor)
		})
	}
}
