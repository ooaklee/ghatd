package revenuestore

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/repository/recordstore"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// Audit disposition: named native rollback, concurrency, replay, binding and
// encrypted-join cases. Every case owns an isolated database and clock.
type subscriptionClock struct{ at time.Time }

func (c *subscriptionClock) Now() time.Time { return c.at }
func nativeStatus(t *testing.T, r billing.RevenueRepository, ctx context.Context) (*billing.RevenueService, *subscriptionClock, string) {
	t.Helper()
	c := &subscriptionClock{time.Date(2026, 10, 7, 14, 0, 0, 7, time.UTC)}
	s, err := billing.NewRevenueService(r, c)
	require.NoError(t, err)
	f := revenueFact()
	f.ProviderCustomerID = "private-provider-customer"
	o := accept(t, s, ctx, "status-payment", f)
	return s, c, o.FactIDs[0]
}
func nativeStatusEvidence(p billing.SubscriptionStatusPreparation, state string) billing.VerifiedSubscriptionStatusEvidence {
	return billing.VerifiedSubscriptionStatusEvidence{Scope: p.Scope, SubscriptionID: p.SubscriptionID, ProviderCustomerID: p.ProviderCustomerID, Status: state}
}

type statusFailStore struct {
	recordstore.Store
	at    int
	after bool
}
type statusFailTx struct {
	recordstore.Tx
	at, count int
	after     bool
}

