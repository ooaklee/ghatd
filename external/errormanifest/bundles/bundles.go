package bundles

import (
	"github.com/ooaklee/ghatd/external/accessmanager"
	"github.com/ooaklee/ghatd/external/apitoken"
	"github.com/ooaklee/ghatd/external/auth"
	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/billingmanager"
	"github.com/ooaklee/ghatd/external/contacter"
	"github.com/ooaklee/ghatd/external/ephemeral"
	"github.com/ooaklee/ghatd/external/group"
	"github.com/ooaklee/ghatd/external/notifier"
	"github.com/ooaklee/ghatd/external/paymentprovider"
	"github.com/ooaklee/ghatd/external/post"
	"github.com/ooaklee/ghatd/external/pricer"
	"github.com/ooaklee/ghatd/external/reminder"
	"github.com/ooaklee/ghatd/external/toolbox"
	user "github.com/ooaklee/ghatd/external/user/v2"
	"github.com/ooaklee/ghatd/external/vision"
	"github.com/ooaklee/reply/v2"
)

// AccessManager returns the standard cross-package error maps used by the
// accessmanager handler. The handler adds AccessmanagerErrorMap itself.
func AccessManager() []reply.ErrorManifest {
	return cloneBundle([]reply.ErrorManifest{
		user.UserErrorMap,
		auth.AuthErrorMap,
		apitoken.ApitokenErrorMap,
		toolbox.ToolboxErrorMap,
		group.GroupErrorMap,
		billingmanager.BillingManagerErrorMap,
		paymentprovider.PaymentProviderErrorMap,
		billing.BillingErrorMap,
	})
}

// UserManager returns the standard cross-package error maps used by the
// usermanager handler. The handler adds UsermanagerErrorMap itself.
func UserManager() []reply.ErrorManifest {
	return cloneBundle([]reply.ErrorManifest{
		user.UserErrorMap,
		contacter.ContacterErrorMap,
		toolbox.ToolboxErrorMap,
		group.GroupErrorMap,
		notifier.NotifierErrorMap,
		reminder.ReminderErrorMap,
		vision.VisionErrorMap,
	})
}

// ContentManager returns the standard cross-package error maps used by the
// contentmanager handler. The handler adds ContentManagerErrorMap itself.
func ContentManager() []reply.ErrorManifest {
	return cloneBundle([]reply.ErrorManifest{
		post.PostErrorMap,
		toolbox.ToolboxErrorMap,
		user.UserErrorMap,
	})
}

// BillingManager returns the standard cross-package error maps used by the
// billingmanager handler. The handler adds BillingManagerErrorMap itself.
func BillingManager() []reply.ErrorManifest {
	return cloneBundle([]reply.ErrorManifest{
		pricer.PricerErrorMap,
		paymentprovider.PaymentProviderErrorMap,
		billing.BillingErrorMap,
		toolbox.ToolboxErrorMap,
		user.UserErrorMap,
	})
}

// AuthMiddleware returns the standard error maps used by accessmanager
// authentication and rate-limit middleware.
func AuthMiddleware() []reply.ErrorManifest {
	return cloneBundle([]reply.ErrorManifest{
		user.UserErrorMap,
		toolbox.ToolboxErrorMap,
		auth.AuthErrorMap,
		apitoken.ApitokenErrorMap,
		ephemeral.EphemeralStoreErrorMap,
		accessmanager.AccessmanagerErrorMap,
		contacter.ContacterErrorMap,
		notifier.NotifierErrorMap,
		post.PostErrorMap,
		billingmanager.BillingManagerErrorMap,
		paymentprovider.PaymentProviderErrorMap,
		billing.BillingErrorMap,
	})
}

// cloneBundle copies a bundle of error manifests so callers cannot mutate shared manifests.
func cloneBundle(bundle []reply.ErrorManifest) []reply.ErrorManifest {
	cloned := make([]reply.ErrorManifest, len(bundle))
	for i, manifest := range bundle {
		if manifest == nil {
			continue
		}

		clonedManifest := make(reply.ErrorManifest, len(manifest))
		for err, item := range manifest {
			clonedManifest[err] = item
		}
		cloned[i] = clonedManifest
	}

	return cloned
}
