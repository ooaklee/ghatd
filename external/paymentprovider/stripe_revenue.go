package paymentprovider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

var stripeRevenueObjectID = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{1,255}$`)

func validStripeRevenueConfig(c *RevenueConfig) bool {
	if c == nil || !strings.HasPrefix(c.AccountID, "acct_") || !stripeRevenueObjectID.MatchString(c.AccountID) || len(c.ConnectedAccountIDs) > 100 || len(c.CurrencyExponents) == 0 {
		return false
	}
	seen := map[string]bool{c.AccountID: true}
	for _, id := range c.ConnectedAccountIDs {
		if !strings.HasPrefix(id, "acct_") || !stripeRevenueObjectID.MatchString(id) || seen[id] {
			return false
		}
		seen[id] = true
	}
	for currency, exponent := range c.CurrencyExponents {
		if len(currency) != 3 || currency != strings.ToUpper(currency) || exponent < 0 || exponent > 3 {
			return false
		}
		for _, c := range currency {
			if c < 'A' || c > 'Z' {
				return false
			}
		}
	}
	return true
}

func (s *StripeProvider) revenueScope(account string, live bool) (RevenueScope, error) {
	if s == nil || s.config == nil || s.config.Revenue == nil {
		return RevenueScope{}, ErrRevenueNotEnabled
	}
	c := s.config.Revenue
	if !validStripeRevenueConfig(c) || strings.TrimSpace(s.config.APIKey) == "" {
		return RevenueScope{}, ErrPaymentProviderInvalidConfiguration
	}
	if account == "" {
		account = c.AccountID
	}
	allowed := account == c.AccountID
	for _, id := range c.ConnectedAccountIDs {
		if id == account {
			allowed = true
		}
	}
	if !allowed || live != c.LiveMode {
		return RevenueScope{}, ErrRevenueUnassessable
	}
	return RevenueScope{Provider: stripeProviderName, AccountID: account, LiveMode: live}, nil
}

// ResolveRevenueWebhook verifies the signed envelope before using any field or
// making an authenticated lookup. The signed scope and retrieved objects must
// agree; unavailable APIs remain errors so reception cannot be acknowledged.
func (s *StripeProvider) ResolveRevenueWebhook(ctx context.Context, req *http.Request) (*RevenueEvidence, error) {
	if ctx == nil {
		return nil, ErrPaymentProviderInvalidPayload
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s == nil || s.config == nil || s.config.Revenue == nil {
		return nil, ErrRevenueNotEnabled
	}
	if err := s.VerifyWebhook(ctx, req); err != nil {
		return nil, err
	}
	body, err := readAndRestoreWebhookBody(req, s.maxWebhookBodySize)
	if err != nil {
		return nil, err
	}
	var event stripeRevenueEvent
	if json.Unmarshal(body, &event) != nil || !stripeRevenueObjectID.MatchString(event.ID) || event.LiveMode == nil || event.Created <= 0 || event.Data.Object == nil {
		return nil, ErrPaymentProviderInvalidPayload
	}
	return s.resolveStripeRevenueEvent(ctx, event)
}

type stripeRevenueEvent struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Account  string `json:"account"`
	LiveMode *bool  `json:"livemode"`
	Created  int64  `json:"created"`
	Data     struct {
		Object map[string]json.RawMessage `json:"object"`
	} `json:"data"`
}

func (s *StripeProvider) resolveStripeRevenueEvent(ctx context.Context, event stripeRevenueEvent) (*RevenueEvidence, error) {
	if !stripeRevenueObjectID.MatchString(event.ID) || event.LiveMode == nil || event.Created <= 0 || event.Data.Object == nil {
		return nil, ErrPaymentProviderAPIResponseInvalid
	}
	scope, err := s.revenueScope(event.Account, *event.LiveMode)
	if err != nil {
		return nil, err
	}
	digest, err := canonicalStripeRevenueDigest(event)
	if err != nil {
		return nil, err
	}
	result := &RevenueEvidence{Scope: scope, EnvelopeID: event.ID, EffectiveAt: time.Unix(event.Created, 0).UTC(), SourceFingerprint: digest}

	switch event.Type {
	case "invoice.paid", "invoice.payment_succeeded":
		result.Kind = "payment"
		result.InvoiceID = rawStripeID(event.Data.Object["id"])
		if !stripeRevenueObjectID.MatchString(result.InvoiceID) {
			return nil, ErrPaymentProviderInvalidPayload
		}
		result.Invoice, err = s.LookupRevenueInvoice(ctx, RevenueInvoiceRequest{Scope: scope, InvoiceID: result.InvoiceID})
		if errors.Is(err, ErrRevenueUnassessable) {
			result.QuarantineReason = "invoice_economics_unassessable"
			return result, nil
		}
		if err != nil {
			return nil, err
		}
		result.PaymentID = result.Invoice.PaymentID
		result.EffectiveAt = result.Invoice.PaidAt
	case "charge.refunded", "refund.created", "refund.updated", "refund.failed", "charge.dispute.created", "charge.dispute.updated", "charge.dispute.closed":
		objectID := rawStripeID(event.Data.Object["id"])
		if !stripeRevenueObjectID.MatchString(objectID) {
			return nil, ErrPaymentProviderInvalidPayload
		}
		var object map[string]json.RawMessage
		account, accountErr := s.revenueGet(ctx, scope, "/v1/account")
		if accountErr != nil {
			return nil, accountErr
		}
		if rawStripeID(account["id"]) != scope.AccountID {
			return nil, ErrRevenueUnassessable
		}
		switch {
		case strings.HasPrefix(event.Type, "refund."):
			// Individual refund deliveries do not establish a cumulative line
			// allocation. charge.refunded supplies the immutable cumulative
			// snapshot used below; legacy refund ledger handling remains intact.
			return nil, ErrRevenueEventNotRelevant
		case strings.HasPrefix(event.Type, "charge.dispute."):
			object, err = s.revenueGet(ctx, scope, "/v1/disputes/"+url.PathEscape(objectID))
			if err != nil {
				return nil, err
			}
			if !rawStripeMode(object, scope) || rawStripeID(object["id"]) != objectID {
				return nil, ErrRevenueUnassessable
			}
			result.Kind = "dispute_hold"
			result.AdjustmentID = objectID
			created, known := rawStripeInt(object["created"])
			if !known || created <= 0 {
				return nil, ErrRevenueUnassessable
			}
			result.EffectiveAt = time.Unix(created, 0).UTC()
			result.AffectedMinor, known = rawStripeInt(object["amount"])
			if !known || result.AffectedMinor <= 0 {
				return nil, ErrRevenueUnassessable
			}
			result.Currency = strings.ToUpper(rawStripeString(object["currency"]))
			switch rawStripeString(object["status"]) {
			case "won":
				result.Kind = "dispute_won"
			case "lost":
				result.Kind = "dispute_lost"
			case "needs_response", "under_review":
			default:
				result.QuarantineReason = "dispute_status_unassessable"
			}
		default:
			object, err = s.revenueGet(ctx, scope, "/v1/charges/"+url.PathEscape(objectID))
			if err != nil {
				return nil, err
			}
			result.Kind = "refund"
			result.AdjustmentID = event.ID
			signedAmount, known := rawStripeInt(event.Data.Object["amount_refunded"])
			if !known || signedAmount <= 0 {
				return nil, ErrRevenueUnassessable
			}
			result.CumulativeRefundedGrossMinor = signedAmount
			result.Currency = strings.ToUpper(rawStripeString(event.Data.Object["currency"]))
		}
		charge := object
		if strings.HasPrefix(event.Type, "refund.") || strings.HasPrefix(event.Type, "charge.dispute.") {
			id := rawStripeID(object["charge"])
			if !stripeRevenueObjectID.MatchString(id) {
				result.QuarantineReason = "payment_reference_missing"
				return result, nil
			}
			charge, err = s.revenueGet(ctx, scope, "/v1/charges/"+url.PathEscape(id))
			if err != nil {
				return nil, err
			}
			if rawStripeID(charge["id"]) != id {
				return nil, ErrRevenueUnassessable
			}
		}
		if !rawStripeMode(charge, scope) || rawStripeID(charge["id"]) == "" {
			return nil, ErrRevenueUnassessable
		}
		result.PaymentID = rawStripeID(charge["payment_intent"])
		result.InvoiceID = rawStripeID(charge["invoice"])
		if !stripeRevenueObjectID.MatchString(result.PaymentID) {
			result.QuarantineReason = "payment_reference_missing"
		}
		// Modern charge objects may omit invoice. Billing resolves this only from
		// its original verified payment feed, never customer/email/current access.
	default:
		return nil, ErrRevenueEventNotRelevant
	}
	return result, nil
}

// ReconcileRevenueEvent retrieves the original event through the authenticated
// API, bound to the already accepted source scope. It does not forge a webhook
// signature or trust a browser payload to replace historical provider evidence.
func (s *StripeProvider) ReconcileRevenueEvent(ctx context.Context, scope RevenueScope, eventID string) (*RevenueEvidence, error) {
	if !stripeRevenueObjectID.MatchString(eventID) {
		return nil, ErrRevenueUnassessable
	}
	object, err := s.revenueGet(ctx, scope, "/v1/events/"+url.PathEscape(eventID))
	if err != nil {
		return nil, err
	}
	body, err := json.Marshal(object)
	if err != nil {
		return nil, ErrPaymentProviderAPIResponseInvalid
	}
	var event stripeRevenueEvent
	if json.Unmarshal(body, &event) != nil || event.ID != eventID || event.LiveMode == nil {
		return nil, ErrPaymentProviderAPIResponseInvalid
	}
	resolved, err := s.revenueScope(event.Account, *event.LiveMode)
	if err != nil {
		return nil, err
	}
	if resolved != scope {
		return nil, ErrRevenueUnassessable
	}
	return s.resolveStripeRevenueEvent(ctx, event)
}

func rawStripeString(b json.RawMessage) string { var s string; _ = json.Unmarshal(b, &s); return s }
func rawStripeID(b json.RawMessage) string {
	if s := rawStripeString(b); s != "" {
		return s
	}
	var o map[string]json.RawMessage
	if json.Unmarshal(b, &o) != nil {
		return ""
	}
	return rawStripeString(o["id"])
}
func rawStripeInt(b json.RawMessage) (int64, bool) {
	var n int64
	if len(b) == 0 || string(b) == "null" || json.Unmarshal(b, &n) != nil {
		return 0, false
	}
	return n, true
}
func rawStripeMode(o map[string]json.RawMessage, scope RevenueScope) bool {
	var mode bool
	return len(o["livemode"]) > 0 && string(o["livemode"]) != "null" && json.Unmarshal(o["livemode"], &mode) == nil && mode == scope.LiveMode
}
func revenueMath(values ...int64) (int64, error) {
	sum := new(big.Int)
	for _, n := range values {
		sum.Add(sum, big.NewInt(n))
	}
	if !sum.IsInt64() {
		return 0, ErrRevenueUnassessable
	}
	return sum.Int64(), nil
}

func (s *StripeProvider) revenueGet(ctx context.Context, scope RevenueScope, path string) (map[string]json.RawMessage, error) {
	if ctx == nil {
		return nil, ErrPaymentProviderInvalidPayload
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	checked, err := s.revenueScope(scope.AccountID, scope.LiveMode)
	if err != nil {
		return nil, err
	}
	if checked != scope {
		return nil, ErrRevenueUnassessable
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, s.apiBaseURL+path, nil)
	if err != nil {
		return nil, ErrPaymentProviderAPIRequestFailed
	}
	request.Header.Set("Authorization", "Bearer "+strings.TrimSpace(s.config.APIKey))
	request.Header.Set("Stripe-Version", s.apiVersion)
	request.Header.Set("Accept", "application/json")
	if scope.AccountID != s.config.Revenue.AccountID {
		request.Header.Set("Stripe-Account", scope.AccountID)
	}
	client := *s.httpClient
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("%w: revenue lookup", ErrPaymentProviderAPIRequestFailed)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("%w: revenue status %d", ErrPaymentProviderAPIRequestFailed, response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20+1))
	if err != nil || len(body) > 1<<20 {
		return nil, ErrPaymentProviderAPIResponseInvalid
	}
	var object map[string]json.RawMessage
	if json.Unmarshal(body, &object) != nil || object == nil {
		return nil, ErrPaymentProviderAPIResponseInvalid
	}
	return object, nil
}

// revenueList fetches every page with a bounded complete-evidence limit. It
// never returns a partial list on later-page failure, duplicate/stalled cursors
// or a provider object that does not match the expected collection.
func (s *StripeProvider) revenueList(ctx context.Context, scope RevenueScope, path string) ([]map[string]json.RawMessage, error) {
	var result []map[string]json.RawMessage
	seen := map[string]bool{}
	cursor := ""
	for {
		separator := "?"
		if strings.Contains(path, "?") {
			separator = "&"
		}
		query := url.Values{"limit": {"100"}}
		if cursor != "" {
			query.Set("starting_after", cursor)
		}
		object, err := s.revenueGet(ctx, scope, path+separator+query.Encode())
		if err != nil {
			return nil, err
		}
		var data []map[string]json.RawMessage
		var more *bool
		if rawStripeString(object["object"]) != "list" || json.Unmarshal(object["data"], &data) != nil || json.Unmarshal(object["has_more"], &more) != nil || more == nil {
			return nil, ErrPaymentProviderAPIResponseInvalid
		}
		for _, item := range data {
			id := rawStripeID(item["id"])
			if !stripeRevenueObjectID.MatchString(id) || seen[id] {
				return nil, ErrRevenueUnassessable
			}
			seen[id] = true
			result = append(result, item)
			if len(result) > 200 {
				return nil, ErrRevenueUnassessable
			}
			cursor = id
		}
		if !*more {
			return result, nil
		}
		if len(data) == 0 {
			return nil, ErrRevenueUnassessable
		}
	}
}

func (s *StripeProvider) LookupRevenueInvoice(ctx context.Context, req RevenueInvoiceRequest) (*RevenueInvoiceEvidence, error) {
	if !stripeRevenueObjectID.MatchString(req.InvoiceID) || (req.PaymentID != "" && !stripeRevenueObjectID.MatchString(req.PaymentID)) {
		return nil, ErrRevenueUnassessable
	}
	invoice, err := s.revenueGet(ctx, req.Scope, "/v1/invoices/"+url.PathEscape(req.InvoiceID))
	if err != nil {
		return nil, err
	}
	account, err := s.revenueGet(ctx, req.Scope, "/v1/account")
	if err != nil {
		return nil, err
	}
	if rawStripeID(account["id"]) != req.Scope.AccountID {
		return nil, ErrRevenueUnassessable
	}
	if rawStripeID(invoice["id"]) != req.InvoiceID || !rawStripeMode(invoice, req.Scope) || rawStripeString(invoice["status"]) != "paid" {
		return nil, ErrRevenueUnassessable
	}
	currency := strings.ToUpper(rawStripeString(invoice["currency"]))
	exponent, ok := s.config.Revenue.CurrencyExponents[currency]
	if !ok {
		return nil, ErrRevenueUnassessable
	}
	paid, ok := rawStripeInt(invoice["amount_paid"])
	if !ok || paid <= 0 {
		return nil, ErrRevenueUnassessable
	}
	total, ok := rawStripeInt(invoice["total"])
	if !ok || paid != total {
		return nil, ErrRevenueUnassessable
	}
	net, ok := rawStripeInt(invoice["total_excluding_tax"])
	if !ok || net < 0 || net > paid {
		return nil, ErrRevenueUnassessable
	}
	for _, field := range []string{"starting_balance", "pre_payment_credit_notes_amount", "amount_paid_off_stripe"} {
		if n, present := rawStripeInt(invoice[field]); present && n != 0 {
			return nil, ErrRevenueUnassessable
		}
	}
	if remaining, ok := rawStripeInt(invoice["amount_remaining"]); !ok || remaining != 0 {
		return nil, ErrRevenueUnassessable
	}
	var transitions struct {
		PaidAt int64 `json:"paid_at"`
	}
	if json.Unmarshal(invoice["status_transitions"], &transitions) != nil || transitions.PaidAt <= 0 {
		return nil, ErrRevenueUnassessable
	}
	subscription := rawStripeID(invoice["subscription"])
	if subscription == "" {
		var parent struct {
			SubscriptionDetails struct {
				Subscription json.RawMessage `json:"subscription"`
			} `json:"subscription_details"`
		}
		if json.Unmarshal(invoice["parent"], &parent) == nil {
			subscription = rawStripeID(parent.SubscriptionDetails.Subscription)
		}
	}
	result := &RevenueInvoiceEvidence{Scope: req.Scope, InvoiceID: req.InvoiceID, CustomerID: rawStripeID(invoice["customer"]), SubscriptionID: subscription, Currency: currency, CurrencyExponent: exponent, PaidAt: time.Unix(transitions.PaidAt, 0).UTC(), GrossPaidMinor: paid}
	if !stripeRevenueObjectID.MatchString(result.CustomerID) || !stripeRevenueObjectID.MatchString(subscription) {
		return nil, ErrRevenueUnassessable
	}
	payments, err := s.revenueList(ctx, req.Scope, "/v1/invoice_payments?invoice="+url.QueryEscape(req.InvoiceID))
	if err != nil {
		return nil, err
	}
	var matched int
	for _, payment := range payments {
		if rawStripeString(payment["status"]) != "paid" {
			continue
		}
		value, valid := rawStripeInt(payment["amount_paid"])
		if !valid || value != paid || rawStripeID(payment["invoice"]) != req.InvoiceID || !rawStripeMode(payment, req.Scope) || strings.ToUpper(rawStripeString(payment["currency"])) != currency {
			return nil, ErrRevenueUnassessable
		}
		var reference struct {
			Type          string          `json:"type"`
			PaymentIntent json.RawMessage `json:"payment_intent"`
		}
		if json.Unmarshal(payment["payment"], &reference) != nil || reference.Type != "payment_intent" {
			return nil, ErrRevenueUnassessable
		}
		result.PaymentID = rawStripeID(reference.PaymentIntent)
		matched++
	}
	if matched != 1 || !stripeRevenueObjectID.MatchString(result.PaymentID) || (req.PaymentID != "" && result.PaymentID != req.PaymentID) {
		return nil, ErrRevenueUnassessable
	}
	intent, err := s.revenueGet(ctx, req.Scope, "/v1/payment_intents/"+url.PathEscape(result.PaymentID))
	if err != nil {
		return nil, err
	}
	received, valid := rawStripeInt(intent["amount_received"])
	if !valid || received != paid || rawStripeID(intent["id"]) != result.PaymentID || !rawStripeMode(intent, req.Scope) || rawStripeString(intent["status"]) != "succeeded" || strings.ToUpper(rawStripeString(intent["currency"])) != currency || rawStripeID(intent["customer"]) != result.CustomerID {
		return nil, ErrRevenueUnassessable
	}
	lines, err := s.revenueList(ctx, req.Scope, "/v1/invoices/"+url.PathEscape(req.InvoiceID)+"/lines")
	if err != nil {
		return nil, err
	}
	var netSum int64
	for _, line := range lines {
		if !rawStripeMode(line, req.Scope) || strings.ToUpper(rawStripeString(line["currency"])) != currency {
			return nil, ErrRevenueUnassessable
		}
		value, err := stripeRevenueLineNet(line)
		if err != nil {
			return nil, err
		}
		netSum, err = revenueMath(netSum, value)
		if err != nil {
			return nil, err
		}
		var parent struct {
			SubscriptionItemDetails struct {
				Subscription json.RawMessage `json:"subscription"`
			} `json:"subscription_item_details"`
		}
		_ = json.Unmarshal(line["parent"], &parent)
		lineSub := rawStripeID(parent.SubscriptionItemDetails.Subscription)
		if lineSub == "" {
			lineSub = rawStripeID(line["subscription"])
		}
		var pricing struct {
			PriceDetails struct {
				Price json.RawMessage `json:"price"`
			} `json:"price_details"`
		}
		_ = json.Unmarshal(line["pricing"], &pricing)
		price := rawStripeID(pricing.PriceDetails.Price)
		if price == "" {
			price = rawStripeID(line["price"])
		}
		if lineSub != "" && (lineSub != subscription || !stripeRevenueObjectID.MatchString(price)) {
			return nil, ErrRevenueUnassessable
		}
		result.Lines = append(result.Lines, RevenueLineEvidence{ID: rawStripeID(line["id"]), SubscriptionID: lineSub, PriceID: price, NetPaidMinor: value})
	}
	if req.IncludeRefunds {
		if err := s.populateStripeRevenueRefunds(ctx, req, intent, result); err != nil {
			return nil, err
		}
	}
	if len(lines) == 0 || netSum != net {
		return nil, ErrRevenueUnassessable
	}
	return result, nil
}

func stripeRevenueLineNet(line map[string]json.RawMessage) (int64, error) {
	base, ok := rawStripeInt(line["subtotal"])
	if !ok {
		base, ok = rawStripeInt(line["amount_excluding_tax"])
	}
	if !ok || base < 0 {
		return 0, ErrRevenueUnassessable
	}
	// Modern pretax credits already contain discounts. Subtract one complete
	// representation, never both. Legacy APIs expose discounts separately.
	credits := line["pretax_credit_amounts"]
	if len(credits) == 0 || string(credits) == "null" {
		credits = line["discount_amounts"]
	}
	if len(credits) == 0 {
		return 0, ErrRevenueUnassessable
	}
	var adjustments []struct {
		Amount *int64 `json:"amount"`
	}
	if json.Unmarshal(credits, &adjustments) != nil {
		return 0, ErrRevenueUnassessable
	}
	sum := big.NewInt(base)
	for _, c := range adjustments {
		if c.Amount == nil || *c.Amount < 0 {
			return 0, ErrRevenueUnassessable
		}
		sum.Sub(sum, big.NewInt(*c.Amount))
	}
	if !sum.IsInt64() || sum.Sign() < 0 {
		return 0, ErrRevenueUnassessable
	}
	return sum.Int64(), nil
}
