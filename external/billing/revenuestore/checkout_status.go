package revenuestore

import (
	"context"

	"github.com/ooaklee/ghatd/external/billing"
)

// FindCheckoutIntentIDBySession uses the existing encrypted reverse record.
// The billing owner separately validates the forward acknowledgement and intent
// through this same snapshot before returning detached authorization material.
func (b *checkoutBound) FindCheckoutIntentIDBySession(ctx context.Context, scope billing.RevenueScope, session string) (string, error) {
	if err := b.checkScope(scope); err != nil {
		return "", err
	}
	reverse, _, err := get[checkoutAcknowledgement](ctx, b.tx, kindCheckoutSession, associationKey(scope, session, ""), checkoutPartition)
	if err != nil {
		return "", err
	}
	if reverse.SessionID != session || reverse.IntentID == "" {
		return "", billing.ErrRevenueUnavailable
	}
	return reverse.IntentID, nil
}

var _ billing.CheckoutSessionTx = (*checkoutBound)(nil)
