package paymentprovider

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"time"
)

// CheckoutRevenueScope verifies the configured merchant with the authenticated
// API. Live/test remains explicit and is subsequently checked on every session
// and price; it is never inferred from environment or secret-key prefixes.
func (s *StripeProvider) CheckoutRevenueScope(ctx context.Context) (RevenueScope, error) {
	if s == nil || s.config == nil || s.config.Revenue == nil {
		return RevenueScope{}, ErrRevenueNotEnabled
	}
	scope, err := s.revenueScope(s.config.Revenue.AccountID, s.config.Revenue.LiveMode)
	if err != nil {
		return RevenueScope{}, err
	}
	if err := s.verifyCheckoutMerchant(ctx, scope); err != nil {
		return RevenueScope{}, err
	}
	return scope, nil
}

// verifyCheckoutMerchant fetches /v1/account and requires its ID to equal
// scope.AccountID, returning ErrRevenueUnassessable on mismatch and the request
// error otherwise.
func (s *StripeProvider) verifyCheckoutMerchant(ctx context.Context, scope RevenueScope) error {
	account, err := s.revenueGet(ctx, scope, "/v1/account")
	if err != nil {
		return err
	}
	if rawStripeID(account["id"]) != scope.AccountID {
		return ErrRevenueUnassessable
	}
	return nil
}

// LookupRevenueCheckout uses Stripe's documented subscription session filter and
// complete pagination. Multiple sessions, missing intent pointers, mismatched
// scope or incomplete original line evidence are never guessed from metadata.
func (s *StripeProvider) LookupRevenueCheckout(ctx context.Context, scope RevenueScope, subscription string) (RevenueCheckoutEvidence, error) {
	if !stripeRevenueObjectID.MatchString(subscription) || !strings.HasPrefix(subscription, "sub_") {
		return RevenueCheckoutEvidence{}, ErrRevenueUnassessable
	}
	if err := s.verifyCheckoutMerchant(ctx, scope); err != nil {
		return RevenueCheckoutEvidence{}, err
	}
	sessions, err := s.revenueList(ctx, scope, "/v1/checkout/sessions?"+url.Values{"subscription": {subscription}}.Encode())
	if err != nil {
		return RevenueCheckoutEvidence{}, err
	}
	if len(sessions) != 1 {
		return RevenueCheckoutEvidence{}, ErrRevenueUnassessable
	}
	return s.checkoutSessionEvidence(ctx, scope, sessions[0], "", subscription)
}

// LookupRevenueCheckoutSessionEvidence retrieves the exact retained checkout
// session and all original line items without requiring an already known
// subscription or payment. It makes authenticated GETs only; the billing owner
// must still bind the returned evidence to its original server authorization.
func (s *StripeProvider) LookupRevenueCheckoutSessionEvidence(ctx context.Context, scope RevenueScope, id string) (RevenueCheckoutEvidence, error) {
	if !stripeRevenueObjectID.MatchString(id) || !strings.HasPrefix(id, "cs_") {
		return RevenueCheckoutEvidence{}, ErrRevenueUnassessable
	}
	if err := s.verifyCheckoutMerchant(ctx, scope); err != nil {
		return RevenueCheckoutEvidence{}, err
	}
	session, err := s.revenueGet(ctx, scope, "/v1/checkout/sessions/"+url.PathEscape(id))
	if err != nil {
		return RevenueCheckoutEvidence{}, err
	}
	return s.checkoutSessionEvidence(ctx, scope, session, id, "")
}

