package paymentprovider

import (
	"context"
	"errors"
	"testing"
)

type registryCheckoutProvider struct {
	*MockProvider
	returnURL string
}

func (*registryCheckoutProvider) GetProviderName() string { return "checkout" }

func (*registryCheckoutProvider) CreateCheckoutSession(context.Context, *CheckoutSessionRequest) (*CheckoutSession, error) {
	return &CheckoutSession{ID: "checkout_123", URL: "https://checkout.example.test/session"}, nil
}

func (p *registryCheckoutProvider) GetCheckoutReturnURL() string { return p.returnURL }

func TestProviderRegistryGetCheckoutProviderUsesOptionalCapability(t *testing.T) {
	registry := NewProviderRegistry()
	checkoutProvider := &registryCheckoutProvider{MockProvider: NewMockProvider("checkout"), returnURL: "https://app.example.test/return"}
	registry.Register(checkoutProvider)
	registry.Register(NewMockProvider("webhook-only"))

	got, err := registry.GetCheckoutProvider("checkout")
	if err != nil {
		t.Fatalf("GetCheckoutProvider() error = %v", err)
	}
	if got != checkoutProvider {
		t.Fatalf("GetCheckoutProvider() = %#v, want registered capability", got)
	}
	configured, ok := got.(CheckoutReturnURLProvider)
	if !ok || configured.GetCheckoutReturnURL() != checkoutProvider.returnURL {
		t.Fatalf("checkout return URL capability = %#v", got)
	}
	if _, err := registry.GetCheckoutProvider("mock-webhook-only"); !errors.Is(err, ErrPaymentProviderUnsupportedProvider) {
		t.Fatalf("webhook-only error = %v", err)
	}
	if _, err := registry.GetCheckoutProvider("missing"); !errors.Is(err, ErrPaymentProviderNotFound) {
		t.Fatalf("missing error = %v", err)
	}
}

func TestProviderRegistryRejectsTypedNilProviders(t *testing.T) {
	registry := NewProviderRegistry()
	var provider *registryCheckoutProvider
	registry.Register(provider)
	if registry.Has("checkout") {
		t.Fatal("typed-nil provider was registered")
	}
}
