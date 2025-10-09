package billing

import (
	"time"

	"github.com/ooaklee/ghatd/external/toolbox"
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

// CreateSubscriptionResponse holds everything needed to return
// the response to creating a subscription
type CreateSubscriptionResponse struct {
	// Subscription is the subscription that was created
	Subscription *Subscription `json:"subscription"`
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

// UpdateSubscriptionResponse holds everything needed to return
// the response to updating a subscription
type UpdateSubscriptionResponse struct {
	// Subscription is the subscription that was updated
	Subscription *Subscription `json:"subscription"`
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

// GetSubscriptionsResponse holds everything needed to return
// the response to get subscriptions
type GetSubscriptionsResponse struct {
	Subscriptions []Subscription `json:"subscriptions"`

	// Total number of subscriptions found that matched provided
	// filters
	Total int

	// TotalPages total pages available, based on the provided
	// filters and resources per page
	TotalPages int

	// PerPage number of subscriptions set to be returned per page
	PerPage int

	// Page specifies the page results were taken from. Default 1.
	Page int
}

// GetMetaData returns a map containing metadata about the GetSubscriptionsResponse,
// including the number of resources per page, total resources, total pages,
// and the current page.
func (g *GetSubscriptionsResponse) GetMetaData() map[string]interface{} {
	var responseMap = make(map[string]interface{})

	responseMap[string(toolbox.ResponseMetaKeyResourcePerPage)] = g.PerPage
	responseMap[string(toolbox.ResponseMetaKeyTotalResources)] = g.Total
	responseMap[string(toolbox.ResponseMetaKeyTotalPages)] = g.TotalPages
	responseMap[string(toolbox.ResponseMetaKeyPage)] = g.Page

	return responseMap
}

// GetSubscriptionByIDRequest holds everything needed to make
// the request to get a subscription by ID
type GetSubscriptionByIDRequest struct {
	// ID is the internal unique identifier
	ID string
}

// GetSubscriptionByIDResponse holds everything needed to return
// the response to getting a subscription by ID
type GetSubscriptionByIDResponse struct {
	// Subscription is the subscription that was found
	Subscription *Subscription `json:"subscription"`
}

// GetSubscriptionByIntegratorIDRequest holds everything needed to make
// the request to get a subscription by integrator subscription ID
type GetSubscriptionByIntegratorIDRequest struct {
	// IntegratorName is the payment provider name
	IntegratorName string

	// IntegratorSubscriptionID is the provider's subscription ID
	IntegratorSubscriptionID string
}

// GetSubscriptionByIntegratorIDResponse holds everything needed to return
// the response to getting a subscription by integrator subscription ID
type GetSubscriptionByIntegratorIDResponse struct {
	// Subscription is the subscription that was found
	Subscription *Subscription `json:"subscription"`
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

// CancelSubscriptionResponse holds everything needed to return
// the response to cancelling a subscription
type CancelSubscriptionResponse struct {
	// Subscription is the subscription that was cancelled
	Subscription *Subscription `json:"subscription"`
}

// DeleteSubscriptionRequest holds everything needed to make
// the request to delete a subscription
type DeleteSubscriptionRequest struct {
	// ID is the internal unique identifier
	ID string
}

// DeleteSubscriptionResponse holds everything needed to return
// the response to deleting a subscription
type DeleteSubscriptionResponse struct {
	// Success indicates whether the deletion was successful
	Success bool `json:"success"`
}

//

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

// GetBillingEventsResponse holds everything needed to return
// the response to get billing events
type GetBillingEventsResponse struct {
	BillingEvents []BillingEvent `json:"billing_events"`

	// Total number of billing event found that matched provided
	// filters
	Total int

	// TotalPages total pages available, based on the provided
	// filters and resources per page
	TotalPages int

	// PerPage number of billing event set to be returned per page
	PerPage int

	// Page specifies the page results were taken from. Default 1.
	Page int
}

// GetMetaData returns a map containing metadata about the GetBillingEventResponse,
// including the number of resources per page, total resources, total pages,
// and the current page.
func (g *GetBillingEventsResponse) GetMetaData() map[string]interface{} {
	var responseMap = make(map[string]interface{})

	responseMap[string(toolbox.ResponseMetaKeyResourcePerPage)] = g.PerPage
	responseMap[string(toolbox.ResponseMetaKeyTotalResources)] = g.Total
	responseMap[string(toolbox.ResponseMetaKeyTotalPages)] = g.TotalPages
	responseMap[string(toolbox.ResponseMetaKeyPage)] = g.Page

	return responseMap
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
