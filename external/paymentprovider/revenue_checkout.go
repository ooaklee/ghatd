package paymentprovider

import (
	"context"
	"time"
)

// RevenueCheckoutEvidence contains authenticated complete checkout evidence.
// IntentID/client reference are correlation pointers, not ownership authority.
// The owning billing service must match a previously persisted authorization.
type RevenueCheckoutEvidence struct {
	Scope                                                              RevenueScope
	SessionID, IntentID, ClientReferenceID, CustomerID, SubscriptionID string
	PriceID, Currency, Mode, Status                                    string
	UnitAmountMinor, IntervalCount                                     int64
	BillingCadence                                                     string
	CreatedAt                                                          time.Time
}

// RevenueCheckoutProvider is optional. Existing webhook-only providers retain
// their interface; callers must not replace missing evidence with email matches.
type RevenueCheckoutProvider interface {
	CheckoutRevenueScope(context.Context) (RevenueScope, error)
	LookupRevenueCheckout(context.Context, RevenueScope, string) (RevenueCheckoutEvidence, error)
	RetrieveRevenueCheckoutSession(context.Context, RevenueScope, string) (*CheckoutSession, error)
}

// RevenueCheckoutSessionEvidenceProvider reads complete subscription checkout
// evidence by an already retained session ID, including before the first charge.
// The billing owner must match its frozen intent before establishing association;
// completed checkout and original price evidence never establish paid revenue.
// This optional capability leaves existing checkout providers unchanged.
type RevenueCheckoutSessionEvidenceProvider interface {
	LookupRevenueCheckoutSessionEvidence(context.Context, RevenueScope, string) (RevenueCheckoutEvidence, error)
}
