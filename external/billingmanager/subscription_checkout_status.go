package billingmanager

import (
	"context"
	"time"

	"github.com/ooaklee/ghatd/external/billing"
)

// CheckoutSubscriptionStatusService is an optional capability on the same
// configured revenue owner. Existing paid-only integrations remain compatible.
type CheckoutSubscriptionStatusService interface {
	PrepareSubscriptionStatusForCheckout(context.Context, string, billing.RevenueScope, string) (billing.SubscriptionStatusPreparation, error)
	GetSubscriptionStatusForCheckout(context.Context, billing.RevenueScope, string, time.Duration) (billing.SubscriptionStatus, error)
}

// PrepareSubscriptionStatusForCheckout uses the existing refresh permission,
// checking program authority before discovery and derived owning scope before
// disclosure. Scope/subscription select a trusted host source, not payer proof.
// The returned original preparation must be retained before provider lookup.
func (s *Service) PrepareSubscriptionStatusForCheckout(ctx context.Context, actor string, scope billing.RevenueScope, subscription string) (billing.SubscriptionStatusPreparation, error) {
	owner, err := s.statusService(ctx, actor)
	if err != nil {
		return billing.SubscriptionStatusPreparation{}, err
	}
	if err := s.statusAuthorize(ctx, actor, SubscriptionStatusRefresh, SubscriptionStatusTarget{}); err != nil {
		return billing.SubscriptionStatusPreparation{}, err
	}
	checkout, ok := owner.(CheckoutSubscriptionStatusService)
	if !ok || nilRevenueDependency(checkout) {
		return billing.SubscriptionStatusPreparation{}, billing.ErrRevenueUnavailable
	}
	p, err := checkout.PrepareSubscriptionStatusForCheckout(ctx, actor, scope, subscription)
	if err != nil {
		return billing.SubscriptionStatusPreparation{}, err
	}
	if p.Validate() != nil || p.Source != billing.SubscriptionStatusCheckoutSource || p.ActorID != actor || p.Scope != scope || p.SubscriptionID != subscription {
		return billing.SubscriptionStatusPreparation{}, billing.ErrRevenueConflict
	}
	if err := s.statusAuthorize(ctx, actor, SubscriptionStatusRefresh, statusTarget(p)); err != nil {
		return billing.SubscriptionStatusPreparation{}, err
	}
	return p, nil
}

// GetSubscriptionStatusForCheckout checks current read permission before the
// owning snapshot and again before disclosure. maxAge is trusted configuration.
func (s *Service) GetSubscriptionStatusForCheckout(ctx context.Context, actor string, scope billing.RevenueScope, subscription string, maxAge time.Duration) (billing.SubscriptionStatus, error) {
	owner, err := s.statusService(ctx, actor)
	if err != nil {
		return billing.SubscriptionStatus{}, err
	}
	if err := s.statusAuthorize(ctx, actor, SubscriptionStatusRead, SubscriptionStatusTarget{}); err != nil {
		return billing.SubscriptionStatus{}, err
	}
	checkout, ok := owner.(CheckoutSubscriptionStatusService)
	if !ok || nilRevenueDependency(checkout) {
		return billing.SubscriptionStatus{}, billing.ErrRevenueUnavailable
	}
	v, err := checkout.GetSubscriptionStatusForCheckout(ctx, scope, subscription, maxAge)
	if err != nil {
		return billing.SubscriptionStatus{}, err
	}
	if v.Validate() != nil || v.Preparation.Scope != scope || v.Preparation.SubscriptionID != subscription {
		return billing.SubscriptionStatus{}, billing.ErrRevenueConflict
	}
	if err := s.statusAuthorize(ctx, actor, SubscriptionStatusRead, statusTarget(v.Preparation)); err != nil {
		return billing.SubscriptionStatus{}, err
	}
	return v, nil
}
