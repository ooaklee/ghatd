package partnerstore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/encryption"
	"github.com/ooaklee/ghatd/external/partnermanager"
	"github.com/ooaklee/ghatd/external/repository/recordstore"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
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
			// Receipts now carry a bounded TTL expiry, so this test drives a
			// current clock; the suite's fixed historical clock would make every
			// receipt immediately TTL-eligible.
			clock = &mongoTestClock{time.Now().UTC().Truncate(time.Millisecond)}
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

// Audit disposition for this addition: independent named cases verify idle and
// replaying discovery cannot grow page receipts, and receipts carry bounded
// expiry. Existing unrelated tests still need full audit.
func TestMongoWorkPageReceiptGrowthBounded(t *testing.T) {
	_, _, store, db, _, ctx := mongoEarnings(t)
	// Receipts now carry a bounded expiry enforced by the real TTL monitor, so
	// this test drives a current clock instead of the suite's fixed historical
	// one whose seven-day receipts would already be expired.
	clock := &mongoTestClock{time.Now().UTC().Truncate(time.Millisecond)}
	queue := workQueueFixture(t, store, clock)
	receipts := func() int64 {
		n, err := db.Collection("ghatd_owned_records").CountDocuments(ctx, bson.M{"kind": kindWorkPage})
		require.NoError(t, err)
		return n
	}
	// A real first page seeds position 2 at revision 1.
	page := workCandidates(2)
	require.NoError(t, queue.EnqueuePage(ctx, partnermanager.WorkRevenue, partnermanager.DiscoveryCursor{}, partnermanager.DiscoveryCursor{AfterSequence: 2}, page))
	require.EqualValues(t, 1, receipts())
	seeded, err := queue.Cursor(ctx, partnermanager.WorkRevenue)
	require.NoError(t, err)
	require.EqualValues(t, 2, seeded.AfterSequence)
	// An idle page (empty candidates with a non-zero position to reset) writes
	// no receipt but still resets the cursor.
	require.NoError(t, queue.EnqueuePage(ctx, partnermanager.WorkRevenue, seeded, partnermanager.DiscoveryCursor{}, nil))
	require.EqualValues(t, 1, receipts())
	reset, err := queue.Cursor(ctx, partnermanager.WorkRevenue)
	require.NoError(t, err)
	require.Zero(t, reset.AfterSequence)
	// The same retained page replayed after an intervening revision bump lands
	// on the same receipt instead of writing a new document per tick.
	require.NoError(t, queue.EnqueuePage(ctx, partnermanager.WorkRevenue, reset, partnermanager.DiscoveryCursor{AfterSequence: 2}, page))
	require.EqualValues(t, 1, receipts())
	bumped, err := queue.Cursor(ctx, partnermanager.WorkRevenue)
	require.NoError(t, err)
	require.EqualValues(t, 3, bumped.Revision)
	// A resweep from zero over unchanged sources replays the seeded page body;
	// the cursor has moved on, but the stored receipt proves the replay is safe
	// and idempotent: no failure and no duplicate receipt.
	require.NoError(t, queue.EnqueuePage(ctx, partnermanager.WorkRevenue, reset, partnermanager.DiscoveryCursor{AfterSequence: 2}, page))
	require.EqualValues(t, 1, receipts())
	// Retained receipts carry a bounded expiry the TTL monitor can act on.
	var row struct {
		ExpiresAt *time.Time `bson:"expires_at"`
	}
	err = db.Collection("ghatd_owned_records").FindOne(ctx, bson.M{"kind": kindWorkPage}).Decode(&row)
	require.NoError(t, err)
	require.NotNil(t, row.ExpiresAt)
	require.True(t, row.ExpiresAt.After(clock.now))
}

