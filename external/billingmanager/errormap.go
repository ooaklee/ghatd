package billingmanager

import (
	"github.com/ooaklee/reply/v2"
)

// BillingManagerErrorMap holds Error keys, their corresponding human-friendly message, and response status code
// nolint will be used later
var BillingManagerErrorMap reply.ErrorManifest = reply.ErrorManifest{
	ErrBillingManagerUnableToGetProviderNameFromURI:        {Title: "Bad Request", Detail: "Unable to get provider name from URI", StatusCode: 400, Code: "BM00-001"},
	ErrBillingManagerUnableToIdentifyUser:                  {Title: "Unauthorized", Detail: "Unable to identify user making the request", StatusCode: 401, Code: "BM00-002"},
	ErrBillingManagerUnableToGetUserIdFromURI:              {Title: "Bad Request", Detail: "Unable to get user ID from URI", StatusCode: 400, Code: "BM00-003"},
	ErrInvalidBillingManagerRequestPayload:                 {Title: "Bad Request", Detail: "Invalid billing manager request payload", StatusCode: 400, Code: "BM00-004"},
	ErrBillingManagerFailedWebhookVerification:             {Title: "Internal Server Error", Detail: "Failed to verify webhook", StatusCode: 500, Code: "BM00-005"},
	ErrBillingManagerFailedToProcessEvent:                  {Title: "Internal Server Error", Detail: "Failed to process billing event", StatusCode: 500, Code: "BM00-006"},
	ErrBillingManagerFailedToRetrieveSubscriptionStatus:    {Title: "Internal Server Error", Detail: "Failed to retrieve subscription status", StatusCode: 500, Code: "BM00-007"},
	ErrBillingManagerFailedToRetrieveBillingEvents:         {Title: "Internal Server Error", Detail: "Failed to retrieve billing events", StatusCode: 500, Code: "BM00-008"},
	ErrBillingManagerUnableToResolveUserId:                 {Title: "Not Found", Detail: "Unable to resolve user ID from payload", StatusCode: 404, Code: "BM00-009"},
	ErrBillingManagerRequiresUserIdIsMissing:               {Title: "Bad Request", Detail: "User ID is required", StatusCode: 400, Code: "BM00-010"},
	ErrBillingManagerUserUnauthorisedToCarryOutOperation:   {Title: "Forbidden", Detail: "User not authorised to carry out operation", StatusCode: 403, Code: "BM00-011"},
	ErrBillingManagerNoUserIdentifyingInformationInPayload: {Title: "Bad Request", Detail: "No user identifying information present in payload", StatusCode: 400, Code: "BM00-012"},
	ErrBillingManagerPricerServiceNotSet:                   {Title: "Internal Server Error", Detail: "Pricing service is not configured", StatusCode: 500, Code: "BM00-013"},
}
