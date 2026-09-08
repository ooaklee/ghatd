package helpers

import "errors"

const (
	// ErrKeyStripeSettingsRequired identifies a missing Stripe settings value.
	ErrKeyStripeSettingsRequired = "PaymentProviderHelperStripeSettingsRequired"
	// ErrKeyStripeSettingsNotConfigured identifies provider construction before settings validation.
	ErrKeyStripeSettingsNotConfigured = "PaymentProviderHelperStripeSettingsNotConfigured"
	// ErrKeyStripeEnvironmentRequired identifies a missing host environment policy.
	ErrKeyStripeEnvironmentRequired = "PaymentProviderHelperStripeEnvironmentRequired"
	// ErrKeyStripeCredentialsIncomplete identifies partially configured Stripe credentials.
	ErrKeyStripeCredentialsIncomplete = "PaymentProviderHelperStripeCredentialsIncomplete"
	// ErrKeyStripeKeyModeInvalid identifies an unrecognised Stripe key prefix.
	ErrKeyStripeKeyModeInvalid = "PaymentProviderHelperStripeKeyModeInvalid"
	// ErrKeyStripeKeyModeMismatch identifies secret and publishable keys from different modes.
	ErrKeyStripeKeyModeMismatch = "PaymentProviderHelperStripeKeyModeMismatch"
	// ErrKeyStripeURLInvalid identifies malformed Stripe integration URLs.
	ErrKeyStripeURLInvalid = "PaymentProviderHelperStripeURLInvalid"
	// ErrKeyStripeReturnURLOriginMismatch identifies a return URL outside the trusted frontend origin.
	ErrKeyStripeReturnURLOriginMismatch = "PaymentProviderHelperStripeReturnURLOriginMismatch"
	// ErrKeyStripeHTTPSRequired identifies an insecure return URL outside development.
	ErrKeyStripeHTTPSRequired = "PaymentProviderHelperStripeHTTPSRequired"
	// ErrKeyStripeAPIBaseURLUnsafe identifies a non-Stripe API endpoint outside development.
	ErrKeyStripeAPIBaseURLUnsafe = "PaymentProviderHelperStripeAPIBaseURLUnsafe"
	// ErrKeyStripeProviderConfigurationInvalid identifies provider-specific validation failures.
	ErrKeyStripeProviderConfigurationInvalid = "PaymentProviderHelperStripeProviderConfigurationInvalid"
)

var (
	// ErrStripeSettingsRequired is returned when Stripe settings are nil.
	ErrStripeSettingsRequired = errors.New(ErrKeyStripeSettingsRequired)
	// ErrStripeSettingsNotConfigured is returned when NewProvider is called before Configure.
	ErrStripeSettingsNotConfigured = errors.New(ErrKeyStripeSettingsNotConfigured)
	// ErrStripeEnvironmentRequired is returned when enabled Stripe settings omit the host environment.
	ErrStripeEnvironmentRequired = errors.New(ErrKeyStripeEnvironmentRequired)
	// ErrStripeCredentialsIncomplete is returned when only some Stripe credentials are set.
	ErrStripeCredentialsIncomplete = errors.New(ErrKeyStripeCredentialsIncomplete)
	// ErrStripeKeyModeInvalid is returned when a Stripe key has an unrecognised mode prefix.
	ErrStripeKeyModeInvalid = errors.New(ErrKeyStripeKeyModeInvalid)
	// ErrStripeKeyModeMismatch is returned when Stripe secret and publishable key modes differ.
	ErrStripeKeyModeMismatch = errors.New(ErrKeyStripeKeyModeMismatch)
	// ErrStripeURLInvalid is returned when an integration URL is not an absolute HTTP(S) URL.
	ErrStripeURLInvalid = errors.New(ErrKeyStripeURLInvalid)
	// ErrStripeReturnURLOriginMismatch is returned when a return URL is outside the frontend origin.
	ErrStripeReturnURLOriginMismatch = errors.New(ErrKeyStripeReturnURLOriginMismatch)
	// ErrStripeHTTPSRequired is returned for an insecure production return URL.
	ErrStripeHTTPSRequired = errors.New(ErrKeyStripeHTTPSRequired)
	// ErrStripeAPIBaseURLUnsafe is returned for a non-Stripe production API endpoint.
	ErrStripeAPIBaseURLUnsafe = errors.New(ErrKeyStripeAPIBaseURLUnsafe)
	// ErrStripeProviderConfigurationInvalid is returned when Stripe rejects the resolved configuration.
	ErrStripeProviderConfigurationInvalid = errors.New(ErrKeyStripeProviderConfigurationInvalid)
)
