package billinglifecycle

import (
	"context"
	"errors"
	"math"
	"time"

	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/repository/recordstore"
)

const completionKind = "partners_lifecycle_completion"

// CompletionKey names one original native capture, not a lease or permission.
// A replacement can inspect a committed completion without impersonating its author.
type CompletionKey struct {
	Source          ScheduledSource `json:"-"`
	NativeCaptureID string          `json:"-"`
}

// CompletionReceipt attests a host transition referencing an immutable native
// receipt. Native fingerprints are opaque owning references, not host proof.
type CompletionReceipt struct {
	NativeCaptureID, NativeFingerprint               string    `json:"-"`
	NativeRequestedAt, NativeObservedAt, CompletedAt time.Time `json:"-"`
}

// CompletedExecution is an immutable historical snapshot, NEVER a live lease.
// Current jobs may have progressed through later cycles when this is inspected.
type CompletedExecution struct {
	OriginalStatus *billing.SubscriptionStatusPreparation `json:"-"`
	Job            ScheduledJob                           `json:"-"`
	Receipt        CompletionReceipt                      `json:"-"`
}

// StatusCompletion carries one status observation with its service-selected
// next attempt time; both fields are private process state excluded from JSON
// transport.
type StatusCompletion struct {
	Observation   StatusObservation `json:"-"`
	NextAttemptAt time.Time         `json:"-"`
}

// CompletionRepository atomically retains host completion identity and clears
// the exact execution. The service confirms current native receipts/permission;
// no foreign I/O or authorization occurs in these retryable callbacks.
type CompletionRepository interface {
	// CompleteCheckout atomically retains the host completion identity from the
	// observation's native receipt, clears the exact bound execution, and returns
	// the completed execution; no foreign I/O or authorization occurs.
	CompleteCheckout(context.Context, CheckoutObservation) (CompletedExecution, error)
	// CompleteStatus clears the exact confirmed active original, resets that
	// cycle's retry attempts, schedules the supplied next observation time, and
	// returns the completed execution with its native receipt identity.
	CompleteStatus(context.Context, StatusCompletion) (CompletedExecution, error)
	// FindCompletion returns the immutable completed execution matching the
	// completion key; it is a historical read that neither acquires a lease nor
	// authorizes fresh work.
	FindCompletion(context.Context, CompletionKey) (CompletedExecution, error)
	// FindLastCompletion joins the scheduled source's current job reference with
	// its immutable last completion in one snapshot for crash inspection; the
	// returned historical job cannot authorize subsequent execution.
	FindLastCompletion(context.Context, ScheduledSource) (CompletedExecution, error)
}

// completionPayload is the encrypted-record codec retaining the completed job,
// native receipt timestamps and optional original status preparation that
// public JSON tags omit.
type completionPayload struct {
	OriginalStatus                                   *statusPreparationPayload
	Schema                                           int
	JobRevision                                      int64
	Job                                              scheduleJobPayload
	NativeCaptureID, NativeFingerprint               string
	NativeRequestedAt, NativeObservedAt, CompletedAt time.Time
}

// completionIdentity derives the deterministic completion record ID and scope
// partition for one completion key.
func completionIdentity(k CompletionKey) (string, string) {
	return digest([]any{"partners.lifecycle.completion.v1", k.Source.Scope, k.Source.Kind, k.Source.SourceID, k.NativeCaptureID}), digest([]any{"partners.lifecycle.scope.v1", k.Source.Scope})
}

// completionKeyShape checks source shape, non-empty native capture ID, and for
// checkout sources that the capture ID equals the acknowledged checkout's ID.
func completionKeyShape(k CompletionKey) bool {
	return scheduleSourceShape(k.Source) && scheduleText(k.NativeCaptureID) && (k.Source.Kind != billing.LifecycleCheckoutSources || k.NativeCaptureID == k.Source.Checkout.ID)
}

