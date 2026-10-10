package billingmanager

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/paymentprovider"
)

const (
	SubscriptionStatusRead    = "billing.subscription-status.read"
	SubscriptionStatusRefresh = "billing.subscription-status.refresh"
)

// SubscriptionStatusTarget is established by owning payment or checkout proof.
// An empty target requests program-level permission before any source lookup.
// It is private in-process scope, never a caller assertion from HTTP input.
type SubscriptionStatusTarget struct {
	Scope                       billing.RevenueScope `json:"-"`
	PrincipalID, SubscriptionID string               `json:"-"`
}

// SubscriptionStatusAuthority checks current actor/action and owning scope on
// every stage and replay. Stored preparing authorship is not current authority.
type SubscriptionStatusAuthority interface {
	// AuthorizeSubscriptionStatus checks the current actor and action against the
	// SubscriptionStatusTarget on every stage and replay through the
	// SubscriptionStatusAuthority port. Returns a non-nil error when the action is
	// not currently authorized.
	AuthorizeSubscriptionStatus(context.Context, string, string, SubscriptionStatusTarget) error
}

// SubscriptionStatusService is derived from the SAME configured revenue feed.
// A second lifecycle service could silently read a different billing ledger.
type SubscriptionStatusService interface {
	// PrepareSubscriptionStatus freezes billing provenance and revision for the
	// fact before provider I/O via the SubscriptionStatusService derived from the
	// configured revenue feed. Returns the preparation or an error; hosts must
	// durably retain it before capture.
	PrepareSubscriptionStatus(context.Context, string, string) (billing.SubscriptionStatusPreparation, error)
	// ValidateSubscriptionStatusPreparation rechecks a retained preparation through
	// the SubscriptionStatusService under the current actor's refresh permission
	// without performing fresh preparation or writes. Returns a non-nil error when
	// the preparation is invalid.
	ValidateSubscriptionStatusPreparation(context.Context, billing.SubscriptionStatusPreparation) error
	// CaptureVerifiedSubscriptionStatus records the verified status evidence
	// against the retained preparation via the SubscriptionStatusService derived
	// from the configured revenue feed. Returns the captured status or an error.
	CaptureVerifiedSubscriptionStatus(context.Context, billing.SubscriptionStatusPreparation, billing.VerifiedSubscriptionStatusEvidence) (billing.SubscriptionStatus, error)
	// GetSubscriptionStatusForFact reads retained fresh status evidence for the
	// fact within maxAge via the SubscriptionStatusService without provider I/O.
	// Returns the status or an error; maxAge is host configuration.
	GetSubscriptionStatusForFact(context.Context, string, time.Duration) (billing.SubscriptionStatus, error)
}

// statusService validates actor shape and wiring and returns the configured
// revenue feed cast to SubscriptionStatusService. Missing authority, registry
// or feed capability returns ErrRevenueUnavailable.
func (s *Service) statusService(ctx context.Context, actor string) (SubscriptionStatusService, error) {
	if ctx == nil || actor == "" || strings.TrimSpace(actor) != actor || len(actor) > 256 || strings.ContainsAny(actor, "\r\n\x00") {
		return nil, billing.ErrRevenueInvalid
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s == nil || nilRevenueDependency(s.subscriptionStatusAuthority) || nilRevenueDependency(s.revenueRegistry) || nilRevenueDependency(s.revenueFeed) {
		return nil, billing.ErrRevenueUnavailable
	}
	owner, ok := s.revenueFeed.(SubscriptionStatusService)
	if !ok || nilRevenueDependency(owner) {
		return nil, billing.ErrRevenueUnavailable
	}
	return owner, nil
}

// statusTarget derives the authorization target from a retained preparation's
// scope, principal and subscription IDs for permission checks.
func statusTarget(p billing.SubscriptionStatusPreparation) SubscriptionStatusTarget {
	return SubscriptionStatusTarget{Scope: p.Scope, PrincipalID: p.PrincipalID, SubscriptionID: p.SubscriptionID}
}

// statusAuthorize checks context cancellation, delegates to the subscription
// status authority for the actor/action/target, and rechecks cancellation so a
// cancelled context fails the stage.
func (s *Service) statusAuthorize(ctx context.Context, actor, action string, target SubscriptionStatusTarget) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.subscriptionStatusAuthority.AuthorizeSubscriptionStatus(ctx, actor, action, target); err != nil {
		return err
	}
	return ctx.Err()
}

