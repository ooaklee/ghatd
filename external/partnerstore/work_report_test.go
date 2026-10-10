package partnerstore

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/partnermanager"
	"github.com/ooaklee/ghatd/external/repository/recordstore"
	"github.com/stretchr/testify/require"
)

// Audit disposition: named page-boundary cases and isolated encrypted Mongo
// journeys verify complete reads, pending-only selection and failure/retry reset.
type workReportPageTx struct {
	recordstore.Tx
	rows  []recordstore.Record
	mode  string
	calls int
}

func (t *workReportPageTx) Find(_ context.Context, q recordstore.Query) ([]recordstore.Record, error) {
	t.calls++
	if t.mode == "failed_later_page" && t.calls == 2 {
		return nil, recordstore.ErrUnavailable
	}
	var out []recordstore.Record
	for _, r := range t.rows {
		if r.ID > q.AfterID {
			out = append(out, r)
			if len(out) == q.Limit {
				break
			}
		}
	}
	if len(out) > 0 {
		switch t.mode {
		case "wrong_state":
			out[0].State = partnermanager.WorkComplete
		case "wrong_partition":
			out[0].Partition = "other"
		case "wrong_kind":
			out[0].Kind = kindWorkCursor
		case "unordered":
			if len(out) > 1 {
				out[0], out[1] = out[1], out[0]
			}
		case "unexpected_expiry":
			at := time.Now().UTC()
			out[0].ExpiresAt = &at
		case "oversized_page":
			out = append(out, out[len(out)-1])
		}
	}
	return out, nil
}
func TestPendingWorkReportPages(t *testing.T) {
	for _, tc := range []struct {
		name, mode   string
		rows, budget int
		want         error
		calls        int
	}{
		{"complete_multiple_pages", "", 205, 205, nil, 2},
		{"exact_boundary_requires_empty_probe", "", 200, 200, nil, 2},
		{"one_extra_row_is_capacity", "", 201, 200, partnermanager.ErrReportCapacity, 2},
		{"zero_remaining_budget_probes_next_scope", "", 1, 0, partnermanager.ErrReportCapacity, 1},
		{"failed_later_page_discards_prefix", "failed_later_page", 205, 205, recordstore.ErrUnavailable, 2},
		{"state_filter_not_honoured", "wrong_state", 2, 10, recordstore.ErrUnavailable, 1},
		{"foreign_partition", "wrong_partition", 2, 10, recordstore.ErrUnavailable, 1},
		{"foreign_kind", "wrong_kind", 2, 10, recordstore.ErrUnavailable, 1},
		{"unordered_page", "unordered", 2, 10, recordstore.ErrUnavailable, 1},
		{"queue_cannot_have_expiry", "unexpected_expiry", 2, 10, recordstore.ErrUnavailable, 1},
		{"adapter_violates_page_limit", "oversized_page", 200, 300, recordstore.ErrUnavailable, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tx := &workReportPageTx{mode: tc.mode}
			for n := 0; n < tc.rows; n++ {
				tx.rows = append(tx.rows, recordstore.Record{Kind: kindWorkItem, Partition: "scope", State: partnermanager.WorkPending, ID: fmt.Sprintf("row_%05d", n)})
			}
			budget := tc.budget
			out, err := pendingWorkRecords(context.Background(), tx, recordstore.Query{Kind: kindWorkItem, Partition: "scope", State: partnermanager.WorkPending}, &budget)
			require.ErrorIs(t, err, tc.want)
			require.Equal(t, tc.calls, tx.calls)
			if tc.want != nil {
				require.Empty(t, out)
			} else {
				require.Len(t, out, tc.rows)
				require.Equal(t, tc.budget-tc.rows, budget)
			}
		})
	}
}

type workReportReadStore struct {
	recordstore.Store
	retry                     bool
	failPartition, failCursor string
	reads                     int
	queries                   []recordstore.Query
	afterFirstScope           func() error
	firstScopeHooked          bool
}
type workReportReadTx struct {
	recordstore.Tx
	owner *workReportReadStore
}

