package billing

import (
	"context"
	"time"
)

// Legacy paid-only repositories have no checkout capability. Where the same
// owner has retained a checkout anchor, its immutable payer cannot be reassigned
// by an otherwise valid payment fact, including before the first status head.
func (s *RevenueService) validatePaidCheckoutOwner(ctx context.Context, f RevenueFact) error {
	repo, ok := s.repo.(CheckoutRepository)
	if !ok || revenueNil(repo) {
		return nil
	}
	return repo.ReadCheckout(ctx, func(tx CheckoutTx) error {
		if _, ok := tx.(CheckoutLifecycleTx); !ok {
			return nil
		}
		a, err := readCheckoutLifecycleAnchor(ctx, tx, f.Scope, f.SubscriptionID)
		if singleRevenueCause(err, ErrRevenueNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if a.PrincipalID != f.PrincipalID || a.Evidence.CustomerID != f.ProviderCustomerID {
			return ErrRevenueConflict
		}
		return nil
	})
}

func (s *RevenueService) checkoutStatusAnchor(ctx context.Context, scope RevenueScope, subscription string) (CheckoutLifecycleAnchor, error) {
	repo, ok := s.repo.(CheckoutRepository)
	if !ok || revenueNil(repo) {
		return CheckoutLifecycleAnchor{}, ErrRevenueUnavailable
	}
	var a CheckoutLifecycleAnchor
	err := repo.ReadCheckout(ctx, func(tx CheckoutTx) error {
		var err error
		a, err = readCheckoutLifecycleAnchor(ctx, tx, scope, subscription)
		return err
	})
	if err != nil {
		return CheckoutLifecycleAnchor{}, err
	}
	return a, nil
}

func (s *RevenueService) validateStatusProvenance(ctx context.Context, p SubscriptionStatusPreparation) error {
	if p.Source == "" {
		f, err := s.statusFact(ctx, p.FactID)
		if err != nil {
			return err
		}
		if !matchesStatusFact(p, f) {
			return ErrRevenueConflict
		}
		return nil
	}
	if p.Source != SubscriptionStatusCheckoutSource {
		return ErrRevenueInvalid
	}
	a, err := s.checkoutStatusAnchor(ctx, p.Scope, p.SubscriptionID)
	if err != nil {
		return lifecycleJoinedError(err)
	}
	if a.IntentID != p.CheckoutIntentID || a.Fingerprint != p.CheckoutFingerprint || a.PrincipalID != p.PrincipalID || a.Evidence.CustomerID != p.ProviderCustomerID || p.RequestedAt.Before(a.AnchoredAt) {
		return ErrRevenueConflict
	}
	return nil
}

// PrepareSubscriptionStatusForCheckout freezes authenticated pre-payment
// ownership and the current subscription revision before provider lookup. The
// SAME revenue repository must optionally implement owning checkout reads.
// An anchor creates no accepted payment fact or partner financial entitlement.
func (s *RevenueService) PrepareSubscriptionStatusForCheckout(ctx context.Context, actor string, scope RevenueScope, subscription string) (SubscriptionStatusPreparation, error) {
	repo, err := s.statusRepository(ctx)
	if err != nil {
		return SubscriptionStatusPreparation{}, err
	}
	if !cleanStatusID(actor) || !validRevenueScope(scope) || !cleanStatusID(subscription) {
		return SubscriptionStatusPreparation{}, ErrRevenueInvalid
	}
	a, err := s.checkoutStatusAnchor(ctx, scope, subscription)
	if err != nil {
		return SubscriptionStatusPreparation{}, err
	}
	p := SubscriptionStatusPreparation{Source: SubscriptionStatusCheckoutSource, CheckoutIntentID: a.IntentID, CheckoutFingerprint: a.Fingerprint, ActorID: actor, Scope: scope, PrincipalID: a.PrincipalID, ProviderCustomerID: a.Evidence.CustomerID, SubscriptionID: subscription}
	return s.prepareStatus(ctx, repo, p, a.AnchoredAt)
}

// GetSubscriptionStatusForCheckout reads the same joined current status used
// by paid reporting, validating the selected checkout owner and the head's own
// payment or checkout provenance. Freshness is measured from original lookup
// start; trial or active status never manufactures paid eligibility.
func (s *RevenueService) GetSubscriptionStatusForCheckout(ctx context.Context, scope RevenueScope, subscription string, maxAge time.Duration) (SubscriptionStatus, error) {
	repo, err := s.statusRepository(ctx)
	if err != nil {
		return SubscriptionStatus{}, err
	}
	if !validRevenueScope(scope) || !cleanStatusID(subscription) || maxAge < time.Second || maxAge > 24*time.Hour {
		return SubscriptionStatus{}, ErrRevenueInvalid
	}
	a, err := s.checkoutStatusAnchor(ctx, scope, subscription)
	if err != nil {
		return SubscriptionStatus{}, err
	}
	return s.readStatusForIdentity(ctx, repo, scope, subscription, a.PrincipalID, a.Evidence.CustomerID, maxAge)
}