// Final authority applies to errors and absence as well as successful data.
// An observed unknown commit remains available to private recovery callers even
// when a later authority failure or cancellation suppresses the payload.
func (s *Service) statusFinish(ctx context.Context, actor, action string, target SubscriptionStatusTarget, operationErr error) error {
	if err := s.statusAuthorize(ctx, actor, action, target); err != nil {
		if errors.Is(operationErr, billing.ErrRevenueUncertain) {
			return errors.Join(err, operationErr)
		}
		return err
	}
	return operationErr
}

// retainedStatusOwner resolves the status owner, validates the retained
// preparation, and authorizes both empty-scope refresh and the preparation's
// selected target before owner provenance validation. A retained input is a
// selection to authorize, never proof of permission.
func (s *Service) retainedStatusOwner(ctx context.Context, actor string, p billing.SubscriptionStatusPreparation) (SubscriptionStatusService, error) {
	owner, err := s.statusService(ctx, actor)
	if err != nil {
		return nil, err
	}
	if err := p.Validate(); err != nil {
		return nil, err
	}
	if err := s.statusAuthorize(ctx, actor, SubscriptionStatusRefresh, SubscriptionStatusTarget{}); err != nil {
		return nil, err
	}
	// A canonical retained input is a selection to authorize, not proof. Check
	// its selected permission before asking the owner to validate provenance.
	if err := s.statusAuthorize(ctx, actor, SubscriptionStatusRefresh, statusTarget(p)); err != nil {
		return nil, err
	}
	err = owner.ValidateSubscriptionStatusPreparation(ctx, p)
	if err = s.statusFinish(ctx, actor, SubscriptionStatusRefresh, statusTarget(p), err); err != nil {
		return nil, err
	}
	return owner, nil
}

// ValidateSubscriptionStatusPreparation rechecks retained original provenance
// under the current actor's refresh permission. The actor may differ from the
// original author. No fresh preparation, provider request or write is performed.
func (s *Service) ValidateSubscriptionStatusPreparation(ctx context.Context, actor string, p billing.SubscriptionStatusPreparation) error {
	_, err := s.retainedStatusOwner(ctx, actor, p)
	return err
}

// WithSubscriptionStatusAuthority enables private lifecycle collection/read
// stages only after verified revenue wiring. Legacy flows remain unchanged.
func (s *Service) WithSubscriptionStatusAuthority(authority SubscriptionStatusAuthority) (*Service, error) {
	if s == nil || nilRevenueDependency(authority) || nilRevenueDependency(s.revenueRegistry) || nilRevenueDependency(s.revenueFeed) {
		return nil, billing.ErrRevenueUnavailable
	}
	owner, ok := s.revenueFeed.(SubscriptionStatusService)
	if !ok || nilRevenueDependency(owner) {
		return nil, billing.ErrRevenueUnavailable
	}
	s.subscriptionStatusAuthority = authority
	return s, nil
}

// PrepareSubscriptionStatus freezes billing provenance and revision before
// provider I/O. The trusted host must durably retain this preparation and then
// the returned lookup evidence BEFORE capture when crash recovery is required.
func (s *Service) PrepareSubscriptionStatus(ctx context.Context, actor, factID string) (billing.SubscriptionStatusPreparation, error) {
	owner, err := s.statusService(ctx, actor)
	if err != nil {
		return billing.SubscriptionStatusPreparation{}, err
	}
	if err := s.statusAuthorize(ctx, actor, SubscriptionStatusRefresh, SubscriptionStatusTarget{}); err != nil {
		return billing.SubscriptionStatusPreparation{}, err
	}
	p, err := owner.PrepareSubscriptionStatus(ctx, actor, factID)
	target := SubscriptionStatusTarget{}
	if err == nil {
		if p.Validate() != nil || p.ActorID != actor || p.FactID != factID || p.Source != "" {
			err = billing.ErrRevenueConflict
		} else {
			target = statusTarget(p)
		}
	}
	if err = s.statusFinish(ctx, actor, SubscriptionStatusRefresh, target, err); err != nil {
		return billing.SubscriptionStatusPreparation{}, err
	}
	return p, nil
}

