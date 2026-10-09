package billinglifecycle

import (
	"context"
	"errors"
	"math"
	"time"

	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/repository/recordstore"
)

const statusSupersessionKind = "partners_lifecycle_status_supersession"

type StatusSupersessionKey struct {
	Source    ScheduledSource `json:"-"`
	CaptureID string          `json:"-"`
}
type StatusSupersessionRequest struct {
	Job        ScheduledJob                         `json:"-"`
	Input      StatusInput                          `json:"-"`
	Resolution billing.SubscriptionStatusResolution `json:"-"`
}

// StatusSupersession is an immutable historical transition, not a live lease.
// Current execution may have advanced beyond this job snapshot on inspection.
type StatusSupersession struct {
	Job        ScheduledJob                         `json:"-"`
	Input      StatusInput                          `json:"-"`
	Resolution billing.SubscriptionStatusResolution `json:"-"`
	RecordedAt time.Time                            `json:"-"`
}

// StatusSupersessionRepository clears an exact active pointer only after the
// service confirms conclusive native supersession. It preserves execution credit
// and lease; immutable history plus the job reference commit in one transaction.
type StatusSupersessionRepository interface {
	SupersedeStatus(context.Context, StatusSupersessionRequest) (StatusSupersession, error)
	FindStatusSupersession(context.Context, StatusSupersessionKey) (StatusSupersession, error)
	FindLastStatusSupersession(context.Context, ScheduledSource) (StatusSupersession, error)
}

type statusSupersessionPayload struct {
	Schema                int
	Job                   scheduleJobPayload
	JobRevision           int64
	Input                 statusPayload
	CurrentPreparation    statusPreparationPayload
	CurrentStatus         string
	CancellationScheduled bool
	ObservedAt            time.Time
	NativeRevision        int64
	NativeFingerprint     string
	RecordedAt            time.Time
}

