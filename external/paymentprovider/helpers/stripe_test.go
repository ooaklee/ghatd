package helpers

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/kelseyhightower/envconfig"
	"github.com/ooaklee/ghatd/external/paymentprovider"
)

// TestStripeSettingsLoadConfigureAndBuildProvider verifies environment settings can build a complete Stripe provider.
func TestStripeSettingsLoadConfigureAndBuildProvider(t *testing.T) {
	clearStripeEnvironment(t)
	t.Setenv("STRIPE_API_KEY", "sk_live_example")
	t.Setenv("STRIPE_PUBLISHABLE_KEY", "pk_live_example")
	t.Setenv("STRIPE_WEBHOOK_SECRET", "whsec_example")
	t.Setenv("STRIPE_PORTAL_CONFIGURATION_ID", "bpc_example")

	type hostSettings struct {
		StripeSettings
	}
	var host hostSettings
	if err := envconfig.Process("", &host); err != nil {
		t.Fatalf("envconfig.Process() error = %v", err)
	}
	if err := host.StripeSettings.Configure(DefaultStripeConfiguration("production", "https://app.example.test/")); err != nil {
		t.Fatalf("Configure() error = %v", err)
	}

	if host.StripeAPIBaseURL != paymentprovider.StripeDefaultAPIBaseURL {
		t.Fatalf("StripeAPIBaseURL = %q", host.StripeAPIBaseURL)
	}
	if host.StripeAPIVersion != paymentprovider.StripeDefaultAPIVersion {
		t.Fatalf("StripeAPIVersion = %q", host.StripeAPIVersion)
	}
	if host.StripeSuccessURL != "https://app.example.test/app/plan?stripe=success&session_id={CHECKOUT_SESSION_ID}" {
		t.Fatalf("StripeSuccessURL = %q", host.StripeSuccessURL)
	}
	if host.StripePortalReturnURL != "https://app.example.test/settings#billing" {
		t.Fatalf("StripePortalReturnURL = %q", host.StripePortalReturnURL)
	}
	if !host.StripeSettings.IsEnabled() {
		t.Fatal("IsEnabled() = false, want true")
	}

	provider, err := host.StripeSettings.NewProvider()
	if err != nil {
		t.Fatalf("NewProvider() error = %v", err)
	}
	if provider == nil || provider.GetProviderName() != "stripe" {
		t.Fatalf("NewProvider() = %#v", provider)
	}
	if provider.GetCheckoutReturnURL() != host.StripeSuccessURL {
		t.Fatalf("GetCheckoutReturnURL() = %q", provider.GetCheckoutReturnURL())
	}
	if provider.GetCustomerPortalReturnURL() != host.StripePortalReturnURL {
		t.Fatalf("GetCustomerPortalReturnURL() = %q", provider.GetCustomerPortalReturnURL())
	}
}

// TestStripeSettingsDisabledAndCheckoutOnlyModes verifies each supported Stripe feature mode.
func TestStripeSettingsDisabledAndCheckoutOnlyModes(t *testing.T) {
	t.Run("disabled", func(t *testing.T) {
		settings := &StripeSettings{}
		if err := settings.Configure(DefaultStripeConfiguration("local", "http://localhost:5173")); err != nil {
			t.Fatalf("Configure() error = %v", err)
		}
		provider, err := settings.NewProvider()
		if err != nil {
			t.Fatalf("NewProvider() error = %v", err)
		}
		if provider != nil {
			t.Fatalf("NewProvider() = %#v, want nil", provider)
		}
		providers, err := settings.AppendProvider(nil)
		if err != nil || len(providers) != 0 {
			t.Fatalf("AppendProvider() = (%#v, %v), want empty slice", providers, err)
		}
	})

	t.Run("checkout only", func(t *testing.T) {
		settings := completeTestStripeSettings()
		if err := settings.Configure(StripeCheckoutConfiguration("local", "http://localhost:5173")); err != nil {
			t.Fatalf("Configure() error = %v", err)
		}
		if settings.StripeSuccessURL == "" {
			t.Fatal("StripeSuccessURL is empty")
		}
		if settings.StripePortalReturnURL != "" {
			t.Fatalf("StripePortalReturnURL = %q, want empty", settings.StripePortalReturnURL)
		}
		if _, err := settings.NewProvider(); err != nil {
			t.Fatalf("NewProvider() error = %v", err)
		}
		providers, err := settings.AppendProvider(nil)
		if err != nil || len(providers) != 1 || providers[0].GetProviderName() != "stripe" {
			t.Fatalf("AppendProvider() = (%#v, %v), want one Stripe provider", providers, err)
		}
	})

	t.Run("webhook only", func(t *testing.T) {
		settings := completeLiveStripeSettings()
		if err := settings.Configure(StripeConfiguration{Environment: "production", FrontendBaseURL: "://unused"}); err != nil {
			t.Fatalf("Configure() error = %v", err)
		}
		if _, err := settings.NewProvider(); err != nil {
			t.Fatalf("NewProvider() error = %v", err)
		}
	})
}

