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

// Audit disposition: related correction lifecycle cases use actual isolated
// Mongo transactions, then bounded lifetime-membership pagination.
func TestMongoRelationshipSurvivesOwnershipChanges(t *testing.T) {
	for _, reacquire := range []bool{false, true} {
		t.Run(fmt.Sprintf("reacquire_%t", reacquire), func(t *testing.T) {
			_, _, store, db, clock, ctx := mongoEarnings(t)
			r, err := NewReferralRepository(store)
			require.NoError(t, err)
			s, err := referral.NewService(r, clock, randomIDs{}, 30*24*time.Hour)
			require.NoError(t, err)
			for n := 0; n < 3; n++ {
				customer := fmt.Sprintf("customer-%d", n)
				first, err := s.AssignAttribution(ctx, reviewedCorrection(t, s, referral.CorrectionRequest{Partner: referral.PartnerState{PartnerID: "original", CustomerID: "owner", CanAcquireReferrals: true}, ReferredCustomer: customer, ActorID: "operator", Reason: "initial assignment", Terms: frozenTerms()}))
				require.NoError(t, err)
				clock.now = clock.now.Add(time.Minute)
				second, err := s.AssignAttribution(ctx, reviewedCorrection(t, s, referral.CorrectionRequest{Partner: referral.PartnerState{PartnerID: "new", CustomerID: "new-owner", CanAcquireReferrals: true}, ReferredCustomer: customer, ActorID: "operator", Reason: "prospective transfer", ExpectedRevision: first.Revision, ExpectedReferralID: first.ID, Terms: frozenTerms()}))
				require.NoError(t, err)
				if reacquire {
					clock.now = clock.now.Add(time.Minute)
					_, err = s.AssignAttribution(ctx, reviewedCorrection(t, s, referral.CorrectionRequest{Partner: referral.PartnerState{PartnerID: "original", CustomerID: "owner", CanAcquireReferrals: true}, ReferredCustomer: customer, ActorID: "operator", Reason: "prospective return", ExpectedRevision: second.Revision, ExpectedReferralID: second.ID, Terms: frozenTerms()}))
					require.NoError(t, err)
				}
			}
			count, err := db.Collection("ghatd_owned_records").CountDocuments(ctx, bson.M{"kind": kindReferralRelationship})
			require.NoError(t, err)
			require.EqualValues(t, 6, count, "reacquisition cannot add a second lifetime membership")
			after := ""
			seen := map[string]bool{}
			for n := 0; n < 3; n++ {
				page, err := s.ListRelationships(ctx, "original", referral.RelationshipQuery{Limit: 1, After: after})
				require.NoError(t, err)
				require.Len(t, page.Items, 1)
				item := page.Items[0]
				require.False(t, seen[item.ID])
				seen[item.ID] = true
				require.Equal(t, reacquire, item.Current)
				require.NotNil(t, item.Periods[0].Until)
				if reacquire {
					require.Len(t, item.Periods, 2)
					require.Nil(t, item.Periods[1].Until)
				} else {
					require.Len(t, item.Periods, 1)
				}
				require.Equal(t, n < 2, page.HasMore)
				after = page.NextAfter
			}
			newPage, err := s.ListRelationships(ctx, "new", referral.RelationshipQuery{Limit: 100})
			require.NoError(t, err)
			require.Len(t, newPage.Items, 3)
			for _, item := range newPage.Items {
				require.Equal(t, !reacquire, item.Current)
				require.False(t, seen[item.ID], "cursor IDs are partner-scoped")
			}
			foreign := newPage.Items[0].ID
			out, err := s.ListRelationships(ctx, "original", referral.RelationshipQuery{Limit: 1, After: foreign})
			require.Error(t, err)
			require.Empty(t, out)
		})
	}
}

type relationshipSnapshotStore struct {
	recordstore.Store
	selected, release chan struct{}
	failHistory       bool
}
type relationshipSnapshotTx struct {
	recordstore.Tx
	store *relationshipSnapshotStore
}

