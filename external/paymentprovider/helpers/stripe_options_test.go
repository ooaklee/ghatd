package helpers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/ooaklee/ghatd/external/paymentprovider"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStripeProviderOptionsKeepConfigurationBoundary(t *testing.T) {
	for _, tc := range []struct {
		name                                                                      string
		disabled, unconfigured, mutated, invalidRevenue, nilSettings, zeroOptions bool
		want                                                                      error
	}{
		{name: "explicit_client_and_revenue_forwarded"},
		{name: "disabled_ignores_options", disabled: true},
		{name: "disabled_ignores_invalid_options", disabled: true, invalidRevenue: true},
		{name: "nil_settings", nilSettings: true, want: ErrStripeSettingsRequired},
		{name: "zero_options_keep_revenue_disabled", zeroOptions: true},
		{name: "configure_required", unconfigured: true, want: ErrStripeSettingsNotConfigured},
		{name: "settings_mutation_revalidated", mutated: true, want: ErrStripeCredentialsIncomplete},
		{name: "invalid_revenue_refused", invalidRevenue: true, want: ErrStripeProviderConfigurationInvalid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var requests atomic.Int32
			api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				assert.Equal(t, "/v1/account", r.URL.Path)
				assert.Equal(t, "Bearer sk_test_example", r.Header.Get("Authorization"))
				assert.NoError(t, json.NewEncoder(w).Encode(map[string]string{"id": "acct_fixture"}))
			}))
			t.Cleanup(api.Close)
			settings := completeTestStripeSettings()
			if tc.disabled {
				settings = StripeSettings{}
			}
			settings.StripeAPIBaseURL = api.URL
			if !tc.unconfigured {
				require.NoError(t, settings.Configure(DefaultStripeConfiguration("local", "https://app.example.test")))
			}
			if tc.mutated {
				settings.StripePublishableKey = ""
			}
			revenue := &paymentprovider.RevenueConfig{AccountID: "acct_fixture", CurrencyExponents: map[string]int{"GBP": 2}}
			if tc.invalidRevenue {
				revenue.AccountID = ""
			}
			client := &http.Client{Transport: optionsRoundTripper{target: api.URL, client: api.Client()}}
			// This origin cannot resolve; success proves the explicit client is used.
			settings.StripeAPIBaseURL = "http://provider.invalid"
			options := StripeProviderOptions{HTTPClient: client, Revenue: revenue, AllowPromotionCodes: true}
			if tc.zeroOptions {
				options = StripeProviderOptions{}
			}
			prior, err := paymentprovider.NewKofiProvider(&paymentprovider.Config{WebhookSecret: "fixture_token"})
			require.NoError(t, err)
			selected := &settings
			if tc.nilSettings {
				selected = nil
			}
			providers, err := selected.AppendProviderWithOptions([]paymentprovider.Provider{prior}, options)
			require.NotEmpty(t, providers)
			require.Same(t, prior, providers[0])
			if tc.want != nil {
				require.ErrorIs(t, err, tc.want)
				require.Len(t, providers, 1)
				require.Zero(t, requests.Load())
				return
			}
			require.NoError(t, err)
			if tc.disabled {
				require.Len(t, providers, 1)
				require.Zero(t, requests.Load())
				return
			}
			require.Len(t, providers, 2)
			require.Zero(t, requests.Load(), "construction must perform no provider I/O")
			scope, err := providers[1].(paymentprovider.RevenueCheckoutProvider).CheckoutRevenueScope(t.Context())
			if tc.zeroOptions {
				require.ErrorIs(t, err, paymentprovider.ErrRevenueNotEnabled)
				require.Zero(t, requests.Load())
				return
			}
			require.NoError(t, err)
			require.Equal(t, paymentprovider.RevenueScope{Provider: "stripe", AccountID: "acct_fixture"}, scope)
			require.Equal(t, int32(1), requests.Load())
		})
	}
}

// Only this injected client can reach the synthetic merchant at provider.invalid.
type optionsRoundTripper struct {
	target string
	client *http.Client
}

func (r optionsRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	clone := request.Clone(request.Context())
	u := *clone.URL
	clone.URL = &u
	endpoint, err := http.NewRequest(http.MethodGet, r.target, nil)
	if err != nil {
		return nil, err
	}
	clone.URL.Scheme, clone.URL.Host = endpoint.URL.Scheme, endpoint.URL.Host
	return r.client.Transport.RoundTrip(clone)
}
