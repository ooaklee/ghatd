package billingmanager

import (
	"context"
	"strings"
	"time"

	"github.com/ooaklee/ghatd/external/billing"
)

// CheckoutSubscriptionStatusService is an optional capability of the SAME
// configured revenue feed. Legacy payment-only status adapters need not add it.
// No second status owner or payment-fact fallback is accepted.
type CheckoutSubscriptionStatusService interface {
	PrepareSubscriptionStatusForCheckout(context.Context, string, billing.RevenueScope, string) (billing.SubscriptionStatusPreparation, error)
	GetSubscriptionStatusForCheckout(context.Context, billing.RevenueScope, string, time.Duration) (billing.SubscriptionStatus, error)
}

func (s *Service) checkoutStatusService(ctx context.Context, actor string, scope billing.RevenueScope, subscription string) (SubscriptionStatusService, error) {
	owner, err := s.statusService(ctx, actor)
	if err != nil {
		return nil, err
	}
	if billing.ValidateRevenueHistoryScopes([]billing.RevenueScope{scope}) != nil || subscription == "" || len(subscription) > 256 || strings.TrimSpace(subscription) != subscription || strings.ContainsAny(subscription, "\r\n\x00") {
		return nil, billing.ErrRevenueInvalid
	}
	return owner, nil
}

// PrepareSubscriptionStatusForCheckout freezes native pre-payment ownership
// under current refresh authority. Retain the returned original before lookup;
// this preparation creates neither paid revenue nor financial entitlement.
func (s *Service) PrepareSubscriptionStatusForCheckout(ctx context.Context, actor string, scope billing.RevenueScope, subscription string) (billing.SubscriptionStatusPreparation, error) {
	owner, err := s.checkoutStatusService(ctx, actor, scope, subscription)
	if err != nil {
		return billing.SubscriptionStatusPreparation{}, err
	}
	if err := s.statusAuthorize(ctx, actor, SubscriptionStatusRefresh, SubscriptionStatusTarget{}); err != nil {
		return billing.SubscriptionStatusPreparation{}, err
	}
	checkout, ok := owner.(CheckoutSubscriptionStatusService)
	if !ok || nilRevenueDependency(checkout) {
		return billing.SubscriptionStatusPreparation{}, s.statusFinish(ctx, actor, SubscriptionStatusRefresh, SubscriptionStatusTarget{}, billing.ErrRevenueUnavailable)
	}
	p, err := checkout.PrepareSubscriptionStatusForCheckout(ctx, actor, scope, subscription)
	target := SubscriptionStatusTarget{}
	if err == nil {
		if p.Validate() != nil || p.Source != billing.SubscriptionStatusCheckoutSource || p.Scope != scope || p.SubscriptionID != subscription || p.ActorID != actor {
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

// GetSubscriptionStatusForCheckout reads the same fresh head used by paid
// reporting. Its native provenance may be checkout or payment after a later
// charge. Missing/stale status is unknown, never an invented inactive result.
func (s *Service) GetSubscriptionStatusForCheckout(ctx context.Context, actor string, scope billing.RevenueScope, subscription string, maxAge time.Duration) (billing.SubscriptionStatus, error) {
	owner, err := s.checkoutStatusService(ctx, actor, scope, subscription)
	if err != nil {
		return billing.SubscriptionStatus{}, err
	}
	if maxAge < time.Second || maxAge > 24*time.Hour {
		return billing.SubscriptionStatus{}, billing.ErrRevenueInvalid
	}
	if err := s.statusAuthorize(ctx, actor, SubscriptionStatusRead, SubscriptionStatusTarget{}); err != nil {
		return billing.SubscriptionStatus{}, err
	}
	checkout, ok := owner.(CheckoutSubscriptionStatusService)
	if !ok || nilRevenueDependency(checkout) {
		return billing.SubscriptionStatus{}, s.statusFinish(ctx, actor, SubscriptionStatusRead, SubscriptionStatusTarget{}, billing.ErrRevenueUnavailable)
	}
	v, err := checkout.GetSubscriptionStatusForCheckout(ctx, scope, subscription, maxAge)
	target := SubscriptionStatusTarget{}
	if err == nil {
		if v.Validate() != nil || v.Preparation.Scope != scope || v.Preparation.SubscriptionID != subscription {
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
