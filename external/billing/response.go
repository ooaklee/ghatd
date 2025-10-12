package billing

import "github.com/ooaklee/ghatd/external/toolbox"

// CreateSubscriptionResponse holds everything needed to return
// the response to creating a subscription
type CreateSubscriptionResponse struct {
	// Subscription is the subscription that was created
	Subscription *Subscription `json:"subscription"`
}

// UpdateSubscriptionResponse holds everything needed to return
// the response to updating a subscription
type UpdateSubscriptionResponse struct {
	// Subscription is the subscription that was updated
	Subscription *Subscription `json:"subscription"`
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

// GetSubscriptionByIDResponse holds everything needed to return
// the response to getting a subscription by ID
type GetSubscriptionByIDResponse struct {
	// Subscription is the subscription that was found
	Subscription *Subscription `json:"subscription"`
}

// GetSubscriptionByIntegratorIDResponse holds everything needed to return
// the response to getting a subscription by integrator subscription ID
type GetSubscriptionByIntegratorIDResponse struct {
	// Subscription is the subscription that was found
	Subscription *Subscription `json:"subscription"`
}

// CancelSubscriptionResponse holds everything needed to return
// the response to cancelling a subscription
type CancelSubscriptionResponse struct {
	// Subscription is the subscription that was cancelled
	Subscription *Subscription `json:"subscription"`
}

// DeleteSubscriptionResponse holds everything needed to return
// the response to deleting a subscription
type DeleteSubscriptionResponse struct {
	// Success indicates whether the deletion was successful
	Success bool `json:"success"`
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

// GetSubscriptionsByEmailResponse holds everything needed to return
// the response to getting subscriptions by email
type GetSubscriptionsByEmailResponse struct {
	// Subscriptions is the list of subscriptions found
	Subscriptions []Subscription `json:"subscriptions"`

	// Total is the total number of subscriptions found
	Total int `json:"total"`
}

// GetBillingEventsByEmailResponse holds everything needed to return
// the response to getting billing events by email
type GetBillingEventsByEmailResponse struct {
	// BillingEvents is the list of billing events found
	BillingEvents []BillingEvent `json:"billing_events"`

	// Total is the total number of billing events found
	Total int `json:"total"`
}

// AssociateSubscriptionsWithUserResponse holds everything needed to return
// the response to associating subscriptions with a user
type AssociateSubscriptionsWithUserResponse struct {
	// AssociatedCount is the number of subscriptions that were associated
	AssociatedCount int `json:"associated_count"`

	// Success indicates whether the association was successful
	Success bool `json:"success"`
}

// GetUnassociatedSubscriptionsResponse holds everything needed to return
// the response to getting unassociated subscriptions
type GetUnassociatedSubscriptionsResponse struct {
	// Subscriptions is the list of unassociated subscriptions found
	Subscriptions []Subscription `json:"subscriptions"`

	// Total is the total number of unassociated subscriptions found
	Total int `json:"total"`
}

// UpdateSubscriptionUserIDResponse holds everything needed to return
// the response to updating a subscription's user ID
type UpdateSubscriptionUserIDResponse struct {
	// Subscription is the updated subscription
	Subscription *Subscription `json:"subscription"`

	// Success indicates whether the update was successful
	Success bool `json:"success"`
}

// AssociateBillingEventsWithUserResponse holds everything needed to return
// the response to associating billing events with a user
type AssociateBillingEventsWithUserResponse struct {
	// AssociatedCount is the number of billing events that were associated
	AssociatedCount int `json:"associated_count"`

	// Success indicates whether the association was successful
	Success bool `json:"success"`
}

// GetUnassociatedBillingEventsResponse holds everything needed to return
// the response to getting unassociated billing events
type GetUnassociatedBillingEventsResponse struct {
	// BillingEvents is the list of unassociated billing events found
	BillingEvents []BillingEvent `json:"billing_events"`

	// Total is the total number of unassociated billing events found
	Total int `json:"total"`
}
