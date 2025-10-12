package billing

import "github.com/ooaklee/reply"

// BillingErrorMap holds Error keys, their corresponding human-friendly message, and response status code
// nolint will be used later
var BillingErrorMap reply.ErrorManifest = reply.ErrorManifest{
	ErrKeyBillingSubscriptionNotFound:             {Title: "Not Found", Detail: "Subscription not found", StatusCode: 404, Code: "BIL00-001"},
	ErrKeyBillingEventNotFound:                    {Title: "Not Found", Detail: "Billing event not found", StatusCode: 404, Code: "BIL00-002"},
	ErrKeyBillingInvalidSubscriptionID:            {Title: "Bad Request", Detail: "Invalid subscription ID", StatusCode: 400, Code: "BIL00-003"},
	ErrKeyBillingInvalidUserID:                    {Title: "Bad Request", Detail: "Invalid user ID", StatusCode: 400, Code: "BIL00-004"},
	ErrKeyBillingInvalidEmail:                     {Title: "Bad Request", Detail: "Invalid email address", StatusCode: 400, Code: "BIL00-005"},
	ErrKeyBillingInvalidStatus:                    {Title: "Bad Request", Detail: "Invalid subscription status", StatusCode: 400, Code: "BIL00-006"},
	ErrKeyBillingMissingRequiredField:             {Title: "Bad Request", Detail: "Missing required field", StatusCode: 400, Code: "BIL00-007"},
	ErrKeyBillingInvalidIntegrator:                {Title: "Bad Request", Detail: "Invalid payment provider integrator", StatusCode: 400, Code: "BIL00-008"},
	ErrKeyBillingInvalidAmount:                    {Title: "Bad Request", Detail: "Invalid amount", StatusCode: 400, Code: "BIL00-009"},
	ErrKeyBillingInvalidCurrency:                  {Title: "Bad Request", Detail: "Invalid currency code", StatusCode: 400, Code: "BIL00-010"},
	ErrKeyBillingSubscriptionAlreadyExists:        {Title: "Conflict", Detail: "Subscription already exists for this user", StatusCode: 409, Code: "BIL00-011"},
	ErrKeyBillingEventAlreadyProcessed:            {Title: "Conflict", Detail: "Billing event already processed", StatusCode: 409, Code: "BIL00-012"},
	ErrKeyBillingDuplicateIntegratorID:            {Title: "Conflict", Detail: "Subscription with this integrator ID already exists", StatusCode: 409, Code: "BIL00-013"},
	ErrKeyBillingFailedToCreateSubscription:       {Title: "Internal Server Error", Detail: "Failed to create subscription", StatusCode: 500, Code: "BIL00-014"},
	ErrKeyBillingFailedToUpdateSubscription:       {Title: "Internal Server Error", Detail: "Failed to update subscription", StatusCode: 500, Code: "BIL00-015"},
	ErrKeyBillingFailedToDeleteSubscription:       {Title: "Internal Server Error", Detail: "Failed to delete subscription", StatusCode: 500, Code: "BIL00-016"},
	ErrKeyBillingFailedToCreateEvent:              {Title: "Internal Server Error", Detail: "Failed to create billing event", StatusCode: 500, Code: "BIL00-017"},
	ErrKeyBillingFailedToGetSubscriptions:         {Title: "Internal Server Error", Detail: "Failed to retrieve subscriptions", StatusCode: 500, Code: "BIL00-018"},
	ErrKeyBillingFailedToGetEvents:                {Title: "Internal Server Error", Detail: "Failed to retrieve billing events", StatusCode: 500, Code: "BIL00-019"},
	ErrKeyBillingUnauthorisedAccess:               {Title: "Unauthorized", Detail: "Unauthorised to access billing information", StatusCode: 401, Code: "BIL00-020"},
	ErrKeyBillingForbiddenOperation:               {Title: "Forbidden", Detail: "Forbidden to perform this billing operation", StatusCode: 403, Code: "BIL00-021"},
	ErrKeyBillingNoSubscriptionsFoundForEmail:     {Title: "Not Found", Detail: "No subscriptions found for the provided email", StatusCode: 404, Code: "BIL00-022"},
	ErrKeyBillingNoEventsFoundForEmail:            {Title: "Not Found", Detail: "No billing events found for the provided email", StatusCode: 404, Code: "BIL00-023"},
	ErrKeyBillingAssociationFailed:                {Title: "Internal Server Error", Detail: "Failed to associate subscriptions with user", StatusCode: 500, Code: "BIL00-024"},
	ErrKeyBillingNoUnassociatedSubscriptionsFound: {Title: "Not Found", Detail: "No unassociated subscriptions found", StatusCode: 404, Code: "BIL00-025"},
	ErrKeyBillingUpdateUserIDFailed:               {Title: "Internal Server Error", Detail: "Failed to update subscription user ID", StatusCode: 500, Code: "BIL00-026"},
}
