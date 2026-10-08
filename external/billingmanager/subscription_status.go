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
	AuthorizeSubscriptionStatus(context.Context, string, string, SubscriptionStatusTarget) error
}

// SubscriptionStatusService is derived from the SAME configured revenue feed.
// A second lifecycle service could silently read a different billing ledger.
type SubscriptionStatusService interface {
	PrepareSubscriptionStatus(context.Context, string, string) (billing.SubscriptionStatusPreparation, error)
	ValidateSubscriptionStatusPreparation(context.Context, billing.SubscriptionStatusPreparation) error
	CaptureVerifiedSubscriptionStatus(context.Context, billing.SubscriptionStatusPreparation, billing.VerifiedSubscriptionStatusEvidence) (billing.SubscriptionStatus, error)
	GetSubscriptionStatusForFact(context.Context, string, time.Duration) (billing.SubscriptionStatus, error)
}

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
func statusTarget(p billing.SubscriptionStatusPreparation) SubscriptionStatusTarget {
	return SubscriptionStatusTarget{Scope: p.Scope, PrincipalID: p.PrincipalID, SubscriptionID: p.SubscriptionID}
}
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
// fresh preparation is appropriate only after a known revision conflict.
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
