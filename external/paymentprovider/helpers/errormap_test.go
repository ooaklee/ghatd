package helpers

import (
	"testing"

	"github.com/ooaklee/ghatd/external/paymentprovider"
)

func TestPaymentProviderHelperErrorMapIsCompleteAndCollisionFree(t *testing.T) {
	errors := []error{
		ErrStripeSettingsRequired,
		ErrStripeSettingsNotConfigured,
		ErrStripeEnvironmentRequired,
		ErrStripeCredentialsIncomplete,
		ErrStripeKeyModeInvalid,
		ErrStripeKeyModeMismatch,
		ErrStripeURLInvalid,
		ErrStripeReturnURLOriginMismatch,
		ErrStripeHTTPSRequired,
		ErrStripeAPIBaseURLUnsafe,
		ErrStripeProviderConfigurationInvalid,
	}
	baseCodes := make(map[string]struct{}, len(paymentprovider.PaymentProviderErrorMap))
	for _, item := range paymentprovider.PaymentProviderErrorMap {
		baseCodes[item.Code] = struct{}{}
	}
	seenCodes := make(map[string]struct{}, len(errors))
	for _, err := range errors {
		item, ok := PaymentProviderHelperErrorMap[err]
		if !ok {
			t.Errorf("PaymentProviderHelperErrorMap is missing %v", err)
			continue
		}
		if item.StatusCode != 500 {
			t.Errorf("PaymentProviderHelperErrorMap[%v].StatusCode = %d, want 500", err, item.StatusCode)
		}
		if item.Code == "" {
			t.Errorf("PaymentProviderHelperErrorMap[%v].Code is empty", err)
			continue
		}
		if _, exists := seenCodes[item.Code]; exists {
			t.Errorf("duplicate helper error code %q", item.Code)
		}
		if _, exists := baseCodes[item.Code]; exists {
			t.Errorf("helper error code %q collides with PaymentProviderErrorMap", item.Code)
		}
		seenCodes[item.Code] = struct{}{}
	}
	if len(PaymentProviderHelperErrorMap) != len(errors) {
		t.Fatalf("PaymentProviderHelperErrorMap has %d entries, want %d", len(PaymentProviderHelperErrorMap), len(errors))
	}
}
