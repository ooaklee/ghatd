package billinglifecycle

import (
	"context"
	"errors"
	"time"

	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/repository/recordstore"
)

// NextRefreshSlot coalesces missed observations into the first strictly future
// slot aligned with the job's original cadence anchor. It has no provider I/O,
// unbounded catch-up loop or dependency on capture acknowledgement latency.
func NextRefreshSlot(anchor, now time.Time, cadence time.Duration) (time.Time, error) {
	if anchor.IsZero() || now.IsZero() || now.Before(anchor) || cadence < time.Second || cadence > 24*time.Hour {
		return time.Time{}, billing.ErrRevenueInvalid
	}
	elapsed := now.Sub(anchor)
	if !anchor.Add(elapsed).Equal(now) {
		return time.Time{}, billing.ErrRevenueInvalid
	}
	step := cadence - elapsed%cadence
	next := now.Add(step)
	if !next.After(now) || next.Sub(now) != step {
		return time.Time{}, billing.ErrRevenueInvalid
	}
	return next, nil
}

type CheckoutCompletion struct {
	execution *CheckoutExecution
	repo      CompletionRepository
}
type StatusCompletionService struct {
	execution *StatusExecution
	repo      CompletionRepository
	cadence   time.Duration
}

func NewCheckoutCompletion(x *CheckoutExecution, repo CompletionRepository) (*CheckoutCompletion, error) {
	if x == nil || x.scheduler == nil || x.binder == nil || x.outbox == nil || nilPort(x.owner) || nilPort(repo) {
		return nil, billing.ErrRevenueUnavailable
	}
	return &CheckoutCompletion{x, repo}, nil
}
func NewStatusCompletion(x *StatusExecution, repo CompletionRepository, cadence time.Duration) (*StatusCompletionService, error) {
	if x == nil || x.scheduler == nil || x.binder == nil || x.outbox == nil || nilPort(x.owner) || nilPort(repo) {
		return nil, billing.ErrRevenueUnavailable
	}
	if cadence < time.Second || cadence > 24*time.Hour {
		return nil, billing.ErrRevenueInvalid
	}
	return &StatusCompletionService{x, repo, cadence}, nil
}

func completionAuthority(ctx context.Context, s *Scheduler, source ScheduledSource, operationErr error) error {
	if err := s.finish(ctx, []ScheduledSource{source}, nil); err != nil {
		if errors.Is(operationErr, recordstore.ErrUncertain) || errors.Is(operationErr, billing.ErrRevenueUncertain) {
			return errors.Join(err, operationErr)
		}
		return err
	}
	return operationErr
}
func sameCompletion(a, b CompletedExecution) bool {
	if !sameExecutionJob(a.Job, b.Job) || a.Receipt.NativeCaptureID != b.Receipt.NativeCaptureID || a.Receipt.NativeFingerprint != b.Receipt.NativeFingerprint || !a.Receipt.NativeRequestedAt.Equal(b.Receipt.NativeRequestedAt) || !a.Receipt.NativeObservedAt.Equal(b.Receipt.NativeObservedAt) || !a.Receipt.CompletedAt.Equal(b.Receipt.CompletedAt) || (a.OriginalStatus == nil) != (b.OriginalStatus == nil) {
		return false
	}
	return a.OriginalStatus == nil || sameStatusPreparation(*a.OriginalStatus, *b.OriginalStatus)
}
func sameCheckoutInput(a, b CheckoutInput) bool {
	return a.Intent.ValidateAcknowledgedInput(b.Intent) == nil && a.Evidence != nil && b.Evidence != nil && sameCheckoutEvidence(*a.Evidence, *b.Evidence)
}
func sameStatusInput(a, b StatusInput) bool {
	return sameStatusPreparation(a.Preparation, b.Preparation) && a.Evidence != nil && b.Evidence != nil && *a.Evidence == *b.Evidence
}
func boundInputError(err error) error {
	if soleNotFound(err) {
		return errors.Join(recordstore.ErrUnavailable, err)
	}
	return err
}

