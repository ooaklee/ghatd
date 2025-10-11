package billing

import (
	"time"
)

// CreateSubscriptionRequest contains data for creating a subscription
type CreateSubscriptionRequest struct {
	UserID                   string
	Email                    string
	Status                   string
	Integrator               string
	IntegratorSubscriptionID string
	IntegratorCustomerID     string
	PlanName                 string
	PlanID                   string
	Amount                   int64
	Currency                 string
	BillingInterval          string
	NextBillingDate          *time.Time
	AvailableUntilDate       *time.Time
	TrialEndsAt              *time.Time
	CancelURL                string
	UpdateURL                string
	Metadata                 map[string]interface{}
}

// UpdateSubscriptionRequest contains data for updating a subscription
type UpdateSubscriptionRequest struct {
	ID                 string
	Status             *string
	PlanName           *string
	PlanID             *string
	Amount             *int64
	Currency           *string
	BillingInterval    *string
	NextBillingDate    *time.Time
	AvailableUntilDate *time.Time
	TrialEndsAt        *time.Time
	CancelledAt        *time.Time
	CancelURL          *string
	UpdateURL          *string
	Metadata           map[string]interface{}
}

// GetTotalSubscriptionsRequest holds everything needed to make
// the request to get the total count of subscriptions from repository
type GetTotalSubscriptionsRequest struct {
	// IntegratorName is the provider name to filter by
	IntegratorName string

	// IntegratorSubscriptionID is the provider subscription ID to filter by
	IntegratorSubscriptionID string

	// IntegratorCustomerID is the provider customer ID to filter by
	IntegratorCustomerID string

	// UserIDs is the list of user IDs to filter by
	UserIDs []string

	// Emails is the list of emails to filter by
	Emails []string

	// Statuses is the list of statuses to filter by
	Statuses []string

	// PlanNameContains is the subtext to filter by
	PlanNameContains string

	// Currency is the list of currencies to filter by
	Currency []string

	// BillingInterval is the list of billing intervals to filter by
	BillingInterval []string

	// CreatedAtFrom is to filter by the date to which the subscription was created from
	CreatedAtFrom string

	// CreatedAtTo is to filter by the date to which the subscription was created at up to
	CreatedAtTo string

	// NextBillingDateFrom is to filter by the date to which the next billing date is from
	NextBillingDateFrom string

	// NextBillingDateTo is to filter by the date to which the next billing date is up to
	NextBillingDateTo string
}

// GetSubscriptionsRequest holds everything needed to make
// the request to get subscriptions
type GetSubscriptionsRequest struct {
	// Order defines how should response be sorted. Default: newest -> oldest (created_at_desc)
	// Valid options: created_at_asc, created_at_desc, updated_at_asc, updated_at_desc
	Order string `query:"order"`

	// Total number of subscriptions to return per page, if available. Default 25.
	// Accepts anything between 1 and 100
	PerPage int `query:"per_page"`

	// Page specifies the page results should be taken from. Default 1.
	Page int `query:"page"`

	// TotalCount specifies the total count of all subscriptions
	TotalCount int

	// TotalPages specifies the total pages of results
	TotalPages int

	// Meta whether response should contain meta information
	Meta bool `query:"meta"`

	// IntegratorName is the provider name to filter by
	IntegratorName string `query:"integrator_name"`

	// IntegratorSubscriptionID is the provider subscription ID to filter by
	IntegratorSubscriptionID string `query:"integrator_subscription_id"`

	// IntegratorCustomerID is the provider customer ID to filter by
	IntegratorCustomerID string `query:"integrator_customer_id"`

	// ForUserIDs is the list of user IDs to filter by
	// comma-separated list of user IDs
	ForUserIDs []string `query:"for_user_ids"`

	// ForEmails is the list of emails to filter by
	// comma-separated list of emails
	ForEmails []string `query:"for_emails"`

	// Statuses is the list of statuses to filter by
	// comma-separated list of statuses
	Statuses []string `query:"statuses"`

	// PlanNameContains is the subtext to filter by
	PlanNameContains string `query:"plan_name_contains"`

	// Currency is the list of currencies to filter by
	// comma-separated list of currencies
	Currency []string `query:"currency"`

	// BillingInterval is the list of billing intervals to filter by
	// comma-separated list of billing intervals
	BillingInterval []string `query:"billing_interval"`

	// NextBillingDateFrom is to filter by the date to which the next billing date is from
	NextBillingDateFrom string `query:"next_billing_date_from"`

	// NextBillingDateTo is to filter by the date to which the next billing date is up to
	NextBillingDateTo string `query:"next_billing_date_to"`

	// CreatedAtFrom filters for subscriptions created at from the provided date
	CreatedAtFrom string `query:"created_at_from"`

	// CreatedAtTo filters for subscriptions created at up to the provided date
	CreatedAtTo string `query:"created_at_to"`
}

// GetSubscriptionByIDRequest holds everything needed to make
// the request to get a subscription by ID
type GetSubscriptionByIDRequest struct {
	// ID is the internal unique identifier
	ID string
}

// GetSubscriptionByIntegratorIDRequest holds everything needed to make
// the request to get a subscription by integrator subscription ID
type GetSubscriptionByIntegratorIDRequest struct {
	// IntegratorName is the payment provider name
	IntegratorName string

	// IntegratorSubscriptionID is the provider's subscription ID
	IntegratorSubscriptionID string
}

// CancelSubscriptionRequest holds everything needed to make
// the request to cancel a subscription
type CancelSubscriptionRequest struct {
	// ID is the internal unique identifier
	ID string

	// CancelledAt is when the subscription was cancelled
	CancelledAt *time.Time

	// Status is the new status of the subscription
	Status string
}

