package billinglifecycle

import (
	"context"
	"math"
	"sync"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/repository/recordstore"
	"github.com/stretchr/testify/require"
)

var _ ExecutionRepository = (*RecordExecutionRepository)(nil)

type executionTestClock struct {
	mu       sync.Mutex
	at       time.Time
	sequence []time.Time
}

func (c *executionTestClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.sequence) > 0 {
		at := c.sequence[0]
		c.sequence = c.sequence[1:]
		return at
	}
	return c.at
}
func (c *executionTestClock) set(at time.Time) { c.mu.Lock(); defer c.mu.Unlock(); c.at = at }

// Encrypted storage protocol cases use descriptor fixtures, not owning-source
// discovery or authority. Native original-preservation cases below use actual
// billing originals. No provider, scheduler or platform proof is implied.
func nativeExecutionFixture(t *testing.T) (*inputNativeFixture, *RecordExecutionRepository, *executionTestClock, ScheduledJob) {
	t.Helper()
	f, r, jobs := nativeScheduleFixture(t)
	j := jobs[0]
	require.NoError(t, r.CommitScan(f.ctx, ScheduleCommit{Scope: j.Source.Scope, Lane: ColdLane, Writes: []ScheduleWrite{{0, j}}}))
	clock := &executionTestClock{at: f.clock.at}
	e, err := NewRecordExecutionRepository(f.reply, clock)
	require.NoError(t, err)
	return f, e, clock, j
}

