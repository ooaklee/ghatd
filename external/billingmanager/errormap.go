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
	ErrBillingManagerCheckoutProviderUnavailable:           {Title: "Bad Request", Detail: "The requested checkout provider is unavailable", StatusCode: 400, Code: "BM00-014"},
	ErrBillingManagerCheckoutConfigurationInvalid:          {Title: "Internal Server Error", Detail: "Checkout is not configured correctly", StatusCode: 500, Code: "BM00-015"},
	ErrBillingManagerCheckoutOriginRejected:                {Title: "Forbidden", Detail: "Checkout request origin was rejected", StatusCode: 403, Code: "BM00-016"},
	ErrBillingManagerCheckoutPriceUnavailable:              {Title: "Bad Request", Detail: "The requested price is not available for checkout", StatusCode: 400, Code: "BM00-017"},
	ErrBillingManagerCheckoutPriceAmbiguous:                {Title: "Conflict", Detail: "The requested price has conflicting catalogue assignments", StatusCode: 409, Code: "BM00-018"},
	ErrBillingManagerCheckoutTermsUnsupported:              {Title: "Unprocessable Entity", Detail: "The selected catalogue terms are not supported by checkout", StatusCode: 422, Code: "BM00-019"},
	ErrBillingManagerCheckoutUserUnavailable:               {Title: "Conflict", Detail: "The authenticated account cannot start checkout", StatusCode: 409, Code: "BM00-020"},
	ErrBillingManagerCheckoutCatalogueUnavailable:          {Title: "Bad Gateway", Detail: "The pricing catalogue could not be validated", StatusCode: 502, Code: "BM00-021"},
	ErrBillingManagerCheckoutProviderRequestFailed:         {Title: "Bad Gateway", Detail: "The checkout provider could not start a session", StatusCode: 502, Code: "BM00-022"},
	ErrBillingManagerCheckoutSessionInvalid:                {Title: "Bad Gateway", Detail: "The checkout provider returned an invalid session", StatusCode: 502, Code: "BM00-023"},
	ErrBillingManagerCheckoutIdempotencyFailed:             {Title: "Internal Server Error", Detail: "A retry-safe checkout request could not be created", StatusCode: 500, Code: "BM00-024"},
	ErrBillingManagerPortalProviderUnavailable:             {Title: "Bad Request", Detail: "The requested customer portal provider is unavailable", StatusCode: 400, Code: "BM00-025"},
	ErrBillingManagerPortalConfigurationInvalid:            {Title: "Internal Server Error", Detail: "The customer portal is not configured correctly", StatusCode: 500, Code: "BM00-026"},
	ErrBillingManagerPortalOriginRejected:                  {Title: "Forbidden", Detail: "Customer portal request origin was rejected", StatusCode: 403, Code: "BM00-027"},
	ErrBillingManagerPortalBillingUnavailable:              {Title: "Bad Gateway", Detail: "Customer billing could not be validated", StatusCode: 502, Code: "BM00-028"},
	ErrBillingManagerPortalSubscriptionUnavailable:         {Title: "Conflict", Detail: "An eligible recurring subscription is required", StatusCode: 409, Code: "BM00-029"},
	ErrBillingManagerPortalProviderRequestFailed:           {Title: "Bad Gateway", Detail: "The customer portal provider could not start a session", StatusCode: 502, Code: "BM00-030"},
	ErrBillingManagerPortalSessionInvalid:                  {Title: "Bad Gateway", Detail: "The customer portal provider returned an invalid session", StatusCode: 502, Code: "BM00-031"},
	ErrBillingManagerPortalCustomerAmbiguous:               {Title: "Conflict", Detail: "The customer portal billing identity is ambiguous", StatusCode: 409, Code: "BM00-032"},
}
