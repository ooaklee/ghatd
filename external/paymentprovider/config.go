package paymentprovider

import "errors"

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
}

// Validate checks if the configuration has the required fields
func (c *Config) Validate() error {
	if c.ProviderName == "" {
		return errors.New(ErrKeyPaymentProviderMissingRequiredField)
	}

	if c.WebhookSecret == "" {
		return errors.New(ErrKeyPaymentProviderRequiredWebhookSecretIsMissing)
	}

	return nil
}

// IsSandbox returns true if the provider is configured for sandbox/test mode
func (c *Config) IsSandbox() bool {
	return c.Environment == "sandbox" || c.Environment == "test"
}

// ProviderConfig is a map of provider names to their configurations
type ProviderConfig map[string]*Config