// Audit disposition for this addition: independent named cases verify replay
// acknowledgement is content-proven, not position-proven, that legacy-format
// receipts still reconcile cross-version lost-ack replays, and that an empty
// page's atomic cursor advance is itself durable replay evidence. Existing
// unrelated tests still need full audit.
func TestMongoWorkPageReplayProofs(t *testing.T) {
	type proof struct {
		name string
		run  func(t *testing.T, queue *partnermanager.WorkQueue, db *mongo.Database, ctx context.Context)
	}
	pos2 := partnermanager.DiscoveryCursor{AfterSequence: 2}
	proofs := []proof{
		{"same_endpoint_different_content_is_proven_not_assumed", func(t *testing.T, queue *partnermanager.WorkQueue, _ *mongo.Database, ctx context.Context) {
			page := workCandidates(2)
			require.NoError(t, queue.EnqueuePage(ctx, partnermanager.WorkRevenue, partnermanager.DiscoveryCursor{}, pos2, page))
			at, err := queue.Cursor(ctx, partnermanager.WorkRevenue)
			require.NoError(t, err)
			// A different page reaching the same endpoint is never falsely
			// acknowledged: its extra source is actually retained through the
			// compare-and-set commit, while a changed immutable fingerprint on
			// an already-retained source conflicts.
			require.NoError(t, queue.EnqueuePage(ctx, partnermanager.WorkRevenue, at, pos2, append(page, workCandidateAt(2))))
			_, err = queue.Get(ctx, partnermanager.WorkRevenue, "source_0002")
			require.NoError(t, err)
			require.ErrorIs(t, queue.EnqueuePage(ctx, partnermanager.WorkRevenue, at, pos2, changedFingerprint(page)), partnermanager.ErrWorkConflict)
		}},
		{"legacy_receipt_reconciles_cross_version_replay", func(t *testing.T, queue *partnermanager.WorkQueue, db *mongo.Database, ctx context.Context) {
			page := workCandidates(2)
			// Simulate a pre-fix receipt for the same durable page: the legacy
			// digest hashed the expected cursor revision. Receipt payloads are
			// encrypted by the store, so the legacy document is written through
			// the store transaction rather than a raw insert.
			store, err := NewWorkRepository(workStore(t, db))
			require.NoError(t, err)
			at := partnermanager.DiscoveryCursor{}
			require.NoError(t, store.EnqueueWorkPageLegacy(ctx, "fixture-program", partnermanager.WorkRevenue, at, pos2, legacyItems(t, queue, page)))
			moved, err := queue.Cursor(ctx, partnermanager.WorkRevenue)
			require.NoError(t, err)
			require.EqualValues(t, 1, moved.Revision)
			// An intervening empty advance moves the cursor past the legacy page.
			require.NoError(t, queue.EnqueuePage(ctx, partnermanager.WorkRevenue, moved, partnermanager.DiscoveryCursor{}, nil))
			// Replaying the legacy page after the cursor moved on must reconcile
			// against the legacy receipt instead of conflicting.
			require.NoError(t, queue.EnqueuePage(ctx, partnermanager.WorkRevenue, at, pos2, page))
		}},
		{"empty_page_reset_is_replay_safe", func(t *testing.T, queue *partnermanager.WorkQueue, _ *mongo.Database, ctx context.Context) {
			page := workCandidates(2)
			require.NoError(t, queue.EnqueuePage(ctx, partnermanager.WorkRevenue, partnermanager.DiscoveryCursor{}, pos2, page))
			at, err := queue.Cursor(ctx, partnermanager.WorkRevenue)
			require.NoError(t, err)
			// An empty reset's whole commit is the cursor advance: replaying it
			// is acknowledged by the committed position itself.
			require.NoError(t, queue.EnqueuePage(ctx, partnermanager.WorkRevenue, at, partnermanager.DiscoveryCursor{}, nil))
			require.NoError(t, queue.EnqueuePage(ctx, partnermanager.WorkRevenue, at, partnermanager.DiscoveryCursor{}, nil))
			reset, err := queue.Cursor(ctx, partnermanager.WorkRevenue)
			require.NoError(t, err)
			require.Zero(t, reset.AfterSequence)
		}},
	}
	for _, p := range proofs {
		t.Run(p.name, func(t *testing.T) {
			_, _, store, db, _, ctx := mongoEarnings(t)
			p.run(t, workQueueFixture(t, store, &mongoTestClock{time.Now().UTC().Truncate(time.Millisecond)}), db, ctx)
		})
	}
}

