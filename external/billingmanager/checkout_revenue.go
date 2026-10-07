package billingmanager

import (
	"context"
	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/paymentprovider"
)

// CheckoutRevenueCapture is optional owning persistence for pre-submission
// authorization. Supply it alongside revenue receiving before enabling facts.
type CheckoutRevenueCapture interface {
	FindCheckoutIntent(context.Context, billing.RevenueScope, string) (billing.CheckoutIntent, error)
	PrepareCheckout(context.Context, billing.RevenueScope, paymentprovider.CheckoutSessionRequest) (billing.CheckoutIntent, error)
	AcknowledgeCheckout(context.Context, billing.CheckoutIntent, string) error
	CanSubmitCheckout(context.Context, billing.CheckoutIntent) error
}

// CheckoutPayerAuthority confirms that the current verified caller is an owning
// paying account, rather than an organization seat or delegated billing viewer.
// It runs for every retry, before historical receipt/session recovery.
type CheckoutPayerAuthority interface {
	AuthorizeCheckoutPayer(context.Context, string) error
}

func (s *Service) WithCheckoutRevenueCapture(capture CheckoutRevenueCapture, authority CheckoutPayerAuthority) (*Service, error) {
	if s == nil || nilRevenueDependency(capture) || nilRevenueDependency(authority) {
		return nil, billing.ErrRevenueUnavailable
	}
	s.checkoutRevenueCapture = capture
	s.checkoutPayerAuthority = authority
	return s, nil
}
