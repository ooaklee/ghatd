package paymentprovider

import (
	"context"
	"time"
)

// CheckoutStatusEvidence is a fresh authenticated read of one checkout. It is
// not persisted revenue or lifecycle evidence and must be matched against the
// owning frozen authorization before any browser-safe status is disclosed.
type CheckoutStatusEvidence struct {
	Scope                                                           RevenueScope
	SessionID, IntentID, ClientReferenceID, PriceID, Currency, Mode string
	SessionStatus, PaymentStatus, BillingCadence                    string
	UnitAmountMinor, IntervalCount                                  int64
	AmountTotalMinor                                                int64
	AmountTotalKnown                                                bool
	CreatedAt                                                       time.Time
}

// CheckoutStatusProvider is optional and read-only. Implementations must never
// submit a checkout, change metadata, capture revenue or grant access here.
type CheckoutStatusProvider interface {
	LookupCheckoutStatus(context.Context, RevenueScope, string) (CheckoutStatusEvidence, error)
}

// State distinguishes completed checkout from payment. A trial or zero-payment
// completion is never "paid"; unsupported or contradictory pairs fail closed.
func (e CheckoutStatusEvidence) State() (string, error) {
	if !e.AmountTotalKnown || e.AmountTotalMinor < 0 {
		return "", ErrRevenueUnassessable
	}
	switch {
	case e.SessionStatus == "complete" && e.PaymentStatus == "paid" && e.AmountTotalMinor > 0:
		return "paid", nil
	// Stripe may report a successfully processed zero-total trial invoice as
	// paid. Completion is useful for access refresh, but is not a payment.
	case e.SessionStatus == "complete" && e.AmountTotalMinor == 0 && (e.PaymentStatus == "paid" || e.PaymentStatus == "no_payment_required"):
		return "no_payment_required", nil
	case e.SessionStatus == "complete" && e.PaymentStatus == "unpaid":
		return "pending", nil
	case e.SessionStatus == "open" && e.PaymentStatus == "unpaid":
		return "unpaid", nil
	case e.SessionStatus == "expired" && e.PaymentStatus == "unpaid":
		return "expired", nil
	default:
		return "", ErrRevenueUnassessable
	}
}
