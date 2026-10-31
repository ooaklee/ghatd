package partners

import (
	"github.com/ooaklee/ghatd/external/billingmanager"
	"github.com/ooaklee/ghatd/external/partnermanager"
	partnerruntime "github.com/ooaklee/ghatd/external/partnermanager/runtime"
	"github.com/ooaklee/ghatd/external/paymentprovider"
)

// PaidAcquisition composes the owning billing and provider evidence with an
// explicit list of eligible billing intervals. Disabled configuration does no I/O.
func PaidAcquisition(cfg partnerruntime.Config, billing *billingmanager.Service, providers billingmanager.ProviderRegistry, clock partnermanager.Clock, intervals []string) (partnermanager.AcquisitionEligibility, error) {
	if !cfg.RequireAcquisitionEligibility {
		return nil, nil
	}
	if billing == nil || cfg.RevenueReporting == nil || len(cfg.RevenueReporting.Scopes) != 1 || absent(clock) {
		return nil, partnermanager.ErrUnavailable
	}
	registry, ok := providers.(billingmanager.RevenueProviderRegistry)
	if !ok || absent(registry) {
		return nil, partnermanager.ErrUnavailable
	}
	scope := cfg.RevenueReporting.Scopes[0]
	provider, err := registry.GetRevenueProvider(scope.Provider)
	if err != nil {
		return nil, err
	}
	paid, ok := provider.(paymentprovider.PaidSubscriptionProvider)
	if !ok || absent(paid) {
		return nil, partnermanager.ErrUnavailable
	}
	return partnermanager.NewPaidAcquisition(billing.BillingService, paid, clock, partnermanager.PaidAcquisitionConfig{Scope: paymentprovider.RevenueScope{Provider: scope.Provider, AccountID: scope.AccountID, LiveMode: scope.LiveMode}, EligiblePlanIDs: cfg.Program.EligiblePlanIDs, Intervals: intervals})
}

// PaidAcquisitionWithConfig composes a separately configured paid-plan
// allowlist and provider scope. A nil policy disables the gate without I/O.
func PaidAcquisitionWithConfig(cfg *partnermanager.PaidAcquisitionConfig, billing *billingmanager.Service, providers billingmanager.ProviderRegistry, clock partnermanager.Clock) (partnermanager.AcquisitionEligibility, error) {
	if cfg == nil {
		return nil, nil
	}
	if billing == nil || absent(billing.BillingService) || absent(clock) {
		return nil, partnermanager.ErrUnavailable
	}
	registry, ok := providers.(interface {
		Get(string) (paymentprovider.Provider, error)
	})
	if !ok || absent(registry) {
		return nil, partnermanager.ErrUnavailable
	}
	provider, err := registry.Get(cfg.Scope.Provider)
	if err != nil {
		return nil, partnermanager.ErrUnavailable
	}
	paid, ok := provider.(paymentprovider.PaidSubscriptionProvider)
	if !ok || absent(paid) {
		return nil, partnermanager.ErrUnavailable
	}
	return partnermanager.NewPaidAcquisition(billing.BillingService, paid, clock, *cfg)
}
