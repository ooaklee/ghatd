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
	// CheckoutRevenueScope returns the verified merchant RevenueScope for checkout
	// operations, checking the configured merchant with the authenticated API. Part
	// of the optional RevenueCheckoutProvider contract; the returned scope scopes
	// subsequent session lookups.
	CheckoutRevenueScope(context.Context) (RevenueScope, error)
	// LookupRevenueCheckout returns RevenueCheckoutEvidence for the checkout
	// session matching the given subscription ID within the supplied RevenueScope.
	// Part of the optional RevenueCheckoutProvider contract; missing or ambiguous
	// evidence is never guessed from metadata or email matches.
	LookupRevenueCheckout(context.Context, RevenueScope, string) (RevenueCheckoutEvidence, error)
	// RetrieveRevenueCheckoutSession returns the CheckoutSession identified by the
	// given cs_ session ID within the supplied RevenueScope, recovering an
	// acknowledged session without a second POST. Part of the optional
	// RevenueCheckoutProvider contract for retrieval after idempotency retention
	// has elapsed.
	RetrieveRevenueCheckoutSession(context.Context, RevenueScope, string) (*CheckoutSession, error)
}

// RevenueCheckoutSessionEvidenceProvider reads complete subscription checkout
// evidence by an already retained session ID, including before the first charge.
// The billing owner must match its frozen intent before establishing association;
// completed checkout and original price evidence never establish paid revenue.
// This optional capability leaves existing checkout providers unchanged.
type RevenueCheckoutSessionEvidenceProvider interface {
	// LookupRevenueCheckoutSessionEvidence returns complete RevenueCheckoutEvidence
	// for an already retained cs_ session ID within the supplied RevenueScope,
	// including before the first charge. The billing owner must match the frozen
	// intent; completed checkout evidence never establishes paid revenue.
	LookupRevenueCheckoutSessionEvidence(context.Context, RevenueScope, string) (RevenueCheckoutEvidence, error)
}