func (t workReportReadTx) Find(ctx context.Context, q recordstore.Query) ([]recordstore.Record, error) {
	t.owner.queries = append(t.owner.queries, q)
	if q.Partition == t.owner.failPartition {
		return nil, recordstore.ErrUnavailable
	}
	rows, err := t.Tx.Find(ctx, q)
	if err == nil && !t.owner.firstScopeHooked && t.owner.afterFirstScope != nil {
		t.owner.firstScopeHooked = true
		if err := t.owner.afterFirstScope(); err != nil {
			return nil, err
		}
	}
	return rows, err
}
func (t workReportReadTx) Get(ctx context.Context, kind, id string) (recordstore.Record, error) {
	if kind == kindWorkCursor && id == t.owner.failCursor {
		return recordstore.Record{}, errors.Join(recordstore.ErrNotFound, recordstore.ErrUnavailable)
	}
	return t.Tx.Get(ctx, kind, id)
}
func (s *workReportReadStore) Read(ctx context.Context, fn func(recordstore.Tx) error) error {
	s.reads++
	return s.Store.Read(ctx, func(tx recordstore.Tx) error {
		bound := workReportReadTx{tx, s}
		if s.retry {
			if err := fn(bound); err != nil {
				return err
			}
		}
		return fn(bound)
	})
}

func enqueueReportSources(t *testing.T, ctx context.Context, q *partnermanager.WorkQueue, kind string, n int) {
	t.Helper()
	for offset := 0; offset < n; offset += 200 {
		cursor, err := q.Cursor(ctx, kind)
		require.NoError(t, err)
		var candidates []partnermanager.WorkCandidate
		end := offset + 200
		if end > n {
			end = n
		}
		for i := offset; i < end; i++ {
			candidate := partnermanager.WorkCandidate{SourceID: fmt.Sprintf("source_%05d", i), SourceFingerprint: fmt.Sprintf("private_digest_%05d", i)}
			if kind == partnermanager.WorkMaturity {
				candidate.DueAt = time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
			}
			candidates = append(candidates, candidate)
		}
		next := partnermanager.DiscoveryCursor{AfterID: candidates[len(candidates)-1].SourceID}
		if kind == partnermanager.WorkRevenue {
			next = partnermanager.DiscoveryCursor{AfterSequence: int64(end)}
		}
		require.NoError(t, q.EnqueuePage(ctx, kind, cursor, next, candidates))
	}
}