// completedShape validates a completed execution against its key: unleased
// valid job at revision >= 3, consistent receipt chronology, linked completion
// ID, and lane-specific requirements for retired checkout versus refresh-
// scheduled status completions.
func completedShape(v CompletedExecution, k CompletionKey) bool {
	j, c := v.Job, v.Receipt
	if !completionKeyShape(k) || validateScheduledJob(j) != nil || !sameScheduledSource(j.Source, k.Source) || j.Revision < 3 || j.Fence < 1 || j.LeaseActor != "" || j.LeaseToken != "" || !j.LeasedUntil.IsZero() || j.OriginalStatus != nil || c.NativeCaptureID != k.NativeCaptureID || !scheduleText(c.NativeFingerprint) || c.NativeRequestedAt.IsZero() || c.NativeObservedAt.Before(c.NativeRequestedAt) || c.CompletedAt.IsZero() || c.CompletedAt.Before(c.NativeObservedAt) || c.CompletedAt.Before(j.CreatedAt) {
		return false
	}
	id, _ := completionIdentity(k)
	if j.LastCompletionID != id {
		return false
	}
	if j.Source.Kind == billing.LifecycleCheckoutSources {
		return v.OriginalStatus == nil && j.CadenceAnchor.IsZero() && j.Lane == RetiredLane && j.CheckoutPrepared && j.Attempts > 0 && c.NativeRequestedAt.Equal(j.Source.Checkout.CreatedAt)
	}
	return v.OriginalStatus != nil && v.OriginalStatus.Validate() == nil && v.OriginalStatus.CaptureID == k.NativeCaptureID && statusMatchesSource(*v.OriginalStatus, j.Source) && v.OriginalStatus.RequestedAt.Equal(c.NativeRequestedAt) && !j.CadenceAnchor.IsZero() && !c.NativeRequestedAt.Before(j.CadenceAnchor) && j.Lane == RefreshLane && !j.CheckoutPrepared && j.Attempts == 0 && j.NextAttemptAt.After(c.CompletedAt)
}

// encodeCompletion builds the immutable completion record, first requiring
// completedShape; invalid executions are rejected before any record exists.
func encodeCompletion(v CompletedExecution) (recordstore.Record, error) {
	k := CompletionKey{v.Job.Source, v.Receipt.NativeCaptureID}
	if !completedShape(v, k) {
		return recordstore.Record{}, recordstore.ErrInvalid
	}
	c := v.Receipt
	p := completionPayload{Schema: 1, JobRevision: v.Job.Revision, Job: scheduleJob(v.Job), NativeCaptureID: c.NativeCaptureID, NativeFingerprint: c.NativeFingerprint, NativeRequestedAt: c.NativeRequestedAt, NativeObservedAt: c.NativeObservedAt, CompletedAt: c.CompletedAt}
	if v.OriginalStatus != nil {
		original := statusPreparation(*v.OriginalStatus)
		p.OriginalStatus = &original
	}

	id, partition := completionIdentity(k)
	row, err := recordstore.NewRecord(completionKind, id, partition, 1, p)
	row.State = "completed"
	return row, err
}

// decodeCompletion strictly decodes and shape-validates a completion row for
// its key; any divergence is unavailable rather than a partial value.
func decodeCompletion(row recordstore.Record, k CompletionKey) (CompletedExecution, error) {
	id, partition := completionIdentity(k)
	if row.Kind != completionKind || row.ID != id || row.Partition != partition || row.Revision != 1 || row.State != "completed" || row.Sequence != 0 || row.ExpiresAt != nil {
		return CompletedExecution{}, recordstore.ErrUnavailable
	}
	var p completionPayload
	if strictScheduleDecode(row.Data, &p) != nil || p.Schema != 1 || p.Job.Schema != 1 {
		return CompletedExecution{}, recordstore.ErrUnavailable
	}
	out := CompletedExecution{Job: p.Job.job(p.JobRevision), Receipt: CompletionReceipt{p.NativeCaptureID, p.NativeFingerprint, p.NativeRequestedAt, p.NativeObservedAt, p.CompletedAt}}
	if p.OriginalStatus != nil {
		original := p.OriginalStatus.input()
		out.OriginalStatus = &original
	}

	if !completedShape(out, k) {
		return CompletedExecution{}, recordstore.ErrUnavailable
	}
	return out, nil
}

