package billingmanager

import "net/http"

// ProcessBillingProviderWebhooksRequest represents a request to process webhooks
type ProcessBillingProviderWebhooksRequest struct {

	// ProviderName is the name of the payment provider (e.g., "stripe", "paddle")
	ProviderName string

	// Request is the incoming HTTP request containing the webhook payload
	Request *http.Request
}

// GetUserSubscriptionStatusRequest represents a request to get a user's subscription status
type GetUserSubscriptionStatusRequest struct {
	// UserID is the unique identifier of the user
	UserID string

	// RequestingUserID is the ID of the user making the
	// request (for permission checks)
	RequestingUserID string
}

// GetUserBillingEventsRequest represents a request to get billing events for a user
type GetUserBillingEventsRequest struct {
	// UserID is the unique identifier of the user
	UserID string

	// RequestingUserID is the ID of the user making the
	// request (for permission checks)
	RequestingUserID string

	// Order defines how should response be sorted. Default: newest -> oldest (created_at_desc)
	// Valid options: created_at_asc, created_at_desc, updated_at_asc, updated_at_desc,
	Order string `query:"order"`

	// Total number of comms to return per page, if available. Default 25.
	// Accepts anything between 1 and 100
	PerPage int `query:"per_page"`

	// Page specifies the page results should be taken from. Default 1.
	Page int `query:"page"`

	// TotalCount specifies the total count of all comms
	TotalCount int

	// TotalPages specifies the total pages of results
	TotalPages int

	// Meta whether response should contain meta information
	Meta bool `query:"meta"`
}

// GetUserBillingDetailRequest represents a request to get the billing information for a user
type GetUserBillingDetailRequest struct {
	// UserID is the unique identifier of the user
	UserID string

	// RequestingUserID is the ID of the user making the
	// request (for permission checks)
	RequestingUserID string
}
