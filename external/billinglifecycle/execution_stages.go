package billinglifecycle

import (
	"context"
	"errors"
	"time"

	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/paymentprovider"
	"github.com/ooaklee/ghatd/external/repository/recordstore"
)

// These private owning ports must be the configured billing manager. They are
// derived from each outbox's validator so binding, lookup and capture cannot
// silently use a different manager. No raw billing repository enters a service.
type CheckoutExecutionOwner interface {
	CheckoutValidator
	PrepareCheckoutLifecycle(context.Context, string, billing.CheckoutIntent) (billing.CheckoutIntent, error)
	FindCheckoutLifecycleReceipt(context.Context, string, billing.CheckoutIntent) (billing.CheckoutLifecycleAnchor, error)
	LookupCheckoutLifecycleEvidence(context.Context, string, billing.CheckoutIntent) (paymentprovider.RevenueCheckoutEvidence, error)
	CaptureCheckoutLifecycleEvidence(context.Context, string, billing.CheckoutIntent, paymentprovider.RevenueCheckoutEvidence) (billing.CheckoutLifecycleAnchor, error)
}

type StatusExecutionOwner interface {
	StatusValidator
	PrepareSubscriptionStatus(context.Context, string, string) (billing.SubscriptionStatusPreparation, error)
	PrepareSubscriptionStatusForCheckout(context.Context, string, billing.RevenueScope, string) (billing.SubscriptionStatusPreparation, error)
	LookupSubscriptionStatus(context.Context, string, billing.SubscriptionStatusPreparation) (billing.VerifiedSubscriptionStatusEvidence, error)
	CaptureSubscriptionStatus(context.Context, string, billing.SubscriptionStatusPreparation, billing.VerifiedSubscriptionStatusEvidence) (billing.SubscriptionStatus, error)
}

// An observation is a confirmed owning receipt under a still-current handle,
// not job completion. Fenced retirement/rescheduling must consume it separately.
// Any error withholds all data and may leave committed inputs or native receipts.
type CheckoutObservation struct {
	Job     ScheduledJob                    `json:"-"`
	Input   CheckoutInput                   `json:"-"`
	Receipt billing.CheckoutLifecycleAnchor `json:"-"`
}
type StatusObservation struct {
	Job     ScheduledJob               `json:"-"`
	Input   StatusInput                `json:"-"`
	Receipt billing.SubscriptionStatus `json:"-"`
}

type CheckoutExecution struct {
	scheduler *Scheduler
	binder    *CheckoutBinder
	outbox    *CheckoutOutbox
	owner     CheckoutExecutionOwner
}
type StatusExecution struct {
	scheduler *Scheduler
	binder    *StatusBinder
	outbox    *StatusOutbox
	owner     StatusExecutionOwner
}

func NewCheckoutExecution(s *Scheduler, repo CheckoutBindingRepository, outbox *CheckoutOutbox) (*CheckoutExecution, error) {
	if outbox == nil || nilPort(outbox.store) || nilPort(outbox.validator) {
		return nil, billing.ErrRevenueUnavailable
	}
	owner, ok := outbox.validator.(CheckoutExecutionOwner)
	if !ok || nilPort(owner) {
		return nil, billing.ErrRevenueUnavailable
	}
	binder, err := NewCheckoutBinder(s, repo, owner)
	if err != nil {
		return nil, err
	}
	return &CheckoutExecution{s, binder, outbox, owner}, nil
}
func NewStatusExecution(s *Scheduler, repo StatusBindingRepository, outbox *StatusOutbox) (*StatusExecution, error) {
	if outbox == nil || nilPort(outbox.store) || nilPort(outbox.validator) {
		return nil, billing.ErrRevenueUnavailable
	}
	owner, ok := outbox.validator.(StatusExecutionOwner)
	if !ok || nilPort(owner) {
		return nil, billing.ErrRevenueUnavailable
	}
	binder, err := NewStatusBinder(s, repo, owner)
	if err != nil {
		return nil, err
	}
	return &StatusExecution{s, binder, outbox, owner}, nil
}

// Checks are outside foreign transactions, not cross-domain fencing. Preserve
// either domain's observed uncertainty when later lease/authority checks fail.
func executionStage(ctx context.Context, s *Scheduler, h LeaseHandle, operationErr error) (ScheduledJob, error) {
	j, err := s.Check(ctx, h)
	if err != nil {
		if errors.Is(operationErr, billing.ErrRevenueUncertain) || errors.Is(operationErr, recordstore.ErrUncertain) {
			err = errors.Join(err, operationErr)
		}
		return ScheduledJob{}, err
	}
	if operationErr != nil {
		return ScheduledJob{}, operationErr
	}
	return j, nil
}

