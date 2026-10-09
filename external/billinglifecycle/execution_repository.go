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

type ExecutionRepository interface {
	ReadJob(context.Context, ScheduledSource) (ScheduledJob, error)
	Acquire(context.Context, LeaseRequest) (ScheduledJob, error)
	CheckLease(context.Context, LeaseHandle) (ScheduledJob, error)
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

func executionLane(lane string) bool { return lane == ColdLane || lane == RefreshLane }

func (r *RecordExecutionRepository) executionReady(ctx context.Context) error {
	if r == nil || r.RecordScheduleRepository == nil || nilPort(r.clock) {
		return recordstore.ErrUnavailable
	}
	return r.ready(ctx)
}

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

func leaseShape(h LeaseHandle) bool {
	return scheduleSourceShape(h.Source) && h.Revision > 0 && h.Fence > 0 && executionLane(h.Lane) && scheduleText(h.Actor) && scheduleText(h.Token) && !h.Until.IsZero()
}

func exactLease(j ScheduledJob, h LeaseHandle, now time.Time) bool {
	return !now.IsZero() && j.Revision == h.Revision && j.Fence == h.Fence && j.Lane == h.Lane && j.LeaseActor == h.Actor && j.LeaseToken == h.Token && j.LeasedUntil.Equal(h.Until) && j.LeasedUntil.After(now)
}

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
