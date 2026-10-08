package partnerstore

import (
	"context"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/partnerearnings"
	"github.com/ooaklee/ghatd/external/repository/recordstore"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
)

func nativeCancellation(t *testing.T, store recordstore.Store, clock *mongoTestClock) *partnerearnings.Service {
	t.Helper()
	r, err := NewEarningsRepository(store, partnerearnings.Config{ProgramID: "fixture-program", Currency: "EUR"})
	require.NoError(t, err)
	s, err := partnerearnings.NewService(r, clock, randomIDs{}, partnerearnings.Config{ProgramID: "fixture-program", Currency: "EUR"})
	require.NoError(t, err)
	return s
}
func cancellationRequest(c partnerearnings.Claim, key string) partnerearnings.CancelClaimRequest {
	return partnerearnings.CancelClaimRequest{PartnerID: c.PartnerID, ActorID: "owner", ClaimID: c.ID, ExpectedRevision: c.Revision, Reason: "customer changed plans", IdempotencyKey: key}
}

func TestMongoCustomerCancellationOriginalKeyRecovery(t *testing.T) {
	for _, tc := range []struct {
		name string
		want error
	}{
		{"identical_replay", nil}, {"uncertain_commit", nil}, {"later_refund", nil},
		{"changed_reason", partnerearnings.ErrConflict}, {"changed_revision", partnerearnings.ErrConflict},
		{"different_claim", partnerearnings.ErrConflict}, {"new_key_stale_revision", partnerearnings.ErrStaleWrite},
		{"new_key_terminal_state", partnerearnings.ErrInvalidState},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, repo, store, _, clock, ctx := mongoEarnings(t)
			accrue(t, s, ctx, clock, "payment", 10000)
			claim, err := s.RequestClaim(ctx, claimRequest(1500, "claim"))
			require.NoError(t, err)
			req := cancellationRequest(claim, "original-cancel-key")
			first := s
			if tc.name == "uncertain_commit" {
				first = nativeCancellation(t, injectedStore{Store: store, uncertain: true}, clock)
			}
			cancelled, err := first.CancelRequestedClaim(ctx, req)
			if tc.name == "uncertain_commit" {
				require.ErrorIs(t, err, partnerearnings.ErrUncertain)
				require.Empty(t, cancelled.ID)
				cancelled, err = repo.GetClaim(ctx, "fixture-program", claim.ID)
			}
			require.NoError(t, err)
			require.Equal(t, partnerearnings.ClaimCancelled, cancelled.State)
			require.EqualValues(t, claim.Revision+1, cancelled.Revision)
			switch tc.name {
			case "later_refund":
				clock.now = clock.now.Add(time.Hour)
				_, err = s.Reverse(ctx, partnerearnings.ReversalRequest{PartnerID: claim.PartnerID, PaymentID: "payment", RefundID: "refund", CumulativeRefundedMinor: 5000, Currency: "EUR", OccurredAt: clock.Now()})
				require.NoError(t, err)
			case "changed_reason":
				req.Reason = "changed intent"
			case "changed_revision":
				req.ExpectedRevision++
			case "different_claim":
				req.ClaimID = "different-claim"
			case "new_key_stale_revision":
				req.IdempotencyKey = "new-key"
			case "new_key_terminal_state":
				req.IdempotencyKey = "new-key"
				req.ExpectedRevision = cancelled.Revision
			}
			got, err := s.CancelRequestedClaim(ctx, req)
			if tc.want == nil {
				require.NoError(t, err)
				require.Equal(t, cancelled, got)
			} else {
				require.ErrorIs(t, err, tc.want)
				require.Empty(t, got.ID)
			}
			lines, err := s.ListJournal(ctx, claim.PartnerID)
			require.NoError(t, err)
			released := 0
			for _, e := range lines {
				if e.Kind == partnerearnings.EntryAllocationReleased {
					released++
					require.EqualValues(t, -1500, e.AmountMinor)
				}
			}
			require.Equal(t, 1, released)
			b, err := s.Balances(ctx, claim.PartnerID)
			require.NoError(t, err)
			require.Zero(t, b.ReservedMinor)
			require.Zero(t, b.PaidOutMinor)
			wantAvailable := int64(2000)
			if tc.name == "later_refund" {
				wantAvailable = 1000
			}
			require.Equal(t, wantAvailable, b.AvailableMinor)
		})
	}
}

