package revenuestore

import (
	"context"
	"math"

	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/paymentprovider"
	"github.com/ooaklee/ghatd/external/repository/recordstore"
)

const (
	kindLifecycleCheckoutSource     = "billing_lifecycle_checkout_source"
	kindLifecycleSubscriptionSource = "billing_lifecycle_subscription_source"
)

// These are native source projections, not worker jobs. Original pointers and
// identities are encrypted; indexed references reveal neither provider IDs nor
// customer details. There is deliberately no lease, freshness or done state.
type lifecycleCheckoutSource struct {
	Scope                                  billing.RevenueScope
	IntentID, IntentFingerprint, SessionID string
}
type lifecycleSubscriptionSource struct {
	Scope                                   billing.RevenueScope
	SubscriptionID, PrincipalID, CustomerID string
	FactID, FactFingerprint                 string
	AnchorIntentID, AnchorFingerprint       string
}

func lifecycleSourcePartition(scope billing.RevenueScope) string {
	return "billing-lifecycle-source-v1:" + checkoutScopeKey(scope)
}
func lifecycleSubscriptionKey(scope billing.RevenueScope, sub string) string {
	return key("billing-lifecycle-source-v1", checkoutScopeKey(scope), sub)
}
func validLifecycleOwner(v lifecycleSubscriptionSource) bool {
	if !statusScopeValid(v.Scope, v.SubscriptionID) || !statusScopeValid(v.Scope, v.PrincipalID) || !statusScopeValid(v.Scope, v.CustomerID) {
		return false
	}
	return (v.FactID == "") == (v.FactFingerprint == "") && (v.AnchorIntentID == "") == (v.AnchorFingerprint == "")
}

// Both financial and checkout guards touch this SAME row. Mongo's uniqueness
// and revision CAS close cross-guard write skew; a read-only opposite-source
// check cannot provide that guarantee. Later renewals never replace originals.
func retainLifecycleSubscription(ctx context.Context, tx recordstore.Tx, v lifecycleSubscriptionSource) error {
	if !validLifecycleOwner(v) {
		return billing.ErrRevenueInvalid
	}
	// Existing pre-projection checkout ownership remains authoritative during
	// upgrade. Never let a new projection hide contradictory retained history.
	checkout := &checkoutBound{tx: tx}
	anchor, err := checkout.GetCheckoutLifecycleAnchor(ctx, v.Scope, v.SubscriptionID)
	if err == nil {
		receipt, receiptErr := checkout.GetCheckoutLifecycleReceipt(ctx, anchor.IntentID)
		if receiptErr != nil {
			if singleCause(receiptErr, billing.ErrRevenueNotFound) {
				return billing.ErrRevenueUnavailable
			}
			return receiptErr
		}
		if receipt.Fingerprint != anchor.Fingerprint || !receipt.AnchoredAt.Equal(anchor.AnchoredAt) {
			return billing.ErrRevenueUnavailable
		}
		if anchor.PrincipalID != v.PrincipalID || anchor.Evidence.CustomerID != v.CustomerID {
			return billing.ErrRevenueConflict
		}
		// Upgrade or a later same-owner intent retains the first native anchor.
		v.AnchorIntentID, v.AnchorFingerprint = anchor.IntentID, anchor.Fingerprint
	} else if !singleCause(err, billing.ErrRevenueNotFound) {
		return err
	}
	paid, _, err := get[billing.CheckoutAssociation](ctx, tx, kindCheckoutPrincipal, associationKey(v.Scope, v.SubscriptionID, ""), checkoutPartition)
	if err == nil {
		if paid.Scope != v.Scope || paid.SubscriptionID != v.SubscriptionID || paid.LinkedAt.IsZero() {
			return billing.ErrRevenueUnavailable
		}
		if paid.PrincipalID != v.PrincipalID || paid.CustomerID != v.CustomerID {
			return billing.ErrRevenueConflict
		}
	} else if !singleCause(err, billing.ErrRevenueNotFound) {
		return err
	}
	id, part := lifecycleSubscriptionKey(v.Scope, v.SubscriptionID), lifecycleSourcePartition(v.Scope)
	old, row, err := get[lifecycleSubscriptionSource](ctx, tx, kindLifecycleSubscriptionSource, id, part)
	revision := int64(1)
	if err == nil {
		if !validLifecycleOwner(old) || row.Revision < 1 || row.Sequence != 0 || row.State != "" || row.ExpiresAt != nil {
			return billing.ErrRevenueUnavailable
		}
		if old.Scope != v.Scope || old.SubscriptionID != v.SubscriptionID || old.PrincipalID != v.PrincipalID || old.CustomerID != v.CustomerID {
			return billing.ErrRevenueConflict
		}
		// An exact original source may not change fingerprint. Distinct renewals or
		// same-owner later checkout receipts do not retarget first-source pointers.
		if (old.FactID != "" && old.FactID == v.FactID && old.FactFingerprint != v.FactFingerprint) || (old.AnchorIntentID != "" && old.AnchorIntentID == v.AnchorIntentID && old.AnchorFingerprint != v.AnchorFingerprint) {
			return billing.ErrRevenueConflict
		}
		changed := false
		if old.FactID == "" && v.FactID != "" {
			old.FactID, old.FactFingerprint = v.FactID, v.FactFingerprint
			changed = true
		}
		if old.AnchorIntentID == "" && v.AnchorIntentID != "" {
			old.AnchorIntentID, old.AnchorFingerprint = v.AnchorIntentID, v.AnchorFingerprint
			changed = true
		}
		if !changed {
			return ctx.Err()
		}
		if row.Revision == math.MaxInt64 {
			return billing.ErrRevenueUnavailable
		}
		v, revision = old, row.Revision+1
	} else if !singleCause(err, billing.ErrRevenueNotFound) {
		return err
	}
	next, err := recordstore.NewRecord(kindLifecycleSubscriptionSource, id, part, revision, v)
	if err != nil {
		return mapped(err)
	}
	if revision == 1 {
		return mapped(tx.Insert(ctx, next))
	}
	return mapped(tx.Replace(ctx, next, revision-1))
}

func retainLifecycleCheckout(ctx context.Context, tx recordstore.Tx, intent billing.CheckoutIntent, session string) error {
	if intent.Request.Mode != paymentprovider.CheckoutModeSubscription {
		return nil
	}
	v := lifecycleCheckoutSource{intent.Scope, intent.ID, intent.Fingerprint, session}
	id, part := key("billing-lifecycle-checkout-v1", checkoutScopeKey(intent.Scope), intent.ID), lifecycleSourcePartition(intent.Scope)
	return insert(ctx, tx, kindLifecycleCheckoutSource, id, part, v)
}
