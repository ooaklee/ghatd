package paymentprovider

import (
	"context"
	"net/http"
	"net/url"
	"strings"

	"github.com/ooaklee/ghatd/external/logger"
)

// Provider defines the interface that all payment providers must implement
// This abstraction allows the billing system to work with multiple payment providers
// (Paddle, Stripe, Lemon Squeezy, Ko-fi, etc.) using a common interface
type Provider interface {
	// GetProviderName returns the unique identifier for this payment provider
	// e.g., "paddle", "stripe", "lemonsqueezy", "kofi"
	GetProviderName() string

	// VerifyWebhook verifies the authenticity of an incoming webhook request
	// It checks the signature/headers to ensure the webhook came from the payment provider
	// Returns error if verification fails
	VerifyWebhook(ctx context.Context, req *http.Request) error

	// ParsePayload extracts and normalizes webhook data from the provider's format
	// into a common WebhookPayload structure that the billing system can work with
	ParsePayload(ctx context.Context, req *http.Request) (*WebhookPayload, error)

	// GetSubscriptionInfo retrieves current subscription details from the provider's API
	// This is useful for syncing state or retrieving information not in webhooks
	GetSubscriptionInfo(ctx context.Context, subscriptionID string) (*SubscriptionInfo, error)
}

// CheckoutProvider is an optional capability. It intentionally remains outside
// Provider so existing providers and test doubles do not need to implement checkout.
type CheckoutProvider interface {
	CreateCheckoutSession(ctx context.Context, request *CheckoutSessionRequest) (*CheckoutSession, error)
}

// CheckoutReturnURLProvider is an optional checkout configuration capability.
// It keeps the trusted return destination on the same provider instance that
// the registry resolves for checkout, without widening Provider or
// CheckoutProvider for webhook-only and custom implementations.
type CheckoutReturnURLProvider interface {
	GetCheckoutReturnURL() string
}

// CheckoutConfigValidator is an optional validation capability for providers
// that can distinguish webhook-only configuration from checkout opt-in.
// Implementations should treat an empty checkout ReturnURL as not opted in.
type CheckoutConfigValidator interface {
	ValidateCheckoutConfig() error
}

// ValidateCheckoutProviderConfig validates provider-owned checkout settings
// when a non-empty return URL implicitly opts the provider into checkout.
// Providers without the optional validation capability retain their existing
// behaviour and are validated by Billing Manager and the provider call.
func ValidateCheckoutProviderConfig(provider Provider) error {
	if IsNilProvider(provider) {
		return ErrPaymentProviderInvalidConfiguration
	}
	configured, ok := provider.(CheckoutReturnURLProvider)
	if !ok || strings.TrimSpace(configured.GetCheckoutReturnURL()) == "" {
		return nil
	}
	validator, ok := provider.(CheckoutConfigValidator)
	if !ok {
		return nil
	}
	return validator.ValidateCheckoutConfig()
}

// CustomerPortalProvider is an optional capability for providers that expose
// a hosted customer billing-management portal.
type CustomerPortalProvider interface {
	CreateCustomerPortalSession(ctx context.Context, request *CustomerPortalSessionRequest) (*CustomerPortalSession, error)
}

// endpointHostForLog returns only the host portion of an endpoint for logging.
func endpointHostForLog(endpoint string) string {
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return ""
	}
	return parsed.Host
}

// emailPresentForLog reports whether an email value is present without exposing it.
func emailPresentForLog(value string) bool {
	return logger.EmailPresentForLog(value)
}

// emailDomainForLog returns a privacy-safe email domain for structured logs.
func emailDomainForLog(value string) string {
	return logger.EmailDomainForLog(value)
}