func statusSupersessionIdentity(k StatusSupersessionKey) (string, string) {
	return digest([]any{"partners.lifecycle.status-supersession.v1", k.Source.Scope, k.Source.Kind, k.Source.SourceID, k.CaptureID}), digest([]any{"partners.lifecycle.scope.v1", k.Source.Scope})
}
func statusSupersessionKeyShape(k StatusSupersessionKey) bool {
	return scheduleSourceShape(k.Source) && k.Source.Kind == billing.LifecycleSubscriptionSources && scheduleText(k.CaptureID)
}
func supersessionShape(v StatusSupersession, k StatusSupersessionKey) bool {
	j, p, r := v.Job, v.Input.Preparation, v.Resolution
	if !statusSupersessionKeyShape(k) || validateScheduledJob(j) != nil || j.Revision < 3 || !leaseShape(JobLease(j)) || j.Attempts < 1 || j.OriginalStatus != nil || j.CheckoutPrepared || !sameScheduledSource(j.Source, k.Source) || p.CaptureID != k.CaptureID || !statusMatchesSource(p, j.Source) || r.State != billing.SubscriptionStatusSuperseded || r.Validate() != nil || !sameStatusPreparation(r.Preparation, p) || v.RecordedAt.IsZero() || v.RecordedAt.Before(j.CreatedAt) || v.RecordedAt.Before(p.RequestedAt) || v.RecordedAt.Before(r.Current.ObservedAt) || !j.LeasedUntil.After(v.RecordedAt) {
		return false
	}
	if _, err := encodeStatus(v.Input); err != nil {
		return false
	}
	id, _ := statusSupersessionIdentity(k)
	return j.LastSupersessionID == id
}
func encodeStatusSupersession(v StatusSupersession) (recordstore.Record, error) {
	k := StatusSupersessionKey{v.Job.Source, v.Input.Preparation.CaptureID}
	if !supersessionShape(v, k) {
		return recordstore.Record{}, recordstore.ErrInvalid
	}
	current := *v.Resolution.Current
	p := statusSupersessionPayload{Schema: 1, Job: scheduleJob(v.Job), JobRevision: v.Job.Revision, Input: statusPayloadFor(v.Input), CurrentPreparation: statusPreparation(current.Preparation), CurrentStatus: current.Status, CancellationScheduled: current.CancellationScheduled, ObservedAt: current.ObservedAt, NativeRevision: current.Revision, NativeFingerprint: current.Fingerprint, RecordedAt: v.RecordedAt}
	id, partition := statusSupersessionIdentity(k)
	row, err := recordstore.NewRecord(statusSupersessionKind, id, partition, 1, p)
	row.State = "superseded"
	return row, err
}
func decodeStatusSupersession(row recordstore.Record, k StatusSupersessionKey) (StatusSupersession, error) {
	id, partition := statusSupersessionIdentity(k)
	if row.Kind != statusSupersessionKind || row.ID != id || row.Partition != partition || row.Revision != 1 || row.State != "superseded" || row.Sequence != 0 || row.ExpiresAt != nil {
		return StatusSupersession{}, recordstore.ErrUnavailable
	}
	var p statusSupersessionPayload
	if strictScheduleDecode(row.Data, &p) != nil || p.Schema != 1 || p.Job.Schema != 1 || p.Input.Schema != 1 {
		return StatusSupersession{}, recordstore.ErrUnavailable
	}
	input := p.Input.input()
	current := billing.SubscriptionStatus{Preparation: p.CurrentPreparation.input(), Status: p.CurrentStatus, CancellationScheduled: p.CancellationScheduled, ObservedAt: p.ObservedAt, Revision: p.NativeRevision, Fingerprint: p.NativeFingerprint}
	out := StatusSupersession{Job: p.Job.job(p.JobRevision), Input: input, Resolution: billing.SubscriptionStatusResolution{Preparation: input.Preparation, State: billing.SubscriptionStatusSuperseded, Current: &current}, RecordedAt: p.RecordedAt}
	if !supersessionShape(out, k) {
		return StatusSupersession{}, recordstore.ErrUnavailable
	}
	return out, nil
}
func sameRetainedStatus(a, b StatusInput) bool {
	if !sameStatusPreparation(a.Preparation, b.Preparation) || (a.Evidence == nil) != (b.Evidence == nil) {
		return false
	}
	return a.Evidence == nil || *a.Evidence == *b.Evidence
}
func sameStatusSupersession(a, b StatusSupersession) bool {
	if !sameExecutionJob(a.Job, b.Job) || !sameRetainedStatus(a.Input, b.Input) || !a.RecordedAt.Equal(b.RecordedAt) || a.Resolution.Current == nil || b.Resolution.Current == nil {
		return false
	}
	x, y := *a.Resolution.Current, *b.Resolution.Current
	return a.Resolution.State == b.Resolution.State && sameStatusPreparation(a.Resolution.Preparation, b.Resolution.Preparation) && sameStatusPreparation(x.Preparation, y.Preparation) && x.Status == y.Status && x.CancellationScheduled == y.CancellationScheduled && x.ObservedAt.Equal(y.ObservedAt) && x.Revision == y.Revision && x.Fingerprint == y.Fingerprint
}
func supersessionRequestShape(q StatusSupersessionRequest) bool {
	j, r := q.Job, q.Resolution
	return validateScheduledJob(j) == nil && leaseShape(JobLease(j)) && j.OriginalStatus != nil && statusMatchesSource(q.Input.Preparation, j.Source) && sameStatusPreparation(*j.OriginalStatus, q.Input.Preparation) && r.State == billing.SubscriptionStatusSuperseded && r.Validate() == nil && sameStatusPreparation(r.Preparation, q.Input.Preparation)
}

