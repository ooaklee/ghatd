package providers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/ooaklee/ghatd/external/paymentprovider"
	paymentproviderhelpers "github.com/ooaklee/ghatd/external/paymentprovider/helpers"
	"github.com/stretchr/testify/require"
)

func paymentSettingsFixture(t *testing.T, kofi, stripe bool) PaymentConfig {
	t.Helper()
	s := PaymentConfig{Environment: "production", Stripe: &paymentproviderhelpers.StripeSettings{}}
	if kofi {
		s.KofiWebhookSecret = "fixture-kofi-private-verification-token"
	}
	if stripe {
		s.Stripe.StripeAPIKey = "sk_test_fixture_not_a_real_key"
		s.Stripe.StripePublishableKey = "pk_test_fixture_not_a_real_key"
		s.Stripe.StripeWebhookSecret = "whsec_fixture_not_a_real_secret"
	}
	require.NoError(t, s.Stripe.Configure(paymentproviderhelpers.DefaultStripeConfiguration(s.Environment, "https://app.example")))
	return s
}

func TestPaymentProvidersKoFiOptInPreservesStripeAppend(t *testing.T) {
	for _, tc := range []struct {
		name                   string
		kofi, stripe           bool
		omitStripe, whitespace bool
		want                   []string
	}{
		{"both disabled", false, false, false, false, []string{}}, {"kofi only", true, false, false, false, []string{"kofi"}},
		{"stripe only", false, true, false, false, []string{"stripe"}}, {"both enabled", true, true, false, false, []string{"kofi", "stripe"}},
		{"nil stripe omitted", true, false, true, false, []string{"kofi"}}, {"whitespace kofi disabled", false, false, false, true, []string{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := paymentSettingsFixture(t, tc.kofi, tc.stripe)
			if tc.omitStripe {
				cfg.Stripe = nil
			}
			if tc.whitespace {
				cfg.KofiWebhookSecret = " \t\n"
			}
			providers, err := BuildPayments(cfg)
			require.NoError(t, err)
			names := []string{}
			registry := paymentprovider.NewProviderRegistry()
			for _, provider := range providers {
				require.False(t, paymentprovider.IsNilProvider(provider))
				names = append(names, provider.GetProviderName())
				registry.Register(provider)
			}
			require.Equal(t, tc.want, names)
			if !tc.kofi {
				_, err = registry.Get("kofi")
				require.ErrorIs(t, err, paymentprovider.ErrPaymentProviderNotFound)
			}
		})
	}
}

func TestConfiguredKoFiKeepsExactWebhookVerification(t *testing.T) {
	for _, tc := range []struct {
		name, secret string
		valid        bool
	}{{"exact", " fixture-kofi-private-verification-token ", true}, {"trimmed", "fixture-kofi-private-verification-token", false}, {"wrong", "different-fixture-token", false}, {"missing", "", false}} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := paymentSettingsFixture(t, true, false)
			cfg.KofiWebhookSecret = " " + cfg.KofiWebhookSecret + " "
			providers, err := BuildPayments(cfg)
			require.NoError(t, err)
			require.Len(t, providers, 1)
			body, err := json.Marshal(map[string]string{"verification_token": tc.secret})
			require.NoError(t, err)
			req := httptest.NewRequest(http.MethodPost, "https://app.example/api/v1/bms/webhooks/kofi", strings.NewReader(url.Values{"data": {string(body)}}.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			err = providers[0].VerifyWebhook(context.Background(), req)
			if tc.valid {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, paymentprovider.ErrPaymentProviderInvalidWebhookSignature)
			}
		})
	}
	// Only local authentication is exercised, with no billing writes or requests.
}

func TestPaymentProvidersPreserveStripeErrorsWithoutSecretLeak(t *testing.T) {
	for _, name := range []string{"changed settings", "unconfigured settings"} {
		t.Run(name, func(t *testing.T) {
			cfg := paymentSettingsFixture(t, true, true)
			if name == "changed settings" {
				cfg.Stripe.StripePublishableKey = ""
			} else {
				cfg.Stripe = &paymentproviderhelpers.StripeSettings{StripeAPIKey: "sk_test_fixture_not_a_real_key", StripeWebhookSecret: "whsec_fixture_not_a_real_secret"}
			}
			providers, err := BuildPayments(cfg)
			require.Error(t, err)
			require.Contains(t, err.Error(), "stripe-payment-provider-initialisation-failed")
			for _, secret := range []string{cfg.Stripe.StripeAPIKey, cfg.Stripe.StripeWebhookSecret, cfg.KofiWebhookSecret} {
				require.NotContains(t, err.Error(), secret)
			}
			require.Len(t, providers, 1, "Stripe append failure preserves the configured Ko-fi provider slice")
			require.Equal(t, "kofi", providers[0].GetProviderName())
		})
	}
}