func (s *relationshipSnapshotStore) Read(ctx context.Context, fn func(recordstore.Tx) error) error {
	return s.Store.Read(ctx, func(tx recordstore.Tx) error { return fn(relationshipSnapshotTx{Tx: tx, store: s}) })
}
func (t relationshipSnapshotTx) Find(ctx context.Context, q recordstore.Query) ([]recordstore.Record, error) {
	rows, err := t.Tx.Find(ctx, q)
	if err != nil {
		return nil, err
	}
	if q.Kind == kindReferralRelationship && t.store.selected != nil {
		close(t.store.selected)
		select {
		case <-t.store.release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if q.Kind == kindReferralRevision && t.store.failHistory {
		return nil, injectedFailure
	}
	return rows, nil
}

func TestMongoRelationshipReadSnapshot(t *testing.T) {
	cases := []struct {
		name string
		fail bool
	}{
		{name: "correction_after_page_selection_cannot_change_that_snapshot"},
		{name: "history_read_failure_discards_selected_membership", fail: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, store, _, clock, ctx := mongoEarnings(t)
			r, err := NewReferralRepository(store)
			require.NoError(t, err)
			s, err := referral.NewService(r, clock, randomIDs{}, time.Hour)
			require.NoError(t, err)
			first, err := s.AssignAttribution(ctx, reviewedCorrection(t, s, referral.CorrectionRequest{Partner: referral.PartnerState{PartnerID: "original", CustomerID: "owner", CanAcquireReferrals: true}, ReferredCustomer: "customer", ActorID: "operator", Reason: "initial assignment", Terms: frozenTerms()}))
			require.NoError(t, err)
			snapshot := &relationshipSnapshotStore{Store: store, failHistory: tc.fail}
			if !tc.fail {
				snapshot.selected, snapshot.release = make(chan struct{}), make(chan struct{})
			}
			readRepo, err := NewReferralRepository(snapshot)
			require.NoError(t, err)
			read, err := referral.NewService(readRepo, clock, randomIDs{}, time.Hour)
			require.NoError(t, err)
			type result struct {
				page referral.RelationshipPage
				err  error
			}
			completed := make(chan result, 1)
			go func() {
				v, e := read.ListRelationships(ctx, "original", referral.RelationshipQuery{Limit: 100})
				completed <- result{v, e}
			}()
			if !tc.fail {
				select {
				case <-snapshot.selected:
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
				clock.now = clock.now.Add(time.Minute)
				_, err = s.AssignAttribution(ctx, reviewedCorrection(t, s, referral.CorrectionRequest{Partner: referral.PartnerState{PartnerID: "new", CustomerID: "new-owner", CanAcquireReferrals: true}, ReferredCustomer: "customer", ActorID: "operator", Reason: "prospective correction", ExpectedRevision: first.Revision, ExpectedReferralID: first.ID, Terms: frozenTerms()}))
				close(snapshot.release)
				require.NoError(t, err)
			}
			out := <-completed
			if tc.fail {
				require.ErrorIs(t, out.err, injectedFailure)
				require.Empty(t, out.page)
				return
			}
			require.NoError(t, out.err)
			require.Len(t, out.page.Items, 1)
			require.True(t, out.page.Items[0].Current)
			require.Equal(t, first.ID, out.page.Items[0].Periods[0].ReferralID)
			fresh, err := s.ListRelationships(ctx, "original", referral.RelationshipQuery{Limit: 100})
			require.NoError(t, err)
			require.False(t, fresh.Items[0].Current)
		})
	}
}

func TestMongoRelationshipReadsCompleteHistory(t *testing.T) {
	cases := []struct {
		name      string
		revisions int
	}{
		{name: "short_ownership_chain", revisions: 3},
		{name: "history_crosses_recordstore_page_boundary", revisions: 205},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, store, _, clock, ctx := mongoEarnings(t)
			r, err := NewReferralRepository(store)
			require.NoError(t, err)
			s, err := referral.NewService(r, clock, randomIDs{}, time.Hour)
			require.NoError(t, err)
			err = r.WithAttributionTransaction(ctx, referral.ProgramID, "customer", func(tx referral.Repository) error {
				var previous referral.Referral
				for n := 1; n <= tc.revisions; n++ {
					partner := "original"
					if n%2 == 0 {
						partner = "other"
					}
					row := referral.Referral{ID: fmt.Sprintf("history-%04d", n), ProgramID: referral.ProgramID, PartnerID: partner, ReferredCustomer: "customer", Revision: int64(n), LockedAt: clock.Now().Add(time.Duration(n) * time.Minute), PostedAt: clock.Now().Add(time.Duration(n) * time.Minute), TermsSnapshot: frozenTerms(), CorrectionOf: previous.ID, PriorPartnerID: previous.PartnerID}
					if err := tx.InsertReferral(ctx, row, int64(n-1)); err != nil {
						return err
					}
					previous = row
				}
				return nil
			})
			require.NoError(t, err)
			out, err := s.ListRelationships(ctx, "original", referral.RelationshipQuery{Limit: 1})
			require.NoError(t, err)
			require.Len(t, out.Items, 1)
			require.True(t, out.Items[0].Current)
			require.False(t, out.HasMore)
			require.Len(t, out.Items[0].Periods, (tc.revisions+1)/2)
			require.Equal(t, "history-0001", out.Items[0].Periods[0].ReferralID)
			last := out.Items[0].Periods[len(out.Items[0].Periods)-1]
			require.Equal(t, fmt.Sprintf("history-%04d", tc.revisions), last.ReferralID)
			require.Nil(t, last.Until)
		})
	}
}
