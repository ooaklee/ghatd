package paymentprovider

import (
	"net/http"
	"time"
)

// Config holds configuration for a payment provider
type Config struct {
	// ProviderName is the unique identifier for this provider
	ProviderName string

	// APIKey is the API key or secret key for the provider
	APIKey string

	// APISecret is an additional secret (used by some providers)
	APISecret string

	// WebhookSecret is the secret used to verify webhook signatures
	WebhookSecret string

	// VendorID is the vendor/merchant ID (used by some providers like Paddle)
	VendorID string

	// Environment specifies the environment ("sandbox" or "production")
	Environment string

	// APIBaseURL is the base URL for API requests (optional, uses provider default if empty)
	APIBaseURL string

	// APIVersion pins the provider API contract used for outbound requests.
	// Providers that support version headers use a tested default when this is empty.
	APIVersion string

	// PublishableKey is returned to browser clients by providers that support embedded checkout.
	PublishableKey string

	// ReturnURL is the trusted default URL used after checkout completes.
	// Checkout-capable providers may expose it to Billing Manager through
	// CheckoutReturnURLProvider; it must not come from browser input.
	ReturnURL string

	// HTTPClient allows applications to configure transport policy and test provider calls.
	HTTPClient *http.Client

	// SignatureTolerance controls the accepted webhook timestamp window.
	SignatureTolerance time.Duration

	// MaxWebhookBodySize bounds direct provider verification and parsing. Zero uses the default.
	MaxWebhookBodySize int64
}

// Validate checks if the configuration has the required fields
func (c *Config) Validate() error {
	if c.ProviderName == "" {
		return ErrPaymentProviderMissingRequiredField
	}

	if c.WebhookSecret == "" {
		return ErrPaymentProviderRequiredWebhookSecretIsMissing
	}

	return nil
}

// IsSandbox returns true if the provider is configured for sandbox/test mode
func (c *Config) IsSandbox() bool {
	return c.Environment == "sandbox" || c.Environment == "test"
}

// ProviderConfig is a map of provider names to their configurations
type ProviderConfig map[string]*Config
