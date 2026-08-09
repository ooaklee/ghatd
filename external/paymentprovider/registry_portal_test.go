package paymentprovider

import (
	"context"
	"errors"
	"testing"
)

type registryPortalProvider struct {
	*MockProvider
	returnURL string
	configErr error
}

type registryPortalProviderWithoutSessionValidator struct {
	*MockProvider
	returnURL string
}

func (*registryPortalProviderWithoutSessionValidator) GetProviderName() string {
	return "unsafe-portal"
}
func (*registryPortalProviderWithoutSessionValidator) CreateCustomerPortalSession(context.Context, *CustomerPortalSessionRequest) (*CustomerPortalSession, error) {
	return &CustomerPortalSession{ID: "bps_unsafe", URL: "https://attacker.example.test/session"}, nil
}
func (p *registryPortalProviderWithoutSessionValidator) GetCustomerPortalReturnURL() string {
	return p.returnURL
}

func (*registryPortalProvider) GetProviderName() string { return "portal" }
func (*registryPortalProvider) CreateCustomerPortalSession(context.Context, *CustomerPortalSessionRequest) (*CustomerPortalSession, error) {
	return &CustomerPortalSession{ID: "bps_123", URL: "https://portal.example.test/session"}, nil
}
func (p *registryPortalProvider) GetCustomerPortalReturnURL() string {
	if p == nil {
		return ""
	}
	return p.returnURL
}
func (p *registryPortalProvider) ValidateCustomerPortalConfig() error {
	if p == nil {
		return ErrPaymentProviderInvalidConfiguration
	}
	return p.configErr
}
func (*registryPortalProvider) ValidateCustomerPortalSessionURL(string) error { return nil }

func TestProviderRegistryGetCustomerPortalProviderUsesOptionalCapability(t *testing.T) {
	registry := NewProviderRegistry()
	portalProvider := &registryPortalProvider{MockProvider: NewMockProvider("portal"), returnURL: "https://app.example.test/settings"}
	registry.Register(portalProvider)
	registry.Register(NewMockProvider("webhook-only"))

	got, err := registry.GetCustomerPortalProvider("portal")
	if err != nil || got != portalProvider {
		t.Fatalf("GetCustomerPortalProvider() = %#v, %v", got, err)
	}
	configured, ok := got.(CustomerPortalReturnURLProvider)
	if !ok || configured.GetCustomerPortalReturnURL() != portalProvider.returnURL {
		t.Fatalf("portal return URL capability = %#v", got)
	}
	if _, err := registry.GetCustomerPortalProvider("mock-webhook-only"); !errors.Is(err, ErrPaymentProviderUnsupportedProvider) {
		t.Fatalf("webhook-only error = %v", err)
	}
	if _, err := registry.GetCustomerPortalProvider("missing"); !errors.Is(err, ErrPaymentProviderNotFound) {
		t.Fatalf("missing error = %v", err)
	}

	portalProvider.configErr = ErrPaymentProviderInvalidConfiguration
	if _, err := registry.GetCustomerPortalProvider("portal"); !errors.Is(err, ErrPaymentProviderInvalidConfiguration) {
		t.Fatalf("invalid config error = %v", err)
	}
}

func TestProviderRegistryGetCustomerPortalProviderRejectsTypedNilCapability(t *testing.T) {
	registry := NewProviderRegistry()
	var provider *registryPortalProvider
	// Injecting simulates a custom registry implementation returning a typed nil;
	// Register itself rejects this value.
	registry.providers["portal"] = provider
	if _, err := registry.GetCustomerPortalProvider("portal"); !errors.Is(err, ErrPaymentProviderUnsupportedProvider) {
		t.Fatalf("typed nil error = %v", err)
	}
}

func TestValidateCustomerPortalProviderConfigUsesNonEmptyReturnURLAsOptIn(t *testing.T) {
	tests := []struct {
		name     string
		provider *registryPortalProvider
		want     error
	}{
		{name: "blank is not opted in", provider: &registryPortalProvider{}},
		{name: "valid opt in", provider: &registryPortalProvider{returnURL: "https://app.example.test/settings"}},
		{name: "invalid opt in", provider: &registryPortalProvider{returnURL: "https://app.example.test/settings", configErr: ErrPaymentProviderInvalidConfiguration}, want: ErrPaymentProviderInvalidConfiguration},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := ValidateCustomerPortalProviderConfig(test.provider)
			if !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want %v", err, test.want)
			}
		})
	}
	var typedNil *registryPortalProvider
	if err := ValidateCustomerPortalProviderConfig(typedNil); !errors.Is(err, ErrPaymentProviderInvalidConfiguration) {
		t.Fatalf("typed nil error = %v", err)
	}
	unsafeProvider := &registryPortalProviderWithoutSessionValidator{returnURL: "https://app.example.test/settings"}
	if err := ValidateCustomerPortalProviderConfig(unsafeProvider); !errors.Is(err, ErrPaymentProviderInvalidConfiguration) {
		t.Fatalf("missing session validator error = %v", err)
	}
}