// Run observes the original and commits its confirmed checkout retirement.
// Unknown/denied replies expose no payload; use InspectLast after any attempted
// completion failure. An expired or consumed handle cannot blindly replay writes.
func (c *CheckoutCompletion) Run(ctx context.Context, h LeaseHandle) (CompletedExecution, error) {
	if c == nil || c.execution == nil || nilPort(c.repo) {
		return CompletedExecution{}, billing.ErrRevenueUnavailable
	}
	o, err := c.execution.Observe(ctx, h)
	if err != nil {
		return CompletedExecution{}, err
	}
	return c.Complete(ctx, o)
}
func (c *CheckoutCompletion) Complete(ctx context.Context, o CheckoutObservation) (CompletedExecution, error) {
	if c == nil || c.execution == nil || nilPort(c.repo) {
		return CompletedExecution{}, billing.ErrRevenueUnavailable
	}
	x := c.execution
	if !checkoutObservationShape(o) {
		return CompletedExecution{}, billing.ErrRevenueInvalid
	}
	if _, err := executionStep(ctx, x.scheduler, o.Job, nil); err != nil {
		return CompletedExecution{}, err
	}
	retained, err := x.outbox.Find(ctx, x.scheduler.cfg.ActorID, o.Input.Intent)
	if _, err = executionStep(ctx, x.scheduler, o.Job, boundInputError(err)); err != nil {
		return CompletedExecution{}, err
	}
	if !sameCheckoutInput(retained, o.Input) {
		return CompletedExecution{}, billing.ErrRevenueConflict
	}
	native, err := x.owner.FindCheckoutLifecycleReceipt(ctx, x.scheduler.cfg.ActorID, o.Input.Intent)
	if _, err = executionStep(ctx, x.scheduler, o.Job, err); err != nil {
		return CompletedExecution{}, err
	}
	if native.ValidateCapturedEvidence(retained.Intent, *retained.Evidence) != nil || native.Fingerprint != o.Receipt.Fingerprint || !native.AnchoredAt.Equal(o.Receipt.AnchoredAt) {
		return CompletedExecution{}, billing.ErrRevenueConflict
	}
	out, err := c.repo.CompleteCheckout(ctx, o)
	if err = x.binder.finish(ctx, JobLease(o.Job), o.Input.Intent, err); err != nil {
		return CompletedExecution{}, err
	}
	expected := o.Job
	expected.Revision++
	expected.Lane = RetiredLane
	expected.LeaseActor, expected.LeaseToken, expected.LeasedUntil = "", "", time.Time{}
	id, _ := completionIdentity(CompletionKey{o.Job.Source, o.Input.Intent.ID})
	expected.LastCompletionID = id
	if !sameExecutionJob(expected, out.Job) || out.Receipt.NativeCaptureID != native.IntentID || out.Receipt.NativeFingerprint != native.Fingerprint || !out.Receipt.NativeRequestedAt.Equal(o.Input.Intent.CreatedAt) || !out.Receipt.NativeObservedAt.Equal(native.AnchoredAt) || out.Receipt.CompletedAt.After(x.scheduler.clock.Now()) || !completedShape(out, CompletionKey{o.Job.Source, o.Input.Intent.ID}) {
		return CompletedExecution{}, billing.ErrRevenueUnavailable
	}
	stored, err := c.repo.FindCompletion(ctx, CompletionKey{o.Job.Source, o.Input.Intent.ID})
	if err = x.binder.finish(ctx, JobLease(o.Job), o.Input.Intent, err); err != nil {
		return CompletedExecution{}, err
	}
	if !sameCompletion(out, stored) {
		return CompletedExecution{}, billing.ErrRevenueUnavailable
	}
	return out, nil
}