func (t *statusFailTx) write(call func() error) error {
	t.count++
	if t.count == t.at && !t.after {
		return failWrite
	}
	if err := call(); err != nil {
		return err
	}
	if t.count == t.at && t.after {
		return failWrite
	}
	return nil
}
func (t *statusFailTx) Insert(ctx context.Context, r recordstore.Record) error {
	return t.write(func() error { return t.Tx.Insert(ctx, r) })
}
func (t *statusFailTx) Replace(ctx context.Context, r recordstore.Record, expected int64) error {
	return t.write(func() error { return t.Tx.Replace(ctx, r, expected) })
}
func (s statusFailStore) Transact(ctx context.Context, guard string, fn func(recordstore.Tx) error) error {
	return s.Store.Transact(ctx, guard, func(tx recordstore.Tx) error { return fn(&statusFailTx{Tx: tx, at: s.at, after: s.after}) })
}
func TestMongoSubscriptionStatusAtomicCapture(t *testing.T) {
	type testCase struct {
		name            string
		at              int
		after, existing bool
	}
	cases := []testCase{
		{name: "before_capture_insert", at: 1},
		{name: "after_capture_insert", at: 1, after: true},
		{name: "before_first_head_insert", at: 2},
		{name: "after_first_head_insert", at: 2, after: true},
		{name: "before_head_replace", at: 2, existing: true},
		{name: "after_head_replace", at: 2, after: true, existing: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, store, db, ctx := revenueFixture(t)
			s, c, id := nativeStatus(t, r, ctx)
			var before billing.SubscriptionStatus
			p, err := s.PrepareSubscriptionStatus(ctx, "original", id)
			require.NoError(t, err)
			if tc.existing {
				before, err = s.CaptureVerifiedSubscriptionStatus(ctx, p, nativeStatusEvidence(p, "active"))
				require.NoError(t, err)
				c.at = c.at.Add(time.Second)
				p, err = s.PrepareSubscriptionStatus(ctx, "update", id)
				require.NoError(t, err)
			}
			broken, err := NewRepository(statusFailStore{Store: store, at: tc.at, after: tc.after})
			require.NoError(t, err)
			attempt, err := billing.NewRevenueService(broken, c)
			require.NoError(t, err)
			v, err := attempt.CaptureVerifiedSubscriptionStatus(ctx, p, nativeStatusEvidence(p, "canceled"))
			require.ErrorIs(t, err, failWrite)
			require.Zero(t, v)
			count, err := db.Collection("ghatd_owned_records").CountDocuments(ctx, bson.M{"kind": bson.M{"$in": []string{kindSubscriptionStatusHead, kindSubscriptionStatusCapture}}})
			require.NoError(t, err)
			if tc.existing {
				require.EqualValues(t, 2, count)
				v, err = s.GetSubscriptionStatusForFact(ctx, id, time.Minute)
				require.NoError(t, err)
				require.Equal(t, before, v)
			} else {
				require.Zero(t, count)
				v, err = s.GetSubscriptionStatusForFact(ctx, id, time.Minute)
				require.ErrorIs(t, err, billing.ErrRevenueNotFound)
				require.Zero(t, v)
			}
		})
	}
}
func TestMongoSubscriptionStatusOriginalRecovery(t *testing.T) {
	type testCase struct {
		name    string
		lostAck bool
	}
	cases := []testCase{
		{name: "receipt_after_new_head_and_restart"},
		{name: "uncertain_commit_same_capture_recovery", lostAck: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, store, db, ctx := revenueFixture(t)
			s, c, id := nativeStatus(t, r, ctx)
			p, err := s.PrepareSubscriptionStatus(ctx, "original-author", id)
			require.NoError(t, err)
			e := nativeStatusEvidence(p, "active")
			if tc.lostAck {
				unknown, err := NewRepository(failingStore{Store: store, uncertain: true})
				require.NoError(t, err)
				attempt, err := billing.NewRevenueService(unknown, c)
				require.NoError(t, err)
				v, err := attempt.CaptureVerifiedSubscriptionStatus(ctx, p, e)
				require.ErrorIs(t, err, billing.ErrRevenueUncertain)
				require.Zero(t, v)
			}
			first, err := s.CaptureVerifiedSubscriptionStatus(ctx, p, e)
			require.NoError(t, err)
			c.at = c.at.Add(time.Second)
			next, err := s.PrepareSubscriptionStatus(ctx, "next-author", id)
			require.NoError(t, err)
			later, err := s.CaptureVerifiedSubscriptionStatus(ctx, next, nativeStatusEvidence(next, "paused"))
			require.NoError(t, err)
			require.EqualValues(t, 2, later.Revision)
			restarted, err := NewRepository(store)
			require.NoError(t, err)
			again, err := billing.NewRevenueService(restarted, c)
			require.NoError(t, err)
			c.at = time.Time{}
			recovered, err := again.CaptureVerifiedSubscriptionStatus(ctx, p, e)
			require.NoError(t, err)
			require.Equal(t, first, recovered)
			c.at = later.ObservedAt
			current, err := again.GetSubscriptionStatusForFact(ctx, id, time.Minute)
			require.NoError(t, err)
			require.Equal(t, later, current)
			count, err := db.Collection("ghatd_owned_records").CountDocuments(ctx, bson.M{"kind": kindSubscriptionStatusCapture})
			require.NoError(t, err)
			require.EqualValues(t, 2, count)
			var raw bson.M
			require.NoError(t, db.Collection("ghatd_owned_records").FindOne(ctx, bson.M{"kind": kindSubscriptionStatusHead}).Decode(&raw))
			require.NotContains(t, fmt.Sprint(raw), "private-provider-customer")
			require.NotContains(t, fmt.Sprint(raw), "original-author")
			require.NotContains(t, raw, "expires_at")
		})
	}
}
func TestMongoSubscriptionStatusCompetingResponses(t *testing.T) {
	type testCase struct {
		name        string
		sameCapture bool
	}
	cases := []testCase{
		{name: "same_capture_concurrent_replay", sameCapture: true},
		{name: "different_prepared_responses_only_one_wins"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, _, db, ctx := revenueFixture(t)
			s, _, id := nativeStatus(t, r, ctx)
			preps := make([]billing.SubscriptionStatusPreparation, 8)
			for i := range preps {
				actor := fmt.Sprintf("worker-%d", i)
				if tc.sameCapture {
					actor = "same-worker"
				}
				var err error
				preps[i], err = s.PrepareSubscriptionStatus(ctx, actor, id)
				require.NoError(t, err)
			}
			start := make(chan struct{})
			results := make(chan error, 8)
			for i := range preps {
				go func(i int) {
					<-start
					_, err := s.CaptureVerifiedSubscriptionStatus(ctx, preps[i], nativeStatusEvidence(preps[i], "active"))
					results <- err
				}(i)
			}
			close(start)
			wins := 0
			for range preps {
				err := <-results
				if err == nil {
					wins++
				} else {
					require.ErrorIs(t, err, billing.ErrRevenueConflict)
				}
			}
			if tc.sameCapture {
				require.Equal(t, 8, wins)
			} else {
				require.Equal(t, 1, wins)
			}
			count, err := db.Collection("ghatd_owned_records").CountDocuments(ctx, bson.M{"kind": kindSubscriptionStatusCapture})
			require.NoError(t, err)
			require.EqualValues(t, 1, count)
			v, err := s.GetSubscriptionStatusForFact(ctx, id, time.Minute)
			require.NoError(t, err)
			require.EqualValues(t, 1, v.Revision)
		})
	}
}
func TestMongoSubscriptionStatusScopeIsolation(t *testing.T) {
	type testCase struct {
		name                   string
		provider, account, sub string
		live                   bool
	}
	cases := []testCase{
		{name: "provider", provider: "other-provider"},
		{name: "merchant", account: "other-account"},
		{name: "live_mode", live: true},
		{name: "subscription", sub: "other-subscription"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, _, _, ctx := revenueFixture(t)
			s, _, id := nativeStatus(t, r, ctx)
			p, err := s.PrepareSubscriptionStatus(ctx, "worker", id)
			require.NoError(t, err)
			_, err = s.CaptureVerifiedSubscriptionStatus(ctx, p, nativeStatusEvidence(p, "active"))
			require.NoError(t, err)
			f := revenueFact()
			f.PaymentID = "other-payment"
			f.ProviderCustomerID = "different-provider-customer"
			f.PrincipalID = "different-principal"
			if tc.provider != "" {
				f.Scope.Provider = tc.provider
			}
			if tc.account != "" {
				f.Scope.AccountID = tc.account
			}
			f.Scope.LiveMode = tc.live
			if tc.sub != "" {
				f.SubscriptionID = tc.sub
			}
			o := accept(t, s, ctx, "other-scope-payment", f)
			other, err := s.PrepareSubscriptionStatus(ctx, "worker", o.FactIDs[0])
			require.NoError(t, err)
			require.Zero(t, other.ExpectedRevision)
			_, err = s.CaptureVerifiedSubscriptionStatus(ctx, other, nativeStatusEvidence(other, "canceled"))
			require.NoError(t, err)
			one, err := s.GetSubscriptionStatusForFact(ctx, id, time.Minute)
			require.NoError(t, err)
			require.Equal(t, "active", one.Status)
			two, err := s.GetSubscriptionStatusForFact(ctx, o.FactIDs[0], time.Minute)
			require.NoError(t, err)
			require.Equal(t, "canceled", two.Status)
			err = r.WithSubscriptionStatusTransaction(ctx, p.Scope, p.SubscriptionID, func(tx billing.SubscriptionStatusTx) error { _, err := tx.GetCapture(ctx, other.CaptureID); return err })
			require.ErrorIs(t, err, billing.ErrRevenueUnavailable)
		})
	}
}
func TestMongoSubscriptionStatusJoinedEvidence(t *testing.T) {
	type testCase struct {
		name                        string
		missingReceipt, corruptHead bool
	}
	cases := []testCase{
		{name: "missing_linked_capture_is_not_first_absence", missingReceipt: true},
		{name: "tampered_head_metadata_is_not_fresh_status", corruptHead: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, _, db, ctx := revenueFixture(t)
			s, _, id := nativeStatus(t, r, ctx)
			p, err := s.PrepareSubscriptionStatus(ctx, "worker", id)
			require.NoError(t, err)
			_, err = s.CaptureVerifiedSubscriptionStatus(ctx, p, nativeStatusEvidence(p, "active"))
			require.NoError(t, err)
			if tc.missingReceipt {
				_, err = db.Collection("ghatd_owned_records").DeleteOne(ctx, bson.M{"kind": kindSubscriptionStatusCapture, "id": p.CaptureID})
			} else {
				_, err = db.Collection("ghatd_owned_records").UpdateOne(ctx, bson.M{"kind": kindSubscriptionStatusHead}, bson.M{"$set": bson.M{"state": "canceled"}})
			}
			require.NoError(t, err)
			v, err := s.GetSubscriptionStatusForFact(ctx, id, time.Minute)
			require.Error(t, err)
			require.Zero(t, v)
			require.False(t, errors.Is(err, billing.ErrRevenueNotFound))
			next, err := s.PrepareSubscriptionStatus(ctx, "worker", id)
			require.Error(t, err)
			require.Zero(t, next)
			require.False(t, errors.Is(err, billing.ErrRevenueNotFound))
		})
	}
}
func TestMongoSubscriptionStatusPaymentIsNotPaidEligibility(t *testing.T) {
	type testCase struct{ name, adjustment string }
	cases := []testCase{
		{name: "fully_refunded_original_still_has_lifecycle", adjustment: billing.RevenueRefund},
		{name: "disputed_original_still_has_lifecycle", adjustment: billing.RevenueDisputeHold},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, _, _, ctx := revenueFixture(t)
			s, _, id := nativeStatus(t, r, ctx)
			f := revenueFact()
			f.ProviderCustomerID = "private-provider-customer"
			f.Kind = tc.adjustment
			f.AdjustmentID = "adjustment"
			if tc.adjustment == billing.RevenueRefund {
				f.CumulativeRefundedMinor = f.PaidMinor
			}
			o := accept(t, s, ctx, "adjustment", f)
			p, err := s.PrepareSubscriptionStatus(ctx, "worker", id)
			require.NoError(t, err)
			_, err = s.CaptureVerifiedSubscriptionStatus(ctx, p, nativeStatusEvidence(p, "active"))
			require.NoError(t, err)
			v, err := s.GetSubscriptionStatusForFact(ctx, id, time.Minute)
			require.NoError(t, err)
			require.Equal(t, "active", v.Status)
			p, err = s.PrepareSubscriptionStatus(ctx, "worker", o.FactIDs[0])
			require.ErrorIs(t, err, billing.ErrRevenueUnassessable)
			require.Zero(t, p)
			facts, err := s.PendingRevenueFacts(ctx, "reporting", 200)
			require.NoError(t, err)
			require.Len(t, facts, 2, "status never changes the economics")
		})
	}
}

