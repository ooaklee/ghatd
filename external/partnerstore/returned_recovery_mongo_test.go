package partnerstore

import (
	"sync"
	"testing"

	"github.com/ooaklee/ghatd/external/partnerearnings"
	"github.com/stretchr/testify/require"
)

// Audit disposition: new named returned-transfer rollback/uncertain cases and
// one justified simultaneous CAS race, using isolated replica-set databases.
func TestMongoReturnedTransferAtomicRecovery(t *testing.T) {
	type testCase struct {
		name      string
		failAt    int
		uncertain bool
	}
	cases := []testCase{
		{name: "returned_journal_rollback", failAt: 1},
		{name: "returned_source_anchor_rollback", failAt: 2},
		{name: "backing_journal_rollback", failAt: 3},
		{name: "backing_source_anchor_rollback", failAt: 4},
		{name: "immutable_claim_revision_rollback", failAt: 5},
		{name: "receipt_rollback", failAt: 6},
		{name: "lost_commit_acknowledgement", uncertain: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, repo, store, _, clock, ctx := mongoEarnings(t)
			accrue(t, svc, ctx, clock, "payment", 10000)
			claim, err := svc.RequestClaim(ctx, claimRequest(1000, "claim"))
			require.NoError(t, err)
			process(t, svc, ctx, claim)
			paid, err := svc.RecordPayment(ctx, paymentRequest(claim, clock, "record"))
			require.NoError(t, err)
			before, err := repo.ListEntries(ctx, "fixture-program", "partner")
			require.NoError(t, err)
			faultRepo, err := NewEarningsRepository(injectedStore{Store: store, failAt: tc.failAt, uncertain: tc.uncertain}, partnerearnings.Config{ProgramID: "fixture-program", Currency: "EUR"})
			require.NoError(t, err)
			fault, err := partnerearnings.NewService(faultRepo, clock, randomIDs{}, partnerearnings.Config{ProgramID: "fixture-program", Currency: "EUR"})
			require.NoError(t, err)
			req := partnerearnings.ReturnRequest{ClaimID: claim.ID, ActorID: "operator", ExpectedRevision: paid.Revision, IdempotencyKey: "return", AmountMinor: 500, Currency: "EUR", ReturnedAt: clock.Now(), Reference: "returned-reference", Reason: "verified_return"}
			result, err := fault.RecordReturnedTransfer(ctx, req)
			if tc.uncertain {
				require.ErrorIs(t, err, partnerearnings.ErrUncertain)
			} else {
				require.ErrorIs(t, err, injectedFailure)
			}
			require.Empty(t, result.Claim.ID)
			require.Empty(t, result.Entries)
			if !tc.uncertain {
				after, err := repo.ListEntries(ctx, "fixture-program", "partner")
				require.NoError(t, err)
				require.Equal(t, before, after)
				fresh, err := svc.GetClaim(ctx, claim.ID)
				require.NoError(t, err)
				require.Equal(t, paid, fresh)
				balance, err := svc.Balances(ctx, "partner")
				require.NoError(t, err)
				require.EqualValues(t, 1000, balance.AvailableMinor)
			}
			recovered, err := svc.RecordReturnedTransfer(ctx, req)
			require.NoError(t, err)
			require.Len(t, recovered.Claim.ReturnedAdjustments, 1)
			require.Equal(t, paid.Payment, recovered.Claim.Payment)
			replay, err := svc.RecordReturnedTransfer(ctx, req)
			require.NoError(t, err)
			require.Equal(t, recovered, replay)
			entries, err := repo.ListEntries(ctx, "fixture-program", "partner")
			require.NoError(t, err)
			returns, paidDebits := 0, 0
			for _, e := range entries {
				if e.Kind == partnerearnings.EntryReturned {
					returns++
				}
				if e.Kind == partnerearnings.EntryPaid {
					paidDebits++
				}
			}
			require.Equal(t, 1, returns)
			require.Equal(t, 1, paidDebits)
			balance, err := svc.Balances(ctx, "partner")
			require.NoError(t, err)
			require.EqualValues(t, 1500, balance.AvailableMinor)
			require.EqualValues(t, 1000, balance.PaidOutMinor)
		})
	}
}
func TestMongoConcurrentReturnedTransfersRequireCurrentRevision(t *testing.T) {
	// Two different proven-return commands overlap against one original revision.
	// The transaction guard and claim CAS, rather than receipt identity, serialize
	// them. A stale competing command cannot exceed the original settlement cap.
	svc, repo, _, _, clock, ctx := mongoEarnings(t)
	accrue(t, svc, ctx, clock, "payment", 10000)
	claim, err := svc.RequestClaim(ctx, claimRequest(1000, "claim"))
	require.NoError(t, err)
	process(t, svc, ctx, claim)
	paid, err := svc.RecordPayment(ctx, paymentRequest(claim, clock, "record"))
	require.NoError(t, err)
	requests := []partnerearnings.ReturnRequest{
		{ClaimID: claim.ID, ActorID: "operator", ExpectedRevision: paid.Revision, IdempotencyKey: "one", AmountMinor: 400, Currency: "EUR", ReturnedAt: clock.Now(), Reference: "return-one", Reason: "verified_return"},
		{ClaimID: claim.ID, ActorID: "operator", ExpectedRevision: paid.Revision, IdempotencyKey: "two", AmountMinor: 700, Currency: "EUR", ReturnedAt: clock.Now(), Reference: "return-two", Reason: "verified_return"},
	}
	start := make(chan struct{})
	results := make([]error, 2)
	var wg sync.WaitGroup
	for i, request := range requests {
		wg.Add(1)
		go func(i int, request partnerearnings.ReturnRequest) {
			defer wg.Done()
			<-start
			_, results[i] = svc.RecordReturnedTransfer(ctx, request)
		}(i, request)
	}
	close(start)
	wg.Wait()
	winner := -1
	for i, err := range results {
		if err == nil {
			require.Equal(t, -1, winner)
			winner = i
		} else {
			require.ErrorIs(t, err, partnerearnings.ErrStaleWrite)
		}
	}
	require.NotEqual(t, -1, winner)
	fresh, err := svc.GetClaim(ctx, claim.ID)
	require.NoError(t, err)
	require.Len(t, fresh.ReturnedAdjustments, 1)
	require.Equal(t, paid.Payment, fresh.Payment)
	loser := requests[1-winner]
	loser.ExpectedRevision = fresh.Revision
	_, err = svc.RecordReturnedTransfer(ctx, loser)
	require.ErrorIs(t, err, partnerearnings.ErrInvalid)
	entries, err := repo.ListEntries(ctx, "fixture-program", "partner")
	require.NoError(t, err)
	returns := 0
	for _, e := range entries {
		if e.Kind == partnerearnings.EntryReturned {
			returns++
		}
	}
	require.Equal(t, 1, returns)
	balance, err := svc.Balances(ctx, "partner")
	require.NoError(t, err)
	require.EqualValues(t, 1000+requests[winner].AmountMinor, balance.AvailableMinor)
}