// checkoutSessionEvidence validates a checkout session against the scope,
// expected session/subscription IDs, subscription mode, complete status and a
// non-empty checkout_intent_id metadata entry, then fetches its line items via
// complete pagination. It accepts exactly one quantity-1 recurring price with a
// supported weekly/monthly/yearly cadence and returns ErrRevenueUnassessable
// for any other shape instead of guessing.
func (s *StripeProvider) checkoutSessionEvidence(ctx context.Context, scope RevenueScope, session map[string]json.RawMessage, expectedSession, expectedSubscription string) (RevenueCheckoutEvidence, error) {
	id := rawStripeID(session["id"])
	subscription := rawStripeID(session["subscription"])
	if rawStripeString(session["object"]) != "checkout.session" || !stripeRevenueObjectID.MatchString(id) || !strings.HasPrefix(id, "cs_") || (expectedSession != "" && id != expectedSession) || !stripeRevenueObjectID.MatchString(subscription) || !strings.HasPrefix(subscription, "sub_") || (expectedSubscription != "" && subscription != expectedSubscription) || !rawStripeMode(session, scope) || rawStripeString(session["mode"]) != CheckoutModeSubscription || rawStripeString(session["status"]) != "complete" {
		return RevenueCheckoutEvidence{}, ErrRevenueUnassessable
	}
	var metadata map[string]string
	if json.Unmarshal(session["metadata"], &metadata) != nil || strings.TrimSpace(metadata["checkout_intent_id"]) == "" {
		return RevenueCheckoutEvidence{}, ErrRevenueUnassessable
	}
	created, known := rawStripeInt(session["created"])
	customer := rawStripeID(session["customer"])
	reference := rawStripeString(session["client_reference_id"])
	currency := strings.ToUpper(rawStripeString(session["currency"]))
	if !known || created <= 0 || !stripeRevenueObjectID.MatchString(customer) || !strings.HasPrefix(customer, "cus_") || reference == "" || !validCheckoutEvidenceCurrency(currency) {
		return RevenueCheckoutEvidence{}, ErrRevenueUnassessable
	}
	lines, err := s.revenueList(ctx, scope, "/v1/checkout/sessions/"+url.PathEscape(id)+"/line_items")
	if err != nil {
		return RevenueCheckoutEvidence{}, err
	}
	if len(lines) != 1 {
		return RevenueCheckoutEvidence{}, ErrRevenueUnassessable
	}
	line := lines[0]
	quantity, known := rawStripeInt(line["quantity"])
	var price map[string]json.RawMessage
	if !known || quantity != 1 || json.Unmarshal(line["price"], &price) != nil || rawStripeString(price["object"]) != "price" || !rawStripeMode(price, scope) || rawStripeString(price["type"]) != "recurring" || strings.ToUpper(rawStripeString(price["currency"])) != currency || strings.ToUpper(rawStripeString(line["currency"])) != currency {
		return RevenueCheckoutEvidence{}, ErrRevenueUnassessable
	}
	amount, amountKnown := rawStripeInt(price["unit_amount"])
	var recurring map[string]json.RawMessage
	if !amountKnown || amount <= 0 || json.Unmarshal(price["recurring"], &recurring) != nil {
		return RevenueCheckoutEvidence{}, ErrRevenueUnassessable
	}
	count, countKnown := rawStripeInt(recurring["interval_count"])
	cadence := rawStripeString(recurring["interval"])
	if !countKnown || count != 1 || (cadence != "week" && cadence != "month" && cadence != "year") {
		return RevenueCheckoutEvidence{}, ErrRevenueUnassessable
	}
	priceID := rawStripeID(price["id"])
	if !stripeRevenueObjectID.MatchString(priceID) || !strings.HasPrefix(priceID, "price_") {
		return RevenueCheckoutEvidence{}, ErrRevenueUnassessable
	}
	return RevenueCheckoutEvidence{Scope: scope, SessionID: id, IntentID: metadata["checkout_intent_id"], ClientReferenceID: reference, CustomerID: customer, SubscriptionID: subscription, PriceID: priceID, Currency: currency, Mode: CheckoutModeSubscription, Status: "complete", CreatedAt: time.Unix(created, 0).UTC(), UnitAmountMinor: amount, IntervalCount: count, BillingCadence: cadence}, nil
}

// validCheckoutEvidenceCurrency reports whether currency is exactly three
// uppercase ASCII letters.
func validCheckoutEvidenceCurrency(currency string) bool {
	if len(currency) != 3 {
		return false
	}
	for _, character := range currency {
		if character < 'A' || character > 'Z' {
			return false
		}
	}
	return true
}

// RetrieveRevenueCheckoutSession recovers an acknowledged session without a
// second POST, including after provider idempotency retention has elapsed.
func (s *StripeProvider) RetrieveRevenueCheckoutSession(ctx context.Context, scope RevenueScope, id string) (*CheckoutSession, error) {
	if !stripeRevenueObjectID.MatchString(id) || !strings.HasPrefix(id, "cs_") {
		return nil, ErrRevenueUnassessable
	}
	if err := s.verifyCheckoutMerchant(ctx, scope); err != nil {
		return nil, err
	}
	object, err := s.revenueGet(ctx, scope, "/v1/checkout/sessions/"+url.PathEscape(id))
	if err != nil {
		return nil, err
	}
	if rawStripeString(object["object"]) != "checkout.session" || rawStripeID(object["id"]) != id || !rawStripeMode(object, scope) {
		return nil, ErrRevenueUnassessable
	}
	session := &CheckoutSession{ID: id, ClientSecret: rawStripeString(object["client_secret"]), URL: rawStripeString(object["url"]), PublishableKey: strings.TrimSpace(s.config.PublishableKey)}
	if session.ClientSecret == "" && session.URL == "" {
		return nil, ErrPaymentProviderAPIResponseInvalid
	}
	return session, nil
}

var _ RevenueCheckoutProvider = (*StripeProvider)(nil)
var _ RevenueCheckoutSessionEvidenceProvider = (*StripeProvider)(nil)
