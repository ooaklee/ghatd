package paymentprovider

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"reflect"

	"github.com/ooaklee/ghatd/external/logger"
	"go.uber.org/zap"
)

const defaultMaxWebhookBodySize int64 = 2 << 20

// ProviderRegistry manages multiple payment providers
type ProviderRegistry struct {
	providers map[string]Provider
}

// NewProviderRegistry creates a new provider registry
func NewProviderRegistry() *ProviderRegistry {
	return &ProviderRegistry{
		providers: make(map[string]Provider),
	}
}

// Register adds a provider to the registry
func (r *ProviderRegistry) Register(provider Provider) {
	if r == nil || IsNilProvider(provider) {
		return
	}
	if r.providers == nil {
		r.providers = make(map[string]Provider)
	}
	r.providers[provider.GetProviderName()] = provider
}

// Get retrieves a provider by name
func (r *ProviderRegistry) Get(name string) (Provider, error) {
	if r == nil {
		return nil, ErrPaymentProviderNotFound
	}
	provider, ok := r.providers[name]
	if !ok {
		return nil, ErrPaymentProviderNotFound
	}
	return provider, nil
}

// GetCheckoutProvider retrieves the optional checkout capability exposed by a
// registered payment provider. Keeping checkout lookup additive avoids making
// checkout mandatory for webhook-only providers.
func (r *ProviderRegistry) GetCheckoutProvider(name string) (CheckoutProvider, error) {
	provider, err := r.Get(name)
	if err != nil {
		return nil, err
	}

	checkoutProvider, ok := provider.(CheckoutProvider)
	if !ok || isNilProviderCapability(checkoutProvider) {
		return nil, ErrPaymentProviderUnsupportedProvider
	}
	if validator, ok := checkoutProvider.(CheckoutConfigValidator); ok {
		if err := validator.ValidateCheckoutConfig(); err != nil {
			return nil, err
		}
	}

	return checkoutProvider, nil
}

// IsNilProvider reports whether a Provider is nil, including an interface that
// contains a typed-nil provider pointer.
func IsNilProvider(provider Provider) bool {
	return isNilProviderCapability(provider)
}

func isNilProviderCapability(capability interface{}) bool {
	if capability == nil {
		return true
	}
	value := reflect.ValueOf(capability)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Ptr, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

// Has checks if a provider is registered
func (r *ProviderRegistry) Has(name string) bool {
	if r == nil {
		return false
	}
	_, ok := r.providers[name]
	return ok
}

// List returns all registered provider names
func (r *ProviderRegistry) List() []string {
	if r == nil {
		return nil
	}
	names := make([]string, 0, len(r.providers))
	for name := range r.providers {
		names = append(names, name)
	}
	return names
}

// VerifyAndParseWebhookPayload is a convenience method that identifies the provider,
// verifies the webhook, and parses the payload
func (r *ProviderRegistry) VerifyAndParseWebhookPayload(ctx context.Context, providerName string, req *http.Request) (*WebhookPayload, error) {
	logger := logger.AcquireOperationFrom(ctx, "external/paymentprovider", "verify-and-parse-webhook-payload", zap.String("provider", providerName))
	logger.Info("payment-provider-webhook-processing-started")

	provider, err := r.Get(providerName)
	if err != nil {
		logger.Warn("payment-provider-not-registered", zap.Error(err))
		return nil, err
	}
	if req == nil || req.Body == nil {
		return nil, ErrPaymentProviderInvalidPayload
	}

	body, err := io.ReadAll(io.LimitReader(req.Body, defaultMaxWebhookBodySize+1))
	if err != nil || int64(len(body)) > defaultMaxWebhookBodySize {
		return nil, ErrPaymentProviderInvalidPayload
	}
	resetBody := func() { req.Body = io.NopCloser(bytes.NewReader(body)) }
	resetBody()
	defer resetBody()

	// Verify the webhook
	if err := provider.VerifyWebhook(ctx, req); err != nil {
		logger.Warn("payment-provider-webhook-verification-failed", zap.Error(err))
		return nil, err
	}
	resetBody()

	// Parse the payload
	payload, err := provider.ParsePayload(ctx, req)
	if err != nil {
		logger.Warn("payment-provider-webhook-payload-parse-failed", zap.Error(err))
		return nil, err
	}
	resetBody()
	if payload == nil {
		return nil, ErrPaymentProviderInvalidPayload
	}

	logger.Info("payment-provider-webhook-processing-completed",
		zap.String("event-type", payload.EventType),
		zap.String("event-id", payload.EventID),
		zap.String("subscription-id", payload.SubscriptionID),
		zap.Bool("is-subscription", payload.IsSubscription()),
	)
	return payload, nil
}

// CreateProviderFromConfig creates a provider instance from configuration
func CreateProviderFromConfig(config *Config) (Provider, error) {
	if config == nil {
		return nil, ErrPaymentProviderInvalidConfiguration
	}
	if err := config.Validate(); err != nil {
		return nil, err
	}

	switch config.ProviderName {
	case "stripe":
		return NewStripeProvider(config)
	case "lemonsqueezy":
		return NewLemonSqueezyProvider(config)
	case "kofi":
		return NewKofiProvider(config)
	default:
		return nil, ErrPaymentProviderUnsupportedProvider
	}
}

// CreateRegistryFromConfigs creates a provider registry from multiple configurations
func CreateRegistryFromConfigs(configs []*Config) (*ProviderRegistry, error) {
	registry := NewProviderRegistry()

	for _, config := range configs {
		provider, err := CreateProviderFromConfig(config)
		if err != nil {
			return nil, err
		}
		registry.Register(provider)
	}

	return registry, nil
}

// WebhookPayloadToJSON converts a webhook payload to JSON string
func WebhookPayloadToJSON(payload *WebhookPayload) (string, error) {
	bytes, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return "", err
	}
	return string(bytes), nil
}
