package billingmanager

import (
	"context"
	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/paymentprovider"
)

// CheckoutRevenueCapture is optional owning persistence for pre-submission
// authorization. Supply it alongside revenue receiving before enabling facts.
type CheckoutRevenueCapture interface {
	// FindCheckoutIntent returns the retained checkout intent for the revenue scope
	// and identifier from the optional owning pre-submission authorization
	// persistence.
	FindCheckoutIntent(context.Context, billing.RevenueScope, string) (billing.CheckoutIntent, error)
	// PrepareCheckout returns a checkout intent persisted for the revenue scope
	// from the supplied checkout session request within optional owning
	// authorization persistence.
	PrepareCheckout(context.Context, billing.RevenueScope, paymentprovider.CheckoutSessionRequest) (billing.CheckoutIntent, error)
	// AcknowledgeCheckout marks the checkout intent as acknowledged with the
	// supplied string identifier in the optional owning authorization persistence.
	AcknowledgeCheckout(context.Context, billing.CheckoutIntent, string) error
	// CanSubmitCheckout checks whether the retained checkout intent may be
	// submitted, returning an error when submission is not permitted by the owning
	// persistence.
	CanSubmitCheckout(context.Context, billing.CheckoutIntent) error
}

// CheckoutPayerAuthority confirms that the current verified caller is an owning
// paying account, rather than an organization seat or delegated billing viewer.
// It runs for every retry, before historical receipt/session recovery.
type CheckoutPayerAuthority interface {
	// AuthorizeCheckoutPayer confirms the actor is an owning, active paying account
	// with verified email by loading it from the user service; denial returns
	// ErrBillingManagerUserUnauthorisedToCarryOutOperation.
	AuthorizeCheckoutPayer(context.Context, string) error
}

// WithCheckoutRevenueCapture installs both the pre-submission revenue capture
// persistence and the payer authority required before checkout capture facts
// are enabled. Either dependency being nil returns ErrRevenueUnavailable
// without mutating the service.
func (s *Service) WithCheckoutRevenueCapture(capture CheckoutRevenueCapture, authority CheckoutPayerAuthority) (*Service, error) {
	if s == nil || nilRevenueDependency(capture) || nilRevenueDependency(authority) {
		return nil, billing.ErrRevenueUnavailable
	}
	s.checkoutRevenueCapture = capture
	s.checkoutPayerAuthority = authority
	return s, nil
}
