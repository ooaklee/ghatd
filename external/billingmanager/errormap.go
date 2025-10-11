package billingmanager

import (
	"github.com/ooaklee/reply"
)

// BillingManagerErrorMap holds Error keys, their corresponding human-friendly message, and response status code
// nolint will be used later
var BillingManagerErrorMap reply.ErrorManifest = reply.ErrorManifest{
	ErrKeyBillingManagerUnableToGetProviderNameFromURI:      {Title: "Bad Request", Detail: "Unable to get provider name from URI", StatusCode: 400, Code: "BM00-001"},
	ErrKeyBillingManagerUnableToIdentifyUser:                {Title: "Unauthorized", Detail: "Unable to identify user making the request", StatusCode: 401, Code: "BM00-002"},
	ErrKeyBillingManagerUnableToGetUserIdFromURI:            {Title: "Bad Request", Detail: "Unable to get user ID from URI", StatusCode: 400, Code: "BM00-003"},
	ErrKeyInvalidBillingManagerRequestPayload:               {Title: "Bad Request", Detail: "Invalid billing manager request payload", StatusCode: 400, Code: "BM00-004"},
	ErrKeyBillingManagerFailedWebhookVerification:           {Title: "Internal Server Error", Detail: "Failed to verify webhook", StatusCode: 500, Code: "BM00-005"},
	ErrKeyBillingManagerFailedToProcessEvent:                {Title: "Internal Server Error", Detail: "Failed to process billing event", StatusCode: 500, Code: "BM00-006"},
	ErrKeyBillingManagerFailedToRetrieveSubscriptionStatus:  {Title: "Internal Server Error", Detail: "Failed to retrieve subscription status", StatusCode: 500, Code: "BM00-007"},
	ErrKeyBillingManagerFailedToRetrieveBillingEvents:       {Title: "Internal Server Error", Detail: "Failed to retrieve billing events", StatusCode: 500, Code: "BM00-008"},
	ErrKeyBillingManagerUnableToResolveUserId:               {Title: "Not Found", Detail: "Unable to resolve user ID from payload", StatusCode: 404, Code: "BM00-009"},
	ErrKeyBillingManagerRequiresUserIdIsMissing:             {Title: "Bad Request", Detail: "User ID is required", StatusCode: 400, Code: "BM00-010"},
	ErrKeyBillingManagerUserUnauthorisedToCarryOutOperation: {Title: "Forbidden", Detail: "User not authorised to carry out operation", StatusCode: 403, Code: "BM00-011"},
}
