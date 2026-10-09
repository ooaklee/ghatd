package billinglifecycle

import (
	"context"
	"errors"
	"time"

	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/paymentprovider"
	"github.com/ooaklee/ghatd/external/repository/recordstore"
)

// CheckoutBinder owns current authority and original-input conservation around
// atomic storage. It does not look up, capture or complete a checkout.
// Compose its scheduler, repository and configured billing validator over the
// same worker authority/store. Construction creates no identity or grants.
type CheckoutBinder struct {
	scheduler *Scheduler
	repo      CheckoutBindingRepository
	validator CheckoutValidator
}

func NewCheckoutBinder(scheduler *Scheduler, repo CheckoutBindingRepository, validator CheckoutValidator) (*CheckoutBinder, error) {
	if scheduler == nil || nilPort(scheduler.repo) || nilPort(scheduler.authority) || nilPort(scheduler.clock) || nilPort(repo) || nilPort(validator) {
		return nil, billing.ErrRevenueUnavailable
	}
	return &CheckoutBinder{scheduler, repo, validator}, nil
}

func (b *CheckoutBinder) finish(ctx context.Context, h LeaseHandle, p billing.CheckoutIntent, operationErr error) error {
	preserve := func(err error) error {
		if errors.Is(operationErr, recordstore.ErrUncertain) {
			return errors.Join(err, operationErr)
		}
		return err
	}
	if err := b.scheduler.finish(ctx, []ScheduledSource{h.Source}, nil); err != nil {
		return preserve(err)
	}
	if err := b.validator.ValidateCheckoutLifecycle(ctx, b.scheduler.cfg.ActorID, p); err != nil {
		return preserve(err)
	}
	if err := b.scheduler.finish(ctx, []ScheduledSource{h.Source}, nil); err != nil {
		return preserve(err)
	}
	return operationErr
}

// Bind consumes one exact live handle and returns only an acknowledged, current
// successor. Callers must supply the immutable acknowledged checkout from the job source.
// Another intent cannot replace the original. On uncertainty,
// read current job and original input under current authority before continuing.
// Any failure after storage was attempted requires that inspection. In particular,
// denial or lost-lease conflict after an acknowledged write does not mean rollback;
// the commit is known, so those outcomes must not be mislabeled uncertain.
func (b *CheckoutBinder) Bind(ctx context.Context, h LeaseHandle, input CheckoutInput) (CheckoutBinding, error) {
	if b == nil || b.scheduler == nil || nilPort(b.repo) || nilPort(b.validator) {
		return CheckoutBinding{}, billing.ErrRevenueUnavailable
	}
	j, err := b.scheduler.Check(ctx, h)
	if err != nil {
		return CheckoutBinding{}, err
	}
	p := input.Intent
	if p.ValidateAcknowledgedSubscription() != nil || !checkoutMatchesSource(input, h.Source) {
		return CheckoutBinding{}, b.scheduler.finish(ctx, []ScheduledSource{h.Source}, billing.ErrRevenueInvalid)
	}
	if err := b.validator.ValidateCheckoutLifecycle(ctx, b.scheduler.cfg.ActorID, p); err != nil {
		return CheckoutBinding{}, b.finish(ctx, h, p, err)
	}
	if input.Evidence != nil && p.ValidateLifecycleEvidence(*input.Evidence) != nil {
		return CheckoutBinding{}, b.finish(ctx, h, p, billing.ErrRevenueInvalid)
	}
	proposed := j
	proposed.CheckoutPrepared = true
	if input.Evidence != nil && !j.CheckoutPrepared {
		return CheckoutBinding{}, b.finish(ctx, h, p, billing.ErrRevenueConflict)
	}
	if err := b.finish(ctx, h, p, nil); err != nil {
		return CheckoutBinding{}, err
	}
	out, err := b.repo.BindCheckout(ctx, h, input)
	if err := b.finish(ctx, h, p, err); err != nil {
		return CheckoutBinding{}, err
	}
	if validateScheduledJob(out.Job) != nil || !sameScheduledSource(j.Source, out.Job.Source) || !sameOriginalInputs(proposed, out.Job) || out.Job.Revision != j.Revision+1 || out.Job.Fence != j.Fence || out.Job.Attempts != j.Attempts || out.Job.Lane != j.Lane || out.Job.LeaseActor != j.LeaseActor || out.Job.LeaseToken != j.LeaseToken || !out.Job.LeasedUntil.Equal(j.LeasedUntil) || !out.Job.CreatedAt.Equal(j.CreatedAt) || !out.Job.NextAttemptAt.Equal(j.NextAttemptAt) {
		return CheckoutBinding{}, b.finish(ctx, h, p, billing.ErrRevenueUnavailable)
	}
	if out.Input.Intent.ValidateAcknowledgedInput(p) != nil || (out.Input.Evidence != nil && p.ValidateLifecycleEvidence(*out.Input.Evidence) != nil) || (input.Evidence != nil && (out.Input.Evidence == nil || !sameCheckoutEvidence(*input.Evidence, *out.Input.Evidence))) {
		return CheckoutBinding{}, b.finish(ctx, h, p, billing.ErrRevenueUnavailable)
	}
	// An acknowledged transaction may have lost its lease before its reply.
	// Verify the new revision and original against the current durable job.
	current, err := b.scheduler.Check(ctx, JobLease(out.Job))
	if err != nil {
		return CheckoutBinding{}, b.finish(ctx, h, p, err)
	}
	if !sameOriginalInputs(out.Job, current) {
		return CheckoutBinding{}, b.finish(ctx, h, p, billing.ErrRevenueUnavailable)
	}
	if err := b.finish(ctx, h, p, nil); err != nil {
		return CheckoutBinding{}, err
	}
	return out, nil
}

func sameCheckoutEvidence(a, b paymentprovider.RevenueCheckoutEvidence) bool {
	at, bt := a.CreatedAt, b.CreatedAt
	a.CreatedAt, b.CreatedAt = time.Time{}, time.Time{}
	return a == b && at.Equal(bt)
}