func TestExecutionRepositoryEncryptedAcquire(t *testing.T) {
	for _, tc := range []struct {
		name string
		want error
	}{
		{"confirmed", nil}, {"future_due", recordstore.ErrConflict}, {"retired", recordstore.ErrConflict},
		{"stale_revision", recordstore.ErrConflict}, {"expired_proposal", recordstore.ErrConflict},
		{"invalid_token", recordstore.ErrInvalid}, {"zero_clock", recordstore.ErrUnavailable},
		{"attempt_overflow", recordstore.ErrConflict}, {"fence_overflow", recordstore.ErrConflict},
		{"revision_overflow", recordstore.ErrInvalid}, {"wrong_original", recordstore.ErrConflict},
		{"lost_reply", recordstore.ErrUncertain}, {"canceled", context.Canceled},
		{"expires_before_write", recordstore.ErrConflict}, {"clock_moves_backwards", recordstore.ErrConflict},
		{"active_lease_refused", nil}, {"expired_lease_replaced", nil},
		{"scan_cannot_create_lease", nil}, {"scan_cannot_clear_execution", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, r, clock, original := nativeExecutionFixture(t)
			q := LeaseRequest{Source: original.Source, ExpectedRevision: original.Revision, Actor: "worker-one", Token: "random-token-one", Until: clock.at.Add(time.Minute)}
			ctx := f.ctx
			switch tc.name {
			case "future_due", "retired", "attempt_overflow", "fence_overflow", "revision_overflow":
				j := original
				j.Revision++
				switch tc.name {
				case "future_due":
					j.NextAttemptAt = clock.at.Add(time.Hour)
				case "retired":
					j.Lane = RetiredLane
				case "attempt_overflow":
					j.Attempts = math.MaxInt64
				case "fence_overflow":
					j.Fence = math.MaxInt64
				case "revision_overflow":
					q.ExpectedRevision = math.MaxInt64
				}
				if tc.name != "revision_overflow" {
					seedDiscoveryExecution(t, f, j, original.Revision)
					original = j
					q.ExpectedRevision = j.Revision
				}
			case "stale_revision":
				q.ExpectedRevision++
			case "expired_proposal":
				q.Until = clock.at
			case "invalid_token":
				q.Token = "\n"
			case "zero_clock":
				clock.set(time.Time{})
			case "wrong_original":
				q.Source.PrincipalID = "different"
			case "lost_reply":
				f.reply.lose = true
			case "canceled":
				c, cancel := context.WithCancel(ctx)
				cancel()
				ctx = c
			case "expires_before_write":
				clock.sequence = []time.Time{clock.at, q.Until}
			case "clock_moves_backwards":
				clock.sequence = []time.Time{clock.at, clock.at.Add(-time.Second)}
			}
			j, err := r.Acquire(ctx, q)
			if tc.want != nil {
				require.ErrorIs(t, err, tc.want)
				require.Equal(t, ScheduledJob{}, j)
			} else {
				require.NoError(t, err)
				require.Equal(t, original.Revision+1, j.Revision)
				require.Equal(t, original.Attempts+1, j.Attempts)
				require.Equal(t, original.Fence+1, j.Fence)
				require.Equal(t, original.Source, j.Source)
			}
			f.reply.lose = false
			stored, err := r.ReadJob(f.ctx, original.Source)
			require.NoError(t, err)
			if tc.want != nil && tc.name != "lost_reply" {
				require.Equal(t, original, stored)
				return
			}
			if tc.name == "lost_reply" {
				require.Equal(t, q.Token, stored.LeaseToken)
				require.Equal(t, original.Revision+1, stored.Revision)
				retry, err := r.Acquire(f.ctx, q)
				require.ErrorIs(t, err, recordstore.ErrConflict)
				require.Equal(t, ScheduledJob{}, retry)
				return
			}
			require.Equal(t, j, stored)
			switch tc.name {
			case "active_lease_refused", "expired_lease_replaced":
				other := q
				other.ExpectedRevision = j.Revision
				other.Actor, other.Token = "worker-two", "random-token-two"
				if tc.name == "expired_lease_replaced" {
					clock.set(q.Until)
					other.Until = q.Until.Add(time.Minute)
				}
				next, err := r.Acquire(f.ctx, other)
				if tc.name == "active_lease_refused" {
					require.ErrorIs(t, err, recordstore.ErrConflict)
					require.Equal(t, ScheduledJob{}, next)
				} else {
					require.NoError(t, err)
					require.Equal(t, j.Fence+1, next.Fence)
					old, err := r.CheckLease(f.ctx, JobLease(j))
					require.ErrorIs(t, err, recordstore.ErrConflict)
					require.Equal(t, ScheduledJob{}, old)
				}
			case "scan_cannot_create_lease", "scan_cannot_clear_execution":
				write := j
				write.Revision++
				want := recordstore.ErrInvalid
				if tc.name == "scan_cannot_clear_execution" {
					write.Attempts, write.Fence = 0, 0
					write.LeaseActor, write.LeaseToken, write.LeasedUntil = "", "", time.Time{}
					want = recordstore.ErrConflict
				}
				err := r.CommitScan(f.ctx, ScheduleCommit{Scope: j.Source.Scope, Lane: ColdLane, Expected: ScheduleCursor{Revision: 1}, Writes: []ScheduleWrite{{j.Revision, write}}})
				require.ErrorIs(t, err, want)
				unchanged, err := r.ReadJob(f.ctx, j.Source)
				require.NoError(t, err)
				require.Equal(t, j, unchanged)
			}
		})
	}
}