func TestMongoWorkBacklogCompleteOwningSnapshot(t *testing.T) {
	for _, tc := range []struct {
		name                         string
		retry, failQuery, failCursor bool
	}{
		{"all_pending_kinds_and_cursors", false, false, false},
		{"callback_retry_resets_output_and_budget", true, false, false},
		{"later_kind_failure_discards_earlier_pending", false, true, false},
		{"joined_cursor_absence_is_outage_not_empty_scope", false, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, store, _, clock, ctx := mongoEarnings(t)
			q := workQueueFixture(t, store, clock)
			empty, err := q.GetBacklog(ctx)
			require.NoError(t, err)
			require.Zero(t, empty.Counts.Pending)
			require.Len(t, empty.Kinds, 4)
			enqueueReportSources(t, ctx, q, partnermanager.WorkSignup, 205)
			enqueueReportSources(t, ctx, q, partnermanager.WorkRevenue, 2)
			enqueueReportSources(t, ctx, q, partnermanager.WorkRevenueSource, 1)
			enqueueReportSources(t, ctx, q, partnermanager.WorkMaturity, 1)
			jobs, err := q.Lease(ctx, partnermanager.WorkSignup, 2)
			require.NoError(t, err)
			require.Len(t, jobs, 2)
			decision, err := q.Decide(ctx, jobs[0], "worker", partnermanager.WorkAccepted, "owner_receipt", "accepted")
			require.NoError(t, err)
			require.NoError(t, q.Complete(ctx, jobs[0], decision.ID))
			require.NoError(t, q.Retry(ctx, jobs[1], "owning_unavailable"))
			revenue, err := q.Lease(ctx, partnermanager.WorkRevenue, 1)
			require.NoError(t, err)
			require.Len(t, revenue, 1)
			_, err = q.Decide(ctx, revenue[0], "worker", partnermanager.WorkNoEntitlement, "no_entitlement_receipt", "no_entitlement")
			require.NoError(t, err)
			observer := &workReportReadStore{Store: store, retry: tc.retry}
			if tc.failQuery {
				observer.failPartition = workPartition("fixture-program", partnermanager.WorkRevenue)
			}
			if tc.failCursor {
				observer.failCursor = workDigest("fixture-program", partnermanager.WorkRevenue)
			}
			readQueue := workQueueFixture(t, observer, clock)
			out, err := readQueue.GetBacklog(ctx)
			require.Equal(t, 1, observer.reads)
			for _, query := range observer.queries {
				require.Equal(t, kindWorkItem, query.Kind)
				require.Equal(t, partnermanager.WorkPending, query.State)
				require.LessOrEqual(t, query.Limit, 200)
			}
			if tc.failQuery || tc.failCursor {
				require.ErrorIs(t, err, partnermanager.ErrUnavailable)
				require.Empty(t, out)
				return
			}
			require.NoError(t, err)
			require.EqualValues(t, 208, out.Counts.Pending)
			require.EqualValues(t, 206, out.Counts.Ready)
			require.EqualValues(t, 1, out.Counts.Backoff)
			require.EqualValues(t, 1, out.Counts.Leased)
			require.EqualValues(t, 2, out.Counts.AttemptedPending)
			require.EqualValues(t, 1, out.Counts.DecisionAwaitingCompletion)
			clock.now = clock.now.Add(time.Minute)
			expired, err := readQueue.GetBacklog(ctx)
			require.NoError(t, err)
			require.EqualValues(t, 208, expired.Counts.Ready)
			require.Zero(t, expired.Counts.Backoff)
			require.Zero(t, expired.Counts.Leased)
			require.Equal(t, out.Revision, expired.Revision, "clock changes scheduling classification, not persisted state revision")
		})
	}
}

func TestMongoWorkBacklogOverlappingMutation(t *testing.T) {
	for _, tc := range []struct{ name, action string }{
		{"lease_commits_between_kind_reads", "lease"},
		{"retry_commits_between_kind_reads", "retry"},
		{"completion_commits_between_kind_reads", "complete"},
		{"discovery_page_commits_between_kind_reads", "enqueue"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, store, _, clock, ctx := mongoEarnings(t)
			q := workQueueFixture(t, store, clock)
			for _, kind := range []string{partnermanager.WorkSignup, partnermanager.WorkRevenue, partnermanager.WorkRevenueSource, partnermanager.WorkMaturity} {
				enqueueReportSources(t, ctx, q, kind, 1)
			}
			var job partnermanager.WorkItem
			var decision partnermanager.WorkDecision
			if tc.action == "retry" || tc.action == "complete" {
				jobs, err := q.Lease(ctx, partnermanager.WorkRevenue, 1)
				require.NoError(t, err)
				require.Len(t, jobs, 1)
				job = jobs[0]
				if tc.action == "complete" {
					decision, err = q.Decide(ctx, job, "worker", partnermanager.WorkAccepted, "owner_receipt", "accepted")
					require.NoError(t, err)
				}
			}
			before, err := q.GetBacklog(ctx)
			require.NoError(t, err)
			observer := &workReportReadStore{Store: store}
			// The first scope has established the read transaction's snapshot.
			// A distinct writer commits before the second kind is queried, making
			// a mixed pre/post report deterministic if isolation is missing.
			observer.afterFirstScope = func() error {
				result := make(chan error, 1)
				go func() {
					var err error
					switch tc.action {
					case "lease":
						_, err = q.Lease(ctx, partnermanager.WorkRevenue, 1)
					case "retry":
						err = q.Retry(ctx, job, "owning_unavailable")
					case "complete":
						err = q.Complete(ctx, job, decision.ID)
					case "enqueue":
						var cursor partnermanager.DiscoveryCursor
						cursor, err = q.Cursor(ctx, partnermanager.WorkRevenue)
						if err == nil {
							err = q.EnqueuePage(ctx, partnermanager.WorkRevenue, cursor, partnermanager.DiscoveryCursor{AfterSequence: 2}, []partnermanager.WorkCandidate{{SourceID: "later_source", SourceFingerprint: "later_private_digest"}})
						}
					}
					result <- err
				}()
				return <-result
			}
			during, err := workQueueFixture(t, observer, clock).GetBacklog(ctx)
			require.NoError(t, err)
			require.True(t, observer.firstScopeHooked)
			require.Equal(t, before, during, "a concurrent commit must not mix state or cursor generations across kinds")
			after, err := q.GetBacklog(ctx)
			require.NoError(t, err)
			require.NotEqual(t, before.Revision, after.Revision)
			switch tc.action {
			case "lease":
				require.EqualValues(t, 1, after.Counts.Leased)
			case "retry":
				require.EqualValues(t, 1, after.Counts.Backoff)
				require.Zero(t, after.Counts.Leased)
			case "complete":
				require.EqualValues(t, 3, after.Counts.Pending)
				require.Zero(t, after.Counts.DecisionAwaitingCompletion)
			case "enqueue":
				require.EqualValues(t, 5, after.Counts.Pending)
			}
		})
	}
}

