package paymentprovider

import (
	"context"
	"github.com/ooaklee/ghatd/external/logger"
	"go.uber.org/zap"
	"net/url"
	"strings"
)

// RevenuePaymentSafetyError carries a static branch code and preserves the
// original owning cause without rendering raw provider responses or identities.
type RevenuePaymentSafetyError struct {
	Code  string
	Cause error
}

func (e RevenuePaymentSafetyError) Error() string {
	return "paymentprovider/unadjusted-payment/" + e.Code
}
func (e RevenuePaymentSafetyError) Unwrap() error { return e.Cause }
func paymentSafetyRefusal(ctx context.Context, code string) error {
	if ctx != nil {
		logger.AcquirePackageFrom(ctx, "external/paymentprovider").Warn("stripe-legacy-payment-evidence-refused", zap.String("owner_operation", "stripe.revenue_payment.verify_unadjusted"), zap.String("cause_code", code))
	}
	return RevenuePaymentSafetyError{code, ErrRevenueUnassessable}
}

// RevenuePaymentSafetyVerifier is an optional narrow historical-migration
// preflight. It rechecks the authenticated paid payment and complete adjustment
// lists; unadjusted payment proof supplies no application ownership authority.
type RevenuePaymentSafetyVerifier interface {
	VerifyUnadjustedRevenuePayment(context.Context, RevenueInvoiceEvidence) error
}

// VerifyUnadjustedRevenuePayment accepts only a fully paid, unrefunded,
// undisputed invoice payment without issued credit notes or pending refunds.
// Every lookup is an authenticated GET under the invoice's exact scope. A
// provider outage remains its original retryable error, never commit uncertainty.
func (s *StripeProvider) VerifyUnadjustedRevenuePayment(ctx context.Context, invoice RevenueInvoiceEvidence) error {
	scope := invoice.Scope
	if ctx == nil || invoice.GrossPaidMinor <= 0 || !stripeRevenueObjectID.MatchString(invoice.InvoiceID) || !stripeRevenueObjectID.MatchString(invoice.PaymentID) || !stripeRevenueObjectID.MatchString(invoice.CustomerID) {
		return paymentSafetyRefusal(ctx, "invoice_selector_invalid")
	}
	if err := s.verifyCheckoutMerchant(ctx, scope); err != nil {
		return err
	}
	intent, err := s.revenueGet(ctx, scope, "/v1/payment_intents/"+url.PathEscape(invoice.PaymentID))
	if err != nil {
		return err
	}
	received, known := rawStripeInt(intent["amount_received"])
	if !known || received != invoice.GrossPaidMinor || rawStripeID(intent["id"]) != invoice.PaymentID || !rawStripeMode(intent, scope) || rawStripeString(intent["status"]) != "succeeded" || rawStripeID(intent["customer"]) != invoice.CustomerID || strings.ToUpper(rawStripeString(intent["currency"])) != invoice.Currency {
		return paymentSafetyRefusal(ctx, "payment_intent_identity_or_amount_mismatch")
	}
	chargeID := rawStripeID(intent["latest_charge"])
	if !stripeRevenueObjectID.MatchString(chargeID) || !strings.HasPrefix(chargeID, "ch_") {
		return paymentSafetyRefusal(ctx, "charge_reference_missing")
	}
	charge, err := s.revenueGet(ctx, scope, "/v1/charges/"+url.PathEscape(chargeID))
	if err != nil {
		return err
	}
	amount, amountKnown := rawStripeInt(charge["amount"])
	refunded, refundKnown := rawStripeInt(charge["amount_refunded"])
	if !amountKnown || !refundKnown || amount != invoice.GrossPaidMinor || refunded != 0 || rawStripeID(charge["id"]) != chargeID || !rawStripeMode(charge, scope) || rawStripeString(charge["status"]) != "succeeded" || string(charge["paid"]) != "true" || string(charge["disputed"]) != "false" || rawStripeID(charge["payment_intent"]) != invoice.PaymentID || rawStripeID(charge["customer"]) != invoice.CustomerID || strings.ToUpper(rawStripeString(charge["currency"])) != invoice.Currency {
		return paymentSafetyRefusal(ctx, "charge_adjusted_or_payment_mismatch")
	}
	// Newer Charge objects omit invoice. A supplied contradictory link is never
	// ignored; payment intent + authenticated invoice payments bind absence.
	if id := rawStripeID(charge["invoice"]); id != "" && id != invoice.InvoiceID {
		return paymentSafetyRefusal(ctx, "charge_invoice_link_mismatch")
	}
	refunds, err := s.revenueList(ctx, scope, "/v1/refunds?charge="+url.QueryEscape(chargeID))
	if err != nil {
		return err
	}
	for _, refund := range refunds {
		if rawStripeID(refund["charge"]) != chargeID || rawStripeID(refund["payment_intent"]) != invoice.PaymentID || strings.ToUpper(rawStripeString(refund["currency"])) != invoice.Currency {
			return paymentSafetyRefusal(ctx, "refund_identity_mismatch")
		}
		if _, present := refund["livemode"]; present && !rawStripeMode(refund, scope) {
			return paymentSafetyRefusal(ctx, "refund_mode_mismatch")
		}
		status := rawStripeString(refund["status"])
		if status != "failed" && status != "canceled" {
			return paymentSafetyRefusal(ctx, "refund_pending_or_succeeded")
		}
	}
	notes, err := s.revenueList(ctx, scope, "/v1/credit_notes?invoice="+url.QueryEscape(invoice.InvoiceID))
	if err != nil {
		return err
	}
	for _, note := range notes {
		if rawStripeID(note["invoice"]) != invoice.InvoiceID || !rawStripeMode(note, scope) || strings.ToUpper(rawStripeString(note["currency"])) != invoice.Currency || rawStripeString(note["status"]) != "void" {
			return paymentSafetyRefusal(ctx, "credit_note_active_or_identity_mismatch")
		}
	}
	return ctx.Err()
}

var _ RevenuePaymentSafetyVerifier = (*StripeProvider)(nil)
