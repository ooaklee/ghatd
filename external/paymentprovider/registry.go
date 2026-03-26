package paymentprovider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
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
// verifies the webhook, and parses the payload.
// It reads the request body once and resets it before each provider call to avoid
// the body-consumption bug where VerifyWebhook drains req.Body leaving ParsePayload
// with an empty reader.
func (r *ProviderRegistry) VerifyAndParseWebhookPayload(ctx context.Context, providerName string, req *http.Request) (*WebhookPayload, error) {
	provider, err := r.Get(providerName)
	if err != nil {
		return nil, err
	}

	// Read the body once so both VerifyWebhook and ParsePayload can consume it
	bodyBytes, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, errors.New(ErrKeyPaymentProviderInvalidPayload)
	}

	// Reset body for VerifyWebhook
	req.Body = io.NopCloser(bytes.NewReader(bodyBytes))

	// Verify the webhook
	if err := provider.VerifyWebhook(ctx, req); err != nil {
		return nil, err
	}

	// Reset body for ParsePayload
	req.Body = io.NopCloser(bytes.NewReader(bodyBytes))

	// Parse the payload
	return provider.ParsePayload(ctx, req)
}

// SyncSubscriptionByCustomerID attempts to fetch the latest subscription state
// directly from the provider's API using the customer ID. This implements Theo's
// "re-fetch from API" pattern: treat webhooks as triggers, then pull authoritative
// state from the provider rather than trusting the webhook payload data.
//
// Returns (info, true, nil) if the provider supports syncing and data was retrieved.
// Returns (nil, true, nil) if the provider supports syncing but no subscription exists.
// Returns (nil, false, nil) if the provider does not implement SubscriptionSyncer.
func (r *ProviderRegistry) SyncSubscriptionByCustomerID(ctx context.Context, providerName string, customerID string) (*SubscriptionInfo, bool, error) {
	provider, err := r.Get(providerName)
	if err != nil {
		return nil, false, err
	}

	syncer, ok := provider.(SubscriptionSyncer)
	if !ok {
		return nil, false, nil
	}

	info, err := syncer.GetActiveSubscriptionByCustomerID(ctx, customerID)
	if err != nil {
		return nil, true, err
	}

	return info, true, nil
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
