package billingmanager

import (
	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/errormanifest"
	"github.com/ooaklee/ghatd/external/paymentprovider"
	paymentproviderhelpers "github.com/ooaklee/ghatd/external/paymentprovider/helpers"
	"github.com/ooaklee/ghatd/external/pricer"
	"github.com/ooaklee/ghatd/external/toolbox"
	user "github.com/ooaklee/ghatd/external/user/v2"
	"github.com/ooaklee/reply/v2"
)

// DependencyErrorMaps supplies the lower-domain response contracts exposed by
// Billing Manager. The handler includes them after its own map and before host
// overrides, without requiring host knowledge of its collaborators. Results
// match the legacy bundle order and copy each map; referenced metadata remains
// immutable as documented by errormanifest.CloneManifests.
func DependencyErrorMaps() []reply.ErrorManifest {
	return errormanifest.CloneManifests(
		pricer.PricerErrorMap, paymentprovider.PaymentProviderErrorMap,
		paymentproviderhelpers.PaymentProviderHelperErrorMap, billing.BillingErrorMap,
		toolbox.ToolboxErrorMap, user.UserErrorMap,
	)
}