func TestMongoCustomerCancellationAllWritesRollBack(t *testing.T) {
	for _, tc := range []struct {
		name   string
		failAt int
	}{
		{"sequence_head", 1}, {"release_journal", 2}, {"release_source_anchor", 3},
		{"claim_head", 4}, {"claim_audit", 5}, {"cancel_receipt", 6},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, repo, store, _, clock, ctx := mongoEarnings(t)
			accrue(t, s, ctx, clock, "payment", 10000)
			claim, err := s.RequestClaim(ctx, claimRequest(1500, "claim"))
			require.NoError(t, err)
			before, err := s.ListJournal(ctx, claim.PartnerID)
			require.NoError(t, err)
			broken := nativeCancellation(t, rotationFailureStore{Store: store, failAt: tc.failAt}, clock)
			req := cancellationRequest(claim, "original-cancel-key")
			_, err = broken.CancelRequestedClaim(ctx, req)
			require.ErrorIs(t, err, injectedFailure)
			fresh, err := repo.GetClaim(ctx, "fixture-program", claim.ID)
			require.NoError(t, err)
			require.Equal(t, claim, fresh)
			after, err := s.ListJournal(ctx, claim.PartnerID)
			require.NoError(t, err)
			require.Equal(t, before, after)
			_, err = repo.GetReceipt(ctx, partnerearnings.ReceiptKey{ProgramID: "fixture-program", PartnerID: claim.PartnerID, ActorID: req.ActorID, UseCase: partnerearnings.UseCaseCancel, Currency: "EUR", Key: req.IdempotencyKey})
			require.ErrorIs(t, err, partnerearnings.ErrNotFound)
			b, err := s.Balances(ctx, claim.PartnerID)
			require.NoError(t, err)
			require.EqualValues(t, 1500, b.ReservedMinor)
			_, err = s.CancelRequestedClaim(ctx, req)
			require.NoError(t, err)
		})
	}
}

func TestMongoCustomerCancellationCannotReleaseInFlightClaims(t *testing.T) {
	for _, tc := range []struct{ state string }{{partnerearnings.ClaimProcessing}, {partnerearnings.ClaimNeedsReview}} {
		t.Run(tc.state, func(t *testing.T) {
			s, _, _, _, clock, ctx := mongoEarnings(t)
			accrue(t, s, ctx, clock, "payment", 10000)
			claim, err := s.RequestClaim(ctx, claimRequest(1500, "claim"))
			require.NoError(t, err)
			claim, err = s.DecideClaim(ctx, partnerearnings.ClaimDecision{ClaimID: claim.ID, NewState: tc.state, ActorID: "operator", Reason: "operator reviewed transfer", ExpectedRevision: claim.Revision})
			require.NoError(t, err)
			_, err = s.CancelRequestedClaim(ctx, cancellationRequest(claim, "cancel-key"))
			require.ErrorIs(t, err, partnerearnings.ErrInvalidState)
			b, err := s.Balances(ctx, claim.PartnerID)
			require.NoError(t, err)
			require.EqualValues(t, 1500, b.ReservedMinor+b.ReviewHoldMinor)
		})
	}
}

func TestMongoCustomerCancellationConcurrentRequests(t *testing.T) {
	for _, tc := range []struct {
		name, mode string
	}{{"same_key_once", "same"}, {"competing_keys_once", "different"}, {"cancel_versus_processing", "processing"}} {
		t.Run(tc.name, func(t *testing.T) {
			s, _, _, _, clock, ctx := mongoEarnings(t)
			accrue(t, s, ctx, clock, "payment", 10000)
			claim, err := s.RequestClaim(ctx, claimRequest(1500, "claim"))
			require.NoError(t, err)
			type result struct {
				claim partnerearnings.Claim
				err   error
			}
			start, out := make(chan struct{}), make(chan result, 2)
			go func() {
				<-start
				c, err := s.CancelRequestedClaim(ctx, cancellationRequest(claim, "original-key"))
				out <- result{c, err}
			}()
			go func() {
				<-start
				var c partnerearnings.Claim
				var err error
				if tc.mode == "processing" {
					c, err = s.DecideClaim(ctx, partnerearnings.ClaimDecision{ClaimID: claim.ID, NewState: partnerearnings.ClaimProcessing, ActorID: "operator", Reason: "reviewed transfer", ExpectedRevision: claim.Revision})
				} else {
					key := "original-key"
					if tc.mode == "different" {
						key = "other-key"
					}
					c, err = s.CancelRequestedClaim(ctx, cancellationRequest(claim, key))
				}
				out <- result{c, err}
			}()
			close(start)
			winner, successes := "", 0
			for i := 0; i < 2; i++ {
				r := <-out
				if r.err == nil {
					successes++
					winner = r.claim.State
				} else {
					require.ErrorIs(t, r.err, partnerearnings.ErrStaleWrite)
				}
			}
			if tc.mode == "same" {
				require.Equal(t, 2, successes)
			} else {
				require.Equal(t, 1, successes)
			}
			b, err := s.Balances(ctx, claim.PartnerID)
			var wantReserved int64
			if winner == partnerearnings.ClaimProcessing {
				wantReserved = 1500
			}
			require.NoError(t, err)
			require.Equal(t, wantReserved, b.ReservedMinor)
			require.Zero(t, b.PaidOutMinor)
		})
	}
}