// sameExecutionJob compares jobs by source, original inputs, revision, fence,
// attempts, lane, lease fields and timestamps, leaving no field unchecked.
func sameExecutionJob(a, b ScheduledJob) bool {
	return sameScheduledSource(a.Source, b.Source) && sameOriginalInputs(a, b) && a.Revision == b.Revision && a.Fence == b.Fence && a.Attempts == b.Attempts && a.Lane == b.Lane && a.LeaseActor == b.LeaseActor && a.LeaseToken == b.LeaseToken && a.LeasedUntil.Equal(b.LeasedUntil) && a.CreatedAt.Equal(b.CreatedAt) && a.NextAttemptAt.Equal(b.NextAttemptAt)
}

// checkoutObservationShape validates a checkout observation: a leased valid
// prepared job whose input matches the scheduled checkout with retained
// evidence the receipt validates.
func checkoutObservationShape(o CheckoutObservation) bool {
	return validateScheduledJob(o.Job) == nil && leaseShape(JobLease(o.Job)) && o.Job.CheckoutPrepared && checkoutMatchesSource(o.Input, o.Job.Source) && o.Input.Evidence != nil && o.Receipt.ValidateCapturedEvidence(o.Input.Intent, *o.Input.Evidence) == nil
}

// statusObservationShape validates a status observation: a leased job retaining
// the same original preparation, evidence valid for it, and a receipt that
// exactly matches the evidence's status and scheduling.
func statusObservationShape(o StatusObservation) bool {
	return validateScheduledJob(o.Job) == nil && leaseShape(JobLease(o.Job)) && o.Job.OriginalStatus != nil && statusMatchesSource(o.Input.Preparation, o.Job.Source) && sameStatusPreparation(*o.Job.OriginalStatus, o.Input.Preparation) && o.Input.Evidence != nil && o.Input.Evidence.Validate(o.Input.Preparation) == nil && o.Receipt.Validate() == nil && sameStatusPreparation(o.Receipt.Preparation, o.Input.Preparation) && o.Receipt.Status == o.Input.Evidence.Status && o.Receipt.CancellationScheduled == o.Input.Evidence.CancellationScheduled
}

// CompleteCheckout retires only an exact bound original/evidence and confirmed
// native receipt supplied by the service. Positive historical Attempts remain
// because CheckoutPrepared records an executed bind; there is no next cycle.
func (r *RecordExecutionRepository) CompleteCheckout(ctx context.Context, o CheckoutObservation) (CompletedExecution, error) {
	if err := r.executionReady(ctx); err != nil {
		return CompletedExecution{}, err
	}
	if !checkoutObservationShape(o) {
		return CompletedExecution{}, recordstore.ErrInvalid
	}
	input, err := encode(o.Input)
	if err != nil {
		return CompletedExecution{}, err
	}
	c := CompletionReceipt{NativeCaptureID: o.Input.Intent.ID, NativeFingerprint: o.Receipt.Fingerprint, NativeRequestedAt: o.Input.Intent.CreatedAt, NativeObservedAt: o.Receipt.AnchoredAt}
	return r.complete(ctx, o.Job, input, c, time.Time{})
}