type statusInterleavedStore struct {
	recordstore.Store
	headID        string
	readHead      chan struct{}
	updated       chan error
	failedReceipt bool
}
type statusInterleavedTx struct {
	recordstore.Tx
	store *statusInterleavedStore
}

func (s *statusInterleavedStore) Read(ctx context.Context, fn func(recordstore.Tx) error) error {
	return s.Store.Read(ctx, func(tx recordstore.Tx) error { return fn(&statusInterleavedTx{Tx: tx, store: s}) })
}
func (t *statusInterleavedTx) Get(ctx context.Context, kind, id string) (recordstore.Record, error) {
	row, err := t.Tx.Get(ctx, kind, id)
	if err == nil && kind == kindSubscriptionStatusHead && id == t.store.headID {
		t.store.headID = ""
		close(t.store.readHead)
		if err := <-t.store.updated; err != nil {
			return recordstore.Record{}, err
		}
	}
	if kind == kindSubscriptionStatusCapture && t.store.failedReceipt {
		return recordstore.Record{}, recordstore.ErrUnavailable
	}
	return row, err
}
func TestMongoSubscriptionStatusReadSnapshot(t *testing.T) {
	type testCase struct {
		name        string
		failReceipt bool
	}
	cases := []testCase{
		{name: "head_advance_between_reads_keeps_original_snapshot"},
		{name: "late_receipt_read_failure_returns_no_partial_status", failReceipt: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, store, _, ctx := revenueFixture(t)
			s, _, id := nativeStatus(t, r, ctx)
			p, err := s.PrepareSubscriptionStatus(ctx, "first-author", id)
			require.NoError(t, err)
			first, err := s.CaptureVerifiedSubscriptionStatus(ctx, p, nativeStatusEvidence(p, "active"))
			require.NoError(t, err)
			next, err := s.PrepareSubscriptionStatus(ctx, "second-author", id)
			require.NoError(t, err)
			interleave := &statusInterleavedStore{Store: store, headID: billing.SubscriptionStatusIdentity(p.Scope, p.SubscriptionID), readHead: make(chan struct{}), updated: make(chan error, 1), failedReceipt: tc.failReceipt}
			repo, err := NewRepository(interleave)
			require.NoError(t, err)
			reader, err := billing.NewRevenueService(repo, fixtureClock{first.ObservedAt})
			require.NoError(t, err)
			// The writer uses a separate owning native transaction after the reader has
			// pinned its snapshot. Production callbacks perform no external effects.
			go func() {
				<-interleave.readHead
				_, err := s.CaptureVerifiedSubscriptionStatus(ctx, next, nativeStatusEvidence(next, "canceled"))
				interleave.updated <- err
			}()
			v, err := reader.GetSubscriptionStatusForFact(ctx, id, time.Minute)
			if tc.failReceipt {
				require.ErrorIs(t, err, billing.ErrRevenueUnavailable)
				require.Zero(t, v)
			} else {
				require.NoError(t, err)
				require.Equal(t, first, v)
			}
			current, err := s.GetSubscriptionStatusForFact(ctx, id, time.Minute)
			require.NoError(t, err)
			require.Equal(t, "canceled", current.Status)
			require.EqualValues(t, 2, current.Revision)
		})
	}
}