// TestStripeSettingsAppendProviderPreservesSliceOnError verifies a failed append leaves existing providers unchanged.
func TestStripeSettingsAppendProviderPreservesSliceOnError(t *testing.T) {
	kofi, err := paymentprovider.NewKofiProvider(&paymentprovider.Config{
		ProviderName:  "kofi",
		WebhookSecret: "webhook-secret",
	})
	if err != nil {
		t.Fatalf("NewKofiProvider() error = %v", err)
	}
	providers := []paymentprovider.Provider{kofi}
	settings := completeTestStripeSettings()
	if err := settings.Configure(localDefaultStripeConfiguration()); err != nil {
		t.Fatalf("Configure() error = %v", err)
	}
	settings.StripeSuccessURL = "http://untrusted.example/app/plan"

	got, err := settings.AppendProvider(providers)
	if !errors.Is(err, ErrStripeReturnURLOriginMismatch) {
		t.Fatalf("AppendProvider() error = %v, want %v", err, ErrStripeReturnURLOriginMismatch)
	}
	if len(got) != 1 || got[0] != kofi {
		t.Fatalf("AppendProvider() providers = %#v, want original slice", got)
	}
}

// TestStripeSettingsRequireCentralConfiguration verifies provider creation requires configured settings.
func TestStripeSettingsRequireCentralConfiguration(t *testing.T) {
	settings := completeTestStripeSettings()
	if _, err := settings.NewProvider(); !errors.Is(err, ErrStripeSettingsNotConfigured) {
		t.Fatalf("NewProvider() error = %v, want %v", err, ErrStripeSettingsNotConfigured)
	}

	var nilSettings *StripeSettings
	if err := nilSettings.Configure(StripeConfiguration{}); !errors.Is(err, ErrStripeSettingsRequired) {
		t.Fatalf("Configure() error = %v, want %v", err, ErrStripeSettingsRequired)
	}
	if _, err := nilSettings.NewProvider(); !errors.Is(err, ErrStripeSettingsRequired) {
		t.Fatalf("NewProvider() error = %v, want %v", err, ErrStripeSettingsRequired)
	}
}

// TestStripeSettingsNewProviderRevalidatesExportedFields verifies callers cannot bypass validation by mutating settings.
func TestStripeSettingsNewProviderRevalidatesExportedFields(t *testing.T) {
	settings := completeTestStripeSettings()
	if err := settings.Configure(localDefaultStripeConfiguration()); err != nil {
		t.Fatalf("Configure() error = %v", err)
	}
	settings.StripeSuccessURL = "http://untrusted.example/app/plan"

	if _, err := settings.NewProvider(); !errors.Is(err, ErrStripeReturnURLOriginMismatch) {
		t.Fatalf("NewProvider() error = %v, want %v", err, ErrStripeReturnURLOriginMismatch)
	}
}

