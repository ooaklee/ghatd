package billinglifecycle

import (
	"context"
	"math"
	"time"

	"github.com/ooaklee/ghatd/external/repository/recordstore"
)

// LeaseRequest is an owning scheduler's proposed acquisition, not authority.
// The scheduler chooses a bounded lifetime and a fresh cryptographic token.
type LeaseRequest struct {
	Source           ScheduledSource `json:"-"`
	ExpectedRevision int64           `json:"-"`
	Actor, Token     string          `json:"-"`
	Until            time.Time       `json:"-"`
}

// LeaseHandle identifies one exact job execution. A snapshot lease check cannot
// fence a later mutation; every write must enforce this predicate in its txn.
type LeaseHandle struct {
	Source             ScheduledSource `json:"-"`
	Revision, Fence    int64           `json:"-"`
	Actor, Token, Lane string          `json:"-"`
	Until              time.Time       `json:"-"`
}

// JobLease extracts the current lease fields of a scheduled job into an exact
// LeaseHandle snapshot.
func JobLease(j ScheduledJob) LeaseHandle {
	return LeaseHandle{Source: j.Source, Revision: j.Revision, Fence: j.Fence, Actor: j.LeaseActor, Token: j.LeaseToken, Lane: j.Lane, Until: j.LeasedUntil}
}

// LeaseDisposition releases an active execution for retry or another refresh.
// It never clears unresolved originals or retires status work after revenue.
type LeaseDisposition struct {
	Handle        LeaseHandle `json:"-"`
	NextAttemptAt time.Time   `json:"-"`
	Lane          string      `json:"-"`
}

// ExecutionRepository reads, acquires, checks and releases job executions. Each
// write enforces the exact current revision/token/expiry predicate inside its
// transaction.
type ExecutionRepository interface {
	// ReadJob returns the scheduled job for a shape-valid source; a same-identity
	// job stored under a different source digest is a conflict, and any error
	// returns a zero job.
	ReadJob(context.Context, ScheduledSource) (ScheduledJob, error)
	// Acquire grants a lease only when the job matches the expected revision, is
	// due, on an execution lane, unleased and within capacity; the write enforces
	// the exact predicate inside its transaction.
	Acquire(context.Context, LeaseRequest) (ScheduledJob, error)
	// CheckLease re-reads the job and returns it only while the exact lease handle
	// is still active; it is a snapshot check, not a fence for later writes.
	CheckLease(context.Context, LeaseHandle) (ScheduledJob, error)
	// Release clears the exact active lease and moves the job to the requested lane
	// and next attempt time, re-checking the revision/token/expiry predicate at the
	// conditional write.
	Release(context.Context, LeaseDisposition) (ScheduledJob, error)
}

// RecordExecutionRepository supplies current revision/token/epoch/expiry write
// predicates over the borrowed encrypted store. The clock must be local and
// side-effect free: it is read inside retryable callbacks, never remote I/O.
// Authority, due selection, retry policy and provider stages belong to services.
type RecordExecutionRepository struct {
	*RecordScheduleRepository
	clock LifecycleClock
}

// NewRecordExecutionRepository wraps a prepared schedule repository with a
// required non-nil local clock. It borrows the store and creates no indexes or
// services.
func NewRecordExecutionRepository(store recordstore.Store, clock LifecycleClock) (*RecordExecutionRepository, error) {
	if nilPort(clock) {
		return nil, recordstore.ErrUnavailable
	}
	r, err := NewRecordScheduleRepository(store)
	if err != nil {
		return nil, err
	}
	return &RecordExecutionRepository{r, clock}, nil
}

// executionLane reports whether the lane is one of the two lanes in which
// execution may run.
func executionLane(lane string) bool { return lane == ColdLane || lane == RefreshLane }

// executionReady verifies the repository, embedded scheduler repository and
// clock are usable before an execution operation.
func (r *RecordExecutionRepository) executionReady(ctx context.Context) error {
	if r == nil || r.RecordScheduleRepository == nil || nilPort(r.clock) {
		return recordstore.ErrUnavailable
	}
	return r.ready(ctx)
}

// selectedJob reads one job within a transaction and requires its source digest
// to match the caller's expectation, returning ErrConflict otherwise.
func selectedJob(ctx context.Context, tx recordstore.Tx, id, sourceDigest string) (ScheduledJob, error) {
	row, err := tx.Get(ctx, scheduleJobKind, id)
	if err != nil {
		return ScheduledJob{}, err
	}
	j, err := readJob(row)
	if err != nil {
		return ScheduledJob{}, err
	}
	if digest(scheduleSource(j.Source)) != sourceDigest {
		return ScheduledJob{}, recordstore.ErrConflict
	}
	return j, nil
}

// ReadJob returns the job for a shape-valid source. A job stored under the same
// identity with a different source digest is a conflict, and any error returns
// a zero job.
func (r *RecordExecutionRepository) ReadJob(ctx context.Context, source ScheduledSource) (ScheduledJob, error) {
	if err := r.executionReady(ctx); err != nil {
		return ScheduledJob{}, err
	}
	if !scheduleSourceShape(source) {
		return ScheduledJob{}, recordstore.ErrInvalid
	}
	id, original := scheduledIdentity(source), digest(scheduleSource(source))
	var out ScheduledJob
	err := r.store.Read(ctx, func(tx recordstore.Tx) error {
		var err error
		out, err = selectedJob(ctx, tx, id, original)
		return err
	})
	if err == nil {
		err = ctx.Err()
	}
	if err != nil {
		return ScheduledJob{}, err
	}
	return out, nil
}

