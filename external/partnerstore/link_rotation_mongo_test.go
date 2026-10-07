package partnerstore

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/referral"
	"github.com/ooaklee/ghatd/external/repository/recordstore"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
)

type rotationFailureStore struct {
	recordstore.Store
	failAt int
}
type rotationFailureTx struct {
	recordstore.Tx
	failAt, writes int
}

func (s rotationFailureStore) Transact(ctx context.Context, key string, fn func(recordstore.Tx) error) error {
	return s.Store.Transact(ctx, key, func(tx recordstore.Tx) error {
		return fn(&rotationFailureTx{Tx: tx, failAt: s.failAt})
	})
}
func (tx *rotationFailureTx) Insert(ctx context.Context, row recordstore.Record) error {
	tx.writes++
	if tx.writes == tx.failAt {
		return injectedFailure
	}
	return tx.Tx.Insert(ctx, row)
}
func (tx *rotationFailureTx) Replace(ctx context.Context, row recordstore.Record, expected int64) error {
	tx.writes++
	if tx.writes == tx.failAt {
		return injectedFailure
	}
	return tx.Tx.Replace(ctx, row, expected)
}

func rotationService(t *testing.T, store recordstore.Store, clock *mongoTestClock) (*ReferralRepository, *referral.Service) {
	t.Helper()
	r, err := NewReferralRepository(store)
	require.NoError(t, err)
	s, err := referral.NewService(r, clock, randomIDs{}, 7*24*time.Hour)
	require.NoError(t, err)
	return r, s
}
func rotationRequest(code, key string) referral.RotateLinkRequest {
	return referral.RotateLinkRequest{Partner: referral.PartnerState{PartnerID: "partner", CustomerID: "owner", CanAcquireReferrals: true}, ActorID: "owner", ExpectedLinkCode: code, Reason: "replace reviewed link", IdempotencyKey: key}
}

func TestMongoLinkRotationAllWritesRollback(t *testing.T) {
	for _, tc := range []struct {
		name   string
		failAt int
	}{
		{"retire_original", 1}, {"original_audit", 2}, {"clear_head", 3},
		{"reserve_new_code", 4}, {"insert_new_link", 5}, {"new_link_audit", 6},
		{"set_new_head", 7}, {"receipt", 8},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, store, db, clock, ctx := mongoEarnings(t)
			r, s := rotationService(t, store, clock)
			original, err := s.IssueLink(ctx, rotationRequest("unused", "unused").Partner)
			require.NoError(t, err)
			_, broken := rotationService(t, rotationFailureStore{Store: store, failAt: tc.failAt}, clock)
			_, err = broken.RotateLink(ctx, rotationRequest(original.Code, "original-key"))
			require.ErrorIs(t, err, injectedFailure)
			fresh, err := r.GetLinkByCode(ctx, original.Code)
			require.NoError(t, err)
			require.Equal(t, original, fresh)
			active, err := s.IssueLink(ctx, rotationRequest("unused", "unused").Partner)
			require.NoError(t, err)
			require.Equal(t, original, active)
			for kind, want := range map[string]int64{kindLink: 1, kindLinkRevision: 1, kindLinkCode: 1, kindLinkRotation: 0} {
				count, err := db.Collection("ghatd_owned_records").CountDocuments(ctx, bson.M{"kind": kind})
				require.NoError(t, err)
				require.Equal(t, want, count, kind)
			}
			_, err = s.RotateLink(ctx, rotationRequest(original.Code, "original-key"))
			require.NoError(t, err)
		})
	}
}