// A foreign operation does not authorize metadata changes under the same
// revision. Reject inconsistent adapter responses as well as a lost lease.
func executionStep(ctx context.Context, s *Scheduler, expected ScheduledJob, operationErr error) (ScheduledJob, error) {
	current, err := executionStage(ctx, s, JobLease(expected), operationErr)
	if err != nil {
		return ScheduledJob{}, err
	}
	if !sameOriginalInputs(expected, current) || current.Attempts != expected.Attempts || !current.CreatedAt.Equal(expected.CreatedAt) || !current.NextAttemptAt.Equal(expected.NextAttemptAt) {
		return ScheduledJob{}, billing.ErrRevenueUnavailable
	}
	return current, nil
}

func soleRevenueNotFound(err error) bool {
	for n := 0; n < 64 && err != nil; n++ {
		if err == billing.ErrRevenueNotFound {
			return true
		}
		if _, joined := err.(interface{ Unwrap() []error }); joined {
			return false
		}
		err = errors.Unwrap(err)
	}
	return false
}

// Observe inspects the owning receipt before any fresh provider lookup. It binds
// preparation before lookup and evidence before capture. Replacement executions
// reuse retained evidence verbatim. It does not retire the job or spawn work.
func (x *CheckoutExecution) Observe(ctx context.Context, h LeaseHandle) (CheckoutObservation, error) {
	if x == nil || x.scheduler == nil || x.binder == nil || x.outbox == nil || nilPort(x.owner) {
		return CheckoutObservation{}, billing.ErrRevenueUnavailable
	}
	j, err := executionStage(ctx, x.scheduler, h, nil)
	if err != nil {
		return CheckoutObservation{}, err
	}
	if j.Source.Kind != billing.LifecycleCheckoutSources {
		return CheckoutObservation{}, billing.ErrRevenueInvalid
	}
	i := *j.Source.Checkout
	receipt, receiptErr := x.owner.FindCheckoutLifecycleReceipt(ctx, x.scheduler.cfg.ActorID, i)
	// A sole native absence permits continuing; a joined absence/outage does not.
	receiptStageErr := receiptErr
	if soleRevenueNotFound(receiptErr) {
		receiptStageErr = nil
	}
	if _, err = executionStep(ctx, x.scheduler, j, receiptStageErr); err != nil {
		return CheckoutObservation{}, err
	}
	captured := receiptErr == nil
	if receiptErr != nil && !soleRevenueNotFound(receiptErr) {
		return CheckoutObservation{}, receiptErr
	}
	if captured && receipt.ValidateForCheckout(i) != nil {
		return CheckoutObservation{}, billing.ErrRevenueUnavailable
	}
	var input CheckoutInput
	if !j.CheckoutPrepared {
		original, e := x.owner.PrepareCheckoutLifecycle(ctx, x.scheduler.cfg.ActorID, i)
		if _, err = executionStep(ctx, x.scheduler, j, e); err != nil {
			return CheckoutObservation{}, err
		}
		if original.ValidateAcknowledgedInput(i) != nil {
			return CheckoutObservation{}, billing.ErrRevenueUnavailable
		}
		bound, e := x.binder.Bind(ctx, h, CheckoutInput{Intent: original})
		if e != nil {
			return CheckoutObservation{}, e
		}
		j, input = bound.Job, bound.Input
		h = JobLease(j)
	} else {
		input, err = x.outbox.Find(ctx, x.scheduler.cfg.ActorID, i)
		if _, e := executionStep(ctx, x.scheduler, j, err); e != nil {
			if soleNotFound(e) {
				e = errors.Join(recordstore.ErrUnavailable, e)
			}
			return CheckoutObservation{}, e
		}
	}
	if captured {
		if input.Evidence == nil {
			e := receipt.Evidence
			bound, bindErr := x.binder.Bind(ctx, h, CheckoutInput{Intent: input.Intent, Evidence: &e})
			if bindErr != nil {
				return CheckoutObservation{}, bindErr
			}
			j, input = bound.Job, bound.Input
		}
		if receipt.ValidateCapturedEvidence(input.Intent, *input.Evidence) != nil {
			return CheckoutObservation{}, billing.ErrRevenueConflict
		}
	} else {
		if input.Evidence == nil {
			e, lookupErr := x.owner.LookupCheckoutLifecycleEvidence(ctx, x.scheduler.cfg.ActorID, input.Intent)
			if _, err = executionStep(ctx, x.scheduler, j, lookupErr); err != nil {
				return CheckoutObservation{}, err
			}
			if input.Intent.ValidateLifecycleEvidence(e) != nil {
				return CheckoutObservation{}, billing.ErrRevenueUnavailable
			}
			bound, bindErr := x.binder.Bind(ctx, h, CheckoutInput{Intent: input.Intent, Evidence: &e})
			if bindErr != nil {
				return CheckoutObservation{}, bindErr
			}
			j, input = bound.Job, bound.Input
		}
		receipt, err = x.owner.CaptureCheckoutLifecycleEvidence(ctx, x.scheduler.cfg.ActorID, input.Intent, *input.Evidence)
		if _, err = executionStep(ctx, x.scheduler, j, err); err != nil {
			return CheckoutObservation{}, err
		}
		if receipt.ValidateCapturedEvidence(input.Intent, *input.Evidence) != nil {
			return CheckoutObservation{}, billing.ErrRevenueUnavailable
		}
	}
	j, err = executionStep(ctx, x.scheduler, j, nil)
	if err != nil {
		return CheckoutObservation{}, err
	}
	return CheckoutObservation{j, input, receipt}, nil
}