// InspectLast authenticates the current worker and native receipt while reading
// the job's durable completion reference. It requires no live lease: successful
// completion deliberately cleared it. The historical result grants no execution.
func (c *CheckoutCompletion) InspectLast(ctx context.Context, source ScheduledSource) (CompletedExecution, error) {
	if c == nil || c.execution == nil || nilPort(c.repo) {
		return CompletedExecution{}, billing.ErrRevenueUnavailable
	}
	x := c.execution
	if !scheduleSourceShape(source) || source.Kind != billing.LifecycleCheckoutSources {
		return CompletedExecution{}, billing.ErrRevenueInvalid
	}
	if err := x.scheduler.begin(ctx, source.Scope); err != nil {
		return CompletedExecution{}, err
	}
	if err := completionAuthority(ctx, x.scheduler, source, nil); err != nil {
		return CompletedExecution{}, err
	}
	out, err := c.repo.FindLastCompletion(ctx, source)
	if err = completionAuthority(ctx, x.scheduler, source, err); err != nil {
		return CompletedExecution{}, err
	}
	if !completedShape(out, CompletionKey{source, source.Checkout.ID}) {
		return CompletedExecution{}, billing.ErrRevenueUnavailable
	}
	native, err := x.owner.FindCheckoutLifecycleReceipt(ctx, x.scheduler.cfg.ActorID, *source.Checkout)
	if err = completionAuthority(ctx, x.scheduler, source, err); err != nil {
		return CompletedExecution{}, err
	}
	if native.ValidateForCheckout(*source.Checkout) != nil || native.Fingerprint != out.Receipt.NativeFingerprint || !native.AnchoredAt.Equal(out.Receipt.NativeObservedAt) {
		return CompletedExecution{}, billing.ErrRevenueConflict
	}
	return out, nil
}

// Run completes a confirmed original and schedules another lifecycle cycle.
// Payment/financial completion never retires this recurring status job.
func (c *StatusCompletionService) Run(ctx context.Context, h LeaseHandle) (CompletedExecution, error) {
	if c == nil || c.execution == nil || nilPort(c.repo) {
		return CompletedExecution{}, billing.ErrRevenueUnavailable
	}
	o, err := c.execution.Observe(ctx, h)
	if err != nil {
		return CompletedExecution{}, err
	}
	return c.Complete(ctx, o)
}
func (c *StatusCompletionService) Complete(ctx context.Context, o StatusObservation) (CompletedExecution, error) {
	if c == nil || c.execution == nil || nilPort(c.repo) {
		return CompletedExecution{}, billing.ErrRevenueUnavailable
	}
	x := c.execution
	if !statusObservationShape(o) {
		return CompletedExecution{}, billing.ErrRevenueInvalid
	}
	if _, err := executionStep(ctx, x.scheduler, o.Job, nil); err != nil {
		return CompletedExecution{}, err
	}
	retained, err := x.outbox.Find(ctx, x.scheduler.cfg.ActorID, o.Input.Preparation)
	if _, err = executionStep(ctx, x.scheduler, o.Job, boundInputError(err)); err != nil {
		return CompletedExecution{}, err
	}
	if !sameStatusInput(retained, o.Input) {
		return CompletedExecution{}, billing.ErrRevenueConflict
	}
	native, err := x.owner.CaptureSubscriptionStatus(ctx, x.scheduler.cfg.ActorID, retained.Preparation, *retained.Evidence)
	if _, err = executionStep(ctx, x.scheduler, o.Job, err); err != nil {
		return CompletedExecution{}, err
	}
	if native.Validate() != nil || native.Fingerprint != o.Receipt.Fingerprint || native.Revision != o.Receipt.Revision || !sameStatusPreparation(native.Preparation, o.Receipt.Preparation) || !native.ObservedAt.Equal(o.Receipt.ObservedAt) {
		return CompletedExecution{}, billing.ErrRevenueConflict
	}
	anchor := o.Job.CadenceAnchor
	if anchor.IsZero() {
		anchor = o.Input.Preparation.RequestedAt
	}
	now := x.scheduler.clock.Now()
	if now.Before(native.ObservedAt) {
		return CompletedExecution{}, billing.ErrRevenueUnavailable
	}
	next, err := NextRefreshSlot(anchor, now, c.cadence)
	if err != nil {
		return CompletedExecution{}, err
	}
	out, err := c.repo.CompleteStatus(ctx, StatusCompletion{o, next})
	if err = x.binder.finish(ctx, JobLease(o.Job), o.Input.Preparation, err); err != nil {
		return CompletedExecution{}, err
	}
	expected := o.Job
	expected.Revision++
	expected.Lane = RefreshLane
	expected.Attempts = 0
	expected.OriginalStatus = nil
	expected.CadenceAnchor = anchor
	expected.NextAttemptAt = next
	expected.LeaseActor, expected.LeaseToken, expected.LeasedUntil = "", "", time.Time{}
	id, _ := completionIdentity(CompletionKey{o.Job.Source, o.Input.Preparation.CaptureID})
	expected.LastCompletionID = id
	if !sameExecutionJob(expected, out.Job) || out.OriginalStatus == nil || !sameStatusPreparation(*out.OriginalStatus, o.Input.Preparation) || out.Receipt.NativeCaptureID != native.Preparation.CaptureID || out.Receipt.NativeFingerprint != native.Fingerprint || !out.Receipt.NativeRequestedAt.Equal(native.Preparation.RequestedAt) || !out.Receipt.NativeObservedAt.Equal(native.ObservedAt) || out.Receipt.CompletedAt.After(x.scheduler.clock.Now()) || !completedShape(out, CompletionKey{o.Job.Source, o.Input.Preparation.CaptureID}) {
		return CompletedExecution{}, billing.ErrRevenueUnavailable
	}
	stored, err := c.repo.FindCompletion(ctx, CompletionKey{o.Job.Source, o.Input.Preparation.CaptureID})
	if err = x.binder.finish(ctx, JobLease(o.Job), o.Input.Preparation, err); err != nil {
		return CompletedExecution{}, err
	}
	if !sameCompletion(out, stored) {
		return CompletedExecution{}, billing.ErrRevenueUnavailable
	}
	return out, nil
}