func TestMongoLinkRotationOriginalReceiptRecovery(t *testing.T) {
	for _, tc := range []struct {
		name string
		want error
	}{
		{"same_request", nil}, {"uncertain_commit", nil}, {"later_rotation", nil},
		{"paused_replay", nil}, {"changed_reason", referral.ErrStaleWrite},
		{"changed_original_code", referral.ErrStaleWrite}, {"changed_customer", referral.ErrStaleWrite},
		{"new_key_stale_original", referral.ErrStaleWrite}, {"new_rotation_paused", referral.ErrDenied},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, store, db, clock, ctx := mongoEarnings(t)
			r, s := rotationService(t, store, clock)
			original, err := s.IssueLink(ctx, rotationRequest("unused", "unused").Partner)
			require.NoError(t, err)
			req := rotationRequest(original.Code, "original-key")
			first := s
			if tc.name == "uncertain_commit" {
				_, first = rotationService(t, injectedStore{Store: store, uncertain: true}, clock)
			}
			rotated, err := first.RotateLink(ctx, req)
			if tc.name == "uncertain_commit" {
				require.ErrorIs(t, err, referral.ErrUncertain)
				require.Empty(t, rotated.ID)
			} else {
				require.NoError(t, err)
			}
			receipt, err := r.GetLinkRotation(ctx, "partner", "owner", "original-key")
			require.NoError(t, err)
			rotated = receipt.Link
			require.NotEqual(t, original.ID, rotated.ID)
			current := rotated
			switch tc.name {
			case "later_rotation":
				clock.now = clock.now.Add(time.Minute)
				current, err = s.RotateLink(ctx, rotationRequest(rotated.Code, "second-key"))
				require.NoError(t, err)
			case "paused_replay":
				req.Partner.CanAcquireReferrals = false
			case "changed_reason":
				req.Reason = "different intent"
			case "changed_original_code":
				req.ExpectedLinkCode = rotated.Code
			case "changed_customer":
				req.Partner.CustomerID = "different owner"
			case "new_key_stale_original":
				req.IdempotencyKey = "new-key"
			case "new_rotation_paused":
				req.ExpectedLinkCode, req.IdempotencyKey = rotated.Code, "new-key"
				req.Partner.CanAcquireReferrals = false
			}
			result, err := s.RotateLink(ctx, req)
			if tc.want == nil {
				require.NoError(t, err)
				require.Equal(t, rotated, result)
			} else {
				require.ErrorIs(t, err, tc.want)
				require.Empty(t, result.ID)
			}
			active, err := s.IssueLink(ctx, rotationRequest("unused", "unused").Partner)
			require.NoError(t, err)
			require.Equal(t, current, active)
			links, err := r.ListLinksByPartner(ctx, referral.ProgramID, "partner")
			require.NoError(t, err)
			wantLinks := 2
			if tc.name == "later_rotation" {
				wantLinks = 3
			}
			require.Len(t, links, wantLinks)
			count, err := db.Collection("ghatd_owned_records").CountDocuments(ctx, bson.M{"kind": kindLinkRotation})
			require.NoError(t, err)
			require.EqualValues(t, wantLinks-1, count)
		})
	}
}

func TestMongoLinkRotationConcurrentRequests(t *testing.T) {
	for _, tc := range []struct {
		name    string
		sameKey bool
	}{{"same_key_recovers_one_result", true}, {"competing_keys_select_one_original", false}} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, store, db, clock, ctx := mongoEarnings(t)
			r, s := rotationService(t, store, clock)
			original, err := s.IssueLink(ctx, rotationRequest("unused", "unused").Partner)
			require.NoError(t, err)
			type result struct {
				link referral.Link
				err  error
			}
			start, out := make(chan struct{}), make(chan result, 8)
			for i := 0; i < 8; i++ {
				key := "same-key"
				if !tc.sameKey {
					key = fmt.Sprintf("key-%d", i)
				}
				go func(key string) {
					<-start
					link, err := s.RotateLink(ctx, rotationRequest(original.Code, key))
					out <- result{link, err}
				}(key)
			}
			close(start)
			winner, successes := "", 0
			for i := 0; i < 8; i++ {
				res := <-out
				if res.err == nil {
					successes++
					if winner == "" {
						winner = res.link.ID
					}
					require.Equal(t, winner, res.link.ID)
				} else {
					require.False(t, tc.sameKey)
					require.ErrorIs(t, res.err, referral.ErrStaleWrite)
				}
			}
			if tc.sameKey {
				require.Equal(t, 8, successes)
			} else {
				require.Equal(t, 1, successes)
			}
			links, err := r.ListLinksByPartner(ctx, referral.ProgramID, "partner")
			require.NoError(t, err)
			require.Len(t, links, 2)
			count, err := db.Collection("ghatd_owned_records").CountDocuments(ctx, bson.M{"kind": kindLinkRotation})
			require.NoError(t, err)
			require.EqualValues(t, 1, count)
		})
	}
}

