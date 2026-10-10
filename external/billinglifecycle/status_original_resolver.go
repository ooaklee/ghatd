package billinglifecycle

import (
	"context"

	"github.com/ooaklee/ghatd/external/billing"
)

// StatusOriginalResolutionOwner is optional on the same configured manager.
// Legacy execution ports stay compatible; no raw second billing owner is used.
type StatusOriginalResolutionOwner interface {
	// ResolveSubscriptionStatus returns the status resolution for the actor's
	// preparation from the optional same-configured manager; legacy execution ports
	// stay compatible without a second raw billing owner.
	ResolveSubscriptionStatus(context.Context, string, billing.SubscriptionStatusPreparation) (billing.SubscriptionStatusResolution, error)
}

// StatusOriginalOutcome reports the durable resolution state with the current
// job plus at most one observation or supersession result. It is private
// recovery evidence, not a lease or execution grant.
type StatusOriginalOutcome struct {
	State        string              `json:"-"`
	Job          ScheduledJob        `json:"-"`
	Observation  *StatusObservation  `json:"-"`
	Supersession *StatusSupersession `json:"-"`
}

// StatusOriginalResolver handles a retained original before fresh provider I/O.
// Pending preserves it; captured joins actual retained evidence; only native
// conclusive supersession can clear the pointer in a fenced host transaction.
type StatusOriginalResolver struct {
	execution *StatusExecution
	repo      StatusSupersessionRepository
	owner     StatusOriginalResolutionOwner
}

// NewStatusOriginalResolver derives its optional owner from the status
// execution's owner and requires a fully built execution and supersession
// repository. No storage or provider call occurs.
func NewStatusOriginalResolver(x *StatusExecution, repo StatusSupersessionRepository) (*StatusOriginalResolver, error) {
	if x == nil || x.scheduler == nil || x.binder == nil || x.outbox == nil || nilPort(x.owner) || nilPort(repo) {
		return nil, billing.ErrRevenueUnavailable
	}
	owner, ok := x.owner.(StatusOriginalResolutionOwner)
	if !ok || nilPort(owner) {
		return nil, billing.ErrRevenueUnavailable
	}
	return &StatusOriginalResolver{x, repo, owner}, nil
}

// ready verifies the resolver's borrowed execution components, repository and
// owner are all present.
func (s *StatusOriginalResolver) ready() bool {
	return s != nil && s.execution != nil && s.execution.scheduler != nil && s.execution.binder != nil && s.execution.outbox != nil && !nilPort(s.repo) && !nilPort(s.owner)
}

// Resolve requires an exact live lease with an attached original. A failed
// attempted write requires durable inspection, not blind replay. Supersession
// returns a verified successor handle; its immutable history grants no execution.
func (s *StatusOriginalResolver) Resolve(ctx context.Context, h LeaseHandle) (StatusOriginalOutcome, error) {
	if !s.ready() {
		return StatusOriginalOutcome{}, billing.ErrRevenueUnavailable
	}
	x := s.execution
	j, err := executionStage(ctx, x.scheduler, h, nil)
	if err != nil {
		return StatusOriginalOutcome{}, err
	}
	if j.Source.Kind != billing.LifecycleSubscriptionSources || j.OriginalStatus == nil {
		return StatusOriginalOutcome{}, billing.ErrRevenueInvalid
	}
	input, err := x.outbox.Find(ctx, x.scheduler.cfg.ActorID, *j.OriginalStatus)
	if _, err = executionStep(ctx, x.scheduler, j, boundInputError(err)); err != nil {
		return StatusOriginalOutcome{}, err
	}
	if !sameStatusPreparation(input.Preparation, *j.OriginalStatus) {
		return StatusOriginalOutcome{}, billing.ErrRevenueUnavailable
	}
	resolution, err := s.owner.ResolveSubscriptionStatus(ctx, x.scheduler.cfg.ActorID, input.Preparation)
	if _, err = executionStep(ctx, x.scheduler, j, err); err != nil {
		return StatusOriginalOutcome{}, err
	}
	if resolution.Validate() != nil || !sameStatusPreparation(resolution.Preparation, input.Preparation) {
		return StatusOriginalOutcome{}, billing.ErrRevenueUnavailable
	}
	switch resolution.State {
	case billing.SubscriptionStatusPending:
		return StatusOriginalOutcome{State: resolution.State, Job: j}, nil
	case billing.SubscriptionStatusCaptured:
		// Capture cannot repair a missing bound evidence stage. Actual retained
		// evidence must agree with the exact native receipt, not a fresh response.
		if input.Evidence == nil {
			return StatusOriginalOutcome{}, billing.ErrRevenueUnavailable
		}
		observation := StatusObservation{Job: j, Input: input, Receipt: *resolution.Receipt}
		if !statusObservationShape(observation) {
			return StatusOriginalOutcome{}, billing.ErrRevenueConflict
		}
		return StatusOriginalOutcome{State: resolution.State, Job: j, Observation: &observation}, nil
	case billing.SubscriptionStatusSuperseded:
		history, err := s.repo.SupersedeStatus(ctx, StatusSupersessionRequest{j, input, resolution})
		if err = x.binder.finish(ctx, h, input.Preparation, err); err != nil {
			return StatusOriginalOutcome{}, err
		}
		expected := j
		expected.Revision++
		expected.OriginalStatus = nil
		id, _ := statusSupersessionIdentity(StatusSupersessionKey{j.Source, input.Preparation.CaptureID})
		expected.LastSupersessionID = id
		if !sameExecutionJob(expected, history.Job) || !sameRetainedStatus(input, history.Input) || !sameNativeSupersession(resolution, history.Resolution) || !supersessionShape(history, StatusSupersessionKey{j.Source, input.Preparation.CaptureID}) || history.RecordedAt.After(x.scheduler.clock.Now()) {
			return StatusOriginalOutcome{}, billing.ErrRevenueUnavailable
		}
		// The lease is conserved, unlike confirmed completion. Check the exact
		// acknowledged successor before permitting the next owning preparation.
		current, err := executionStep(ctx, x.scheduler, history.Job, nil)
		if err != nil {
			return StatusOriginalOutcome{}, err
		}
		stored, err := s.repo.FindStatusSupersession(ctx, StatusSupersessionKey{j.Source, input.Preparation.CaptureID})
		if err = x.binder.finish(ctx, h, input.Preparation, err); err != nil {
			return StatusOriginalOutcome{}, err
		}
		if _, err = executionStep(ctx, x.scheduler, current, nil); err != nil {
			return StatusOriginalOutcome{}, err
		}
		if !sameStatusSupersession(history, stored) {
			return StatusOriginalOutcome{}, billing.ErrRevenueUnavailable
		}
		return StatusOriginalOutcome{State: resolution.State, Job: current, Supersession: &history}, nil
	}
	return StatusOriginalOutcome{}, billing.ErrRevenueUnavailable
}

