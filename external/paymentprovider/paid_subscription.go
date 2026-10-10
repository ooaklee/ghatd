package paymentprovider

import (
	"context"
	"encoding/json"
	"maps"
	"net/url"
	"strings"
	"time"
)

// PaidSubscriptionProvider proves current subscription payment independently
// of access flags or commission ingestion. Implementations authenticate merchant,
// mode, customer, subscription, payment and service-period relationships.
type PaidSubscriptionProvider interface {
	// LookupPaidSubscription returns assessed evidence; a valid result with no
	// PaidLines does not establish paid access. Unknown evidence remains an error.
	LookupPaidSubscription(context.Context, RevenueScope, string) (PaidSubscriptionEvidence, error)
}

// PaidSubscriptionEvidence is server-only evidence for a host's admission rule.
// It grants no entitlement by itself; the host binds the owning customer and
// allowed plan/price and checks that a proven paid period contains its clock.
type PaidSubscriptionEvidence struct {
	// Scope confines all retrieved objects to the authenticated merchant and mode.
	Scope RevenueScope `json:"-"`
	// SubscriptionID and CustomerID identify the exact provider-owned relationship.
	SubscriptionID, CustomerID string `json:"-"`
	// Status is the current lifecycle state; scheduled cancellation can remain active.
	Status string `json:"-"`
	// PaidAt is the authenticated payment instant, never an admission timestamp.
	PaidAt time.Time `json:"-"`
	// PaidLines contains current recurring service periods with positive net paid.
	PaidLines []PaidSubscriptionLine `json:"-"`
}

// PaidSubscriptionLine binds retained net payment to one current recurring
// price and period. A fully refunded or disputed payment cannot supply a line.
type PaidSubscriptionLine struct {
	// PriceID identifies the provider price that the host maps to an eligible plan.
	PriceID string `json:"-"`
	// Interval and IntervalCount preserve the provider's recurring cadence.
	Interval      string `json:"-"`
	IntervalCount int64  `json:"-"`
	// PeriodStart is inclusive and PeriodEnd exclusive, in UTC.
	PeriodStart, PeriodEnd time.Time `json:"-"`
	// NetPaidMinor excludes tax, discounts and verified allocated refunds.
	NetPaidMinor int64 `json:"-"`
}

