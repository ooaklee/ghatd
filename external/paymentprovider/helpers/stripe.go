// Package helpers centralises safe host-application configuration for payment providers.
package helpers

import (
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"

	"github.com/ooaklee/ghatd/external/paymentprovider"
)

const (
	// StripeDefaultCheckoutReturnPath is the standard post-checkout application route.
	StripeDefaultCheckoutReturnPath = "/app/plan?stripe=success&session_id={CHECKOUT_SESSION_ID}"
	// StripeDefaultCustomerPortalReturnPath is the standard billing settings route.
	StripeDefaultCustomerPortalReturnPath = "/settings#billing"
)

// StripeSettings is an embeddable envconfig-compatible Stripe settings block.
// Hosts retain promoted field access while sharing defaults, validation, and
// provider construction.
type StripeSettings struct {
	StripeAPIKey                string `envconfig:"stripe_api_key"`
	StripePublishableKey        string `envconfig:"stripe_publishable_key"`
	StripeWebhookSecret         string `envconfig:"stripe_webhook_secret"`
	StripeAPIBaseURL            string `envconfig:"stripe_api_base_url" default:"https://api.stripe.com"`
	StripeAPIVersion            string `envconfig:"stripe_api_version"`
	StripeSuccessURL            string `envconfig:"stripe_success_url"`
	StripePortalReturnURL       string `envconfig:"stripe_portal_return_url"`
	StripePortalConfigurationID string `envconfig:"stripe_portal_configuration_id"`

	configured    bool
	environment   string
	configuration StripeConfiguration
}

// StripeConfiguration supplies the host context and optional default routes
// needed to validate StripeSettings. Empty return paths leave that browser
// capability disabled unless its URL is explicitly configured.
type StripeConfiguration struct {
	Environment              string
	FrontendBaseURL          string
	CheckoutReturnPath       string
	CustomerPortalReturnPath string
}

// DefaultStripeConfiguration enables the standard checkout and customer portal routes.
func DefaultStripeConfiguration(environment, frontendBaseURL string) StripeConfiguration {
	return StripeConfiguration{
		Environment:              environment,
		FrontendBaseURL:          frontendBaseURL,
		CheckoutReturnPath:       StripeDefaultCheckoutReturnPath,
		CustomerPortalReturnPath: StripeDefaultCustomerPortalReturnPath,
	}
}

// StripeCheckoutConfiguration enables the standard checkout route without the customer portal.
func StripeCheckoutConfiguration(environment, frontendBaseURL string) StripeConfiguration {
	return StripeConfiguration{
		Environment:        environment,
		FrontendBaseURL:    frontendBaseURL,
		CheckoutReturnPath: StripeDefaultCheckoutReturnPath,
	}
}

// Configure applies shared defaults and validates Stripe settings against the
// host's trusted frontend origin and environment policy.
func (s *StripeSettings) Configure(configuration StripeConfiguration) error {
	return s.configure(configuration, true)
}

