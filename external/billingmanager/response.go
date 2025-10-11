package billingmanager

import "github.com/ooaklee/ghatd/external/toolbox"

// GetUserBillingDetailResponse represents the response containing billing information for a user
type GetUserBillingDetailResponse struct {

	// BillingDetail holds billing information for the user
	BillingDetail *BillingDetail `json:"billing_detail,omitempty"`
}

// GetUserBillingEventsResponse represents the response containing billing events for a user
type GetUserBillingEventsResponse struct {

	// Events is the list of billing events
	Events []EventSummary `json:"events"`

	// Total number of billing events found that matched provided
	// filters
	Total int

	// TotalPages total pages available, based on the provided
	// filters and resources per page
	TotalPages int

	// PerPage number of billing events set to be returned per page
	PerPage int

	// Page specifies the page results were taken from. Default 1.
	Page int
}

// GetMetaData returns a map containing metadata about the GetUserBillingEventsResponse,
// including the number of resources per page, total resources, total pages,
// and the current page.
func (g *GetUserBillingEventsResponse) GetMetaData() map[string]interface{} {
	var responseMap = make(map[string]interface{})

	responseMap[string(toolbox.ResponseMetaKeyResourcePerPage)] = g.PerPage
	responseMap[string(toolbox.ResponseMetaKeyTotalResources)] = g.Total
	responseMap[string(toolbox.ResponseMetaKeyTotalPages)] = g.TotalPages
	responseMap[string(toolbox.ResponseMetaKeyPage)] = g.Page

	return responseMap
}

// GetUserSubscriptionStatusResponse represents the response containing a user's current
// subscription status
type GetUserSubscriptionStatusResponse struct {

	// SubscriptionStatus holds the user's subscription status details
	SubscriptionStatus *SubscriptionStatus `json:"subscription_status,omitempty"`
}
