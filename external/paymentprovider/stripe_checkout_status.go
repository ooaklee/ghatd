package paymentprovider

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"time"
)

// LookupCheckoutStatus authenticates the merchant and reads the exact session
// and its complete original line collection. It makes GETs only and does not
// reuse completed-subscription evidence as proof of a successful payment.
func (s *StripeProvider) LookupCheckoutStatus(ctx context.Context, scope RevenueScope, id string) (CheckoutStatusEvidence, error) {
	if ctx == nil {
		return CheckoutStatusEvidence{}, ErrRevenueUnassessable
	}
	if err := ctx.Err(); err != nil {
		return CheckoutStatusEvidence{}, err
	}
	if !stripeRevenueObjectID.MatchString(id) || !strings.HasPrefix(id, "cs_") {
		return CheckoutStatusEvidence{}, ErrRevenueUnassessable
	}
	if err := s.verifyCheckoutMerchant(ctx, scope); err != nil {
		return CheckoutStatusEvidence{}, err
	}
	session, err := s.revenueGet(ctx, scope, "/v1/checkout/sessions/"+url.PathEscape(id))
	if err != nil {
		return CheckoutStatusEvidence{}, err
	}
	mode := rawStripeString(session["mode"])
	if rawStripeString(session["object"]) != "checkout.session" || rawStripeID(session["id"]) != id || !rawStripeMode(session, scope) || (mode != CheckoutModeSubscription && mode != CheckoutModePayment) {
		return CheckoutStatusEvidence{}, ErrRevenueUnassessable
	}
	e := CheckoutStatusEvidence{Scope: scope, SessionID: id, Mode: mode, SessionStatus: rawStripeString(session["status"]), PaymentStatus: rawStripeString(session["payment_status"]), ClientReferenceID: rawStripeString(session["client_reference_id"]), Currency: strings.ToUpper(rawStripeString(session["currency"]))}
	e.AmountTotalMinor, e.AmountTotalKnown = rawStripeInt(session["amount_total"])
	if _, err := e.State(); err != nil {
		return CheckoutStatusEvidence{}, err
	}
	var metadata map[string]string
	created, known := rawStripeInt(session["created"])
	if !known || created <= 0 || e.ClientReferenceID == "" || !validCheckoutEvidenceCurrency(e.Currency) || json.Unmarshal(session["metadata"], &metadata) != nil || strings.TrimSpace(metadata["checkout_intent_id"]) == "" {
		return CheckoutStatusEvidence{}, ErrRevenueUnassessable
	}
	e.IntentID = metadata["checkout_intent_id"]
	e.CreatedAt = time.Unix(created, 0).UTC()
	if mode == CheckoutModeSubscription && e.SessionStatus == "complete" {
		for key, prefix := range map[string]string{"subscription": "sub_", "customer": "cus_"} {
			value := rawStripeID(session[key])
			if !stripeRevenueObjectID.MatchString(value) || !strings.HasPrefix(value, prefix) {
				return CheckoutStatusEvidence{}, ErrRevenueUnassessable
			}
		}
	}
	lines, err := s.revenueList(ctx, scope, "/v1/checkout/sessions/"+url.PathEscape(id)+"/line_items")
	if err != nil {
		return CheckoutStatusEvidence{}, err
	}
	if len(lines) != 1 {
		return CheckoutStatusEvidence{}, ErrRevenueUnassessable
	}
	line := lines[0]
	quantity, known := rawStripeInt(line["quantity"])
	var price map[string]json.RawMessage
	if !known || quantity != 1 || json.Unmarshal(line["price"], &price) != nil || rawStripeString(price["object"]) != "price" || !rawStripeMode(price, scope) || strings.ToUpper(rawStripeString(price["currency"])) != e.Currency || strings.ToUpper(rawStripeString(line["currency"])) != e.Currency {
		return CheckoutStatusEvidence{}, ErrRevenueUnassessable
	}
	e.PriceID = rawStripeID(price["id"])
	amount, known := rawStripeInt(price["unit_amount"])
	if !known || amount <= 0 || !stripeRevenueObjectID.MatchString(e.PriceID) || !strings.HasPrefix(e.PriceID, "price_") {
		return CheckoutStatusEvidence{}, ErrRevenueUnassessable
	}
	e.UnitAmountMinor = amount
	if mode == CheckoutModeSubscription {
		var recurring map[string]json.RawMessage
		if rawStripeString(price["type"]) != "recurring" || json.Unmarshal(price["recurring"], &recurring) != nil {
			return CheckoutStatusEvidence{}, ErrRevenueUnassessable
		}
		e.IntervalCount, known = rawStripeInt(recurring["interval_count"])
		e.BillingCadence = rawStripeString(recurring["interval"])
		if !known || e.IntervalCount != 1 || (e.BillingCadence != "week" && e.BillingCadence != "month" && e.BillingCadence != "year") {
			return CheckoutStatusEvidence{}, ErrRevenueUnassessable
		}
	} else {
		if rawStripeString(price["type"]) != "one_time" {
			return CheckoutStatusEvidence{}, ErrRevenueUnassessable
		}
		e.BillingCadence = "one_time"
	}
	if err := ctx.Err(); err != nil {
		return CheckoutStatusEvidence{}, err
	}
	return e, nil
}

var _ CheckoutStatusProvider = (*StripeProvider)(nil)