// Standalone audit exception: one retained encrypted queue grows across its
// complete-report boundary. Rebuilding two 10,000-row fixtures would duplicate
// expensive setup instead of testing this exact persisted before/after state.
func TestMongoWorkBacklogCombinedCapacityThenOverflow(t *testing.T) {
	_, _, store, _, clock, _ := mongoEarnings(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	t.Cleanup(cancel)
	program := "fixture-program"
	kinds := []string{partnermanager.WorkSignup, partnermanager.WorkRevenue, partnermanager.WorkRevenueSource, partnermanager.WorkMaturity}
	makeItem := func(n int) partnermanager.WorkItem {
		kind := kinds[n%4]
		source := fmt.Sprintf("source_%05d", n)
		item := partnermanager.WorkItem{ID: "work_" + workDigest(program, kind, source), ProgramID: program, Kind: kind, State: partnermanager.WorkPending, SourceID: source, SourceFingerprint: "private_source_fingerprint", CreatedAt: clock.now, NextAttemptAt: clock.now}
		if kind == partnermanager.WorkMaturity {
			item.InitialDueAt = clock.now
		}
		return item
	}
	require.NoError(t, store.Transact(ctx, "capacity_fixture", func(tx recordstore.Tx) error {
		for _, kind := range kinds {
			cursor := partnermanager.DiscoveryCursor{Revision: 1}
			row, err := recordstore.NewRecord(kindWorkCursor, workDigest(program, kind), workPartition(program, kind), 1, cursor)
			if err != nil {
				return err
			}
			if err := tx.Insert(ctx, row); err != nil {
				return err
			}
		}
		for n := 0; n < partnermanager.WorkReportCapacity; n++ {
			if err := writeWork(ctx, tx, makeItem(n), nil); err != nil {
				return err
			}
		}
		return nil
	}))
	q := workQueueFixture(t, store, clock)
	exact, err := q.GetBacklog(ctx)
	require.NoError(t, err)
	require.EqualValues(t, partnermanager.WorkReportCapacity, exact.Counts.Pending)
	counts := []int64{exact.Kinds[0].Counts.Pending, exact.Kinds[1].Counts.Pending, exact.Kinds[2].Counts.Pending, exact.Kinds[3].Counts.Pending}
	sort.Slice(counts, func(i, j int) bool { return counts[i] < counts[j] })
	require.Equal(t, []int64{2500, 2500, 2500, 2500}, counts)
	require.NoError(t, store.Transact(ctx, "capacity_fixture", func(tx recordstore.Tx) error {
		return writeWork(ctx, tx, makeItem(partnermanager.WorkReportCapacity), nil)
	}))
	over, err := q.GetBacklog(ctx)
	require.ErrorIs(t, err, partnermanager.ErrReportCapacity)
	require.Empty(t, over)
}
