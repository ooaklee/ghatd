package billingmanagerhelper

import (
	"context"
	"reflect"

	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/billingmanager"
	"github.com/ooaklee/ghatd/external/paymentprovider"
)

// Dispatch only by the explicit provider in the owning revenue scope. The
// native checkout service verifies all returned history against that scope and
// its frozen intent. No current catalogue/email mapping is supplied by the host.
type CheckoutEvidence struct {
	registry billingmanager.CheckoutProviderRegistry
}

// LookupRevenueCheckoutSessionEvidence forwards the exact retained session to
// its scoped provider. Native billing still validates the frozen authorization;
// this read neither establishes payment nor falls back to subscription lookup.
func (p CheckoutEvidence) LookupRevenueCheckoutSessionEvidence(ctx context.Context, scope paymentprovider.RevenueScope, session string) (paymentprovider.RevenueCheckoutEvidence, error) {
	if ctx == nil {
		return paymentprovider.RevenueCheckoutEvidence{}, billing.ErrRevenueInvalid
	}
	if err := ctx.Err(); err != nil {
		return paymentprovider.RevenueCheckoutEvidence{}, err
	}
	if nilPort(p.registry) {
		return paymentprovider.RevenueCheckoutEvidence{}, billing.ErrRevenueUnavailable
	}
	provider, err := p.registry.GetCheckoutProvider(scope.Provider)
	if err != nil {
		return paymentprovider.RevenueCheckoutEvidence{}, err
	}
	capability, ok := provider.(paymentprovider.RevenueCheckoutSessionEvidenceProvider)
	if !ok || nilPort(capability) {
		return paymentprovider.RevenueCheckoutEvidence{}, billing.ErrRevenueUnavailable
	}
	evidence, err := capability.LookupRevenueCheckoutSessionEvidence(ctx, scope, session)
	if err != nil {
		return paymentprovider.RevenueCheckoutEvidence{}, err
	}
	if err := ctx.Err(); err != nil {
		return paymentprovider.RevenueCheckoutEvidence{}, err
	}
	return evidence, nil
}

var _ paymentprovider.RevenueCheckoutSessionEvidenceProvider = CheckoutEvidence{}

// LookupRevenueCheckout resolves the scope's checkout provider and forwards the
// exact retained session, returning ErrRevenueUnavailable when the provider
// lacks the revenue checkout capability. It performs no billing validation
// itself.
func (p CheckoutEvidence) LookupRevenueCheckout(ctx context.Context, scope paymentprovider.RevenueScope, subscription string) (paymentprovider.RevenueCheckoutEvidence, error) {
	if ctx == nil {
		return paymentprovider.RevenueCheckoutEvidence{}, billing.ErrRevenueInvalid
	}
	if err := ctx.Err(); err != nil {
		return paymentprovider.RevenueCheckoutEvidence{}, err
	}
	if nilPort(p.registry) {
		return paymentprovider.RevenueCheckoutEvidence{}, billing.ErrRevenueUnavailable
	}
	provider, err := p.registry.GetCheckoutProvider(scope.Provider)
	if err != nil {
		return paymentprovider.RevenueCheckoutEvidence{}, err
	}
	capability, ok := provider.(paymentprovider.RevenueCheckoutProvider)
	if !ok || nilPort(capability) {
		return paymentprovider.RevenueCheckoutEvidence{}, billing.ErrRevenueUnavailable
	}
	return capability.LookupRevenueCheckout(ctx, scope, subscription)
}

// NewCheckoutEvidence selects providers solely by the explicit revenue scope.
// It performs no lookup. A missing registry fails closed when evidence is read.
func NewCheckoutEvidence(registry billingmanager.CheckoutProviderRegistry) CheckoutEvidence {
	return CheckoutEvidence{registry: registry}
}

// SupportsCheckoutStatus checks only the provider selected by this scope. The
// routing wrapper must not advertise status merely because it has a forwarding
// method: a selected legacy provider keeps the complete-evidence fallback.
func (p CheckoutEvidence) SupportsCheckoutStatus(scope paymentprovider.RevenueScope) bool {
	if nilPort(p.registry) || billing.ValidateRevenueHistoryScopes([]billing.RevenueScope{{Provider: scope.Provider, AccountID: scope.AccountID, LiveMode: scope.LiveMode}}) != nil {
		return false
	}
	provider, err := p.registry.GetCheckoutProvider(scope.Provider)
	if err != nil || nilPort(provider) {
		return false
	}
	status, ok := provider.(paymentprovider.CheckoutStatusProvider)
	if !ok || nilPort(status) {
		return false
	}
	if capability, ok := provider.(paymentprovider.CheckoutStatusCapability); ok {
		return !nilPort(capability) && capability.SupportsCheckoutStatus(scope)
	}
	return true
}

// LookupCheckoutStatus dispatches the retained session to its exact provider.
// It creates no checkout and never substitutes a different provider or scope.
func (p CheckoutEvidence) LookupCheckoutStatus(ctx context.Context, scope paymentprovider.RevenueScope, session string) (paymentprovider.CheckoutStatusEvidence, error) {
	if ctx == nil {
		return paymentprovider.CheckoutStatusEvidence{}, billing.ErrRevenueInvalid
	}
	if err := ctx.Err(); err != nil {
		return paymentprovider.CheckoutStatusEvidence{}, err
	}
	if nilPort(p.registry) {
		return paymentprovider.CheckoutStatusEvidence{}, billing.ErrRevenueUnavailable
	}
	provider, err := p.registry.GetCheckoutProvider(scope.Provider)
	if err != nil {
		return paymentprovider.CheckoutStatusEvidence{}, err
	}
	capability, ok := provider.(paymentprovider.CheckoutStatusProvider)
	if !ok || nilPort(capability) {
		return paymentprovider.CheckoutStatusEvidence{}, billing.ErrRevenueUnavailable
	}
	if scoped, ok := provider.(paymentprovider.CheckoutStatusCapability); ok && (nilPort(scoped) || !scoped.SupportsCheckoutStatus(scope)) {
		return paymentprovider.CheckoutStatusEvidence{}, billing.ErrRevenueUnavailable
	}
	evidence, err := capability.LookupCheckoutStatus(ctx, scope, session)
	if err != nil {
		return paymentprovider.CheckoutStatusEvidence{}, err
	}
	if err := ctx.Err(); err != nil {
		return paymentprovider.CheckoutStatusEvidence{}, err
	}
	return evidence, nil
}

var _ paymentprovider.CheckoutStatusProvider = CheckoutEvidence{}
var _ paymentprovider.CheckoutStatusCapability = CheckoutEvidence{}

// nilPort reports whether port is nil, including nil values stored inside
// interface, pointer, func, map, slice or channel wrappers.
func nilPort(port any) bool {
	if port == nil {
		return true
	}
	value := reflect.ValueOf(port)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	}
	return false
}