// TestStripeSettingsValidation verifies unsafe and incomplete Stripe configurations are rejected.
func TestStripeSettingsValidation(t *testing.T) {
	tests := []struct {
		name      string
		settings  StripeSettings
		configure StripeConfiguration
		wantError error
		wantText  string
	}{
		{
			name: "partial credentials", settings: StripeSettings{StripeAPIKey: "sk_test_example"},
			configure: localDefaultStripeConfiguration(), wantError: ErrStripeCredentialsIncomplete, wantText: "stripe_publishable_key",
		},
		{
			name: "portal configuration without credentials", settings: StripeSettings{StripePortalConfigurationID: "bpc_example"},
			configure: localDefaultStripeConfiguration(), wantError: ErrStripeCredentialsIncomplete, wantText: "stripe_portal_configuration_id",
		},
		{
			name: "missing environment", settings: completeTestStripeSettings(),
			configure: StripeConfiguration{FrontendBaseURL: "http://localhost:5173", CheckoutReturnPath: StripeDefaultCheckoutReturnPath},
			wantError: ErrStripeEnvironmentRequired, wantText: "environment must be set",
		},
		{
			name: "invalid secret key mode", settings: withStripeAPIKey(completeTestStripeSettings(), "secret_example"),
			configure: localDefaultStripeConfiguration(), wantError: ErrStripeKeyModeInvalid, wantText: "stripe_api_key",
		},
		{
			name: "invalid publishable key mode", settings: withStripePublishableKey(completeTestStripeSettings(), "public_example"),
			configure: localDefaultStripeConfiguration(), wantError: ErrStripeKeyModeInvalid, wantText: "stripe_publishable_key",
		},
		{
			name: "mismatched key modes", settings: withStripePublishableKey(completeTestStripeSettings(), "pk_live_example"),
			configure: localDefaultStripeConfiguration(), wantError: ErrStripeKeyModeMismatch, wantText: "modes must match",
		},
		{
			name: "untrusted checkout origin", settings: withStripeSuccessURL(completeTestStripeSettings(), "http://example.test/app/plan"),
			configure: localDefaultStripeConfiguration(), wantError: ErrStripeReturnURLOriginMismatch, wantText: "frontend_base_url origin",
		},
		{
			name: "insecure production return", settings: completeLiveStripeSettings(),
			configure: DefaultStripeConfiguration("production", "http://app.example.test"), wantError: ErrStripeHTTPSRequired, wantText: "must use https",
		},
		{
			name: "unsafe production API base", settings: withStripeAPIBaseURL(completeLiveStripeSettings(), "https://stripe-proxy.example"),
			configure: DefaultStripeConfiguration("production", "https://app.example.test"), wantError: ErrStripeAPIBaseURLUnsafe, wantText: paymentprovider.StripeDefaultAPIBaseURL,
		},
		{
			name: "invalid checkout URL", settings: withStripeSuccessURL(completeTestStripeSettings(), "https:app/plan"),
			configure: localDefaultStripeConfiguration(), wantError: ErrStripeURLInvalid, wantText: "stripe_success_url",
		},
		{
			name: "invalid API version", settings: withStripeAPIVersion(completeTestStripeSettings(), "invalid/version"),
			configure: localDefaultStripeConfiguration(), wantError: ErrStripeProviderConfigurationInvalid, wantText: "provider configuration",
		},
		{
			name: "invalid portal configuration ID", settings: withStripePortalConfigurationID(completeTestStripeSettings(), "portal_example"),
			configure: localDefaultStripeConfiguration(), wantError: ErrStripeProviderConfigurationInvalid, wantText: "customer portal",
		},
		{
			name: "invalid default return path", settings: completeTestStripeSettings(),
			configure: StripeConfiguration{Environment: "local", FrontendBaseURL: "http://localhost:5173", CheckoutReturnPath: "app/plan"},
			wantError: ErrStripeURLInvalid, wantText: "single slash",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			settings := test.settings
			err := settings.Configure(test.configure)
			if !errors.Is(err, test.wantError) || !strings.Contains(err.Error(), test.wantText) {
				t.Fatalf("Configure() error = %v, want %v containing %q", err, test.wantError, test.wantText)
			}
			if settings.configured {
				t.Fatal("failed Configure() left settings configured")
			}
		})
	}
}