// LookupPaidSubscription intersects current Stripe lifecycle with the latest
// authenticated paid invoice and its service periods. Trials, paused collection,
// unpaid/zero invoices and disputed/full-refunded charges return no paid lines.
// Incomplete/malformed evidence remains unavailable rather than inferred access.
func (s *StripeProvider) LookupPaidSubscription(ctx context.Context, scope RevenueScope, id string) (PaidSubscriptionEvidence, error) {
	lifecycle, err := s.LookupRevenueSubscription(ctx, scope, id)
	if err != nil {
		return PaidSubscriptionEvidence{}, err
	}
	out := PaidSubscriptionEvidence{Scope: scope, SubscriptionID: id, CustomerID: lifecycle.CustomerID, Status: lifecycle.Status}
	if lifecycle.Status != "active" {
		return out, nil
	}
	object, err := s.revenueGet(ctx, scope, "/v1/subscriptions/"+url.PathEscape(id))
	if err != nil {
		return PaidSubscriptionEvidence{}, err
	}
	if rawStripeString(object["object"]) != "subscription" || rawStripeID(object["id"]) != id || rawStripeID(object["customer"]) != out.CustomerID || !rawStripeMode(object, scope) || rawStripeString(object["status"]) != out.Status {
		return PaidSubscriptionEvidence{}, ErrRevenueUnassessable
	}
	paused, err := paidStripeCollectionPaused(object)
	if err != nil {
		return PaidSubscriptionEvidence{}, err
	}
	if paused {
		return out, nil
	}
	invoiceID := rawStripeID(object["latest_invoice"])
	if string(object["latest_invoice"]) == "null" {
		return out, nil
	}
	if !strings.HasPrefix(invoiceID, "in_") || !stripeRevenueObjectID.MatchString(invoiceID) {
		return PaidSubscriptionEvidence{}, ErrRevenueUnassessable
	}
	items, err := paidStripeSubscriptionItems(object)
	if err != nil {
		return PaidSubscriptionEvidence{}, err
	}
	invoice, err := s.revenueGet(ctx, scope, "/v1/invoices/"+url.PathEscape(invoiceID))
	if err != nil {
		return PaidSubscriptionEvidence{}, err
	}
	if rawStripeID(invoice["id"]) != invoiceID || rawStripeID(invoice["customer"]) != out.CustomerID || !rawStripeMode(invoice, scope) {
		return PaidSubscriptionEvidence{}, ErrRevenueUnassessable
	}
	paid, known := rawStripeInt(invoice["amount_paid"])
	status := rawStripeString(invoice["status"])
	if !known || paid < 0 {
		return PaidSubscriptionEvidence{}, ErrRevenueUnassessable
	}
	switch status {
	case "draft", "open", "void", "uncollectible":
		return out, nil
	case "paid":
		if paid == 0 {
			return out, nil
		}
	default:
		return PaidSubscriptionEvidence{}, ErrRevenueUnassessable
	}
	proof, err := s.LookupRevenueInvoice(ctx, RevenueInvoiceRequest{Scope: scope, InvoiceID: invoiceID})
	if err != nil {
		return PaidSubscriptionEvidence{}, err
	}
	if proof.Scope != scope || proof.SubscriptionID != id || proof.CustomerID != out.CustomerID {
		return PaidSubscriptionEvidence{}, ErrRevenueUnassessable
	}
	out.PaidAt = proof.PaidAt
	intent, err := s.revenueGet(ctx, scope, "/v1/payment_intents/"+url.PathEscape(proof.PaymentID))
	if err != nil {
		return PaidSubscriptionEvidence{}, err
	}
	chargeID := rawStripeID(intent["latest_charge"])
	if rawStripeID(intent["id"]) != proof.PaymentID || !rawStripeMode(intent, scope) || !strings.HasPrefix(chargeID, "ch_") || !stripeRevenueObjectID.MatchString(chargeID) {
		return PaidSubscriptionEvidence{}, ErrRevenueUnassessable
	}
	charge, err := s.revenueGet(ctx, scope, "/v1/charges/"+url.PathEscape(chargeID))
	if err != nil {
		return PaidSubscriptionEvidence{}, err
	}
	gross, grossKnown := rawStripeInt(charge["amount"])
	refunded, refundKnown := rawStripeInt(charge["amount_refunded"])
	var disputed, chargePaid *bool
	if rawStripeID(charge["id"]) != chargeID || rawStripeID(charge["payment_intent"]) != proof.PaymentID || rawStripeID(charge["customer"]) != out.CustomerID || !rawStripeMode(charge, scope) || strings.ToUpper(rawStripeString(charge["currency"])) != proof.Currency || !grossKnown || gross != proof.GrossPaidMinor || !refundKnown || refunded < 0 || refunded > gross || json.Unmarshal(charge["disputed"], &disputed) != nil || disputed == nil || json.Unmarshal(charge["paid"], &chargePaid) != nil || chargePaid == nil {
		return PaidSubscriptionEvidence{}, ErrRevenueUnassessable
	}
	if *disputed || !*chargePaid || refunded == gross {
		return out, nil
	}
	if refunded > 0 {
		proof, err = s.LookupRevenueInvoice(ctx, RevenueInvoiceRequest{Scope: scope, InvoiceID: invoiceID, PaymentID: proof.PaymentID, IncludeRefunds: true, ExpectedCumulativeRefundedGrossMinor: refunded})
		if err != nil {
			return PaidSubscriptionEvidence{}, err
		}
		if proof.Scope != scope || proof.SubscriptionID != id || proof.CustomerID != out.CustomerID {
			return PaidSubscriptionEvidence{}, ErrRevenueUnassessable
		}
	}
	for _, line := range proof.Lines {
		item, exists := items[line.PriceID]
		if line.SubscriptionID != id || !exists {
			continue
		}
		if line.PeriodStart.IsZero() || line.PeriodEnd.IsZero() || line.PeriodStart.Before(item.PeriodStart) || !line.PeriodEnd.Equal(item.PeriodEnd) || !line.PeriodStart.Before(line.PeriodEnd) || line.CumulativeRefundedMinor < 0 || line.CumulativeRefundedMinor > line.NetPaidMinor {
			return PaidSubscriptionEvidence{}, ErrRevenueUnassessable
		}
		if net := line.NetPaidMinor - line.CumulativeRefundedMinor; net > 0 {
			item.PeriodStart, item.NetPaidMinor = line.PeriodStart, net
			out.PaidLines = append(out.PaidLines, item)
		}
	}
	// Long invoice/refund reads must not return an earlier active snapshot after
	// cancellation or a plan/invoice change. No admission decision is cached.
	current, err := s.revenueGet(ctx, scope, "/v1/subscriptions/"+url.PathEscape(id))
	if err != nil {
		return PaidSubscriptionEvidence{}, err
	}
	if rawStripeString(current["object"]) != "subscription" || rawStripeID(current["id"]) != id || rawStripeID(current["customer"]) != out.CustomerID || !rawStripeMode(current, scope) || !validSubscriptionStatus(rawStripeString(current["status"])) {
		return PaidSubscriptionEvidence{}, ErrRevenueUnassessable
	}
	if rawStripeString(current["status"]) != "active" {
		out.Status = rawStripeString(current["status"])
		out.PaidLines = nil
		return out, nil
	}
	paused, err = paidStripeCollectionPaused(current)
	if err != nil {
		return PaidSubscriptionEvidence{}, err
	}
	if paused {
		out.PaidLines = nil
		return out, nil
	}
	currentItems, err := paidStripeSubscriptionItems(current)
	if err != nil || rawStripeID(current["latest_invoice"]) != invoiceID || !maps.Equal(items, currentItems) {
		return PaidSubscriptionEvidence{}, ErrRevenueUnassessable
	}
	if err := ctx.Err(); err != nil {
		return PaidSubscriptionEvidence{}, err
	}
	return out, nil
}