func sameStatusPreparation(a, b billing.SubscriptionStatusPreparation) bool {
	at, bt := a.RequestedAt, b.RequestedAt
	a.RequestedAt, b.RequestedAt = time.Time{}, time.Time{}
	return a == b && at.Equal(bt)
}

// Observe reuses the active original, including its original RequestedAt and
// preparing author. Exact capture replays a receipt before native head CAS. A
// conflict/unknown never authorizes replacing the unresolved original. Confirmed
// pointer clearing and recurring rescheduling are separate fenced operations.
func (x *StatusExecution) Observe(ctx context.Context, h LeaseHandle) (StatusObservation, error) {
	if x == nil || x.scheduler == nil || x.binder == nil || x.outbox == nil || nilPort(x.owner) {
		return StatusObservation{}, billing.ErrRevenueUnavailable
	}
	j, err := executionStage(ctx, x.scheduler, h, nil)
	if err != nil {
		return StatusObservation{}, err
	}
	if j.Source.Kind != billing.LifecycleSubscriptionSources {
		return StatusObservation{}, billing.ErrRevenueInvalid
	}
	var input StatusInput
	if j.OriginalStatus == nil {
		var p billing.SubscriptionStatusPreparation
		if j.Source.FactID != "" {
			p, err = x.owner.PrepareSubscriptionStatus(ctx, x.scheduler.cfg.ActorID, j.Source.FactID)
		} else {
			p, err = x.owner.PrepareSubscriptionStatusForCheckout(ctx, x.scheduler.cfg.ActorID, j.Source.Scope, j.Source.SubscriptionID)
		}
		if _, err = executionStep(ctx, x.scheduler, j, err); err != nil {
			return StatusObservation{}, err
		}
		if p.Validate() != nil || !statusMatchesSource(p, j.Source) || p.ActorID != x.scheduler.cfg.ActorID || (j.Source.FactID != "" && (p.Source != "" || p.FactID != j.Source.FactID)) || (j.Source.FactID == "" && p.Source != billing.SubscriptionStatusCheckoutSource) {
			return StatusObservation{}, billing.ErrRevenueUnavailable
		}
		bound, e := x.binder.Bind(ctx, h, StatusInput{Preparation: p})
		if e != nil {
			return StatusObservation{}, e
		}
		j, input = bound.Job, bound.Input
		h = JobLease(j)
	} else {
		input, err = x.outbox.Find(ctx, x.scheduler.cfg.ActorID, *j.OriginalStatus)
		if _, e := executionStep(ctx, x.scheduler, j, err); e != nil {
			if soleNotFound(e) {
				e = errors.Join(recordstore.ErrUnavailable, e)
			}
			return StatusObservation{}, e
		}
	}
	if input.Evidence == nil {
		e, lookupErr := x.owner.LookupSubscriptionStatus(ctx, x.scheduler.cfg.ActorID, input.Preparation)
		if _, err = executionStep(ctx, x.scheduler, j, lookupErr); err != nil {
			return StatusObservation{}, err
		}
		if e.Validate(input.Preparation) != nil {
			return StatusObservation{}, billing.ErrRevenueUnavailable
		}
		bound, bindErr := x.binder.Bind(ctx, h, StatusInput{Preparation: input.Preparation, Evidence: &e})
		if bindErr != nil {
			return StatusObservation{}, bindErr
		}
		j, input = bound.Job, bound.Input
	}
	receipt, err := x.owner.CaptureSubscriptionStatus(ctx, x.scheduler.cfg.ActorID, input.Preparation, *input.Evidence)
	if _, err = executionStep(ctx, x.scheduler, j, err); err != nil {
		return StatusObservation{}, err
	}
	if receipt.Validate() != nil || !sameStatusPreparation(receipt.Preparation, input.Preparation) || receipt.Status != input.Evidence.Status || receipt.CancellationScheduled != input.Evidence.CancellationScheduled {
		return StatusObservation{}, billing.ErrRevenueUnavailable
	}
	j, err = executionStep(ctx, x.scheduler, j, nil)
	if err != nil {
		return StatusObservation{}, err
	}
	return StatusObservation{j, input, receipt}, nil
}
