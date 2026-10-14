package billinglifecycle

import (
	"testing"

	"github.com/ooaklee/ghatd/external/paymentprovider"
	"github.com/stretchr/testify/require"
)

// Hide only the optional status interface; all old checkout evidence methods
// remain the same real authenticated HTTP adapter.
type checkoutLegacyHTTPProvider struct {
	paymentprovider.CheckoutProvider
	paymentprovider.RevenueCheckoutProvider
	paymentprovider.RevenueCheckoutSessionEvidenceProvider
}

type checkoutScopedHTTPProvider struct {
	*paymentprovider.StripeProvider
	account string
}

func (p checkoutScopedHTTPProvider) SupportsCheckoutStatus(s paymentprovider.RevenueScope) bool {
	return s.AccountID == p.account
}

func TestCheckoutDispositionNativeScopedCapability(t *testing.T) {
	for _, name := range []string{"supported_provider", "legacy_provider", "supported_scope", "unsupported_scope"} {
		t.Run(name, func(t *testing.T) {
			f := nativeCheckoutHTTPFixture(t, "complete", "paid", false, false)
			stripe := f.registry.provider.(*paymentprovider.StripeProvider)
			supported := name == "supported_provider" || name == "supported_scope"
			switch name {
			case "legacy_provider":
				f.registry.provider = checkoutLegacyHTTPProvider{stripe, stripe, stripe}
			case "supported_scope":
				f.registry.provider = checkoutScopedHTTPProvider{stripe, f.f.intent.Scope.AccountID}
			case "unsupported_scope":
				f.registry.provider = checkoutScopedHTTPProvider{stripe, "other_account"}
			}
			require.Equal(t, supported, f.f.native.SupportsCheckoutLifecycleStatus(f.f.intent.Scope))
			c, err := NewCheckoutCompletion(f.execution, f.repo)
			require.NoError(t, err)
			result, err := c.runDisposition(f.ctx, JobLease(f.job))
			require.NoError(t, err)
			require.False(t, result.Expired)
			require.False(t, result.Pending)
			require.Equal(t, RetiredLane, result.Job.Lane)
			want := int64(3)
			if supported {
				want = 6
			}
			require.Equal(t, want, f.calls.Load(), "legacy scoped providers retain original complete flow")
			recovered, err := c.InspectLast(f.ctx, f.job.Source)
			require.NoError(t, err)
			require.Nil(t, recovered.Expiry)
			require.Zero(t, f.writes.Load())
		})
	}
}