func workCandidateAt(i int) partnermanager.WorkCandidate {
	return partnermanager.WorkCandidate{SourceID: fmt.Sprintf("source_%04d", i), SourceFingerprint: fmt.Sprintf("source_digest_%04d", i)}
}

func changedFingerprint(page []partnermanager.WorkCandidate) []partnermanager.WorkCandidate {
	out := append([]partnermanager.WorkCandidate(nil), page...)
	out[0].SourceFingerprint = "different_digest"
	return out
}

// legacyItems rehydrates queue-shaped candidates into stored WorkItems the way
// the queue does, so the legacy receipt writer sees realistic rows.
func legacyItems(t *testing.T, queue *partnermanager.WorkQueue, page []partnermanager.WorkCandidate) []partnermanager.WorkItem {
	t.Helper()
	out := make([]partnermanager.WorkItem, 0, len(page))
	for _, c := range page {
		out = append(out, partnermanager.WorkItem{ID: legacyWorkID("fixture-program", partnermanager.WorkRevenue, c.SourceID), ProgramID: "fixture-program", Kind: partnermanager.WorkRevenue, State: partnermanager.WorkPending, SourceID: c.SourceID, SourceFingerprint: c.SourceFingerprint, CreatedAt: time.Now().UTC(), NextAttemptAt: time.Now().UTC()})
	}
	return out
}

// workStore rebuilds a store handle over an existing database for test-only
// legacy-format writes.
func workStore(t *testing.T, db *mongo.Database) recordstore.Store {
	t.Helper()
	cipher, err := encryption.NewPayloadCipher([]byte("01234567890123456789012345678901"))
	require.NoError(t, err)
	store, err := recordstore.NewMongoStoreFromDatabase(db, cipher)
	require.NoError(t, err)
	return store
}

// EnqueueWorkPageLegacy writes a receipt under the pre-fix page identity that
// hashed the expected cursor revision, advancing the cursor identically. It
// exists only so tests can reproduce historical documents; production code
// always calls EnqueueWorkPage.
func (r *WorkRepository) EnqueueWorkPageLegacy(ctx context.Context, program, kind string, expected, next partnermanager.DiscoveryCursor, items []partnermanager.WorkItem) error {
	type source struct {
		ID, Fingerprint string
		DueAt           *time.Time `json:",omitempty"`
	}
	sources := make([]source, 0, len(items))
	for _, item := range items {
		v := source{ID: item.SourceID, Fingerprint: item.SourceFingerprint}
		if !item.InitialDueAt.IsZero() {
			due := item.InitialDueAt.UTC()
			v.DueAt = &due
		}
		sources = append(sources, v)
	}
	body, _ := json.Marshal([]any{program, kind, expected.Revision, expected.AfterID, expected.AfterSequence, next.AfterID, next.AfterSequence, sources})
	sum := sha256.Sum256(body)
	pageID := hex.EncodeToString(sum[:])
	part := workPartition(program, kind)
	return workMapped(r.store.Transact(ctx, part, func(tx recordstore.Tx) error {
		_, old, err := getCursor(ctx, tx, program, kind)
		absent := singleCauseIs(err, partnermanager.ErrNotFound)
		if err != nil && !absent {
			return err
		}
		row := recordstore.Record{ID: pageID, Kind: kindWorkPage, Partition: part, Revision: 1, Data: body}
		if err := tx.Insert(ctx, row); err != nil {
			return err
		}
		next.Revision = expected.Revision + 1
		cursorRow, err := recordstore.NewRecord(kindWorkCursor, workDigest(program, kind), part, next.Revision, next)
		if err != nil {
			return err
		}
		if absent {
			return tx.Insert(ctx, cursorRow)
		}
		return tx.Replace(ctx, cursorRow, old.Revision)
	}))
}

