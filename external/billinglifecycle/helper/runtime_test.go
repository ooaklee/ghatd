package billinglifecyclehelper

import (
	"context"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/billing/revenuestore"
	"github.com/ooaklee/ghatd/external/billinglifecycle"
	"github.com/ooaklee/ghatd/external/billingmanager"
	"github.com/ooaklee/ghatd/external/partnerprogram"
	"github.com/ooaklee/ghatd/external/repository/recordstore"
	"github.com/stretchr/testify/require"
)

// These non-operational ports deliberately fail if an invalid constructor ever
// gets past its validation boundary and starts storage/discovery/provider work.
type runtimeAuthority struct {
	Authority
	calls int
}

func (a *runtimeAuthority) Bind(ctx context.Context) (context.Context, error) {
	a.calls++
	return ctx, nil
}

type runtimeRecords struct{ recordstore.Store }
type runtimeRegistry struct {
	billingmanager.ProviderRegistry
}

func TestRuntimeRejectsConfigurationBeforeOwnerIO(t *testing.T) {
	for _, state := range []string{"nil-context", "cancelled", "revenue", "records", "checkout", "clock", "registry", "authority", "typed-authority", "invalid-bounds", "invalid-scopes", "missing-revenue-capability"} {
		t.Run(state, func(t *testing.T) {
			auth := &runtimeAuthority{}
			deps := Dependencies{Revenue: &billing.RevenueService{}, Records: &runtimeRecords{}, Checkout: &revenuestore.Repository{}, Clock: partnerprogram.RealClock{}, Providers: &runtimeRegistry{}, Authority: auth}
			cfg := billinglifecycle.RuntimeConfig{ActorID: "worker", Scopes: []billing.RevenueScope{{Provider: "provider", AccountID: "account"}}, PageSize: 2, ColdBudget: 1, RefreshBudget: 1, Interval: time.Second, PassTimeout: time.Second, Lease: time.Minute, RetryBase: time.Second, RetryMax: time.Hour, Cadence: time.Minute}
			ctx := t.Context()
			want := billing.ErrRevenueUnavailable
			switch state {
			case "nil-context":
				ctx = nil
			case "cancelled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
				want = context.Canceled
			case "revenue":
				deps.Revenue = nil
			case "records":
				deps.Records = nil
			case "checkout":
				deps.Checkout = nil
			case "clock":
				deps.Clock = nil
			case "registry":
				deps.Providers = nil
			case "authority":
				deps.Authority = nil
			case "typed-authority":
				deps.Authority = (*runtimeAuthority)(nil)
			case "invalid-bounds":
				cfg.Interval = 0
				want = nil
			case "invalid-scopes":
				cfg.Scopes = nil
				want = nil
			}
			runtime, err := NewRuntime(ctx, cfg, deps)
			require.Error(t, err)
			if want != nil {
				require.ErrorIs(t, err, want)
			}
			require.Nil(t, runtime)
			require.Zero(t, auth.calls)
		})
	}
}
func TestRuntimeZeroValuesDoNotBind(t *testing.T) {
	for _, tc := range []struct {
		name    string
		runtime *Runtime
	}{{"nil", nil}, {"zero", &Runtime{}}, {"typed_nil_authority", &Runtime{authority: (*runtimeAuthority)(nil)}}} {
		t.Run(tc.name, func(t *testing.T) {
			report, err := tc.runtime.RunOnce(t.Context())
			require.ErrorIs(t, err, billing.ErrRevenueUnavailable)
			require.Zero(t, report)
			require.Zero(t, tc.runtime.Interval())
			require.Zero(t, tc.runtime.PassTimeout())
		})
	}
}
