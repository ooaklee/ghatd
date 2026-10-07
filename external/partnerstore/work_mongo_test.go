package partnerstore

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/partnermanager"
	"github.com/ooaklee/ghatd/external/repository/recordstore"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// Audit disposition: named real-Mongo cases cover durable discovery/leases and
// immutable decisions. They do not claim owning-feed worker/host integration.
func workQueueFixture(t *testing.T, store recordstore.Store, clock *mongoTestClock) *partnermanager.WorkQueue {
	t.Helper()
	repo, err := NewWorkRepository(store)
	require.NoError(t, err)
	queue, err := partnermanager.NewWorkQueue(repo, clock, partnermanager.WorkQueueConfig{ProgramID: "fixture-program", LeaseDuration: time.Minute, RetryBase: time.Second, RetryMax: time.Hour})
	require.NoError(t, err)
	return queue
}
func workCandidates(n int) []partnermanager.WorkCandidate {
	result := make([]partnermanager.WorkCandidate, 0, n)
	for i := 0; i < n; i++ {
		result = append(result, partnermanager.WorkCandidate{SourceID: fmt.Sprintf("source_%04d", i), SourceFingerprint: fmt.Sprintf("source_digest_%04d", i)})
	}
	return result
}
func TestMongoWorkDiscoveryAtomicRecovery(t *testing.T) {
	cases := []struct {
		name      string
		failAt    int
		uncertain bool
	}{{"first_job_rollback", 1, false}, {"second_job_rollback", 2, false}, {"discovery_position_rollback", 3, false}, {"page_receipt_rollback", 4, false}, {"lost_page_commit_ack", 0, true}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, store, db, clock, ctx := mongoEarnings(t)
			queue := workQueueFixture(t, store, clock)
			fault := workQueueFixture(t, workFaultStore{Store: store, failAt: tc.failAt, uncertain: tc.uncertain}, clock)
			candidates := workCandidates(2)
			start := partnermanager.DiscoveryCursor{}
			next := partnermanager.DiscoveryCursor{AfterSequence: 2}
			err := fault.EnqueuePage(ctx, partnermanager.WorkRevenue, start, next, candidates)
			require.Error(t, err)
			count, countErr := db.Collection("ghatd_owned_records").CountDocuments(ctx, bson.M{"kind": bson.M{"$in": []string{kindWorkItem, kindWorkCursor, kindWorkPage}}})
			require.NoError(t, countErr)
			if tc.uncertain {
				require.ErrorIs(t, err, partnermanager.ErrWorkUncertain)
				require.Equal(t, int64(4), count)
			} else {
				require.Zero(t, count)
			}
			require.NoError(t, queue.EnqueuePage(ctx, partnermanager.WorkRevenue, start, next, candidates))
			cursor, err := queue.Cursor(ctx, partnermanager.WorkRevenue)
			require.NoError(t, err)
			require.Equal(t, partnermanager.DiscoveryCursor{Revision: 1, AfterSequence: 2}, cursor)
			// A persisted page receipt replays even after a later discovery advance.
			later := partnermanager.DiscoveryCursor{AfterSequence: 3}
			require.NoError(t, queue.EnqueuePage(ctx, partnermanager.WorkRevenue, cursor, later, []partnermanager.WorkCandidate{{SourceID: "later", SourceFingerprint: "later_digest"}}))
			require.NoError(t, queue.EnqueuePage(ctx, partnermanager.WorkRevenue, start, next, candidates))
			unchanged, err := queue.Cursor(ctx, partnermanager.WorkRevenue)
			require.NoError(t, err)
			require.EqualValues(t, 2, unchanged.Revision)
			require.EqualValues(t, 3, unchanged.AfterSequence)
			changed := workCandidates(2)
			changed[0].SourceFingerprint = "changed_source"
			require.ErrorIs(t, queue.EnqueuePage(ctx, partnermanager.WorkRevenue, start, next, changed), partnermanager.ErrWorkConflict)
		})
	}
}
func TestMongoWorkImmutableDecisionRecovery(t *testing.T) {
	cases := []struct {
		name                                                             string
		failDecision, uncertainDecision, failComplete, uncertainComplete bool
	}{{name: "decision_rollback", failDecision: true}, {name: "lost_decision_ack", uncertainDecision: true}, {name: "completion_rollback", failComplete: true}, {name: "lost_completion_ack", uncertainComplete: true}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, store, db, clock, ctx := mongoEarnings(t)
			queue := workQueueFixture(t, store, clock)
			require.NoError(t, queue.EnqueuePage(ctx, partnermanager.WorkRevenue, partnermanager.DiscoveryCursor{}, partnermanager.DiscoveryCursor{AfterSequence: 1}, workCandidates(1)))
			jobs, err := queue.Lease(ctx, partnermanager.WorkRevenue, 1)
			require.NoError(t, err)
			require.Len(t, jobs, 1)
			job := jobs[0]
			// Completion without a committed owning decision cannot retire a job.
			require.ErrorIs(t, queue.Complete(ctx, job, "invented_receipt"), partnermanager.ErrWorkConflict)
			if tc.failDecision || tc.uncertainDecision {
				at := 0
				if tc.failDecision {
					at = 1
				}
				fault := workQueueFixture(t, workFaultStore{Store: store, failAt: at, uncertain: tc.uncertainDecision}, clock)
				_, err = fault.Decide(ctx, job, "current_worker", partnermanager.WorkAccepted, "owning_financial_receipt", "financial_accepted")
				require.Error(t, err)
				stored, err := queue.Get(ctx, job.Kind, job.SourceID)
				require.NoError(t, err)
				if tc.failDecision {
					require.Nil(t, stored.Decision)
				} else {
					require.NotNil(t, stored.Decision)
				}
			}
			decision, err := queue.Decide(ctx, job, "current_worker", partnermanager.WorkAccepted, "owning_financial_receipt", "financial_accepted")
			require.NoError(t, err)
			again, err := queue.Decide(ctx, job, "current_worker", partnermanager.WorkAccepted, "owning_financial_receipt", "financial_accepted")
			require.NoError(t, err)
			require.Equal(t, decision, again)
			_, err = queue.Decide(ctx, job, "current_worker", partnermanager.WorkNoEntitlement, "different_proof", "different_outcome")
			require.ErrorIs(t, err, partnermanager.ErrWorkConflict)
			if tc.failComplete || tc.uncertainComplete {
				at := 0
				if tc.failComplete {
					at = 1
				}
				fault := workQueueFixture(t, workFaultStore{Store: store, failAt: at, uncertain: tc.uncertainComplete}, clock)
				err = fault.Complete(ctx, job, decision.ID)
				require.Error(t, err)
				stored, loadErr := queue.Get(ctx, job.Kind, job.SourceID)
				require.NoError(t, loadErr)
				require.Equal(t, decision, *stored.Decision)
				if tc.failComplete {
					require.Equal(t, partnermanager.WorkPending, stored.State)
				} else {
					require.Equal(t, partnermanager.WorkComplete, stored.State)
				}
			}
			require.NoError(t, queue.Complete(ctx, job, decision.ID))
			require.NoError(t, queue.Complete(ctx, job, decision.ID))
			stored, err := queue.Get(ctx, job.Kind, job.SourceID)
			require.NoError(t, err)
			require.Equal(t, partnermanager.WorkComplete, stored.State)
			require.Equal(t, "current_worker", stored.Decision.ActorID)
			public, err := json.Marshal(stored)
			require.NoError(t, err)
			require.NotContains(t, string(public), "current_worker")
			require.NotContains(t, string(public), job.SourceID)
			require.NotContains(t, string(public), job.SourceFingerprint)
			require.NotContains(t, string(public), job.LeaseToken)
			var raw bson.M
			require.NoError(t, db.Collection("ghatd_owned_records").FindOne(ctx, bson.M{"kind": kindWorkItem}).Decode(&raw))
			require.NotContains(t, fmt.Sprint(raw), "owning_financial_receipt")
			require.NotContains(t, fmt.Sprint(raw), "current_worker")
		})
	}
}

