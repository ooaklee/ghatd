package helpers

import "github.com/ooaklee/reply/v2"

// PaymentProviderHelperErrorMap maps shared payment-provider helper failures to
// stable, non-sensitive API responses. Starter's standard payment bundles add
// this manifest automatically.
var PaymentProviderHelperErrorMap = reply.ErrorManifest{
	ErrStripeSettingsRequired:             {Title: "Internal Server Error", Detail: "Payment provider settings are unavailable", StatusCode: 500, Code: "PPH0-001"},
	ErrStripeSettingsNotConfigured:        {Title: "Internal Server Error", Detail: "Payment provider settings were not configured", StatusCode: 500, Code: "PPH0-002"},
	ErrStripeEnvironmentRequired:          {Title: "Internal Server Error", Detail: "Payment provider environment is unavailable", StatusCode: 500, Code: "PPH0-003"},
	ErrStripeCredentialsIncomplete:        {Title: "Internal Server Error", Detail: "Payment provider credentials are incomplete", StatusCode: 500, Code: "PPH0-004"},
	ErrStripeKeyModeInvalid:               {Title: "Internal Server Error", Detail: "Payment provider credentials are invalid", StatusCode: 500, Code: "PPH0-005"},
	ErrStripeKeyModeMismatch:              {Title: "Internal Server Error", Detail: "Payment provider credential modes do not match", StatusCode: 500, Code: "PPH0-006"},
	ErrStripeURLInvalid:                   {Title: "Internal Server Error", Detail: "Payment provider URL configuration is invalid", StatusCode: 500, Code: "PPH0-007"},
	ErrStripeReturnURLOriginMismatch:      {Title: "Internal Server Error", Detail: "Payment provider return URL is not trusted", StatusCode: 500, Code: "PPH0-008"},
	ErrStripeHTTPSRequired:                {Title: "Internal Server Error", Detail: "Payment provider return URL is insecure", StatusCode: 500, Code: "PPH0-009"},
	ErrStripeAPIBaseURLUnsafe:             {Title: "Internal Server Error", Detail: "Payment provider API endpoint is not trusted", StatusCode: 500, Code: "PPH0-010"},
	ErrStripeProviderConfigurationInvalid: {Title: "Internal Server Error", Detail: "Payment provider configuration is invalid", StatusCode: 500, Code: "PPH0-011"},
}