type rotationConstantIDs struct{}

func (rotationConstantIDs) NewID() string { return "fixture-collision-seed" }

func TestMongoLinkRotationKeepsGlobalReservedCodeAndOwnership(t *testing.T) {
	for _, tc := range []struct {
		name string
		want error
	}{{"other_owner_code", referral.ErrStaleWrite}, {"retired_global_code_collision", referral.ErrCodeTaken}} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, store, _, clock, ctx := mongoEarnings(t)
			r, s := rotationService(t, store, clock)
			original, err := s.IssueLink(ctx, rotationRequest("unused", "unused").Partner)
			require.NoError(t, err)
			other, err := referral.NewService(r, clock, rotationConstantIDs{}, 7*24*time.Hour)
			require.NoError(t, err)
			reserved, err := other.IssueLink(ctx, referral.PartnerState{PartnerID: "other-partner", CustomerID: "other-owner", CanAcquireReferrals: true})
			require.NoError(t, err)
			req := rotationRequest(original.Code, "key")
			if tc.name == "other_owner_code" {
				req.ExpectedLinkCode = reserved.Code
			} else {
				require.NoError(t, other.RetireLink(ctx, reserved.ID, "retire reserved code", "other-owner"))
				s, err = referral.NewService(r, clock, rotationConstantIDs{}, 7*24*time.Hour)
				require.NoError(t, err)
			}
			_, err = s.RotateLink(ctx, req)
			require.ErrorIs(t, err, tc.want)
			fresh, err := r.GetLinkByCode(ctx, original.Code)
			require.NoError(t, err)
			require.Equal(t, original, fresh)
			_, err = r.GetLinkRotation(ctx, "partner", "owner", "key")
			require.ErrorIs(t, err, referral.ErrNotFound)
		})
	}
}

func TestMongoLinkRotationFailsClosedOnBrokenReceiptAndLinkage(t *testing.T) {
	for _, tc := range []struct{ name string }{{"receipt_revision"}, {"replacement_missing"}, {"original_missing"}} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, store, db, clock, ctx := mongoEarnings(t)
			r, s := rotationService(t, store, clock)
			original, err := s.IssueLink(ctx, rotationRequest("unused", "unused").Partner)
			require.NoError(t, err)
			req := rotationRequest(original.Code, "key")
			link, err := s.RotateLink(ctx, req)
			require.NoError(t, err)
			switch tc.name {
			case "receipt_revision":
				err = store.Transact(ctx, linksPartition("partner"), func(tx recordstore.Tx) error {
					row, err := tx.Get(ctx, kindLinkRotation, referral.LinkRotationID("partner", "owner", "key"))
					if err != nil {
						return err
					}
					row.Revision = 2
					return tx.Replace(ctx, row, 1)
				})
			case "replacement_missing", "original_missing":
				id := link.ID
				if tc.name == "original_missing" {
					id = original.ID
				}
				_, err = db.Collection("ghatd_owned_records").DeleteOne(ctx, bson.M{"kind": kindLink, "id": id})
			}
			require.NoError(t, err)
			result, err := s.RotateLink(ctx, req)
			require.ErrorIs(t, err, referral.ErrUnavailable)
			require.Empty(t, result.ID)
			// Bound receipt insertion and anti-nesting cannot be bypassed through
			// the adapter's otherwise callable persistence methods.
			err = r.WithLinkTransaction(ctx, referral.ProgramID, "partner", func(tx referral.LinkRotationTransaction) error {
				bound := tx.(*ReferralRepository)
				return bound.WithLinkTransaction(ctx, referral.ProgramID, "partner", func(referral.LinkRotationTransaction) error { return nil })
			})
			require.ErrorIs(t, err, referral.ErrInvalid)
		})
	}
}