func (s *StripeSettings) configure(configuration StripeConfiguration, validateProvider bool) error {
	if s == nil {
		return ErrStripeSettingsRequired
	}
	s.configured = false
	s.environment = ""
	s.configuration = StripeConfiguration{}
	s.trimValues()
	environment := strings.TrimSpace(configuration.Environment)

	if s.StripeAPIBaseURL == "" {
		s.StripeAPIBaseURL = paymentprovider.StripeDefaultAPIBaseURL
	}
	if s.StripeAPIVersion == "" {
		s.StripeAPIVersion = paymentprovider.StripeDefaultAPIVersion
	}
	if s.StripeSuccessURL == "" && strings.TrimSpace(configuration.CheckoutReturnPath) != "" {
		resolved, err := resolveFrontendPath(configuration.FrontendBaseURL, configuration.CheckoutReturnPath)
		if err != nil {
			return err
		}
		s.StripeSuccessURL = resolved
	}
	if s.StripePortalReturnURL == "" && strings.TrimSpace(configuration.CustomerPortalReturnPath) != "" {
		resolved, err := resolveFrontendPath(configuration.FrontendBaseURL, configuration.CustomerPortalReturnPath)
		if err != nil {
			return err
		}
		s.StripePortalReturnURL = resolved
	}

	credentialsPresent := s.StripeAPIKey != "" || s.StripePublishableKey != "" || s.StripeWebhookSecret != ""
	credentialsComplete := s.StripeAPIKey != "" && s.StripePublishableKey != "" && s.StripeWebhookSecret != ""
	if s.StripePortalConfigurationID != "" && !credentialsComplete {
		return stripeHelperError(ErrStripeCredentialsIncomplete, "stripe_portal_configuration_id requires stripe_api_key, stripe_publishable_key, and stripe_webhook_secret")
	}
	if !credentialsPresent {
		s.environment = environment
		s.configuration = configuration
		s.configured = true
		return nil
	}
	if !credentialsComplete {
		for _, field := range []struct {
			name  string
			value string
		}{
			{name: "stripe_api_key", value: s.StripeAPIKey},
			{name: "stripe_publishable_key", value: s.StripePublishableKey},
			{name: "stripe_webhook_secret", value: s.StripeWebhookSecret},
		} {
			if field.value == "" {
				return stripeHelperError(ErrStripeCredentialsIncomplete, "%s must be set when Stripe is configured", field.name)
			}
		}
	}
	if environment == "" {
		return stripeHelperError(ErrStripeEnvironmentRequired, "environment must be set when Stripe is configured")
	}

	if s.StripeSuccessURL != "" || s.StripePortalReturnURL != "" {
		frontendURL, err := parseAbsoluteHTTPURL(configuration.FrontendBaseURL)
		if err != nil {
			return stripeHelperError(ErrStripeURLInvalid, "frontend_base_url must be an absolute HTTP(S) URL when Stripe is configured")
		}
		if err := validateReturnURL("stripe_success_url", s.StripeSuccessURL, frontendURL, environment); err != nil {
			return err
		}
		if err := validateReturnURL("stripe_portal_return_url", s.StripePortalReturnURL, frontendURL, environment); err != nil {
			return err
		}
	}
	if err := validateStripeAPIBaseURL(s.StripeAPIBaseURL, environment); err != nil {
		return err
	}

	apiKeyMode := stripeAPIKeyMode(s.StripeAPIKey)
	if apiKeyMode == "" {
		return stripeHelperError(ErrStripeKeyModeInvalid, "stripe_api_key must use a recognized Stripe test or live mode prefix")
	}
	publishableKeyMode := stripePublishableKeyMode(s.StripePublishableKey)
	if publishableKeyMode == "" {
		return stripeHelperError(ErrStripeKeyModeInvalid, "stripe_publishable_key must use a recognized Stripe test or live mode prefix")
	}
	if apiKeyMode != publishableKeyMode {
		return stripeHelperError(ErrStripeKeyModeMismatch, "stripe_api_key and stripe_publishable_key modes must match")
	}

	if validateProvider {
		provider, err := s.newProvider(environment)
		if err != nil {
			return stripeHelperError(ErrStripeProviderConfigurationInvalid, "Stripe provider configuration is invalid: %v", err)
		}
		if err := paymentprovider.ValidateCheckoutProviderConfig(provider); err != nil {
			return stripeHelperError(ErrStripeProviderConfigurationInvalid, "Stripe checkout configuration is invalid: %v", err)
		}
		if err := paymentprovider.ValidateCustomerPortalProviderConfig(provider); err != nil {
			return stripeHelperError(ErrStripeProviderConfigurationInvalid, "Stripe customer portal configuration is invalid: %v", err)
		}
	}

	s.environment = environment
	s.configuration = configuration
	s.configured = true
	return nil
}

// IsEnabled reports whether complete Stripe credentials are configured.
func (s *StripeSettings) IsEnabled() bool {
	return s != nil && s.configured && s.StripeAPIKey != "" && s.StripePublishableKey != "" && s.StripeWebhookSecret != ""
}

