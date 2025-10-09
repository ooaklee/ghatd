package paymentprovider

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
)

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
	r.providers[provider.GetProviderName()] = provider
}

// Get retrieves a provider by name
func (r *ProviderRegistry) Get(name string) (Provider, error) {
	provider, ok := r.providers[name]
	if !ok {
		return nil, errors.New(ErrKeyPaymentProviderNotFound)
	}
	return provider, nil
}

// Has checks if a provider is registered
func (r *ProviderRegistry) Has(name string) bool {
	_, ok := r.providers[name]
	return ok
}

// List returns all registered provider names
func (r *ProviderRegistry) List() []string {
	names := make([]string, 0, len(r.providers))
	for name := range r.providers {
		names = append(names, name)
	}
	return names
}

// VerifyAndParseWebhookPayload is a convenience method that identifies the provider,
// verifies the webhook, and parses the payload
func (r *ProviderRegistry) VerifyAndParseWebhookPayload(ctx context.Context, providerName string, req *http.Request) (*WebhookPayload, error) {
	provider, err := r.Get(providerName)
	if err != nil {
		return nil, err
	}

	// Verify the webhook
	if err := provider.VerifyWebhook(ctx, req); err != nil {
		return nil, err
	}

	// Parse the payload
	return provider.ParsePayload(ctx, req)
}

// CreateProviderFromConfig creates a provider instance from configuration
func CreateProviderFromConfig(config *Config) (Provider, error) {
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
		return nil, errors.New(ErrKeyPaymentProviderUnsupportedProvider)
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