// DeleteSubscriptionRequest holds everything needed to make
// the request to delete a subscription
type DeleteSubscriptionRequest struct {
	// ID is the internal unique identifier
	ID string
}

// GetTotalBillingEventsRequest holds everything needed to make
// the request to get the total count of billing event from repository
type GetTotalBillingEventsRequest struct {

	// IntegratorName is the provider name to filter by
	IntegratorName string

	// IntegratorUserID is the provider's user ID to filter by
	IntegratorUserID string

	// IntegratorSubscriptionID is the provider subscription ID to filter by
	IntegratorSubscriptionID string

	// UserIDs is the list of user IDs to filter by
	UserIDs []string

	// Emails is the list of emails to filter by
	Emails []string

	// EventTypes is the list of billing event types to filter by
	EventTypes []string

	// PlanNameContains is the subtext to filter by
	PlanNameContains string

	// Currency is the list of currencies to filter by
	Currency []string

	// Statuses is the list of statuses to filter by
	Statuses []string

	// CreatedAtFrom is to filter by the date to which the billing event was created from
	CreatedAtFrom string

	// CreatedAtTo is to filter by the date to which the billing event was created at up to
	CreatedAtTo string

	// EventTimeFrom is to filter by the date to which the event time was from
	EventTimeFrom string

	// EventTimeTo is to filter by the date to which the event time was up to
	EventTimeTo string
}

// GetBillingEventsRequest holds everything needed to make
// the request to get billing event
type GetBillingEventsRequest struct {

	// Order defines how should response be sorted. Default: newest -> oldest (created_at_desc)
	// Valid options: created_at_asc, created_at_desc, updated_at_asc, updated_at_desc,
	Order string `query:"order"`

	// Total number of billing event to return per page, if available. Default 25.
	// Accepts anything between 1 and 100
	PerPage int `query:"per_page"`

	// Page specifies the page results should be taken from. Default 1.
	Page int `query:"page"`

	// TotalCount specifies the total count of all billing event
	TotalCount int

	// TotalPages specifies the total pages of results
	TotalPages int

	// Meta whether response should contain meta information
	Meta bool `query:"meta"`

	// IntegratorName is the provider name to filter by
	IntegratorName string `query:"integrator_name"`

	// IntegratorUserID is the provider's user ID to filter by
	IntegratorUserID string `query:"integrator_user_id"`

	// IntegratorSubscriptionID is the provider subscription ID to filter by
	IntegratorSubscriptionID string `query:"integrator_subscription_id"`

	// ForUserIDs is the list of user IDs to filter by
	// comma-separated list of user IDs
	ForUserIDs []string `query:"for_user_ids"`

	// EventTypes is the list of billing event types to filter by
	// comma-separated list of event types
	EventTypes []string `query:"event_types"`

	// PlanNameContains is the subtext to filter by
	PlanNameContains string `query:"plan_name_contains"`

	// Currency is the list of currencies to filter by
	// comma-separated list of currencies
	Currency []string `query:"currency"`

	// Statuses is the list of statuses to filter by
	// comma-separated list of statuses
	Statuses []string `query:"statuses"`

	// EventTimeFrom is to filter by the date to which the event time was from
	EventTimeFrom string `query:"event_time_from"`

	// EventTimeTo is to filter by the date to which the event time was up to
	EventTimeTo string `query:"event_time_to"`

	// CreatedAtFrom filters for billing event created at from the provided date
	CreatedAtFrom string `query:"created_at_from"`

	// CreatedAtTo filters for billing event created at up to the provided date
	CreatedAtTo string `query:"created_at_to"`
}

// GetSubscriptionsByEmailRequest holds everything needed to make
// the request to get subscriptions by email
type GetSubscriptionsByEmailRequest struct {
	// Email is the email address to search for
	Email string
}

// GetBillingEventsByEmailRequest holds everything needed to make
// the request to get billing events by email
type GetBillingEventsByEmailRequest struct {
	// Email is the email address to search for
	Email string
}

// AssociateSubscriptionsWithUserRequest holds everything needed to make
// the request to associate subscriptions with a user
type AssociateSubscriptionsWithUserRequest struct {
	// UserID is the user ID to associate subscriptions with
	UserID string

	// Email is the email address to find subscriptions for
	Email string
}

// GetUnassociatedSubscriptionsRequest holds everything needed to make
// the request to get unassociated subscriptions
type GetUnassociatedSubscriptionsRequest struct {
	// IntegratorName optionally filters by payment provider
	IntegratorName string

	// CreatedAtFrom optionally filters subscriptions created from this date
	CreatedAtFrom string

	// CreatedAtTo optionally filters subscriptions created up to this date
	CreatedAtTo string

	// Limit optionally limits the number of results (default: 100)
	Limit int
}

// UpdateSubscriptionUserIDRequest holds everything needed to make
// the request to update a subscription's user ID
type UpdateSubscriptionUserIDRequest struct {
	// SubscriptionID is the internal subscription ID
	SubscriptionID string

	// UserID is the new user ID to associate with the subscription
	UserID string
}

// CreateBillingEventRequest holds everything needed to make
// the request to create a billing event
type CreateBillingEventRequest struct {
	SubscriptionID           string
	UserID                   string
	EventType                string
	Integrator               string
	IntegratorEventID        string
	IntegratorSubscriptionID string
	Status                   string
	Amount                   int64
	Currency                 string
	PlanName                 string
	ReceiptURL               string
	RawPayload               string
	EventTime                time.Time
}

// CreateBillingEventResponse holds everything needed to return
// the response to creating a billing event
type CreateBillingEventResponse struct {

	// BillingEvent is the billing event that was created
	BillingEvent *BillingEvent `json:"billing_event"`
}