// NewProvider constructs a validated Stripe provider. Configure must be called
// first so provider creation cannot bypass the shared host-safety policy. A
// fully configured but disabled Stripe integration returns nil without error.
func (s *StripeSettings) NewProvider() (*paymentprovider.StripeProvider, error) {
	if s == nil {
		return nil, ErrStripeSettingsRequired
	}
	if !s.configured {
		return nil, ErrStripeSettingsNotConfigured
	}
	validated := *s
	if err := validated.configure(s.configuration, false); err != nil {
		return nil, err
	}
	if !validated.IsEnabled() {
		return nil, nil
	}
	provider, err := validated.newProvider(validated.environment)
	if err != nil {
		return nil, stripeHelperError(ErrStripeProviderConfigurationInvalid, "Stripe provider configuration is invalid: %v", err)
	}
	if err := paymentprovider.ValidateCheckoutProviderConfig(provider); err != nil {
		return nil, stripeHelperError(ErrStripeProviderConfigurationInvalid, "Stripe checkout configuration is invalid: %v", err)
	}
	if err := paymentprovider.ValidateCustomerPortalProviderConfig(provider); err != nil {
		return nil, stripeHelperError(ErrStripeProviderConfigurationInvalid, "Stripe customer portal configuration is invalid: %v", err)
	}
	return provider, nil
}

// AppendProvider constructs Stripe when enabled and appends it to the provider
// slice passed to Starter. Disabled Stripe settings leave the slice unchanged.
func (s *StripeSettings) AppendProvider(providers []paymentprovider.Provider) ([]paymentprovider.Provider, error) {
	provider, err := s.NewProvider()
	if err != nil {
		return providers, err
	}
	if provider == nil {
		return providers, nil
	}
	return append(providers, provider), nil
}

func (s *StripeSettings) newProvider(environment string) (*paymentprovider.StripeProvider, error) {
	return paymentprovider.NewStripeProvider(&paymentprovider.Config{
		ProviderName:                  "stripe",
		Environment:                   environment,
		APIKey:                        s.StripeAPIKey,
		PublishableKey:                s.StripePublishableKey,
		WebhookSecret:                 s.StripeWebhookSecret,
		APIBaseURL:                    s.StripeAPIBaseURL,
		APIVersion:                    s.StripeAPIVersion,
		ReturnURL:                     s.StripeSuccessURL,
		CustomerPortalReturnURL:       s.StripePortalReturnURL,
		CustomerPortalConfigurationID: s.StripePortalConfigurationID,
	})
}

func (s *StripeSettings) trimValues() {
	s.StripeAPIKey = strings.TrimSpace(s.StripeAPIKey)
	s.StripePublishableKey = strings.TrimSpace(s.StripePublishableKey)
	s.StripeWebhookSecret = strings.TrimSpace(s.StripeWebhookSecret)
	s.StripeAPIBaseURL = strings.TrimSpace(s.StripeAPIBaseURL)
	s.StripeAPIVersion = strings.TrimSpace(s.StripeAPIVersion)
	s.StripeSuccessURL = strings.TrimSpace(s.StripeSuccessURL)
	s.StripePortalReturnURL = strings.TrimSpace(s.StripePortalReturnURL)
	s.StripePortalConfigurationID = strings.TrimSpace(s.StripePortalConfigurationID)
}

func resolveFrontendPath(frontendBaseURL, returnPath string) (string, error) {
	path := strings.TrimSpace(returnPath)
	if path == "" {
		return "", nil
	}
	if !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") {
		return "", stripeHelperError(ErrStripeURLInvalid, "Stripe return path must begin with a single slash")
	}
	return strings.TrimRight(strings.TrimSpace(frontendBaseURL), "/") + path, nil
}

