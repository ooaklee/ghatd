package revenuestore

import (
	"context"
	"time"

	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/paymentprovider"
)

const (
	kindCheckoutLifecycleAnchor  = "billing_checkout_lifecycle_anchor"
	kindCheckoutLifecycleReceipt = "billing_checkout_lifecycle_receipt"
)

// Explicit private persistence codec preserves fields omitted from public JSON.
type persistedCheckoutLifecycle struct {
	IntentID, IntentFingerprint, PrincipalID, PlanID, CostID, Fingerprint string
	Evidence                                                              paymentprovider.RevenueCheckoutEvidence
	AnchoredAt                                                            time.Time
}

func persistLifecycle(a billing.CheckoutLifecycleAnchor) persistedCheckoutLifecycle {
	return persistedCheckoutLifecycle{a.IntentID, a.IntentFingerprint, a.PrincipalID, a.PlanID, a.CostID, a.Fingerprint, a.Evidence, a.AnchoredAt}
}
func (p persistedCheckoutLifecycle) anchor() billing.CheckoutLifecycleAnchor {
	return billing.CheckoutLifecycleAnchor{IntentID: p.IntentID, IntentFingerprint: p.IntentFingerprint, PrincipalID: p.PrincipalID, PlanID: p.PlanID, CostID: p.CostID, Fingerprint: p.Fingerprint, Evidence: p.Evidence, AnchoredAt: p.AnchoredAt}
}
func anchorScope(a billing.CheckoutLifecycleAnchor) billing.RevenueScope {
	return billing.RevenueScope{Provider: a.Evidence.Scope.Provider, AccountID: a.Evidence.Scope.AccountID, LiveMode: a.Evidence.Scope.LiveMode}
}
func (b *checkoutBound) GetCheckoutLifecycleReceipt(ctx context.Context, intent string) (billing.CheckoutLifecycleAnchor, error) {
	p, _, err := get[persistedCheckoutLifecycle](ctx, b.tx, kindCheckoutLifecycleReceipt, intent, checkoutPartition)
	if err != nil {
		return billing.CheckoutLifecycleAnchor{}, err
	}
	a := p.anchor()
	if a.IntentID != intent || a.Validate() != nil {
		return billing.CheckoutLifecycleAnchor{}, billing.ErrRevenueUnavailable
	}
	if err := b.checkScope(anchorScope(a)); err != nil {
		return billing.CheckoutLifecycleAnchor{}, err
	}
	return a, nil
}
func (b *checkoutBound) GetCheckoutLifecycleAnchor(ctx context.Context, scope billing.RevenueScope, sub string) (billing.CheckoutLifecycleAnchor, error) {
	if err := b.checkScope(scope); err != nil {
		return billing.CheckoutLifecycleAnchor{}, err
	}
	p, _, err := get[persistedCheckoutLifecycle](ctx, b.tx, kindCheckoutLifecycleAnchor, associationKey(scope, sub, ""), checkoutPartition)
	if err != nil {
		return billing.CheckoutLifecycleAnchor{}, err
	}
	a := p.anchor()
	if anchorScope(a) != scope || a.Evidence.SubscriptionID != sub || a.Validate() != nil {
		return billing.CheckoutLifecycleAnchor{}, billing.ErrRevenueUnavailable
	}
	return a, nil
}

func (b *checkoutBound) InsertCheckoutLifecycleAnchor(ctx context.Context, a billing.CheckoutLifecycleAnchor) error {
	if a.Validate() != nil {
		return billing.ErrRevenueInvalid
	}
	scope := anchorScope(a)
	if err := b.checkScope(scope); err != nil {
		return err
	}
	principalKey := associationKey(scope, a.Evidence.SubscriptionID, "")
	paid, _, err := get[billing.CheckoutAssociation](ctx, b.tx, kindCheckoutPrincipal, principalKey, checkoutPartition)
	if err == nil {
		if paid.Scope != scope || paid.SubscriptionID != a.Evidence.SubscriptionID || paid.PrincipalID != a.PrincipalID || paid.CustomerID != a.Evidence.CustomerID || paid.LinkedAt.IsZero() {
			return billing.ErrRevenueConflict
		}
	} else if !singleCause(err, billing.ErrRevenueNotFound) {
		return err
	}
	old, err := b.GetCheckoutLifecycleAnchor(ctx, scope, a.Evidence.SubscriptionID)
	if err == nil {
		receipt, err := b.GetCheckoutLifecycleReceipt(ctx, old.IntentID)
		if err != nil {
			if singleCause(err, billing.ErrRevenueNotFound) {
				return billing.ErrRevenueUnavailable
			}
			return err
		}
		if receipt.Fingerprint != old.Fingerprint || !receipt.AnchoredAt.Equal(old.AnchoredAt) || old.PrincipalID != a.PrincipalID || old.Evidence.CustomerID != a.Evidence.CustomerID {
			return billing.ErrRevenueConflict
		}
	} else if singleCause(err, billing.ErrRevenueNotFound) {
		if err := insert(ctx, b.tx, kindCheckoutLifecycleAnchor, principalKey, checkoutPartition, persistLifecycle(a)); err != nil {
			return err
		}
	} else {
		return err
	}
	return insert(ctx, b.tx, kindCheckoutLifecycleReceipt, a.IntentID, checkoutPartition, persistLifecycle(a))
}

var _ billing.CheckoutLifecycleTx = (*checkoutBound)(nil)
