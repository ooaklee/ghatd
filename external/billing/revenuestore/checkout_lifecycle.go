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

// ValidateCheckoutAcknowledgement checks the original authorization and both
// acknowledgement directions through this same owning snapshot, without writes.
func (b *checkoutBound) ValidateCheckoutAcknowledgement(ctx context.Context, intent billing.CheckoutIntent) error {
	original, err := b.GetCheckoutIntent(ctx, intent.ID)
	if err != nil {
		return discoveryJoinedError(err)
	}
	if original.Scope != intent.Scope || original.Fingerprint != intent.Fingerprint || !original.CreatedAt.Equal(intent.CreatedAt) {
		return billing.ErrRevenueConflict
	}
	session, err := b.GetCheckoutAcknowledgement(ctx, intent.ID)
	if err != nil {
		return discoveryJoinedError(err)
	}
	if session != intent.SessionID {
		return billing.ErrRevenueConflict
	}
	reverse, _, err := get[checkoutAcknowledgement](ctx, b.tx, kindCheckoutSession, associationKey(intent.Scope, session, ""), checkoutPartition)
	if err != nil {
		return discoveryJoinedError(err)
	}
	if reverse.IntentID != intent.ID || reverse.SessionID != session {
		return billing.ErrRevenueConflict
	}
	return ctx.Err()
}

var _ billing.CheckoutAcknowledgementTx = (*checkoutBound)(nil)

// Explicit private persistence codec preserves fields omitted from public JSON.
type persistedCheckoutLifecycle struct {
	IntentID, IntentFingerprint, PrincipalID, PlanID, CostID, Fingerprint string
	Evidence                                                              paymentprovider.RevenueCheckoutEvidence
	AnchoredAt                                                            time.Time
}

// persistLifecycle converts an anchor into the private persistence codec
// carrying all fields, including ones omitted from public JSON.
func persistLifecycle(a billing.CheckoutLifecycleAnchor) persistedCheckoutLifecycle {
	return persistedCheckoutLifecycle{a.IntentID, a.IntentFingerprint, a.PrincipalID, a.PlanID, a.CostID, a.Fingerprint, a.Evidence, a.AnchoredAt}
}

// anchor converts the persisted record back into the public lifecycle anchor.
func (p persistedCheckoutLifecycle) anchor() billing.CheckoutLifecycleAnchor {
	return billing.CheckoutLifecycleAnchor{IntentID: p.IntentID, IntentFingerprint: p.IntentFingerprint, PrincipalID: p.PrincipalID, PlanID: p.PlanID, CostID: p.CostID, Fingerprint: p.Fingerprint, Evidence: p.Evidence, AnchoredAt: p.AnchoredAt}
}

// anchorScope extracts the anchor's owning revenue scope from its embedded
// evidence.
func anchorScope(a billing.CheckoutLifecycleAnchor) billing.RevenueScope {
	return billing.RevenueScope{Provider: a.Evidence.Scope.Provider, AccountID: a.Evidence.Scope.AccountID, LiveMode: a.Evidence.Scope.LiveMode}
}

// GetCheckoutLifecycleReceipt reads the per-intent anchor receipt, rejecting
// records whose stored intent ID disagrees, that fail Validate, or whose scope
// is outside this transaction's binding.
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

// GetCheckoutLifecycleAnchor reads the subscription-keyed first anchor after
// checking the transaction scope, rejecting records that disagree with the
// requested scope or subscription or fail Validate.
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

// InsertCheckoutLifecycleAnchor writes the first anchor plus its per-intent
// receipt in one transaction: it retains the subscription source row, requires
// agreement with any existing paid owner or earlier anchor (exact fingerprint,
// time, principal and customer), touches the source epoch, and conflicts rather
// than replacing prior anchors.
func (b *checkoutBound) InsertCheckoutLifecycleAnchor(ctx context.Context, a billing.CheckoutLifecycleAnchor) error {
	if a.Validate() != nil {
		return billing.ErrRevenueInvalid
	}
	scope := anchorScope(a)
	if err := b.checkScope(scope); err != nil {
		return err
	}
	if err := retainLifecycleSubscription(ctx, b.tx, lifecycleSubscriptionSource{Scope: scope, SubscriptionID: a.Evidence.SubscriptionID, PrincipalID: a.PrincipalID, CustomerID: a.Evidence.CustomerID, AnchorIntentID: a.IntentID, AnchorFingerprint: a.Fingerprint}); err != nil {
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
	if err := insert(ctx, b.tx, kindCheckoutLifecycleReceipt, a.IntentID, checkoutPartition, persistLifecycle(a)); err != nil {
		return err
	}
	return touchLifecycleSourceEpoch(ctx, b.tx, scope)
}

var _ billing.CheckoutLifecycleTx = (*checkoutBound)(nil)