func validateReturnURL(field, value string, frontendURL *url.URL, environment string) error {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	returnURL, err := parseAbsoluteHTTPURL(value)
	if err != nil {
		return stripeHelperError(ErrStripeURLInvalid, "%s must be an absolute HTTP(S) URL", field)
	}
	if canonicalHTTPOrigin(frontendURL) != canonicalHTTPOrigin(returnURL) {
		return stripeHelperError(ErrStripeReturnURLOriginMismatch, "%s must use the frontend_base_url origin", field)
	}
	if !isLocalEnvironment(environment) && returnURL.Scheme != "https" {
		return stripeHelperError(ErrStripeHTTPSRequired, "%s must use https outside local, test, or development", field)
	}
	return nil
}

func validateStripeAPIBaseURL(value, environment string) error {
	apiBaseURL, err := parseAbsoluteHTTPURL(value)
	if err != nil {
		return stripeHelperError(ErrStripeURLInvalid, "stripe_api_base_url must be an absolute HTTP(S) URL")
	}
	if isLocalEnvironment(environment) {
		return nil
	}
	if canonicalHTTPOrigin(apiBaseURL) != paymentprovider.StripeDefaultAPIBaseURL ||
		(apiBaseURL.EscapedPath() != "" && apiBaseURL.EscapedPath() != "/") ||
		apiBaseURL.RawQuery != "" || apiBaseURL.ForceQuery || apiBaseURL.Fragment != "" {
		return stripeHelperError(ErrStripeAPIBaseURLUnsafe, "stripe_api_base_url must use %s outside local, test, or development", paymentprovider.StripeDefaultAPIBaseURL)
	}
	return nil
}

func stripeAPIKeyMode(value string) string {
	switch {
	case strings.HasPrefix(value, "sk_test_"), strings.HasPrefix(value, "rk_test_"):
		return "test"
	case strings.HasPrefix(value, "sk_live_"), strings.HasPrefix(value, "rk_live_"):
		return "live"
	default:
		return ""
	}
}

func stripePublishableKeyMode(value string) string {
	switch {
	case strings.HasPrefix(value, "pk_test_"):
		return "test"
	case strings.HasPrefix(value, "pk_live_"):
		return "live"
	default:
		return ""
	}
}

func parseAbsoluteHTTPURL(value string) (*url.URL, error) {
	parsed, err := url.Parse(strings.ReplaceAll(strings.TrimSpace(value), "{CHECKOUT_SESSION_ID}", "session"))
	if err != nil || !parsed.IsAbs() || parsed.Host == "" || parsed.Hostname() == "" || parsed.User != nil || parsed.Opaque != "" {
		return nil, fmt.Errorf("invalid absolute HTTP(S) URL")
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("invalid absolute HTTP(S) URL")
	}
	if port := parsed.Port(); port != "" {
		portNumber, portErr := strconv.Atoi(port)
		if portErr != nil || portNumber < 1 || portNumber > 65535 {
			return nil, fmt.Errorf("invalid absolute HTTP(S) URL")
		}
	}
	return parsed, nil
}

func canonicalHTTPOrigin(parsed *url.URL) string {
	hostname := strings.ToLower(parsed.Hostname())
	port := parsed.Port()
	if port != "" {
		portNumber, _ := strconv.Atoi(port)
		port = strconv.Itoa(portNumber)
	}
	if (parsed.Scheme == "http" && port == "80") || (parsed.Scheme == "https" && port == "443") {
		port = ""
	}
	host := hostname
	if port != "" {
		host = net.JoinHostPort(hostname, port)
	} else if strings.Contains(hostname, ":") {
		host = "[" + hostname + "]"
	}
	return parsed.Scheme + "://" + host
}

func isLocalEnvironment(environment string) bool {
	switch strings.ToLower(strings.TrimSpace(environment)) {
	case "local", "dev", "test", "development":
		return true
	default:
		return false
	}
}

func stripeHelperError(kind error, format string, args ...interface{}) error {
	return fmt.Errorf("%w: %s", kind, fmt.Sprintf(format, args...))
}