// LookupSubscriptionStatus fetches current authenticated provider evidence;
// no financial or status mutation occurs. A currently authorized replacement
// operator can recover the original preparation without impersonating its author.
func (s *Service) LookupSubscriptionStatus(ctx context.Context, actor string, p billing.SubscriptionStatusPreparation) (billing.VerifiedSubscriptionStatusEvidence, error) {
	_, err := s.retainedStatusOwner(ctx, actor, p)
	if err != nil {
		return billing.VerifiedSubscriptionStatusEvidence{}, err
	}
	base, err := s.revenueRegistry.GetRevenueProvider(p.Scope.Provider)
	if err != nil {
		return billing.VerifiedSubscriptionStatusEvidence{}, s.statusFinish(ctx, actor, SubscriptionStatusRefresh, statusTarget(p), err)
	}
	provider, ok := base.(paymentprovider.RevenueSubscriptionProvider)
	if !ok || nilRevenueDependency(provider) {
		return billing.VerifiedSubscriptionStatusEvidence{}, s.statusFinish(ctx, actor, SubscriptionStatusRefresh, statusTarget(p), billing.ErrRevenueUnavailable)
	}
	if err := ctx.Err(); err != nil {
		return billing.VerifiedSubscriptionStatusEvidence{}, err
	}
	e, err := provider.LookupRevenueSubscription(ctx, paymentprovider.RevenueScope{Provider: p.Scope.Provider, AccountID: p.Scope.AccountID, LiveMode: p.Scope.LiveMode}, p.SubscriptionID)
	if err != nil {
		return billing.VerifiedSubscriptionStatusEvidence{}, s.statusFinish(ctx, actor, SubscriptionStatusRefresh, statusTarget(p), err)
	}
	out := billing.VerifiedSubscriptionStatusEvidence{Scope: billingRevenueScope(e.Scope), SubscriptionID: e.SubscriptionID, ProviderCustomerID: e.CustomerID, Status: e.Status, CancellationScheduled: e.CancellationScheduled}
	if out.Validate(p) != nil {
		err = billing.ErrRevenueConflict
	}
	if err = s.statusFinish(ctx, actor, SubscriptionStatusRefresh, statusTarget(p), err); err != nil {
		return billing.VerifiedSubscriptionStatusEvidence{}, err
	}
	return out, nil
}

// CaptureSubscriptionStatus accepts the previously retained preparation and
// evidence without another provider call. Uncertain commits replay EXACT inputs;
// a generic revision conflict never permits discarding the original. Use the
// optional native resolution before replacing an uncaptured superseded input.
// Post-write revocation suppresses the response; it does not undo committed truth.
func (s *Service) CaptureSubscriptionStatus(ctx context.Context, actor string, p billing.SubscriptionStatusPreparation, e billing.VerifiedSubscriptionStatusEvidence) (billing.SubscriptionStatus, error) {
	owner, err := s.retainedStatusOwner(ctx, actor, p)
	if err != nil {
		return billing.SubscriptionStatus{}, err
	}
	if err := e.Validate(p); err != nil {
		return billing.SubscriptionStatus{}, s.statusFinish(ctx, actor, SubscriptionStatusRefresh, statusTarget(p), err)
	}
	v, err := owner.CaptureVerifiedSubscriptionStatus(ctx, p, e)
	if err == nil && (v.Validate() != nil || v.Preparation.CaptureID != p.CaptureID || v.Status != e.Status || v.CancellationScheduled != e.CancellationScheduled) {
		err = billing.ErrRevenueConflict
	}
	if err = s.statusFinish(ctx, actor, SubscriptionStatusRefresh, statusTarget(p), err); err != nil {
		return billing.SubscriptionStatus{}, err
	}
	return v, nil
}

// GetSubscriptionStatusForFact reads retained fresh evidence without provider
// I/O. maxAge is trusted host configuration, never customer-controlled input.
func (s *Service) GetSubscriptionStatusForFact(ctx context.Context, actor, factID string, maxAge time.Duration) (billing.SubscriptionStatus, error) {
	owner, err := s.statusService(ctx, actor)
	if err != nil {
		return billing.SubscriptionStatus{}, err
	}
	if err := s.statusAuthorize(ctx, actor, SubscriptionStatusRead, SubscriptionStatusTarget{}); err != nil {
		return billing.SubscriptionStatus{}, err
	}
	v, err := owner.GetSubscriptionStatusForFact(ctx, factID, maxAge)
	target := SubscriptionStatusTarget{}
	if err == nil {
		if v.Validate() != nil {
			err = billing.ErrRevenueConflict
		} else {
			target = statusTarget(v.Preparation)
		}
	}
	if err = s.statusFinish(ctx, actor, SubscriptionStatusRead, target, err); err != nil {
		return billing.SubscriptionStatus{}, err
	}
	return v, nil
}
