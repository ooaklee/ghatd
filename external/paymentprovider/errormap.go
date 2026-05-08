package paymentprovider

import "github.com/ooaklee/reply/v2"

// PaymentProviderErrorMap holds Error keys, their corresponding human-friendly message, and response status code
// nolint will be used later
var PaymentProviderErrorMap reply.ErrorManifest = reply.ErrorManifest{
	ErrPaymentProviderMissingConfiguration:           {Title: "Internal Server Error", Detail: "Provider name is required in configuration", StatusCode: 500, Code: "PP00-001"},
	ErrPaymentProviderRequiredWebhookSecretIsMissing: {Title: "Internal Server Error", Detail: "Webhook secret is required in configuration", StatusCode: 500, Code: "PP00-002"},
	ErrPaymentProviderInvalidConfigWebhookSecret:     {Title: "Internal Server Error", Detail: "Webhook secret in configuration is invalid", StatusCode: 500, Code: "PP00-003"},
	ErrPaymentProviderInvalidConfiguration:           {Title: "Bad Request", Detail: "Provider configuration is invalid", StatusCode: 400, Code: "PP00-003"},
	ErrPaymentProviderInvalidWebhookSignature:        {Title: "Bad Request", Detail: "Webhook signature verification failed", StatusCode: 400, Code: "PP00-004"},
	ErrPaymentProviderMissingSignature:               {Title: "Bad Request", Detail: "Webhook signature is missing", StatusCode: 400, Code: "PP00-005"},
	ErrPaymentProviderInvalidPayload:                 {Title: "Bad Request", Detail: "Webhook payload is invalid or malformed", StatusCode: 400, Code: "PP00-006"},
	ErrPaymentProviderUnsupportedProvider:            {Title: "Bad Request", Detail: "Payment provider is not supported", StatusCode: 400, Code: "PP00-007"},
	ErrPaymentProviderPayloadParsing:                 {Title: "Bad Request", Detail: "Failed to parse webhook payload", StatusCode: 400, Code: "PP00-008"},
	ErrPaymentProviderMissingRequiredField:           {Title: "Bad Request", Detail: "Required field missing from webhook payload", StatusCode: 400, Code: "PP00-009"},
	ErrPaymentProviderInvalidEventType:               {Title: "Bad Request", Detail: "Event type is not recognised", StatusCode: 400, Code: "PP00-010"},
	ErrPaymentProviderAPIRequestFailed:               {Title: "Internal Server Error", Detail: "Failed to make API request to provider", StatusCode: 500, Code: "PP00-011"},
	ErrPaymentProviderAPIResponseInvalid:             {Title: "Internal Server Error", Detail: "Provider API returned invalid response", StatusCode: 500, Code: "PP00-012"},
	ErrPaymentProviderSubscriptionNotFound:           {Title: "Not Found", Detail: "Subscription not found", StatusCode: 404, Code: "PP00-013"},
	ErrPaymentProviderKofiNoSubscriptionAPI:          {Title: "Not Implemented", Detail: "Ko-fi does not provide a subscription API", StatusCode: 501, Code: "PP00-014"},
	ErrPaymentProviderWebhookTimestampTooOld:         {Title: "Bad Request", Detail: "Webhook is too old", StatusCode: 400, Code: "PP00-015"},
	ErrPaymentProviderMissingPayloadCustomerEmail:    {Title: "Bad Request", Detail: "Customer email is missing from webhook payload", StatusCode: 400, Code: "PP00-016"},
	ErrPaymentProviderNotFound:                       {Title: "Internal Server Error", Detail: "Payment provider not found in registry", StatusCode: 500, Code: "PP00-017"},
}