// TestStripeSettingsAcceptCanonicalEquivalentOriginsAndRestrictedKeys verifies equivalent origins and restricted keys are valid.
func TestStripeSettingsAcceptCanonicalEquivalentOriginsAndRestrictedKeys(t *testing.T) {
	settings := completeTestStripeSettings()
	settings.StripeAPIKey = "rk_test_example"
	settings.StripeSuccessURL = "http://LOCALHOST:080/app/plan?session_id={CHECKOUT_SESSION_ID}"
	settings.StripePortalReturnURL = "http://localhost/settings#billing"
	if err := settings.Configure(StripeConfiguration{
		Environment:     "development",
		FrontendBaseURL: "http://localhost:80",
	}); err != nil {
		t.Fatalf("Configure() error = %v", err)
	}
}

// TestStripeSettingsErrorsDoNotExposeCredentialValues verifies validation errors never disclose credentials.
func TestStripeSettingsErrorsDoNotExposeCredentialValues(t *testing.T) {
	const credential = "private-credential-value"
	settings := completeTestStripeSettings()
	settings.StripeAPIKey = credential
	err := settings.Configure(localDefaultStripeConfiguration())
	if err == nil {
		t.Fatal("Configure() error = nil")
	}
	if strings.Contains(err.Error(), credential) {
		t.Fatalf("Configure() exposed credential in error: %v", err)
	}
}

// completeTestStripeSettings returns complete Stripe settings for test mode.
func completeTestStripeSettings() StripeSettings {
	return StripeSettings{
		StripeAPIKey:         "sk_test_example",
		StripePublishableKey: "pk_test_example",
		StripeWebhookSecret:  "whsec_example",
	}
}

// completeLiveStripeSettings returns complete Stripe settings for live mode.
func completeLiveStripeSettings() StripeSettings {
	return StripeSettings{
		StripeAPIKey:         "sk_live_example",
		StripePublishableKey: "pk_live_example",
		StripeWebhookSecret:  "whsec_example",
	}
}

// localDefaultStripeConfiguration returns the default local Stripe configuration.
func localDefaultStripeConfiguration() StripeConfiguration {
	return DefaultStripeConfiguration("local", "http://localhost:5173")
}

// withStripeAPIKey returns test settings with the secret API key replaced.
func withStripeAPIKey(settings StripeSettings, value string) StripeSettings {
	settings.StripeAPIKey = value
	return settings
}

// withStripePublishableKey returns test settings with the publishable key replaced.
func withStripePublishableKey(settings StripeSettings, value string) StripeSettings {
	settings.StripePublishableKey = value
	return settings
}

// withStripeSuccessURL returns test settings with the checkout success URL replaced.
func withStripeSuccessURL(settings StripeSettings, value string) StripeSettings {
	settings.StripeSuccessURL = value
	return settings
}

// withStripeAPIBaseURL returns test settings with the API base URL replaced.
func withStripeAPIBaseURL(settings StripeSettings, value string) StripeSettings {
	settings.StripeAPIBaseURL = value
	return settings
}

// withStripeAPIVersion returns test settings with the API version replaced.
func withStripeAPIVersion(settings StripeSettings, value string) StripeSettings {
	settings.StripeAPIVersion = value
	return settings
}

// withStripePortalConfigurationID returns test settings with the portal configuration ID replaced.
func withStripePortalConfigurationID(settings StripeSettings, value string) StripeSettings {
	settings.StripePortalConfigurationID = value
	return settings
}

// clearStripeEnvironment removes Stripe variables for a test and restores their previous values afterwards.
func clearStripeEnvironment(t *testing.T) {
	t.Helper()
	for _, name := range []string{
		"STRIPE_API_KEY",
		"STRIPE_PUBLISHABLE_KEY",
		"STRIPE_WEBHOOK_SECRET",
		"STRIPE_API_BASE_URL",
		"STRIPE_API_VERSION",
		"STRIPE_SUCCESS_URL",
		"STRIPE_PORTAL_RETURN_URL",
		"STRIPE_PORTAL_CONFIGURATION_ID",
	} {
		value, present := os.LookupEnv(name)
		if err := os.Unsetenv(name); err != nil {
			t.Fatalf("unset %s: %v", name, err)
		}
		t.Cleanup(func() {
			if present {
				_ = os.Setenv(name, value)
				return
			}
			_ = os.Unsetenv(name)
		})
	}
}