func (r *RecordExecutionRepository) SupersedeStatus(ctx context.Context, q StatusSupersessionRequest) (StatusSupersession, error) {
	if err := r.executionReady(ctx); err != nil {
		return StatusSupersession{}, err
	}
	if !supersessionRequestShape(q) || q.Job.Revision == math.MaxInt64 {
		return StatusSupersession{}, recordstore.ErrInvalid
	}
	input, err := encodeStatus(q.Input)
	if err != nil {
		return StatusSupersession{}, err
	}
	k := StatusSupersessionKey{q.Job.Source, q.Input.Preparation.CaptureID}
	id, _ := statusSupersessionIdentity(k)
	var out StatusSupersession
	err = r.store.Transact(ctx, scheduleJobKind+":"+scheduledIdentity(q.Job.Source), func(tx recordstore.Tx) error {
		out = StatusSupersession{}
		j, err := selectedJob(ctx, tx, scheduledIdentity(q.Job.Source), digest(scheduleSource(q.Job.Source)))
		if err != nil {
			return err
		}
		before := r.clock.Now()
		if !exactLease(j, JobLease(q.Job), before) || !sameExecutionJob(j, q.Job) || before.Before(q.Input.Preparation.RequestedAt) || before.Before(q.Resolution.Current.ObservedAt) || before.Before(j.CreatedAt) {
			return recordstore.ErrConflict
		}
		row, err := tx.Get(ctx, statusKind, input.ID)
		if err != nil {
			if soleNotFound(err) {
				return errors.Join(recordstore.ErrUnavailable, err)
			}
			return err
		}
		actual, err := decodeStatus(row, q.Input.Preparation)
		if err != nil {
			return err
		}
		if !sameRetainedStatus(actual, q.Input) {
			return recordstore.ErrConflict
		}
		_, err = tx.Get(ctx, statusSupersessionKind, id)
		if err == nil {
			return recordstore.ErrConflict
		}
		if !soleNotFound(err) {
			return err
		}
		j.Revision++
		j.OriginalStatus = nil
		j.LastSupersessionID = id
		result := StatusSupersession{Job: j, Input: actual, Resolution: q.Resolution, RecordedAt: before.UTC()}
		history, err := encodeStatusSupersession(result)
		if err != nil {
			return err
		}
		result, err = decodeStatusSupersession(history, k)
		if err != nil {
			return err
		}
		successor, err := jobRecord(result.Job)
		if err != nil {
			return err
		}
		if err = tx.Insert(ctx, history); err != nil {
			return err
		}
		now := r.clock.Now()
		if now.IsZero() || now.Before(before) || !q.Job.LeasedUntil.After(now) {
			return recordstore.ErrConflict
		}
		if err = tx.Replace(ctx, successor, q.Job.Revision); err != nil {
			return err
		}
		out = result
		return nil
	})
	if err != nil {
		return StatusSupersession{}, err
	}
	if err = ctx.Err(); err != nil {
		return StatusSupersession{}, err
	}
	return out, nil
}
func (r *RecordExecutionRepository) FindStatusSupersession(ctx context.Context, k StatusSupersessionKey) (StatusSupersession, error) {
	if err := r.executionReady(ctx); err != nil {
		return StatusSupersession{}, err
	}
	if !statusSupersessionKeyShape(k) {
		return StatusSupersession{}, recordstore.ErrInvalid
	}
	id, _ := statusSupersessionIdentity(k)
	var out StatusSupersession
	err := r.store.Read(ctx, func(tx recordstore.Tx) error {
		out = StatusSupersession{}
		row, err := tx.Get(ctx, statusSupersessionKind, id)
		if err != nil {
			return err
		}
		out, err = decodeStatusSupersession(row, k)
		return err
	})
	if err == nil {
		err = ctx.Err()
	}
	if err != nil {
		return StatusSupersession{}, err
	}
	return out, nil
}
func (r *RecordExecutionRepository) FindLastStatusSupersession(ctx context.Context, source ScheduledSource) (StatusSupersession, error) {
	if err := r.executionReady(ctx); err != nil {
		return StatusSupersession{}, err
	}
	if !scheduleSourceShape(source) || source.Kind != billing.LifecycleSubscriptionSources {
		return StatusSupersession{}, recordstore.ErrInvalid
	}
	var out StatusSupersession
	err := r.store.Read(ctx, func(tx recordstore.Tx) error {
		out = StatusSupersession{}
		j, err := selectedJob(ctx, tx, scheduledIdentity(source), digest(scheduleSource(source)))
		if err != nil {
			return err
		}
		if j.LastSupersessionID == "" {
			return recordstore.ErrNotFound
		}
		row, err := tx.Get(ctx, statusSupersessionKind, j.LastSupersessionID)
		if err != nil {
			if soleNotFound(err) {
				return errors.Join(recordstore.ErrUnavailable, err)
			}
			return err
		}
		var p statusSupersessionPayload
		if strictScheduleDecode(row.Data, &p) != nil {
			return recordstore.ErrUnavailable
		}
		out, err = decodeStatusSupersession(row, StatusSupersessionKey{source, p.Input.Preparation.CaptureID})
		if err != nil {
			return err
		}
		if out.Job.Revision > j.Revision || out.Job.Fence > j.Fence || out.Job.LastSupersessionID != j.LastSupersessionID {
			return recordstore.ErrUnavailable
		}
		return nil
	})
	if err == nil {
		err = ctx.Err()
	}
	if err != nil {
		return StatusSupersession{}, err
	}
	return out, nil
}
