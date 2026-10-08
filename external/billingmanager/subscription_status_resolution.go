package billingmanager

import (
	"context"

	"github.com/ooaklee/ghatd/external/billing"
)

// SubscriptionStatusResolutionService is an optional capability of the SAME
// configured owning status service. Existing payment-only adapters need not
// implement it; no second owner or provider fallback is accepted.
type SubscriptionStatusResolutionService interface {
	ResolveSubscriptionStatus(context.Context, billing.SubscriptionStatusPreparation) (billing.SubscriptionStatusResolution, error)
}

// ResolveSubscriptionStatus inspects one retained original under current global
// and selected refresh authority, independently of its preparing author. A
// successful superseded result does not itself clear any host execution state.
func (s *Service) ResolveSubscriptionStatus(ctx context.Context, actor string, p billing.SubscriptionStatusPreparation) (billing.SubscriptionStatusResolution, error) {
	if _, err := s.statusService(ctx, actor); err != nil {
		return billing.SubscriptionStatusResolution{}, err
	}
	if err := p.Validate(); err != nil {
		return billing.SubscriptionStatusResolution{}, err
	}
	finish := func(operationErr error) error {
		selected := s.statusFinish(ctx, actor, SubscriptionStatusRefresh, statusTarget(p), operationErr)
		return s.statusFinish(ctx, actor, SubscriptionStatusRefresh, SubscriptionStatusTarget{}, selected)
	}
	owner, err := s.retainedStatusOwner(ctx, actor, p)
	if err != nil {
		return billing.SubscriptionStatusResolution{}, finish(err)
	}
	resolver, ok := owner.(SubscriptionStatusResolutionService)
	if !ok || nilRevenueDependency(resolver) {
		return billing.SubscriptionStatusResolution{}, finish(billing.ErrRevenueUnavailable)
	}
	out, err := resolver.ResolveSubscriptionStatus(ctx, p)
	if err == nil && (out.Validate() != nil || out.Preparation.CaptureID != p.CaptureID) {
		err = billing.ErrRevenueConflict
	}
	if err = finish(err); err != nil {
		return billing.SubscriptionStatusResolution{}, err
	}
	return out, nil
}
