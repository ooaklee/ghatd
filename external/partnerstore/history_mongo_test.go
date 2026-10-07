package partnerstore

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/partnerearnings"
	"github.com/ooaklee/ghatd/external/repository/recordstore"
	"github.com/stretchr/testify/require"
)

func TestMongoCompleteHistoryBalancesAndReservation(t *testing.T) {
	// This one stateful history fixture crosses the old 1000-entry truncation
	// boundary, then reserves funds beyond that boundary using oldest credits.
	svc, repo, store, _, clock, fixtureCtx := mongoEarnings(t)
	ctx, cancel := context.WithTimeout(context.WithoutCancel(fixtureCtx), 60*time.Second)
	defer cancel()
	const credits = 600
	partition := ledgerPartition("fixture-program", "partner", "EUR")
	err := store.Transact(ctx, partition, func(tx recordstore.Tx) error {
		for i := 0; i < credits; i++ {
			payment := fmt.Sprintf("historical-payment-%04d", i)
			for j, kind := range []string{partnerearnings.EntryAccrued, partnerearnings.EntryMatured} {
				e := partnerearnings.Entry{ID: fmt.Sprintf("historical-entry-%04d-%d", i, j), ProgramID: "fixture-program", PartnerID: "partner", Currency: "EUR", Sequence: int64(2*i + j + 1), Kind: kind, SourceEventID: payment, AmountMinor: 10, PaymentMinor: 50, RateBasisPoints: 2000, CreatedAt: clock.Now().Add(-time.Hour), OccurredAt: clock.Now().Add(-time.Hour)}
				row, err := recordstore.NewRecord(kindEntry, e.ID, partition, 1, e)
				if err != nil {
					return err
				}
				row.Sequence = e.Sequence
				if err := tx.Insert(ctx, row); err != nil {
					return err
				}
			}
		}
		return insertRecord(ctx, tx, kindLedgerHead, identity("fixture-program", "partner", "EUR"), partition, 1, ledgerHead{Sequence: 2 * credits})
	})
	require.NoError(t, err)
	entries, err := repo.ListEntries(ctx, "fixture-program", "partner")
	require.NoError(t, err)
	require.Len(t, entries, 2*credits)
	balance, err := svc.Balances(ctx, "partner")
	require.NoError(t, err)
	require.EqualValues(t, 6000, balance.AvailableMinor)
	claim, err := svc.RequestClaim(ctx, claimRequest(5500, "beyond-old-history-cap"))
	require.NoError(t, err)
	require.EqualValues(t, 5500, claim.AmountMinor)
	balance, err = svc.Balances(ctx, "partner")
	require.NoError(t, err)
	require.EqualValues(t, 500, balance.AvailableMinor)
	require.EqualValues(t, 5500, balance.ReservedMinor)
	entries, err = repo.ListEntries(ctx, "fixture-program", "partner")
	require.NoError(t, err)
	allocations := 0
	var first, last string
	for _, e := range entries {
		if e.Kind == partnerearnings.EntryAllocated && e.SourceEventID == claim.ID {
			allocations++
			if first == "" {
				first = e.SourceRef
			}
			last = e.SourceRef
		}
	}
	require.Equal(t, 550, allocations)
	require.Equal(t, "historical-payment-0000", first)
	require.Equal(t, "historical-payment-0549", last)
}