// InspectLast rejoins the durable original preparation, retained evidence and
// immutable native receipt under CURRENT permission. It performs no provider
// lookup and survives later native heads or a newer active host cycle.
func (c *StatusCompletionService) InspectLast(ctx context.Context, source ScheduledSource) (CompletedExecution, error) {
	if c == nil || c.execution == nil || nilPort(c.repo) {
		return CompletedExecution{}, billing.ErrRevenueUnavailable
	}
	x := c.execution
	if !scheduleSourceShape(source) || source.Kind != billing.LifecycleSubscriptionSources {
		return CompletedExecution{}, billing.ErrRevenueInvalid
	}
	if err := x.scheduler.begin(ctx, source.Scope); err != nil {
		return CompletedExecution{}, err
	}
	if err := completionAuthority(ctx, x.scheduler, source, nil); err != nil {
		return CompletedExecution{}, err
	}
	out, err := c.repo.FindLastCompletion(ctx, source)
	if err = completionAuthority(ctx, x.scheduler, source, err); err != nil {
		return CompletedExecution{}, err
	}
	if out.OriginalStatus == nil || !completedShape(out, CompletionKey{source, out.Receipt.NativeCaptureID}) {
		return CompletedExecution{}, billing.ErrRevenueUnavailable
	}
	retained, err := x.outbox.Find(ctx, x.scheduler.cfg.ActorID, *out.OriginalStatus)
	if err = completionAuthority(ctx, x.scheduler, source, boundInputError(err)); err != nil {
		return CompletedExecution{}, err
	}
	if retained.Evidence == nil {
		return CompletedExecution{}, billing.ErrRevenueUnavailable
	}
	native, err := x.owner.CaptureSubscriptionStatus(ctx, x.scheduler.cfg.ActorID, retained.Preparation, *retained.Evidence)
	if err = completionAuthority(ctx, x.scheduler, source, err); err != nil {
		return CompletedExecution{}, err
	}
	if native.Validate() != nil || !sameStatusPreparation(native.Preparation, *out.OriginalStatus) || native.Fingerprint != out.Receipt.NativeFingerprint || !native.ObservedAt.Equal(out.Receipt.NativeObservedAt) {
		return CompletedExecution{}, billing.ErrRevenueConflict
	}
	return out, nil
}