// paidStripeCollectionPaused distinguishes a verified collection pause from
// missing or malformed lifecycle evidence; only explicit null means unpaused.
func paidStripeCollectionPaused(object map[string]json.RawMessage) (bool, error) {
	raw, exists := object["pause_collection"]
	if !exists {
		return false, ErrRevenueUnassessable
	}
	if string(raw) == "null" {
		return false, nil
	}
	var pause struct{ Behavior string }
	if json.Unmarshal(raw, &pause) != nil {
		return false, ErrRevenueUnassessable
	}
	switch pause.Behavior {
	case "keep_as_draft", "mark_uncollectible", "void":
		return true, nil
	default:
		return false, ErrRevenueUnassessable
	}
}

// paidStripeSubscriptionItems accepts a complete bounded item list and exact
// recurring periods. It rejects duplicate prices and uncollected pagination.
func paidStripeSubscriptionItems(object map[string]json.RawMessage) (map[string]PaidSubscriptionLine, error) {
	var list struct {
		Object  string
		HasMore *bool `json:"has_more"`
		Data    []map[string]json.RawMessage
	}
	if json.Unmarshal(object["items"], &list) != nil || list.Object != "list" || list.HasMore == nil || *list.HasMore || len(list.Data) == 0 || len(list.Data) > 100 {
		return nil, ErrRevenueUnassessable
	}
	out := make(map[string]PaidSubscriptionLine, len(list.Data))
	for _, item := range list.Data {
		var price struct {
			ID        string
			Recurring struct {
				Interval string
				Count    int64 `json:"interval_count"`
			}
		}
		if json.Unmarshal(item["price"], &price) != nil || !strings.HasPrefix(price.ID, "price_") || !stripeRevenueObjectID.MatchString(price.ID) || price.Recurring.Count <= 0 {
			return nil, ErrRevenueUnassessable
		}
		start, a := rawStripeInt(item["current_period_start"])
		end, b := rawStripeInt(item["current_period_end"])
		if !a && !b && len(list.Data) == 1 {
			start, a = rawStripeInt(object["current_period_start"])
			end, b = rawStripeInt(object["current_period_end"])
		}
		if !a || !b || start <= 0 || end <= start {
			return nil, ErrRevenueUnassessable
		}
		switch price.Recurring.Interval {
		case "day", "week", "month", "year":
		default:
			return nil, ErrRevenueUnassessable
		}
		if _, duplicate := out[price.ID]; duplicate {
			return nil, ErrRevenueUnassessable
		}
		out[price.ID] = PaidSubscriptionLine{PriceID: price.ID, Interval: price.Recurring.Interval, IntervalCount: price.Recurring.Count, PeriodStart: time.Unix(start, 0).UTC(), PeriodEnd: time.Unix(end, 0).UTC()}
	}
	return out, nil
}

var _ PaidSubscriptionProvider = (*StripeProvider)(nil)
