package billingmanagerhelper

import (
	"context"
	"errors"
	"testing"

	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/paymentprovider"
	"github.com/stretchr/testify/require"
)

type checkoutStatusProvider struct {
	paymentprovider.CheckoutProvider
	calls   int
	scope   paymentprovider.RevenueScope
	session string
	err     error
	cancel  context.CancelFunc
}

func (p *checkoutStatusProvider) LookupCheckoutStatus(_ context.Context, s paymentprovider.RevenueScope, id string) (paymentprovider.CheckoutStatusEvidence, error) {
	p.calls++
	p.scope, p.session = s, id
	if p.cancel != nil {
		p.cancel()
	}
	return paymentprovider.CheckoutStatusEvidence{Scope: s, SessionID: id, SessionStatus: "expired", PaymentStatus: "unpaid", SubscriptionAbsent: true}, p.err
}

type checkoutScopedStatusProvider struct {
	*checkoutStatusProvider
	account string
}

type checkoutStatusMixedRegistry map[string]paymentprovider.CheckoutProvider

func (r checkoutStatusMixedRegistry) GetCheckoutProvider(name string) (paymentprovider.CheckoutProvider, error) {
	p, ok := r[name]
	if !ok {
		return nil, billing.ErrRevenueUnavailable
	}
	return p, nil
}

func TestCheckoutStatusMixedRegistrySelection(t *testing.T) {
	for _, name := range []string{"stripe", "legacy", "missing"} {
		t.Run(name, func(t *testing.T) {
			p := &checkoutStatusProvider{}
			r := checkoutStatusMixedRegistry{"stripe": p, "legacy": &checkoutSessionLegacyProvider{}}
			adapter := NewCheckoutEvidence(r)
			scope := paymentprovider.RevenueScope{Provider: name, AccountID: "acct_exact", LiveMode: true}
			require.Equal(t, name == "stripe", adapter.SupportsCheckoutStatus(scope))
			out, err := adapter.LookupCheckoutStatus(context.Background(), scope, "cs_exact")
			if name == "stripe" {
				require.NoError(t, err)
				require.Equal(t, scope, out.Scope)
				require.Equal(t, 1, p.calls)
			} else {
				require.ErrorIs(t, err, billing.ErrRevenueUnavailable)
				require.Zero(t, out)
				require.Zero(t, p.calls)
			}
		})
	}
}

func (p checkoutScopedStatusProvider) SupportsCheckoutStatus(s paymentprovider.RevenueScope) bool {
	return s.AccountID == p.account
}

func TestCheckoutStatusScopedCapability(t *testing.T) {
	for _, name := range []string{"supported", "legacy", "typed_nil", "registry_error", "missing_registry", "invalid_scope", "scoped_supported", "scoped_unsupported"} {
		t.Run(name, func(t *testing.T) {
			p := &checkoutStatusProvider{}
			r := &checkoutSessionRegistry{provider: p}
			s := paymentprovider.RevenueScope{Provider: "stripe", AccountID: "acct_selected", LiveMode: true}
			port := CheckoutEvidence{registry: r}
			want := name == "supported" || name == "scoped_supported"
			switch name {
			case "legacy":
				r.provider = &checkoutSessionLegacyProvider{}
			case "typed_nil":
				r.provider = (*checkoutStatusProvider)(nil)
			case "registry_error":
				r.err = errors.New("registry outage")
			case "missing_registry":
				port.registry = nil
			case "invalid_scope":
				s.AccountID = ""
			case "scoped_supported":
				r.provider = checkoutScopedStatusProvider{p, s.AccountID}
			case "scoped_unsupported":
				r.provider = checkoutScopedStatusProvider{p, "other_account"}
			}
			require.Equal(t, want, port.SupportsCheckoutStatus(s))
			require.Zero(t, p.calls, "capability does not issue provider HTTP")
			if r.calls > 0 {
				require.Equal(t, s.Provider, r.name)
			}
		})
	}
}

func TestCheckoutStatusScopedForwarding(t *testing.T) {
	outage := errors.New("provider status outage")
	for _, tc := range []struct {
		name string
		want error
	}{
		{"exact_scope_and_session", nil}, {"legacy", billing.ErrRevenueUnavailable}, {"typed_nil", billing.ErrRevenueUnavailable}, {"missing_registry", billing.ErrRevenueUnavailable}, {"registry_error", outage}, {"provider_error", outage}, {"nil_context", billing.ErrRevenueInvalid}, {"cancelled", context.Canceled}, {"late_cancelled", context.Canceled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			t.Cleanup(cancel)
			p := &checkoutStatusProvider{}
			r := &checkoutSessionRegistry{provider: p}
			port := CheckoutEvidence{registry: r}
			switch tc.name {
			case "legacy":
				r.provider = &checkoutSessionLegacyProvider{}
			case "typed_nil":
				r.provider = (*checkoutStatusProvider)(nil)
			case "missing_registry":
				port.registry = nil
			case "registry_error":
				r.err = outage
			case "provider_error":
				p.err = outage
			case "nil_context":
				ctx = nil
			case "cancelled":
				cancel()
			case "late_cancelled":
				p.cancel = cancel
			}
			s := paymentprovider.RevenueScope{Provider: "stripe", AccountID: "acct_exact", LiveMode: true}
			out, err := port.LookupCheckoutStatus(ctx, s, "cs_original")
			require.ErrorIs(t, err, tc.want)
			if tc.want != nil {
				require.Zero(t, out)
			} else {
				require.Equal(t, s, out.Scope)
				require.Equal(t, "cs_original", out.SessionID)
				require.True(t, out.SubscriptionAbsent)
				require.Equal(t, s, p.scope)
				require.Equal(t, "cs_original", p.session)
			}
			if r.calls > 0 {
				require.Equal(t, s.Provider, r.name)
			}
		})
	}
}