// CompleteStatus clears only the exact confirmed active original, resets that
// cycle's retry attempts and schedules the service-selected future observation.
// Native outbox/receipt history remains immutable; Fence never resets.
func (r *RecordExecutionRepository) CompleteStatus(ctx context.Context, q StatusCompletion) (CompletedExecution, error) {
	if err := r.executionReady(ctx); err != nil {
		return CompletedExecution{}, err
	}
	o := q.Observation
	if !statusObservationShape(o) || q.NextAttemptAt.IsZero() {
		return CompletedExecution{}, recordstore.ErrInvalid
	}
	input, err := encodeStatus(o.Input)
	if err != nil {
		return CompletedExecution{}, err
	}
	c := CompletionReceipt{NativeCaptureID: o.Input.Preparation.CaptureID, NativeFingerprint: o.Receipt.Fingerprint, NativeRequestedAt: o.Input.Preparation.RequestedAt, NativeObservedAt: o.Receipt.ObservedAt}
	return r.complete(ctx, o.Job, input, c, q.NextAttemptAt)
}

// complete atomically retires one exact leased job: it re-reads the selected
// job, verifies the exact lease and retained input codec, inserts the shape-
// validated completion and CAS-replaces the unleased successor job. Clock
// regressions and existing completions conflict; missing originals return
// unavailable joined with not-found.
func (r *RecordExecutionRepository) complete(ctx context.Context, expected ScheduledJob, input recordstore.Record, c CompletionReceipt, next time.Time) (CompletedExecution, error) {
	h := JobLease(expected)
	if expected.Revision == math.MaxInt64 {
		return CompletedExecution{}, recordstore.ErrInvalid
	}
	k := CompletionKey{expected.Source, c.NativeCaptureID}
	id, _ := completionIdentity(k)
	var out CompletedExecution
	err := r.store.Transact(ctx, scheduleJobKind+":"+scheduledIdentity(h.Source), func(tx recordstore.Tx) error {
		out = CompletedExecution{}
		j, err := selectedJob(ctx, tx, scheduledIdentity(h.Source), digest(scheduleSource(h.Source)))
		if err != nil {
			return err
		}
		before := r.clock.Now()
		if !exactLease(j, h, before) || !sameExecutionJob(j, expected) || before.Before(c.NativeObservedAt) || before.Before(j.CreatedAt) || (j.Source.Kind == billing.LifecycleSubscriptionSources && !next.After(before)) {
			return recordstore.ErrConflict
		}
		retained, err := tx.Get(ctx, input.Kind, input.ID)
		if err != nil {
			if soleNotFound(err) {
				return errors.Join(recordstore.ErrUnavailable, err)
			}
			return err
		}
		if j.Source.Kind == billing.LifecycleCheckoutSources {
			actual, e := decode(retained, *j.Source.Checkout)
			if e != nil {
				return e
			}
			// This is private storage-codec equivalence, not reconstruction of any
			// billing business fingerprint. The native service owns those references.
			if digest(payload(actual)) != digestPayload(input) {
				return recordstore.ErrConflict
			}
		} else {
			actual, e := decodeStatus(retained, *j.OriginalStatus)
			if e != nil {
				return e
			}
			if digest(statusPayloadFor(actual)) != digestPayload(input) {
				return recordstore.ErrConflict
			}
		}
		_, err = tx.Get(ctx, completionKind, id)
		if err == nil {
			return recordstore.ErrConflict
		}
		if !soleNotFound(err) {
			return err
		}
		j.Revision++
		j.LeaseActor, j.LeaseToken, j.LeasedUntil = "", "", time.Time{}
		originalStatus := j.OriginalStatus
		j.OriginalStatus = nil
		j.LastCompletionID = id
		if j.Source.Kind == billing.LifecycleCheckoutSources {
			j.Lane = RetiredLane
		} else {
			j.Lane = RefreshLane
			if j.CadenceAnchor.IsZero() {
				j.CadenceAnchor = c.NativeRequestedAt
			}
			j.Attempts = 0
			j.NextAttemptAt = next
		}
		c.CompletedAt = before.UTC()
		result := CompletedExecution{Job: j, Receipt: c, OriginalStatus: originalStatus}
		completion, err := encodeCompletion(result)
		if err != nil {
			return err
		}
		// Decode once before publication to detach nested private native maps.
		result, err = decodeCompletion(completion, k)
		if err != nil {
			return err
		}
		job, err := jobRecord(result.Job)
		if err != nil {
			return err
		}
		if err := tx.Insert(ctx, completion); err != nil {
			return err
		}
		now := r.clock.Now()
		if now.IsZero() || now.Before(before) || !h.Until.After(now) || (j.Source.Kind == billing.LifecycleSubscriptionSources && !next.After(now)) {
			return recordstore.ErrConflict
		}
		if err := tx.Replace(ctx, job, h.Revision); err != nil {
			return err
		}
		out = result
		return nil
	})
	if err != nil {
		return CompletedExecution{}, err
	}
	return out, nil
}