// legacyWorkID mirrors the manager's unexported workID derivation for tests.
func legacyWorkID(program, kind, source string) string {
	body, _ := json.Marshal([]string{program, kind, source})
	sum := sha256.Sum256(body)
	return "work_" + hex.EncodeToString(sum[:])
}

// Audit disposition for this addition: named cases extend the replay contract:
// a legacy-format empty-page reset is acknowledged from its legacy receipt
// after intervening advancement, and a replay whose receipt has expired
// conflicts but recovers through a fresh cursor read and rediscovery.
func TestMongoWorkPageReplayContract(t *testing.T) {
	type proof struct {
		name string
		run  func(t *testing.T, queue *partnermanager.WorkQueue, repo *WorkRepository, db *mongo.Database, ctx context.Context)
	}
	pos2 := partnermanager.DiscoveryCursor{AfterSequence: 2}
	zero := partnermanager.DiscoveryCursor{}
	proofs := []proof{
		{"legacy_empty_reset_reconciles_after_advance", func(t *testing.T, queue *partnermanager.WorkQueue, repo *WorkRepository, _ *mongo.Database, ctx context.Context) {
			page := workCandidates(2)
			require.NoError(t, queue.EnqueuePage(ctx, partnermanager.WorkRevenue, zero, pos2, page))
			at, err := queue.Cursor(ctx, partnermanager.WorkRevenue)
			require.NoError(t, err)
			// Pre-fix code committed empty resets with a legacy receipt.
			require.NoError(t, repo.EnqueueWorkPageLegacy(ctx, "fixture-program", partnermanager.WorkRevenue, at, zero, nil))
			after, err := queue.Cursor(ctx, partnermanager.WorkRevenue)
			require.NoError(t, err)
			require.Zero(t, after.AfterSequence)
			// An intervening non-empty advance moves the cursor past the reset.
			require.NoError(t, queue.EnqueuePage(ctx, partnermanager.WorkRevenue, after, pos2, page))
			// Retrying the legacy empty reset after the advance reconciles from
			// the legacy receipt instead of conflicting.
			require.NoError(t, queue.EnqueuePage(ctx, partnermanager.WorkRevenue, at, zero, nil))
		}},
		{"expired_receipt_conflicts_then_recovers_via_cursor", func(t *testing.T, queue *partnermanager.WorkQueue, _ *WorkRepository, db *mongo.Database, ctx context.Context) {
			page := workCandidates(2)
			require.NoError(t, queue.EnqueuePage(ctx, partnermanager.WorkRevenue, zero, pos2, page))
			at, err := queue.Cursor(ctx, partnermanager.WorkRevenue)
			require.NoError(t, err)
			require.NoError(t, queue.EnqueuePage(ctx, partnermanager.WorkRevenue, at, zero, nil))
			// Simulate receipt expiry: the TTL monitor removes the receipt.
			_, err = db.Collection("ghatd_owned_records").DeleteMany(ctx, bson.M{"kind": kindWorkPage})
			require.NoError(t, err)
			// The bounded replay contract: an old retry conflicts...
			require.ErrorIs(t, queue.EnqueuePage(ctx, partnermanager.WorkRevenue, zero, pos2, page), partnermanager.ErrWorkConflict)
			// ...and the caller recovers by re-reading the cursor and
			// restarting discovery from the committed position.
			current, err := queue.Cursor(ctx, partnermanager.WorkRevenue)
			require.NoError(t, err)
			require.Zero(t, current.AfterSequence)
			require.NoError(t, queue.EnqueuePage(ctx, partnermanager.WorkRevenue, current, pos2, page))
		}},
	}
	for _, p := range proofs {
		t.Run(p.name, func(t *testing.T) {
			_, _, store, db, _, ctx := mongoEarnings(t)
			repo, err := NewWorkRepository(store)
			require.NoError(t, err)
			p.run(t, workQueueFixture(t, store, &mongoTestClock{time.Now().UTC().Truncate(time.Millisecond)}), repo, db, ctx)
		})
	}
}
