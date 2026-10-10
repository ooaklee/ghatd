package revenuestore

import (
	"context"
	"encoding/json"

	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/paymentprovider"
	"github.com/ooaklee/ghatd/external/repository/recordstore"
)

const (
	kindCheckoutIntent      = "billing_checkout_intent"
	kindCheckoutAck         = "billing_checkout_acknowledgement"
	kindCheckoutSession     = "billing_checkout_session"
	kindCheckoutAssociation = "billing_checkout_association"
	kindCheckoutPrincipal   = "billing_checkout_principal"
	checkoutPartition       = "billing-checkout-history-v1"
)

// checkoutBound binds a record-store transaction to one revenue scope for
// writes, or an unbound read view when scope is nil.
type checkoutBound struct {
	tx    recordstore.Tx
	scope *billing.RevenueScope
}

// persistedCheckout is the stored intent shape separating the intent metadata,
// original provider request and fingerprint.
type persistedCheckout struct {
	Intent      billing.CheckoutIntent
	Request     paymentprovider.CheckoutSessionRequest
	Fingerprint string
}

// checkoutAcknowledgement persists the immutable intent-to-session binding.
type checkoutAcknowledgement struct{ IntentID, SessionID string }

// checkoutScopeKey hashes a scope into a stable storage key component.
func checkoutScopeKey(s billing.RevenueScope) string { b, _ := json.Marshal(s); return key(string(b)) }

// associationKey builds the composite storage key for a scope's subscription
// and optional price binding.
func associationKey(s billing.RevenueScope, sub, price string) string {
	return key(checkoutScopeKey(s), sub, price)
}

// WithCheckoutTransaction runs the callback inside a transaction partitioned
// and keyed to the exact revenue scope, rejecting nil context or callback as
// invalid.
func (r *Repository) WithCheckoutTransaction(ctx context.Context, scope billing.RevenueScope, fn func(billing.CheckoutTx) error) error {
	if ctx == nil || fn == nil {
		return billing.ErrRevenueInvalid
	}
	return mapped(r.store.Transact(ctx, checkoutPartition+":"+checkoutScopeKey(scope), func(tx recordstore.Tx) error { return fn(&checkoutBound{tx, &scope}) }))
}

// ReadCheckout runs the callback on an unbound read view without a scope
// binding, rejecting nil context or callback as invalid.
func (r *Repository) ReadCheckout(ctx context.Context, fn func(billing.CheckoutTx) error) error {
	if ctx == nil || fn == nil {
		return billing.ErrRevenueInvalid
	}
	return mapped(r.store.Read(ctx, func(tx recordstore.Tx) error { return fn(&checkoutBound{tx, nil}) }))
}

// checkScope conflicts when this bound transaction's scope differs from the
// requested one, and passes for unbound reads.
func (b *checkoutBound) checkScope(scope billing.RevenueScope) error {
	if b.scope != nil && *b.scope != scope {
		return billing.ErrRevenueConflict
	}
	return nil
}

// GetCheckoutIntent returns the stored intent only when its ID, fingerprint,
// creation time and absent session match the stored shape and its scope
// satisfies the transaction binding.
func (b *checkoutBound) GetCheckoutIntent(ctx context.Context, id string) (billing.CheckoutIntent, error) {
	stored, _, err := get[persistedCheckout](ctx, b.tx, kindCheckoutIntent, id, checkoutPartition)
	if err != nil {
		return billing.CheckoutIntent{}, err
	}
	v := stored.Intent
	v.Request = stored.Request
	v.Fingerprint = stored.Fingerprint
	if v.ID != id || v.Fingerprint == "" || v.CreatedAt.IsZero() || v.SessionID != "" {
		return billing.CheckoutIntent{}, billing.ErrRevenueUnavailable
	}
	if err := b.checkScope(v.Scope); err != nil {
		return billing.CheckoutIntent{}, err
	}
	return v, nil
}

// InsertCheckoutIntent stores a new intent in this scope after rejecting one
// that already carries a session ID.
func (b *checkoutBound) InsertCheckoutIntent(ctx context.Context, v billing.CheckoutIntent) error {
	if err := b.checkScope(v.Scope); err != nil {
		return err
	}
	if v.SessionID != "" {
		return billing.ErrRevenueInvalid
	}
	return insert(ctx, b.tx, kindCheckoutIntent, v.ID, checkoutPartition, persistedCheckout{v, v.Request, v.Fingerprint})
}

// GetCheckoutAcknowledgement returns the stored session ID only when the
// record's intent matches and a session is present.
func (b *checkoutBound) GetCheckoutAcknowledgement(ctx context.Context, id string) (string, error) {
	v, _, err := get[checkoutAcknowledgement](ctx, b.tx, kindCheckoutAck, id, checkoutPartition)
	if err != nil {
		return "", err
	}
	if v.IntentID != id || v.SessionID == "" {
		return "", billing.ErrRevenueUnavailable
	}
	return v.SessionID, nil
}

