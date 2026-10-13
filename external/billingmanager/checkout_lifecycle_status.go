package billingmanager

import (
	"context"

	"github.com/ooaklee/ghatd/external/billing"
)

// CheckoutLifecycleStatusService is an optional extension of the same configured
// native lifecycle owner. Unsupported custom providers retain the capture path.
type CheckoutLifecycleStatusService interface {
	SupportsCheckoutLifecycleStatus(billing.RevenueScope) bool
	LookupCheckoutLifecycleStatus(context.Context, billing.CheckoutIntent) (billing.CheckoutLifecycleStatus, error)
	FindCheckoutLifecycleExpiry(context.Context, billing.CheckoutIntent) (billing.CheckoutLifecycleExpiry, error)
}

func (s *Service) SupportsCheckoutLifecycleStatus(scope billing.RevenueScope) bool {
	if s == nil {
		return false
	}
	owner, ok := s.revenueAssociation.(CheckoutLifecycleStatusService)
	return ok && !nilRevenueDependency(owner) && owner.SupportsCheckoutLifecycleStatus(scope)
}

// LookupCheckoutLifecycleStatus uses current selected refresh authority before
// original validation, before provider I/O and after the result. A status read
// does not grant expiry or bypass the host's exact lease fence.
func (s *Service) LookupCheckoutLifecycleStatus(ctx context.Context, actor string, i billing.CheckoutIntent) (billing.CheckoutLifecycleStatus, error) {
	owner, err := s.checkoutLifecycleBegin(ctx, actor, i)
	if err != nil {
		return billing.CheckoutLifecycleStatus{}, err
	}
	status, ok := owner.(CheckoutLifecycleStatusService)
	if !ok || nilRevenueDependency(status) || !status.SupportsCheckoutLifecycleStatus(i.Scope) {
		return billing.CheckoutLifecycleStatus{}, s.checkoutLifecycleFinish(ctx, actor, i, billing.ErrRevenueUnavailable)
	}
	original, err := owner.PrepareCheckoutLifecycle(ctx, i)
	if err == nil {
		err = original.ValidateAcknowledgedInput(i)
	}
	if err = s.checkoutLifecycleFinish(ctx, actor, i, err); err != nil {
		return billing.CheckoutLifecycleStatus{}, err
	}
	out, err := status.LookupCheckoutLifecycleStatus(ctx, i)
	if err == nil {
		err = out.ValidateForCheckout(i)
	}
	if err = s.checkoutLifecycleFinish(ctx, actor, i, err); err != nil {
		return billing.CheckoutLifecycleStatus{}, err
	}
	return out, nil
}

// FindCheckoutLifecycleExpiry authenticates recovery without treating historical
// proof as fresh authority or performing another provider lookup.
func (s *Service) FindCheckoutLifecycleExpiry(ctx context.Context, actor string, i billing.CheckoutIntent) (billing.CheckoutLifecycleExpiry, error) {
	owner, err := s.checkoutLifecycleBegin(ctx, actor, i)
	if err != nil {
		return billing.CheckoutLifecycleExpiry{}, err
	}
	status, ok := owner.(CheckoutLifecycleStatusService)
	if !ok || nilRevenueDependency(status) {
		return billing.CheckoutLifecycleExpiry{}, s.checkoutLifecycleFinish(ctx, actor, i, billing.ErrRevenueUnavailable)
	}
	out, err := status.FindCheckoutLifecycleExpiry(ctx, i)
	if err == nil {
		err = out.ValidateForCheckout(i)
	}
	if err = s.checkoutLifecycleFinish(ctx, actor, i, err); err != nil {
		return billing.CheckoutLifecycleExpiry{}, err
	}
	return out, nil
}
