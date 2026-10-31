package providers

import (
	"fmt"
	"strings"

	"github.com/ooaklee/ghatd/external/paymentprovider"
	paymenthelpers "github.com/ooaklee/ghatd/external/paymentprovider/helpers"
)

// PaymentConfig supplies resolved credentials and optional Stripe capabilities.
// Stripe must already have passed Configure. Nil omits Stripe entirely; a
// configured, disabled settings block also contributes no provider.
type PaymentConfig struct {
	Environment string
	// KofiWebhookSecret is preserved byte-for-byte when nonblank. Whitespace-only
	// configuration disables the optional provider rather than accepting a key.
	KofiWebhookSecret string
	Stripe            *paymenthelpers.StripeSettings
	StripeOptions     paymenthelpers.StripeProviderOptions
}

// BuildPayments composes optional Ko-fi followed by Stripe without contacting
// either provider. Failure retains already constructed providers for diagnostics;
// callers must abort startup rather than serve a partial registry after an error.
// Provider validation and secret-safe errors remain in the owning packages.
func BuildPayments(cfg PaymentConfig) ([]paymentprovider.Provider, error) {
	providers := []paymentprovider.Provider{}
	if strings.TrimSpace(cfg.KofiWebhookSecret) != "" {
		kofi, err := paymentprovider.NewKofiProvider(&paymentprovider.Config{
			ProviderName: "kofi", Environment: cfg.Environment, WebhookSecret: cfg.KofiWebhookSecret,
		})
		if err != nil {
			return providers, fmt.Errorf("serverhelpers/kofi-payment-provider-initialisation-failed: %w", err)
		}
		providers = append(providers, kofi)
	}
	if cfg.Stripe == nil {
		return providers, nil
	}
	providers, err := cfg.Stripe.AppendProviderWithOptions(providers, cfg.StripeOptions)
	if err != nil {
		return providers, fmt.Errorf("serverhelpers/stripe-payment-provider-initialisation-failed: %w", err)
	}
	return providers, nil
}