// This lease race requires concurrent operations over one guarded page, rather
// than independent table fixtures that cannot overlap ownership.
func TestMongoWorkLeaseRaceFencingAndRestart(t *testing.T) {
	_, _, store, _, clock, ctx := mongoEarnings(t)
	queue := workQueueFixture(t, store, clock)
	require.NoError(t, queue.EnqueuePage(ctx, partnermanager.WorkSignup, partnermanager.DiscoveryCursor{}, partnermanager.DiscoveryCursor{AfterID: "source_0001"}, workCandidates(2)))
	var wg sync.WaitGroup
	results := make(chan []partnermanager.WorkItem, 8)
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			jobs, err := queue.Lease(ctx, partnermanager.WorkSignup, 1)
			results <- jobs
			errs <- err
		}()
	}
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	leased := []partnermanager.WorkItem{}
	for result := range results {
		leased = append(leased, result...)
	}
	require.Len(t, leased, 2)
	require.NotEqual(t, leased[0].ID, leased[1].ID)
	clock.now = clock.now.Add(time.Minute)
	restarted := workQueueFixture(t, store, clock)
	newJobs, err := restarted.Lease(ctx, partnermanager.WorkSignup, 2)
	require.NoError(t, err)
	require.Len(t, newJobs, 2)
	for _, old := range leased {
		require.ErrorIs(t, queue.Retry(ctx, old, "stale_worker"), partnermanager.ErrWorkLeaseLost)
		_, err = queue.Decide(ctx, old, "old_worker", partnermanager.WorkAccepted, "stale_acceptance", "accepted")
		require.ErrorIs(t, err, partnermanager.ErrWorkLeaseLost)
	}
	for _, job := range newJobs {
		require.EqualValues(t, 2, job.Attempts)
		require.NoError(t, restarted.Retry(ctx, job, "owning_unavailable"))
	}
	due, err := restarted.Lease(ctx, partnermanager.WorkSignup, 2)
	require.NoError(t, err)
	require.Empty(t, due)
	clock.now = clock.now.Add(2 * time.Second)
	due, err = restarted.Lease(ctx, partnermanager.WorkSignup, 2)
	require.NoError(t, err)
	require.Len(t, due, 2)
	for _, job := range due {
		require.EqualValues(t, 3, job.Attempts)
		require.Equal(t, "owning_unavailable", job.LastErrorCode)
	}
}
func TestMongoWorkPoisonPrefixDoesNotHideLaterSources(t *testing.T) {
	cases := []struct {
		name string
		kind string
	}{{"signup", partnermanager.WorkSignup}, {"revenue", partnermanager.WorkRevenue}, {"source_recovery", partnermanager.WorkRevenueSource}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, store, _, clock, ctx := mongoEarnings(t)
			queue := workQueueFixture(t, store, clock)
			first := workCandidates(200)
			next := partnermanager.DiscoveryCursor{AfterID: first[199].SourceID}
			if tc.kind == partnermanager.WorkRevenue {
				next = partnermanager.DiscoveryCursor{AfterSequence: 200}
			}
			require.NoError(t, queue.EnqueuePage(ctx, tc.kind, partnermanager.DiscoveryCursor{}, next, first))
			cursor, err := queue.Cursor(ctx, tc.kind)
			require.NoError(t, err)
			later := []partnermanager.WorkCandidate{{SourceID: "source_0200", SourceFingerprint: "later"}}
			require.NoError(t, queue.EnqueuePage(ctx, tc.kind, cursor, partnermanager.DiscoveryCursor{}, later))
			jobs, err := queue.Lease(ctx, tc.kind, 200)
			require.NoError(t, err)
			require.Len(t, jobs, 200)
			laterLeased := false
			for _, job := range jobs {
				if job.SourceID == later[0].SourceID {
					laterLeased = true
				}
				require.NoError(t, queue.Retry(ctx, job, "pending_dependency"))
			}
			// Backoff affects jobs only, not discovery/acceptance. Every remaining due
			// source can be leased on the next batch while the failed prefix is retained.
			nextJobs, err := queue.Lease(ctx, tc.kind, 200)
			require.NoError(t, err)
			require.Len(t, nextJobs, 1)
			if nextJobs[0].SourceID == later[0].SourceID {
				laterLeased = true
			}
			require.True(t, laterLeased)
			cursor, err = queue.Cursor(ctx, tc.kind)
			require.NoError(t, err)
			require.Empty(t, cursor.AfterID)
			require.Zero(t, cursor.AfterSequence)
			for _, candidate := range first {
				item, err := queue.Get(ctx, tc.kind, candidate.SourceID)
				require.NoError(t, err)
				require.Equal(t, partnermanager.WorkPending, item.State)
				require.Nil(t, item.Decision)
			}
		})
	}
}

type workFaultStore struct {
	recordstore.Store
	failAt    int
	uncertain bool
}
type workFaultTx struct {
	recordstore.Tx
	writes, failAt int
}

func (t *workFaultTx) Insert(ctx context.Context, row recordstore.Record) error {
	t.writes++
	if t.writes == t.failAt {
		return injectedFailure
	}
	return t.Tx.Insert(ctx, row)
}
func (t *workFaultTx) Replace(ctx context.Context, row recordstore.Record, expected int64) error {
	t.writes++
	if t.writes == t.failAt {
		return injectedFailure
	}
	return t.Tx.Replace(ctx, row, expected)
}
func (s workFaultStore) Transact(ctx context.Context, key string, fn func(recordstore.Tx) error) error {
	err := s.Store.Transact(ctx, key, func(tx recordstore.Tx) error { return fn(&workFaultTx{Tx: tx, failAt: s.failAt}) })
	if err == nil && s.uncertain {
		return recordstore.ErrUncertain
	}
	return err
}
