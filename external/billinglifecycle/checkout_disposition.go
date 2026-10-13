package billinglifecycle

import (
	"context"
	"time"

	"github.com/ooaklee/ghatd/external/billing"
)

type checkoutStatusOwner interface {
	SupportsCheckoutLifecycleStatus(billing.RevenueScope) bool
	LookupCheckoutLifecycleStatus(context.Context, string, billing.CheckoutIntent) (billing.CheckoutLifecycleStatus, error)
	FindCheckoutLifecycleExpiry(context.Context, string, billing.CheckoutIntent) (billing.CheckoutLifecycleExpiry, error)
}

// checkoutDisposition is operational progress, not payment or entitlement.
type checkoutDisposition struct {
	Job              ScheduledJob
	Pending, Expired bool
	stage            string
}

// runDisposition retains preparation before a status probe. Optional status and
// atomic expiry ports must both exist; custom adapters keep the original capture
// flow. Captured/retained complete evidence always wins over a fresh status read.
func (c *CheckoutCompletion) runDisposition(ctx context.Context, h LeaseHandle) (result checkoutDisposition, resultErr error) {
	stage := "checkout_observe"
	defer func() { result.stage = stage }()
	x := c.execution
	owner, statusOK := x.owner.(checkoutStatusOwner)
	repo, expiryOK := c.repo.(CheckoutExpiryRepository)
	if !statusOK || nilPort(owner) || !owner.SupportsCheckoutLifecycleStatus(h.Source.Scope) || !expiryOK || nilPort(repo) {
		observed, err := x.Observe(ctx, h)
		if err != nil {
			return checkoutDisposition{}, err
		}
		stage = "checkout_complete"
		out, err := c.Complete(ctx, observed)
		return checkoutDisposition{Job: out.Job}, err
	}
	prepared, captured, err := x.prepareObservation(ctx, h)
	if err != nil {
		return checkoutDisposition{}, err
	}
	j, input := prepared.Job, prepared.Input
	if captured || input.Evidence != nil {
		observed, err := x.observePrepared(ctx, prepared, captured)
		if err != nil {
			return checkoutDisposition{Job: j}, err
		}
		stage = "checkout_complete"
		out, err := c.Complete(ctx, observed)
		return checkoutDisposition{Job: out.Job}, err
	}
	status, err := owner.LookupCheckoutLifecycleStatus(ctx, x.scheduler.cfg.ActorID, input.Intent)
	if _, err = executionStep(ctx, x.scheduler, j, err); err != nil {
		return checkoutDisposition{Job: j}, err
	}
	if status.ValidateFresh(input.Intent, x.scheduler.clock.Now()) != nil {
		return checkoutDisposition{Job: j}, billing.ErrRevenueUnassessable
	}
	switch status.Evidence.SessionStatus {
	case "complete":
		observed, err := x.observePrepared(ctx, prepared, false)
		if err != nil {
			return checkoutDisposition{Job: j}, err
		}
		stage = "checkout_complete"
		out, err := c.Complete(ctx, observed)
		return checkoutDisposition{Job: out.Job}, err
	case "open":
		if status.Evidence.PaymentStatus != "unpaid" {
			return checkoutDisposition{Job: j}, billing.ErrRevenueUnassessable
		}
		// Expected pending uses the same bounded durable acquisition backoff.
		// The original stays prepared; a later pass must read fresh status.
		stage = "checkout_pending"
		out, err := x.scheduler.Retry(ctx, JobLease(j))
		if err = x.binder.finish(ctx, JobLease(j), input.Intent, err); err != nil {
			return checkoutDisposition{}, err
		}
		return checkoutDisposition{Job: out, Pending: true}, nil
	case "expired":
		proof, err := billing.NewCheckoutLifecycleExpiry(input.Intent, status)
		if err != nil {
			return checkoutDisposition{Job: j}, err
		}
		stage = "checkout_expire"
		out, err := c.completeExpiry(ctx, repo, CheckoutExpiryObservation{Job: j, Input: input, Expiry: proof})
		return checkoutDisposition{Job: out.Job, Expired: err == nil}, err
	default:
		return checkoutDisposition{Job: j}, billing.ErrRevenueUnassessable
	}
}

func (c *CheckoutCompletion) completeExpiry(ctx context.Context, repo CheckoutExpiryRepository, o CheckoutExpiryObservation) (CompletedExecution, error) {
	x := c.execution
	if !expiryObservationShape(o) {
		return CompletedExecution{}, billing.ErrRevenueInvalid
	}
	if _, err := executionStep(ctx, x.scheduler, o.Job, nil); err != nil {
		return CompletedExecution{}, err
	}
	if err := x.binder.finish(ctx, JobLease(o.Job), o.Input.Intent, nil); err != nil {
		return CompletedExecution{}, err
	}
	out, err := repo.ExpireCheckout(ctx, o)
	if err = x.binder.finish(ctx, JobLease(o.Job), o.Input.Intent, err); err != nil {
		return CompletedExecution{}, err
	}
	expected := o.Job
	expected.Revision++
	expected.Lane = RetiredLane
	expected.LeaseActor, expected.LeaseToken, expected.LeasedUntil = "", "", time.Time{}
	expected.LastCompletionID, _ = completionIdentity(CompletionKey{o.Job.Source, o.Input.Intent.ID})
	if !sameExecutionJob(expected, out.Job) || out.Expiry == nil || out.Expiry.Fingerprint != o.Expiry.Fingerprint || !completedShape(out, CompletionKey{o.Job.Source, o.Input.Intent.ID}) || out.Receipt.CompletedAt.After(x.scheduler.clock.Now()) {
		return CompletedExecution{}, billing.ErrRevenueUnavailable
	}
	stored, err := c.InspectLast(ctx, o.Job.Source)
	if err != nil {
		return CompletedExecution{}, err
	}
	if !sameCompletion(out, stored) {
		return CompletedExecution{}, billing.ErrRevenueUnavailable
	}
	current, err := x.scheduler.Inspect(ctx, o.Job.Source)
	if err != nil {
		return CompletedExecution{}, err
	}
	if !sameExecutionJob(current, out.Job) {
		return CompletedExecution{}, billing.ErrRevenueUnavailable
	}
	return out, nil
}

func (c *CheckoutCompletion) inspectExpiry(ctx context.Context, out CompletedExecution) (CompletedExecution, error) {
	x := c.execution
	owner, ok := x.owner.(checkoutStatusOwner)
	repo, repoOK := c.repo.(CheckoutExpiryRepository)
	if !ok || nilPort(owner) || !repoOK || nilPort(repo) || out.Expiry == nil {
		return CompletedExecution{}, billing.ErrRevenueUnavailable
	}
	i := *out.Job.Source.Checkout
	proof, err := owner.FindCheckoutLifecycleExpiry(ctx, x.scheduler.cfg.ActorID, i)
	if err = x.binder.finish(ctx, JobLease(out.Job), i, err); err != nil {
		return CompletedExecution{}, err
	}
	if proof.ValidateForCheckout(i) != nil || digest(expiryProof(proof)) != digest(expiryProof(*out.Expiry)) {
		return CompletedExecution{}, billing.ErrRevenueConflict
	}
	retained, err := repo.FindExpiredCheckoutInput(ctx, i)
	if err = x.binder.finish(ctx, JobLease(out.Job), i, err); err != nil {
		return CompletedExecution{}, err
	}
	if digest(expiryProof(proof)) != digest(expiryProof(retained)) {
		return CompletedExecution{}, billing.ErrRevenueConflict
	}
	return out, nil
}