// sameNativeSupersession compares two valid superseded resolutions, including
// their current status revisions and fingerprints, with preparations compared
// by instant-equal RequestedAt.
func sameNativeSupersession(a, b billing.SubscriptionStatusResolution) bool {
	if a.State != billing.SubscriptionStatusSuperseded || b.State != billing.SubscriptionStatusSuperseded || a.Validate() != nil || b.Validate() != nil || !sameStatusPreparation(a.Preparation, b.Preparation) {
		return false
	}
	x, y := *a.Current, *b.Current
	return sameStatusPreparation(x.Preparation, y.Preparation) && x.Status == y.Status && x.CancellationScheduled == y.CancellationScheduled && x.Revision == y.Revision && x.Fingerprint == y.Fingerprint && x.ObservedAt.Equal(y.ObservedAt)
}

// InspectLast authenticates immutable supersession history against the current
// native resolution without a live lease. The native head may advance further;
// an old original remains conclusively uncapturable. Historical job metadata
// cannot authorize a new execution, and inspection performs no provider GET.
func (s *StatusOriginalResolver) InspectLast(ctx context.Context, source ScheduledSource) (StatusSupersession, error) {
	if !s.ready() {
		return StatusSupersession{}, billing.ErrRevenueUnavailable
	}
	x := s.execution
	if !scheduleSourceShape(source) || source.Kind != billing.LifecycleSubscriptionSources {
		return StatusSupersession{}, billing.ErrRevenueInvalid
	}
	if err := x.scheduler.begin(ctx, source.Scope); err != nil {
		return StatusSupersession{}, err
	}
	if err := completionAuthority(ctx, x.scheduler, source, nil); err != nil {
		return StatusSupersession{}, err
	}
	out, err := s.repo.FindLastStatusSupersession(ctx, source)
	if err = completionAuthority(ctx, x.scheduler, source, err); err != nil {
		return StatusSupersession{}, err
	}
	if !supersessionShape(out, StatusSupersessionKey{source, out.Input.Preparation.CaptureID}) {
		return StatusSupersession{}, billing.ErrRevenueUnavailable
	}
	retained, err := x.outbox.Find(ctx, x.scheduler.cfg.ActorID, out.Input.Preparation)
	if err = completionAuthority(ctx, x.scheduler, source, boundInputError(err)); err != nil {
		return StatusSupersession{}, err
	}
	if !sameRetainedStatus(retained, out.Input) {
		return StatusSupersession{}, billing.ErrRevenueConflict
	}
	native, err := s.owner.ResolveSubscriptionStatus(ctx, x.scheduler.cfg.ActorID, retained.Preparation)
	if err = completionAuthority(ctx, x.scheduler, source, err); err != nil {
		return StatusSupersession{}, err
	}
	if native.Validate() != nil || native.State != billing.SubscriptionStatusSuperseded || !sameStatusPreparation(native.Preparation, out.Input.Preparation) || native.Current.Revision < out.Resolution.Current.Revision || (native.Current.Revision == out.Resolution.Current.Revision && native.Current.Fingerprint != out.Resolution.Current.Fingerprint) {
		return StatusSupersession{}, billing.ErrRevenueConflict
	}
	return out, nil
}
