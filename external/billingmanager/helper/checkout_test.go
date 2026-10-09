package billingmanagerhelper

import (
	"context"
	"errors"
	"testing"

	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/billingmanager"
	"github.com/ooaklee/ghatd/external/paymentprovider"
	"github.com/stretchr/testify/require"
)

// Audit disposition: named fresh provider/registry fixtures cover exact scoped
// forwarding and no fallback, cancellation or partial-evidence disclosure.
type checkoutSessionRegistry struct {
	provider paymentprovider.CheckoutProvider
	err      error
	calls    int
	name     string
}

func (r *checkoutSessionRegistry) GetCheckoutProvider(name string) (paymentprovider.CheckoutProvider, error) {
	r.calls++
	r.name = name
	return r.provider, r.err
}

type checkoutSessionProvider struct {
	paymentprovider.CheckoutProvider
	calls   int
	scope   paymentprovider.RevenueScope
	session string
	err     error
	cancel  context.CancelFunc
}

func (p *checkoutSessionProvider) LookupRevenueCheckoutSessionEvidence(_ context.Context, scope paymentprovider.RevenueScope, session string) (paymentprovider.RevenueCheckoutEvidence, error) {
	p.calls++
	p.scope = scope
	p.session = session
	if p.cancel != nil {
		p.cancel()
	}
	return paymentprovider.RevenueCheckoutEvidence{Scope: scope, SessionID: session, SubscriptionID: "sub_trial"}, p.err
}

type checkoutSessionLegacyProvider struct {
	paymentprovider.CheckoutProvider
}

func TestCheckoutSessionEvidenceForwarding(t *testing.T) {
	outage := errors.New("provider lookup unavailable")
	cases := []struct {
		name                                                                                     string
		nilContext, canceled, lateCancel, noRegistry, typedNilRegistry, legacy, typedNilProvider bool
		registryErr, providerErr, want                                                           error
		registryCalls, providerCalls                                                             int
	}{
		{name: "exact_retained_session_scope", registryCalls: 1, providerCalls: 1},
		{name: "nil_context", nilContext: true, want: billing.ErrRevenueInvalid},
		{name: "early_cancellation", canceled: true, want: context.Canceled},
		{name: "late_cancellation_withholds_evidence", lateCancel: true, want: context.Canceled, registryCalls: 1, providerCalls: 1},
		{name: "missing_registry", noRegistry: true, want: billing.ErrRevenueUnavailable},
		{name: "typed_nil_registry", typedNilRegistry: true, want: billing.ErrRevenueUnavailable},
		{name: "legacy_provider_no_subscription_fallback", legacy: true, want: billing.ErrRevenueUnavailable, registryCalls: 1},
		{name: "typed_nil_provider", typedNilProvider: true, want: billing.ErrRevenueUnavailable, registryCalls: 1},
		{name: "registry_error_preserved", registryErr: outage, want: outage, registryCalls: 1},
		{name: "provider_error_withholds_partial_evidence", providerErr: outage, want: outage, registryCalls: 1, providerCalls: 1},
		{name: "joined_provider_error_not_absence", providerErr: errors.Join(paymentprovider.ErrRevenueUnassessable, outage), want: outage, registryCalls: 1, providerCalls: 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			t.Cleanup(cancel)
			provider := &checkoutSessionProvider{err: tc.providerErr}
			if tc.lateCancel {
				provider.cancel = cancel
			}
			registry := &checkoutSessionRegistry{provider: provider, err: tc.registryErr}
			var port billingmanager.CheckoutProviderRegistry = registry
			if tc.legacy {
				registry.provider = &checkoutSessionLegacyProvider{}
			}
			if tc.typedNilProvider {
				registry.provider = (*checkoutSessionProvider)(nil)
			}
			if tc.noRegistry {
				port = nil
			}
			if tc.typedNilRegistry {
				port = (*checkoutSessionRegistry)(nil)
			}
			if tc.canceled {
				cancel()
			}
			callCtx := ctx
			if tc.nilContext {
				callCtx = nil
			}
			scope := paymentprovider.RevenueScope{Provider: "stripe", AccountID: "acct_explicit", LiveMode: true}
			e, err := (CheckoutEvidence{registry: port}).LookupRevenueCheckoutSessionEvidence(callCtx, scope, "cs_retained")
			if tc.want != nil {
				require.ErrorIs(t, err, tc.want)
				require.Zero(t, e)
			} else {
				require.NoError(t, err)
				require.Equal(t, scope, e.Scope)
				require.Equal(t, "cs_retained", e.SessionID)
				require.Equal(t, scope, provider.scope)
				require.Equal(t, "cs_retained", provider.session)
			}
			require.Equal(t, tc.registryCalls, registry.calls)
			require.Equal(t, tc.providerCalls, provider.calls)
			if registry.calls != 0 {
				require.Equal(t, scope.Provider, registry.name)
			}
		})
	}
}