func TestExecutionRepositoryEncryptedDisposition(t *testing.T) {
	for _, tc := range []struct {
		name string
		want error
	}{
		{"confirmed_retry", nil}, {"lane_transition", nil}, {"stale_revision", recordstore.ErrConflict},
		{"wrong_actor", recordstore.ErrConflict}, {"wrong_token", recordstore.ErrConflict},
		{"wrong_epoch", recordstore.ErrConflict}, {"wrong_expiry", recordstore.ErrConflict},
		{"expired_lease", recordstore.ErrConflict}, {"expires_before_write", recordstore.ErrConflict},
		{"clock_moves_backwards", recordstore.ErrConflict}, {"past_due_disposition", recordstore.ErrConflict},
		{"retirement_refused", recordstore.ErrInvalid}, {"lost_reply", recordstore.ErrUncertain},
		{"canceled", context.Canceled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, r, clock, original := nativeExecutionFixture(t)
			j, err := r.Acquire(f.ctx, LeaseRequest{Source: original.Source, ExpectedRevision: original.Revision, Actor: "worker-one", Token: "random-token-one", Until: clock.at.Add(time.Minute)})
			require.NoError(t, err)
			h := JobLease(j)
			valid, err := r.CheckLease(f.ctx, h)
			require.NoError(t, err)
			require.Equal(t, j, valid)
			q := LeaseDisposition{Handle: h, Lane: ColdLane, NextAttemptAt: clock.at.Add(time.Hour)}
			ctx := f.ctx
			switch tc.name {
			case "lane_transition":
				q.Lane = RefreshLane
			case "stale_revision":
				q.Handle.Revision++
			case "wrong_actor":
				q.Handle.Actor = "worker-two"
			case "wrong_token":
				q.Handle.Token = "different-token"
			case "wrong_epoch":
				q.Handle.Fence++
			case "wrong_expiry":
				q.Handle.Until = q.Handle.Until.Add(time.Second)
			case "expired_lease":
				clock.set(h.Until)
			case "expires_before_write":
				clock.sequence = []time.Time{clock.at, h.Until}
			case "clock_moves_backwards":
				clock.sequence = []time.Time{clock.at, clock.at.Add(-time.Second)}
			case "past_due_disposition":
				q.NextAttemptAt = clock.at
			case "retirement_refused":
				q.Lane = RetiredLane
			case "lost_reply":
				f.reply.lose = true
			case "canceled":
				c, cancel := context.WithCancel(ctx)
				cancel()
				ctx = c
			}
			out, err := r.Release(ctx, q)
			if tc.want != nil {
				require.ErrorIs(t, err, tc.want)
				require.Equal(t, ScheduledJob{}, out)
			} else {
				require.NoError(t, err)
				require.Equal(t, j.Revision+1, out.Revision)
				require.Equal(t, j.Fence, out.Fence)
				require.Equal(t, j.Attempts, out.Attempts)
				require.Empty(t, out.LeaseActor)
				require.Empty(t, out.LeaseToken)
				require.True(t, out.LeasedUntil.IsZero())
				require.Equal(t, q.Lane, out.Lane)
			}
			f.reply.lose = false
			stored, err := r.ReadJob(f.ctx, original.Source)
			require.NoError(t, err)
			if tc.want != nil && tc.name != "lost_reply" {
				require.Equal(t, j, stored)
			} else {
				require.Equal(t, j.Revision+1, stored.Revision)
				require.Empty(t, stored.LeaseToken)
				old, err := r.CheckLease(f.ctx, h)
				require.ErrorIs(t, err, recordstore.ErrConflict)
				require.Equal(t, ScheduledJob{}, old)
			}
		})
	}
}

func TestExecutionRepositoryEncryptedConcurrentAcquire(t *testing.T) {
	for _, name := range []string{"same_worker", "replacement_worker"} {
		t.Run(name, func(t *testing.T) {
			f, r, clock, j := nativeExecutionFixture(t)
			first := LeaseRequest{Source: j.Source, ExpectedRevision: j.Revision, Actor: "worker-one", Token: "token-one", Until: clock.at.Add(time.Minute)}
			second := first
			second.Token = "token-two"
			if name == "replacement_worker" {
				second.Actor = "worker-two"
			}
			start, errs := make(chan struct{}), make(chan error, 2)
			var wg sync.WaitGroup
			for _, q := range []LeaseRequest{first, second} {
				wg.Add(1)
				go func(q LeaseRequest) { defer wg.Done(); <-start; _, err := r.Acquire(f.ctx, q); errs <- err }(q)
			}
			close(start)
			wg.Wait()
			close(errs)
			success, conflicts := 0, 0
			for err := range errs {
				if err == nil {
					success++
				} else {
					require.ErrorIs(t, err, recordstore.ErrConflict)
					conflicts++
				}
			}
			require.Equal(t, 1, success)
			require.Equal(t, 1, conflicts)
			stored, err := r.ReadJob(f.ctx, j.Source)
			require.NoError(t, err)
			require.Equal(t, int64(1), stored.Fence)
			require.Equal(t, int64(1), stored.Attempts)
		})
	}
}

