package partnerstore

import (
	"fmt"
	"testing"

	"github.com/ooaklee/ghatd/external/partnerearnings"
	"github.com/stretchr/testify/require"
)

// Audit disposition: named real-Mongo snapshot, paging and payment-presentation
// cases verify bounded reads without changing historical running balances.
func TestMongoStatementSnapshotAndPaymentPresentation(t *testing.T) {
	cases := []struct {
		name            string
		amended, paging bool
	}{{name: "original_recorded_method_reference_and_amount"}, {name: "amended_details_preserve_one_original_debit", amended: true}, {name: "pages_keep_full_balance_and_stable_sequences", paging: true}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, _, _, _, clock, ctx := mongoEarnings(t)
			accrue(t, s, ctx, clock, "payment", 10000)
			claim, err := s.RequestClaim(ctx, claimRequest(1500, "claim"))
			require.NoError(t, err)
			process(t, s, ctx, claim)
			paid, err := s.RecordPayment(ctx, paymentRequest(claim, clock, "record"))
			require.NoError(t, err)
			if tc.amended {
				_, err = s.AmendPayment(ctx, partnerearnings.AmendPaymentRequest{ClaimID: paid.ID, ActorID: "adjustment-operator", Method: "paypal", Reference: "corrected-customer-reference", PaidAt: paid.Payment.PaidAt, Reason: "recording correction", IdempotencyKey: "amend", ExpectedRevision: paid.Revision})
				require.NoError(t, err)
			}
			query := partnerearnings.StatementQuery{Limit: 100, Kinds: []string{partnerearnings.EntryPaid}}
			if tc.paging {
				query = partnerearnings.StatementQuery{Limit: 2}
			}
			out, err := s.GetStatement(ctx, "partner", query)
			require.NoError(t, err)
			require.EqualValues(t, 500, out.Balances.AvailableMinor)
			require.EqualValues(t, 1500, out.Balances.PaidOutMinor)
			if tc.paging {
				require.True(t, out.HasMore)
				var sequences []int64
				for {
					for _, line := range out.Lines {
						sequences = append(sequences, line.Entry.Sequence)
					}
					if !out.HasMore {
						break
					}
					out, err = s.GetStatement(ctx, "partner", partnerearnings.StatementQuery{Limit: 2, BeforeSequence: out.NextBeforeSequence})
					require.NoError(t, err)
					require.EqualValues(t, 500, out.Balances.AvailableMinor)
				}
				for i := 1; i < len(sequences); i++ {
					require.Equal(t, sequences[i-1]-1, sequences[i])
				}
				require.EqualValues(t, 1, sequences[len(sequences)-1])
				return
			}
			require.Len(t, out.Lines, 1)
			line := out.Lines[0]
			require.EqualValues(t, 500, line.RunningMaturedMinor)
			require.NotNil(t, line.Payment)
			require.EqualValues(t, 1500, line.Payment.AmountMinor)
			require.Equal(t, "EUR", line.Payment.Currency)
			require.Equal(t, paid.Payment.RecordedBy, line.Payment.RecordedBy)
			if tc.amended {
				require.Equal(t, "corrected-customer-reference", line.Payment.Reference)
				require.EqualValues(t, 2, line.PaymentVersion)
			} else {
				require.Equal(t, paid.Payment.Reference, line.Payment.Reference)
				require.EqualValues(t, 1, line.PaymentVersion)
			}
		})
	}
}

func TestMongoStatementBeyondTwoHundredJournalRows(t *testing.T) {
	// One complete history traversal is the behavior under test. Every page
	// retains the full ledger balance, and running values include omitted rows.
	s, _, _, _, clock, ctx := mongoEarnings(t)
	for i := 0; i < 105; i++ {
		accrue(t, s, ctx, clock, fmt.Sprintf("payment_%03d", i), 100)
	}
	out, err := s.GetStatement(ctx, "partner", partnerearnings.StatementQuery{Limit: 100})
	require.NoError(t, err)
	require.EqualValues(t, 2100, out.Balances.AvailableMinor)
	require.EqualValues(t, 210, out.LedgerSequence)
	var count int
	var previous int64 = 211
	for {
		for _, line := range out.Lines {
			require.Equal(t, previous-1, line.Entry.Sequence)
			previous = line.Entry.Sequence
			count++
		}
		if !out.HasMore {
			break
		}
		out, err = s.GetStatement(ctx, "partner", partnerearnings.StatementQuery{Limit: 100, BeforeSequence: out.NextBeforeSequence})
		require.NoError(t, err)
		require.EqualValues(t, 2100, out.Balances.AvailableMinor)
	}
	require.Equal(t, 210, count)
	require.EqualValues(t, 1, previous)
}