// InsertCheckoutAcknowledgement re-reads the intent, reserves the reverse
// session key (conflicting with a different owner), stores the forward binding,
// retains the lifecycle checkout source, and touches the source epoch for
// subscription-mode intents.
func (b *checkoutBound) InsertCheckoutAcknowledgement(ctx context.Context, id, session string) error {
	intent, err := b.GetCheckoutIntent(ctx, id)
	if err != nil {
		return err
	}
	if err := b.checkScope(intent.Scope); err != nil {
		return err
	}
	sessionKey := associationKey(intent.Scope, session, "")
	owner, _, err := get[checkoutAcknowledgement](ctx, b.tx, kindCheckoutSession, sessionKey, checkoutPartition)
	if err == nil {
		if owner.IntentID != id || owner.SessionID != session {
			return billing.ErrRevenueConflict
		}
	} else if singleCause(err, billing.ErrRevenueNotFound) {
		if err := insert(ctx, b.tx, kindCheckoutSession, sessionKey, checkoutPartition, checkoutAcknowledgement{id, session}); err != nil {
			return err
		}
	} else {
		return err
	}
	if err := insert(ctx, b.tx, kindCheckoutAck, id, checkoutPartition, checkoutAcknowledgement{id, session}); err != nil {
		return err
	}
	if err := retainLifecycleCheckout(ctx, b.tx, intent, session); err != nil {
		return err
	}
	if intent.Request.Mode == paymentprovider.CheckoutModeSubscription {
		return touchLifecycleSourceEpoch(ctx, b.tx, intent.Scope)
	}
	return nil
}

// GetCheckoutAssociation returns the stored price-keyed association only when
// it matches the requested scope, subscription, price and has a link time,
// after checking the transaction scope.
func (b *checkoutBound) GetCheckoutAssociation(ctx context.Context, scope billing.RevenueScope, sub, price string) (billing.CheckoutAssociation, error) {
	if err := b.checkScope(scope); err != nil {
		return billing.CheckoutAssociation{}, err
	}
	v, _, err := get[billing.CheckoutAssociation](ctx, b.tx, kindCheckoutAssociation, associationKey(scope, sub, price), checkoutPartition)
	if err != nil {
		return v, err
	}
	if v.Scope != scope || v.SubscriptionID != sub || v.ProviderPriceID != price || v.LinkedAt.IsZero() {
		return billing.CheckoutAssociation{}, billing.ErrRevenueUnavailable
	}
	return v, nil
}

// InsertCheckoutAssociation writes the immutable association after verifying
// agreement with any existing lifecycle anchor and prior principal binding,
// retaining the subscription source row, recording the principal binding,
// storing the price-keyed record and touching the source epoch; disagreements
// conflict.
func (b *checkoutBound) InsertCheckoutAssociation(ctx context.Context, v billing.CheckoutAssociation) error {
	if err := b.checkScope(v.Scope); err != nil {
		return err
	}
	anchor, err := b.GetCheckoutLifecycleAnchor(ctx, v.Scope, v.SubscriptionID)
	if err == nil {
		receipt, err := b.GetCheckoutLifecycleReceipt(ctx, anchor.IntentID)
		if err != nil {
			if singleCause(err, billing.ErrRevenueNotFound) {
				return billing.ErrRevenueUnavailable
			}
			return err
		}
		if receipt.Fingerprint != anchor.Fingerprint || !receipt.AnchoredAt.Equal(anchor.AnchoredAt) || anchor.PrincipalID != v.PrincipalID || anchor.Evidence.CustomerID != v.CustomerID {
			return billing.ErrRevenueConflict
		}
	} else if !singleCause(err, billing.ErrRevenueNotFound) {
		return err
	}
	if err := retainLifecycleSubscription(ctx, b.tx, lifecycleSubscriptionSource{Scope: v.Scope, SubscriptionID: v.SubscriptionID, PrincipalID: v.PrincipalID, CustomerID: v.CustomerID}); err != nil {
		return err
	}
	principalID := associationKey(v.Scope, v.SubscriptionID, "")
	prior, _, err := get[billing.CheckoutAssociation](ctx, b.tx, kindCheckoutPrincipal, principalID, checkoutPartition)
	if err == nil {
		if prior.Scope != v.Scope || prior.SubscriptionID != v.SubscriptionID || prior.PrincipalID != v.PrincipalID || prior.CustomerID != v.CustomerID {
			return billing.ErrRevenueConflict
		}
	} else if singleCause(err, billing.ErrRevenueNotFound) {
		if err := insert(ctx, b.tx, kindCheckoutPrincipal, principalID, checkoutPartition, v); err != nil {
			return err
		}
	} else {
		return err
	}
	if err := insert(ctx, b.tx, kindCheckoutAssociation, associationKey(v.Scope, v.SubscriptionID, v.ProviderPriceID), checkoutPartition, v); err != nil {
		return err
	}
	return touchLifecycleSourceEpoch(ctx, b.tx, v.Scope)
}

var _ billing.CheckoutRepository = (*Repository)(nil)
