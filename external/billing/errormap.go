package billing

import "github.com/ooaklee/reply/v2"

// BillingErrorMap holds Error keys, their corresponding human-friendly message, and response status code
// nolint will be used later
var BillingErrorMap reply.ErrorManifest = reply.ErrorManifest{
	ErrBillingSubscriptionNotFound:             {Title: "Not Found", Detail: "Subscription not found", StatusCode: 404, Code: "BIL00-001"},
	ErrBillingEventNotFound:                    {Title: "Not Found", Detail: "Billing event not found", StatusCode: 404, Code: "BIL00-002"},
	ErrBillingInvalidSubscriptionID:            {Title: "Bad Request", Detail: "Invalid subscription ID", StatusCode: 400, Code: "BIL00-003"},
	ErrBillingInvalidUserID:                    {Title: "Bad Request", Detail: "Invalid user ID", StatusCode: 400, Code: "BIL00-004"},
	ErrBillingInvalidEmail:                     {Title: "Bad Request", Detail: "Invalid email address", StatusCode: 400, Code: "BIL00-005"},
	ErrBillingInvalidStatus:                    {Title: "Bad Request", Detail: "Invalid subscription status", StatusCode: 400, Code: "BIL00-006"},
	ErrBillingMissingRequiredField:             {Title: "Bad Request", Detail: "Missing required field", StatusCode: 400, Code: "BIL00-007"},
	ErrBillingInvalidIntegrator:                {Title: "Bad Request", Detail: "Invalid payment provider integrator", StatusCode: 400, Code: "BIL00-008"},
	ErrBillingInvalidAmount:                    {Title: "Bad Request", Detail: "Invalid amount", StatusCode: 400, Code: "BIL00-009"},
	ErrBillingInvalidCurrency:                  {Title: "Bad Request", Detail: "Invalid currency code", StatusCode: 400, Code: "BIL00-010"},
	ErrBillingSubscriptionAlreadyExists:        {Title: "Conflict", Detail: "Subscription already exists for this user", StatusCode: 409, Code: "BIL00-011"},
	ErrBillingEventAlreadyProcessed:            {Title: "Conflict", Detail: "Billing event already processed", StatusCode: 409, Code: "BIL00-012"},
	ErrBillingDuplicateIntegratorID:            {Title: "Conflict", Detail: "Subscription with this integrator ID already exists", StatusCode: 409, Code: "BIL00-013"},
	ErrBillingFailedToCreateSubscription:       {Title: "Internal Server Error", Detail: "Failed to create subscription", StatusCode: 500, Code: "BIL00-014"},
	ErrBillingFailedToUpdateSubscription:       {Title: "Internal Server Error", Detail: "Failed to update subscription", StatusCode: 500, Code: "BIL00-015"},
	ErrBillingFailedToDeleteSubscription:       {Title: "Internal Server Error", Detail: "Failed to delete subscription", StatusCode: 500, Code: "BIL00-016"},
	ErrBillingFailedToCreateEvent:              {Title: "Internal Server Error", Detail: "Failed to create billing event", StatusCode: 500, Code: "BIL00-017"},
	ErrBillingFailedToGetSubscriptions:         {Title: "Internal Server Error", Detail: "Failed to retrieve subscriptions", StatusCode: 500, Code: "BIL00-018"},
	ErrBillingFailedToGetEvents:                {Title: "Internal Server Error", Detail: "Failed to retrieve billing events", StatusCode: 500, Code: "BIL00-019"},
	ErrBillingUnauthorisedAccess:               {Title: "Unauthorized", Detail: "Unauthorised to access billing information", StatusCode: 401, Code: "BIL00-020"},
	ErrBillingForbiddenOperation:               {Title: "Forbidden", Detail: "Forbidden to perform this billing operation", StatusCode: 403, Code: "BIL00-021"},
	ErrBillingNoSubscriptionsFoundForEmail:     {Title: "Not Found", Detail: "No subscriptions found for the provided email", StatusCode: 404, Code: "BIL00-022"},
	ErrBillingNoEventsFoundForEmail:            {Title: "Not Found", Detail: "No billing events found for the provided email", StatusCode: 404, Code: "BIL00-023"},
	ErrBillingAssociationFailed:                {Title: "Internal Server Error", Detail: "Failed to associate subscriptions with user", StatusCode: 500, Code: "BIL00-024"},
	ErrBillingNoUnassociatedSubscriptionsFound: {Title: "Not Found", Detail: "No unassociated subscriptions found", StatusCode: 404, Code: "BIL00-025"},
	ErrBillingUpdateUserIDFailed:               {Title: "Internal Server Error", Detail: "Failed to update subscription user ID", StatusCode: 500, Code: "BIL00-026"},
}