// Assert the shared fixture remains a narrow storage fault injector; all
// cancellation transitions and reservation algorithms run in the real owner.
var _ recordstore.Store = rotationFailureStore{}

// Alter only selected storage output after a real native read. The owning
// service and transaction are unchanged; these are adapter-integrity faults.
type cancellationReadFaultStore struct {
	recordstore.Store
	fault string
}
type cancellationReadFaultTx struct {
	recordstore.Tx
	fault string
}

func (s cancellationReadFaultStore) Transact(ctx context.Context, key string, fn func(recordstore.Tx) error) error {
	return s.Store.Transact(ctx, key, func(tx recordstore.Tx) error { return fn(cancellationReadFaultTx{Tx: tx, fault: s.fault}) })
}
func (tx cancellationReadFaultTx) Get(ctx context.Context, kind, id string) (recordstore.Record, error) {
	row, err := tx.Tx.Get(ctx, kind, id)
	if err != nil || kind != kindReceipt {
		return row, err
	}
	switch tx.fault {
	case "wrong_kind":
		row.Kind = "wrong-kind"
	case "wrong_id":
		row.ID = "wrong-id"
	case "wrong_partition":
		row.Partition = "wrong-partition"
	case "receipt_revision":
		row.Revision++
	case "receipt_sequence":
		row.Sequence = 1
	case "receipt_state":
		row.State = "done"
	case "receipt_expiry":
		at := time.Now().Add(time.Hour)
		row.ExpiresAt = &at
	}
	return row, nil
}

func TestMongoCustomerCancellationRejectsBrokenReceiptEvidence(t *testing.T) {
	for _, tc := range []struct{ name string }{
		{"wrong_kind"}, {"wrong_id"}, {"wrong_partition"}, {"receipt_revision"},
		{"receipt_sequence"}, {"receipt_state"}, {"receipt_expiry"}, {"linked_claim_missing"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _, store, db, clock, ctx := mongoEarnings(t)
			accrue(t, s, ctx, clock, "payment", 10000)
			claim, err := s.RequestClaim(ctx, claimRequest(1500, "claim"))
			require.NoError(t, err)
			req := cancellationRequest(claim, "original-key")
			_, err = s.CancelRequestedClaim(ctx, req)
			require.NoError(t, err)
			before, err := s.ListJournal(ctx, claim.PartnerID)
			require.NoError(t, err)
			if tc.name == "linked_claim_missing" {
				_, err = db.Collection("ghatd_owned_records").DeleteOne(ctx, bson.M{"kind": kindClaim, "id": claim.ID})
				require.NoError(t, err)
			}
			broken := nativeCancellation(t, cancellationReadFaultStore{Store: store, fault: tc.name}, clock)
			out, err := broken.CancelRequestedClaim(ctx, req)
			require.ErrorIs(t, err, partnerearnings.ErrUnavailable)
			require.Empty(t, out.ID)
			after, err := s.ListJournal(ctx, claim.PartnerID)
			require.NoError(t, err)
			require.Equal(t, before, after)
		})
	}
}

func TestMongoCustomerCancellationRejectsInvalidClockWithoutRelease(t *testing.T) {
	for _, tc := range []struct{ name string }{{"zero_clock"}, {"clock_before_request"}} {
		t.Run(tc.name, func(t *testing.T) {
			s, repo, _, _, clock, ctx := mongoEarnings(t)
			accrue(t, s, ctx, clock, "payment", 10000)
			claim, err := s.RequestClaim(ctx, claimRequest(1500, "claim"))
			require.NoError(t, err)
			before, err := s.ListJournal(ctx, claim.PartnerID)
			require.NoError(t, err)
			if tc.name == "zero_clock" {
				clock.now = time.Time{}
			} else {
				clock.now = claim.RequestedAt.Add(-time.Second)
			}
			_, err = s.CancelRequestedClaim(ctx, cancellationRequest(claim, "original-key"))
			require.ErrorIs(t, err, partnerearnings.ErrInvalid)
			fresh, err := repo.GetClaim(ctx, "fixture-program", claim.ID)
			require.NoError(t, err)
			require.Equal(t, claim, fresh)
			after, err := s.ListJournal(ctx, claim.PartnerID)
			require.NoError(t, err)
			require.Equal(t, before, after)
		})
	}
}