func TestExecutionRepositoryEncryptedNativeOriginal(t *testing.T) {
	for _, name := range []string{"retry_preserves_original", "replacement_preserves_original"} {
		t.Run(name, func(t *testing.T) {
			f := nativeStatusFixture(t)
			r, err := NewRecordExecutionRepository(f.base.reply, f.base.clock)
			require.NoError(t, err)
			source := ScheduledSource{Scope: f.p.Scope, Kind: billing.LifecycleSubscriptionSources, PrincipalID: f.p.PrincipalID, SubscriptionID: f.p.SubscriptionID, SourceID: billing.LifecycleDiscoverySourceID(f.p.Scope, billing.LifecycleSubscriptionSources, f.p.SubscriptionID)}
			j := ScheduledJob{Source: source, Revision: 1, Lane: ColdLane, CreatedAt: f.base.clock.at, NextAttemptAt: f.base.clock.at, OriginalStatus: &f.p}
			require.NoError(t, r.CommitScan(f.ctx, ScheduleCommit{Scope: source.Scope, Lane: ColdLane, Writes: []ScheduleWrite{{0, j}}}))
			clock := &executionTestClock{at: f.base.clock.at}
			r.clock = clock
			j, err = r.Acquire(f.ctx, LeaseRequest{Source: source, ExpectedRevision: 1, Actor: "current-worker", Token: "random-original-token", Until: clock.at.Add(time.Minute)})
			require.NoError(t, err)
			if name == "retry_preserves_original" {
				j, err = r.Release(f.ctx, LeaseDisposition{Handle: JobLease(j), Lane: RefreshLane, NextAttemptAt: clock.at.Add(time.Hour)})
			} else {
				clock.set(j.LeasedUntil)
				j, err = r.Acquire(f.ctx, LeaseRequest{Source: source, ExpectedRevision: j.Revision, Actor: "replacement-worker", Token: "random-new-token", Until: clock.at.Add(time.Minute)})
			}
			require.NoError(t, err)
			require.Equal(t, &f.p, j.OriginalStatus)
			stored, err := r.ReadJob(f.ctx, source)
			require.NoError(t, err)
			require.Equal(t, j, stored)
		})
	}
}

func TestExecutionRepositoryEncryptedLeaseIntegrity(t *testing.T) {
	for _, name := range []string{"dangling_actor", "missing_active_actor", "missing_active_token", "missing_active_epoch", "wrong_lane_handle", "wrong_source_handle", "missing_job"} {
		t.Run(name, func(t *testing.T) {
			f, r, clock, original := nativeExecutionFixture(t)
			j, err := r.Acquire(f.ctx, LeaseRequest{Source: original.Source, ExpectedRevision: 1, Actor: "worker-one", Token: "token-one", Until: clock.at.Add(time.Minute)})
			require.NoError(t, err)
			h := JobLease(j)
			if name == "wrong_lane_handle" || name == "wrong_source_handle" || name == "missing_job" {
				if name == "wrong_lane_handle" {
					h.Lane = RefreshLane
				}
				if name == "wrong_source_handle" {
					h.Source.PrincipalID = "different-owner"
				}
				want := recordstore.ErrConflict
				if name == "missing_job" {
					h.Source.SubscriptionID = "sub_missing"
					h.Source.SourceID = billing.LifecycleDiscoverySourceID(h.Source.Scope, h.Source.Kind, h.Source.SubscriptionID)
					want = recordstore.ErrNotFound
				}
				out, err := r.CheckLease(f.ctx, h)
				require.ErrorIs(t, err, want)
				require.Equal(t, ScheduledJob{}, out)
				return
			}
			p := scheduleJob(j)
			switch name {
			case "dangling_actor":
				p.LeasedUntil = time.Time{}
			case "missing_active_actor":
				p.LeaseActor = ""
			case "missing_active_token":
				p.LeaseToken = ""
			case "missing_active_epoch":
				p.Fence = 0
			}
			row, err := recordstore.NewRecord(scheduleJobKind, scheduledIdentity(j.Source), schedulePartition(j.Source.Scope, j.Lane), j.Revision+1, p)
			require.NoError(t, err)
			row.State = j.Lane
			require.NoError(t, f.store.Transact(f.ctx, "test-corrupt-lease", func(tx recordstore.Tx) error { return tx.Replace(f.ctx, row, j.Revision) }))
			out, err := r.ReadJob(f.ctx, j.Source)
			require.ErrorIs(t, err, recordstore.ErrUnavailable)
			require.Equal(t, ScheduledJob{}, out)
		})
	}
}
