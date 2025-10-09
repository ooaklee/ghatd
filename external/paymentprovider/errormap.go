package paymentprovider

import "github.com/ooaklee/reply"

// PaymentProviderErrorMap holds Error keys, their corresponding human-friendly message, and response status code
// nolint will be used later
var PaymentProviderErrorMap reply.ErrorManifest = reply.ErrorManifest{
	ErrKeyPaymentProviderMissingConfiguration:           {Title: "Internal Server Error", Detail: "Provider name is required in configuration", StatusCode: 500, Code: "PP00-001"},
	ErrKeyPaymentProviderRequiredWebhookSecretIsMissing: {Title: "Internal Server Error", Detail: "Webhook secret is required in configuration", StatusCode: 500, Code: "PP00-002"},
	ErrKeyPaymentProviderInvalidConfigWebhookSecret:     {Title: "Internal Server Error", Detail: "Webhook secret in configuration is invalid", StatusCode: 500, Code: "PP00-003"},
	ErrKeyPaymentProviderInvalidConfiguration:           {Title: "Bad Request", Detail: "Provider configuration is invalid", StatusCode: 400, Code: "PP00-003"},
	ErrKeyPaymentProviderInvalidWebhookSignature:        {Title: "Bad Request", Detail: "Webhook signature verification failed", StatusCode: 400, Code: "PP00-004"},
	ErrKeyPaymentProviderMissingSignature:               {Title: "Bad Request", Detail: "Webhook signature is missing", StatusCode: 400, Code: "PP00-005"},
	ErrKeyPaymentProviderInvalidPayload:                 {Title: "Bad Request", Detail: "Webhook payload is invalid or malformed", StatusCode: 400, Code: "PP00-006"},
	ErrKeyPaymentProviderUnsupportedProvider:            {Title: "Bad Request", Detail: "Payment provider is not supported", StatusCode: 400, Code: "PP00-007"},
	ErrKeyPaymentProviderPayloadParsing:                 {Title: "Bad Request", Detail: "Failed to parse webhook payload", StatusCode: 400, Code: "PP00-008"},
	ErrKeyPaymentProviderMissingRequiredField:           {Title: "Bad Request", Detail: "Required field missing from webhook payload", StatusCode: 400, Code: "PP00-009"},
	ErrKeyPaymentProviderInvalidEventType:               {Title: "Bad Request", Detail: "Event type is not recognised", StatusCode: 400, Code: "PP00-010"},
	ErrKeyPaymentProviderAPIRequestFailed:               {Title: "Internal Server Error", Detail: "Failed to make API request to provider", StatusCode: 500, Code: "PP00-011"},
	ErrKeyPaymentProviderAPIResponseInvalid:             {Title: "Internal Server Error", Detail: "Provider API returned invalid response", StatusCode: 500, Code: "PP00-012"},
	ErrKeyPaymentProviderSubscriptionNotFound:           {Title: "Not Found", Detail: "Subscription not found", StatusCode: 404, Code: "PP00-013"},
	ErrKeyPaymentProviderKofiNoSubscriptionAPI:          {Title: "Not Implemented", Detail: "Ko-fi does not provide a subscription API", StatusCode: 501, Code: "PP00-014"},
	ErrKeyPaymentProviderWebhookTimestampTooOld:         {Title: "Bad Request", Detail: "Webhook is too old", StatusCode: 400, Code: "PP00-015"},
	ErrKeyPaymentProviderMissingPayloadCustomerEmail:    {Title: "Bad Request", Detail: "Customer email is missing from webhook payload", StatusCode: 400, Code: "PP00-016"},
	ErrKeyPaymentProviderNotFound:                       {Title: "Internal Server Error", Detail: "Payment provider not found in registry", StatusCode: 500, Code: "PP00-017"},
}
