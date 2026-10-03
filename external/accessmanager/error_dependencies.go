package accessmanager

import (
	"github.com/ooaklee/ghatd/external/apitoken"
	"github.com/ooaklee/ghatd/external/auth"
	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/billingmanager"
	"github.com/ooaklee/ghatd/external/errormanifest"
	"github.com/ooaklee/ghatd/external/group"
	"github.com/ooaklee/ghatd/external/paymentprovider"
	paymentproviderhelpers "github.com/ooaklee/ghatd/external/paymentprovider/helpers"
	"github.com/ooaklee/ghatd/external/toolbox"
	user "github.com/ooaklee/ghatd/external/user/v2"
	"github.com/ooaklee/reply/v2"
)

// DependencyErrorMaps supplies the lower-domain response contracts exposed by
// Access Manager, not authentication middleware policy. The handler includes
// these maps after its own map and before caller overrides. Its strict resolver
// classifies response errors, not credential authority. Maps are copied in legacy
// bundle order; referenced metadata remains immutable as documented by
// errormanifest.CloneManifests.
func DependencyErrorMaps() []reply.ErrorManifest {
	return errormanifest.CloneManifests(
		user.UserErrorMap, auth.AuthErrorMap, apitoken.ApitokenErrorMap,
		toolbox.ToolboxErrorMap, group.GroupErrorMap, billingmanager.BillingManagerErrorMap,
		paymentprovider.PaymentProviderErrorMap, paymentproviderhelpers.PaymentProviderHelperErrorMap,
		billing.BillingErrorMap,
	)
}
