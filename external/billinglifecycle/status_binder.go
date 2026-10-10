package billinglifecycle

import (
	"context"
	"errors"

	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/repository/recordstore"
)

// StatusBinder owns current authority and original-input conservation around
// atomic storage. It does not prepare, look up, capture or complete an observation.
// Compose its scheduler, repository and configured billing validator over the
// same worker authority/store. Construction creates no identity or grants.
type StatusBinder struct {
	scheduler *Scheduler
	repo      StatusBindingRepository
	validator StatusValidator
}

// NewStatusBinder requires a fully constructed scheduler, a binding repository
// and a status validator. It performs no storage or authority calls.
func NewStatusBinder(scheduler *Scheduler, repo StatusBindingRepository, validator StatusValidator) (*StatusBinder, error) {
	if scheduler == nil || nilPort(scheduler.repo) || nilPort(scheduler.authority) || nilPort(scheduler.clock) || nilPort(repo) || nilPort(validator) {
		return nil, billing.ErrRevenueUnavailable
	}
	return &StatusBinder{scheduler, repo, validator}, nil
}

// finish re-checks scheduler authority and validates the preparation after a
// bind operation, returning the original error on success. Uncertain operation
// errors are joined so unknown commits remain visible.
func (b *StatusBinder) finish(ctx context.Context, h LeaseHandle, p billing.SubscriptionStatusPreparation, operationErr error) error {
	preserve := func(err error) error {
		if errors.Is(operationErr, recordstore.ErrUncertain) {
			return errors.Join(err, operationErr)
		}
		return err
	}
	if err := b.scheduler.finish(ctx, []ScheduledSource{h.Source}, nil); err != nil {
		return preserve(err)
	}
	if err := b.validator.ValidateSubscriptionStatusPreparation(ctx, b.scheduler.cfg.ActorID, p); err != nil {
		return preserve(err)
	}
	if err := b.scheduler.finish(ctx, []ScheduledSource{h.Source}, nil); err != nil {
		return preserve(err)
	}
	return operationErr
}

// Bind consumes one exact live handle and returns only an acknowledged, current
// successor. Callers must supply the existing job original verbatim on recovery;
// a fresh preparation cannot replace an unresolved observation. On uncertainty,
// read current job and original input under current authority before continuing.
// Any failure after storage was attempted requires that inspection. In particular,
// denial or lost-lease conflict after an acknowledged write does not mean rollback;
// the commit is known, so those outcomes must not be mislabeled uncertain.
func (b *StatusBinder) Bind(ctx context.Context, h LeaseHandle, input StatusInput) (StatusBinding, error) {
	if b == nil || b.scheduler == nil || nilPort(b.repo) || nilPort(b.validator) {
		return StatusBinding{}, billing.ErrRevenueUnavailable
	}
	j, err := b.scheduler.Check(ctx, h)
	if err != nil {
		return StatusBinding{}, err
	}
	p := input.Preparation
	if p.Validate() != nil || !statusMatchesSource(p, h.Source) {
		return StatusBinding{}, b.scheduler.finish(ctx, []ScheduledSource{h.Source}, billing.ErrRevenueInvalid)
	}
	if err := b.validator.ValidateSubscriptionStatusPreparation(ctx, b.scheduler.cfg.ActorID, p); err != nil {
		return StatusBinding{}, b.finish(ctx, h, p, err)
	}
	if input.Evidence != nil && input.Evidence.Validate(p) != nil {
		return StatusBinding{}, b.finish(ctx, h, p, billing.ErrRevenueInvalid)
	}
	proposed := j
	proposed.OriginalStatus = &p
	if j.OriginalStatus != nil && !sameOriginalInputs(j, proposed) {
		return StatusBinding{}, b.finish(ctx, h, p, billing.ErrRevenueConflict)
	}
	if input.Evidence != nil && j.OriginalStatus == nil {
		return StatusBinding{}, b.finish(ctx, h, p, billing.ErrRevenueConflict)
	}
	if err := b.finish(ctx, h, p, nil); err != nil {
		return StatusBinding{}, err
	}
	out, err := b.repo.BindStatus(ctx, h, input)
	if err := b.finish(ctx, h, p, err); err != nil {
		return StatusBinding{}, err
	}
	if validateScheduledJob(out.Job) != nil || !sameScheduledSource(j.Source, out.Job.Source) || !sameOriginalInputs(proposed, out.Job) || out.Job.Revision != j.Revision+1 || out.Job.Fence != j.Fence || out.Job.Attempts != j.Attempts || out.Job.Lane != j.Lane || out.Job.LeaseActor != j.LeaseActor || out.Job.LeaseToken != j.LeaseToken || !out.Job.LeasedUntil.Equal(j.LeasedUntil) || !out.Job.CreatedAt.Equal(j.CreatedAt) || !out.Job.NextAttemptAt.Equal(j.NextAttemptAt) {
		return StatusBinding{}, b.finish(ctx, h, p, billing.ErrRevenueUnavailable)
	}
	retained := proposed
	retained.OriginalStatus = &out.Input.Preparation
	if !sameOriginalInputs(proposed, retained) || (out.Input.Evidence != nil && out.Input.Evidence.Validate(p) != nil) || (input.Evidence != nil && (out.Input.Evidence == nil || *input.Evidence != *out.Input.Evidence)) {
		return StatusBinding{}, b.finish(ctx, h, p, billing.ErrRevenueUnavailable)
	}
	// An acknowledged transaction may have lost its lease before its reply.
	// Verify the new revision and original against the current durable job.
	current, err := b.scheduler.Check(ctx, JobLease(out.Job))
	if err != nil {
		return StatusBinding{}, b.finish(ctx, h, p, err)
	}
	if !sameOriginalInputs(out.Job, current) {
		return StatusBinding{}, b.finish(ctx, h, p, billing.ErrRevenueUnavailable)
	}
	if err := b.finish(ctx, h, p, nil); err != nil {
		return StatusBinding{}, err
	}
	return out, nil
}
