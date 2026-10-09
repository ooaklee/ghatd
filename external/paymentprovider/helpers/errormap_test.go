package helpers

import (
	"testing"

	"github.com/ooaklee/ghatd/external/paymentprovider"
)

// This single manifest-wide uniqueness audit accumulates codes across named
// cases; isolating the set per case would miss collisions between helper errors.
func TestPaymentProviderHelperErrorMapIsCompleteAndCollisionFree(t *testing.T) {
	errors := []error{
		ErrStripePaidServicePeriodConfigInvalid,
		ErrStripePaidServicePeriodInvalid,
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
		ErrStripeRetainedSnapshotInvalid,
	}
	baseCodes := make(map[string]struct{}, len(paymentprovider.PaymentProviderErrorMap))
	for _, item := range paymentprovider.PaymentProviderErrorMap {
		baseCodes[item.Code] = struct{}{}
	}
	seenCodes := make(map[string]struct{}, len(errors))
	for _, err := range errors {
		t.Run(err.Error(), func(t *testing.T) {
			item, ok := PaymentProviderHelperErrorMap[err]
			if !ok {
				t.Fatalf("PaymentProviderHelperErrorMap is missing %v", err)
			}
			wantStatus := 500
			if err == ErrStripeRetainedSnapshotInvalid || err == ErrStripePaidServicePeriodInvalid {
				wantStatus = 400
			}
			if item.StatusCode != wantStatus {
				t.Errorf("PaymentProviderHelperErrorMap[%v].StatusCode = %d, want %d", err, item.StatusCode, wantStatus)
			}
			if item.Code == "" {
				t.Fatal("helper error code is empty")
			}
			if _, exists := seenCodes[item.Code]; exists {
				t.Errorf("duplicate helper error code %q", item.Code)
			}
			if _, exists := baseCodes[item.Code]; exists {
				t.Errorf("helper error code %q collides with PaymentProviderErrorMap", item.Code)
			}
			seenCodes[item.Code] = struct{}{}
		})
	}

	if len(PaymentProviderHelperErrorMap) != len(errors) {
		t.Fatalf("PaymentProviderHelperErrorMap has %d entries, want %d", len(PaymentProviderHelperErrorMap), len(errors))
	}
}
