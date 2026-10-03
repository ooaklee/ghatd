package billingmanager

import (
	"net/http"

	"github.com/ooaklee/ghatd/external/pricer"
)

// ProcessBillingProviderWebhooksRequest represents a request to process webhooks
type ProcessBillingProviderWebhooksRequest struct {

	// ProviderName is the name of the payment provider (e.g., "stripe", "paddle")
	ProviderName string

	// Request is the incoming HTTP request containing the webhook payload
	Request *http.Request
}

// ProcessBillingProviderCheckoutRequest contains the authenticated account and
// browser request data needed to create a provider checkout session. ActorID is
// always derived from authentication middleware; Origin and IdempotencyKey are
// HTTP transport inputs and must never be supplied by a request body.
type ProcessBillingProviderCheckoutRequest struct {
	// ActorID is the verified caller and checkout owner, never a browser field.
	ActorID string `json:"-"`
	// ProviderName selects a registered provider from the route, not from JSON.
	ProviderName string
	// PriceID is the browser-selected provider price, checked against the catalogue.
	PriceID string `query:"price" validate:"required"`
	// IdempotencyKey is untrusted retry material scoped to actor, provider and price.
	IdempotencyKey string
	// Origin is the browser Origin header, checked against server-owned return URLs.
	Origin string
	// SecFetchSite is browser Fetch Metadata used to reject cross-site attempts.
	SecFetchSite string
}

// ProcessBillingProviderPortalRequest contains only authenticated and transport
// values needed to create a hosted customer-portal session. The service derives
// the provider customer and return URL from server-owned state.
type ProcessBillingProviderPortalRequest struct {
	// ActorID is the verified caller whose provider customer will be resolved.
	ActorID string `json:"-"`
	// ProviderName selects the registered customer-portal capability.
	ProviderName string
	// Origin is the browser Origin header, not a client-selected return URL.
	Origin string
	// SecFetchSite is browser Fetch Metadata used for the origin check.
	SecFetchSite string
}

// GetUserSubscriptionStatusRequest represents a request to get a user's subscription status
type GetUserSubscriptionStatusRequest struct {
	// UserID selects the billing account; it may differ from an administrator actor.
	UserID string

	// ActorID is the verified caller used for self-or-admin authorization.
	ActorID string `json:"-"`
}

// GetUserBillingEventsRequest represents a request to get billing events for a user
type GetUserBillingEventsRequest struct {
	// UserID selects the billing account; it may differ from an administrator actor.
	UserID string

	// ActorID is the verified caller used for self-or-admin authorization.
	ActorID string `json:"-"`

	// Order defines how should response be sorted. Default: newest -> oldest (created_at_desc)
	// Valid options: created_at_asc, created_at_desc, updated_at_asc, updated_at_desc,
	Order string `query:"order"`

	// PerPage is the number of billing events per page. Default 25.
	// Accepts anything between 1 and 100
	PerPage int `query:"per_page"`

	// Page specifies the page results should be taken from. Default 1.
	Page int `query:"page"`

	// TotalCount specifies the total count of billing events.
	TotalCount int

	// TotalPages specifies the total pages of results
	TotalPages int

	// Meta whether response should contain meta information
	Meta bool `query:"meta"`
}

// GetUserBillingDetailRequest represents a request to get the billing information for a user
type GetUserBillingDetailRequest struct {
	// UserID selects the billing account; it may differ from an administrator actor.
	UserID string

	// ActorID is the verified caller used for self-or-admin authorization.
	ActorID string `json:"-"`
}

// GetPricingPlansRequest wraps the pricer request used to list price plans through BMS.
type GetPricingPlansRequest struct {
	// ActorID is the optional verified caller; empty means public catalogue access.
	ActorID string `json:"-"`

	*pricer.GetPricePlansRequest
}

// GetPricePlanBySlugRequest wraps the pricer request used to get a price plan by slug through BMS.
type GetPricePlanBySlugRequest struct {
	// ActorID is the optional verified caller; empty means public catalogue access.
	ActorID string `json:"-"`

	*pricer.GetPricePlanBySlugRequest
}

// GetPriceFeaturesRequest wraps the pricer request used to list price features through BMS.
type GetPriceFeaturesRequest struct {
	// ActorID is the optional verified caller; empty means public catalogue access.
	ActorID string `json:"-"`

	*pricer.GetFeaturesRequest
}