// Acquire grants a lease only when the job is at ExpectedRevision, on an
// execution lane, due, unleased and within capacity limits. The clock is re-
// read before the conditional write so a retried or delayed callback cannot
// persist an already expired grant; failures return a zero job.
func (r *RecordExecutionRepository) Acquire(ctx context.Context, q LeaseRequest) (ScheduledJob, error) {
	if err := r.executionReady(ctx); err != nil {
		return ScheduledJob{}, err
	}
	if !scheduleSourceShape(q.Source) || q.ExpectedRevision < 1 || q.ExpectedRevision == math.MaxInt64 || !scheduleText(q.Actor) || !scheduleText(q.Token) || q.Until.IsZero() {
		return ScheduledJob{}, recordstore.ErrInvalid
	}
	id, original := scheduledIdentity(q.Source), digest(scheduleSource(q.Source))
	var out ScheduledJob
	err := r.store.Transact(ctx, scheduleJobKind+":"+id, func(tx recordstore.Tx) error {
		out = ScheduledJob{}
		j, err := selectedJob(ctx, tx, id, original)
		if err != nil {
			return err
		}
		now := r.clock.Now()
		if now.IsZero() {
			return recordstore.ErrUnavailable
		}
		if j.Revision != q.ExpectedRevision || !executionLane(j.Lane) || j.NextAttemptAt.After(now) || j.LeasedUntil.After(now) || !q.Until.After(now) || j.Attempts == math.MaxInt64 || j.Fence == math.MaxInt64 {
			return recordstore.ErrConflict
		}
		j.Revision++
		j.Attempts++
		j.Fence++
		j.LeaseActor, j.LeaseToken, j.LeasedUntil = q.Actor, q.Token, q.Until
		row, err := jobRecord(j)
		if err != nil {
			return err
		}
		// A retried/delayed callback must not persist an already expired grant.
		before := now
		now = r.clock.Now()
		if now.IsZero() || now.Before(before) || !q.Until.After(now) {
			return recordstore.ErrConflict
		}
		if err := tx.Replace(ctx, row, q.ExpectedRevision); err != nil {
			return err
		}
		out = j
		return nil
	})
	if err != nil {
		return ScheduledJob{}, err
	}
	return out, nil
}

// leaseShape checks that every lease handle field required for exact matching
// is present and well formed.
func leaseShape(h LeaseHandle) bool {
	return scheduleSourceShape(h.Source) && h.Revision > 0 && h.Fence > 0 && executionLane(h.Lane) && scheduleText(h.Actor) && scheduleText(h.Token) && !h.Until.IsZero()
}

// exactLease reports whether the job currently holds exactly this handle's
// revision, fence, actor, token and lane, and has not expired at now.
func exactLease(j ScheduledJob, h LeaseHandle, now time.Time) bool {
	return !now.IsZero() && j.Revision == h.Revision && j.Fence == h.Fence && j.Lane == h.Lane && j.LeaseActor == h.Actor && j.LeaseToken == h.Token && j.LeasedUntil.Equal(h.Until) && j.LeasedUntil.After(now)
}

// CheckLease re-reads the job and succeeds only while the exact lease is still
// active. It is a snapshot check, not a fence for later writes.
func (r *RecordExecutionRepository) CheckLease(ctx context.Context, h LeaseHandle) (ScheduledJob, error) {
	if err := r.executionReady(ctx); err != nil {
		return ScheduledJob{}, err
	}
	if !leaseShape(h) {
		return ScheduledJob{}, recordstore.ErrInvalid
	}
	j, err := r.ReadJob(ctx, h.Source)
	if err != nil {
		return ScheduledJob{}, err
	}
	if !exactLease(j, h, r.clock.Now()) {
		return ScheduledJob{}, recordstore.ErrConflict
	}
	if err := ctx.Err(); err != nil {
		return ScheduledJob{}, err
	}
	return j, nil
}

// Release clears the exact active lease and moves the job to the requested lane
// and next attempt time. The predicate is re-checked at the conditional write
// against a fresh clock reading; failures return a zero job.
func (r *RecordExecutionRepository) Release(ctx context.Context, q LeaseDisposition) (ScheduledJob, error) {
	if err := r.executionReady(ctx); err != nil {
		return ScheduledJob{}, err
	}
	h := q.Handle
	if !leaseShape(h) || h.Revision == math.MaxInt64 || !executionLane(q.Lane) || q.NextAttemptAt.IsZero() {
		return ScheduledJob{}, recordstore.ErrInvalid
	}
	id, original := scheduledIdentity(h.Source), digest(scheduleSource(h.Source))
	var out ScheduledJob
	err := r.store.Transact(ctx, scheduleJobKind+":"+id, func(tx recordstore.Tx) error {
		out = ScheduledJob{}
		j, err := selectedJob(ctx, tx, id, original)
		if err != nil {
			return err
		}
		now := r.clock.Now()
		if !exactLease(j, h, now) || !q.NextAttemptAt.After(now) {
			return recordstore.ErrConflict
		}
		j.Revision++
		j.Lane, j.NextAttemptAt = q.Lane, q.NextAttemptAt
		j.LeaseActor, j.LeaseToken, j.LeasedUntil = "", "", time.Time{}
		row, err := jobRecord(j)
		if err != nil {
			return err
		}
		// Check the exact observed active lease again at the conditional write.
		before := now
		now = r.clock.Now()
		if now.IsZero() || now.Before(before) || !h.Until.After(now) || !q.NextAttemptAt.After(now) {
			return recordstore.ErrConflict
		}
		if err := tx.Replace(ctx, row, h.Revision); err != nil {
			return err
		}
		out = j
		return nil
	})
	if err != nil {
		return ScheduledJob{}, err
	}
	return out, nil
}