// digestPayload compares canonical private codecs, without interpreting native
// business identity. Data was produced by encode/encodeStatus in this call.
func digestPayload(row recordstore.Record) string {
	// Hash the encoded bytes exactly as those existing codecs do through digest.
	// Use an explicit decode to compare values, not whitespace in JSON storage.
	if row.Kind == checkoutKind {
		var p checkoutPayload
		if strictScheduleDecode(row.Data, &p) == nil {
			return digest(p)
		}
	}
	if row.Kind == statusKind {
		var p statusPayload
		if strictScheduleDecode(row.Data, &p) == nil {
			return digest(p)
		}
	}
	return ""
}

// FindCompletion reads immutable historical success; it neither acquires a
// lease nor authorizes fresh work. Services rejoin current native proof/authority.
func (r *RecordExecutionRepository) FindCompletion(ctx context.Context, k CompletionKey) (CompletedExecution, error) {
	if err := r.executionReady(ctx); err != nil {
		return CompletedExecution{}, err
	}
	if !completionKeyShape(k) {
		return CompletedExecution{}, recordstore.ErrInvalid
	}
	id, _ := completionIdentity(k)
	var out CompletedExecution
	err := r.store.Read(ctx, func(tx recordstore.Tx) error {
		out = CompletedExecution{}
		row, e := tx.Get(ctx, completionKind, id)
		if e != nil {
			return e
		}
		out, e = decodeCompletion(row, k)
		return e
	})
	if err == nil {
		err = ctx.Err()
	}
	if err != nil {
		return CompletedExecution{}, err
	}
	return out, nil
}

// FindLastCompletion joins the durable job reference and immutable completion in
// one snapshot. This supports crash inspection after the active pointer cleared.
// The returned historical job cannot authorize a subsequent execution.
func (r *RecordExecutionRepository) FindLastCompletion(ctx context.Context, source ScheduledSource) (CompletedExecution, error) {
	if err := r.executionReady(ctx); err != nil {
		return CompletedExecution{}, err
	}
	if !scheduleSourceShape(source) {
		return CompletedExecution{}, recordstore.ErrInvalid
	}
	var out CompletedExecution
	err := r.store.Read(ctx, func(tx recordstore.Tx) error {
		out = CompletedExecution{}
		current, e := selectedJob(ctx, tx, scheduledIdentity(source), digest(scheduleSource(source)))
		if e != nil {
			return e
		}
		if current.LastCompletionID == "" {
			return recordstore.ErrNotFound
		}
		row, e := tx.Get(ctx, completionKind, current.LastCompletionID)
		if e != nil {
			if soleNotFound(e) {
				return errors.Join(recordstore.ErrUnavailable, e)
			}
			return e
		}
		var payload completionPayload
		if strictScheduleDecode(row.Data, &payload) != nil {
			return recordstore.ErrUnavailable
		}
		out, e = decodeCompletion(row, CompletionKey{source, payload.NativeCaptureID})
		if e != nil {
			return e
		}
		if out.Job.Revision > current.Revision || out.Job.Fence > current.Fence || out.Job.LastCompletionID != current.LastCompletionID {
			return recordstore.ErrUnavailable
		}
		return nil
	})
	if err == nil {
		err = ctx.Err()
	}
	if err != nil {
		return CompletedExecution{}, err
	}
	return out, nil
}
