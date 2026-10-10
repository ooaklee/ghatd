package helpers

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func paidPeriodFixture(modern bool) (map[string]any, map[string]any, map[string]any, StripePaidServicePeriodConfig) {
	line := map[string]any{"quantity": 1, "period": map[string]any{"start": int64(1791288000), "end": int64(1791892800)}, "price": "price_expected"}
	invoice := map[string]any{"id": "in_paid", "object": "invoice", "status": "paid", "currency": "gbp", "customer": "cus_expected", "subscription": "sub_expected", "billing_reason": "subscription_cycle", "lines": map[string]any{"has_more": false, "data": []any{line}}}
	if modern {
		delete(line, "price")
		line["pricing"] = map[string]any{"price_details": map[string]any{"price": map[string]any{"id": "price_expected"}}}
		line["parent"] = map[string]any{"subscription_item_details": map[string]any{"subscription": map[string]any{"id": "sub_expected"}, "proration": false}}
		delete(invoice, "subscription")
		invoice["parent"] = map[string]any{"subscription_details": map[string]any{"subscription": map[string]any{"id": "sub_expected"}}}
		invoice["customer"] = map[string]any{"id": "cus_expected"}
	}
	event := map[string]any{"id": "evt_paid", "type": "invoice.paid", "livemode": false, "data": map[string]any{"object": invoice}}
	cfg := StripePaidServicePeriodConfig{ExpectedEventID: "evt_paid", SubscriptionID: "sub_expected", CustomerID: "cus_expected", PriceID: "price_expected", Currency: "GBP", Quantity: 1}
	return event, invoice, line, cfg
}
func TestParseStripePaidServicePeriod(t *testing.T) {
	type evidenceCase struct {
		name, eventField, invoiceField, lineField, special string
		value                                              any
		modern, wantError                                  bool
		liveMode                                           bool
		quantity                                           int
	}
	for _, test := range []evidenceCase{
		{name: "legacy IDs"}, {name: "modern expanded IDs", modern: true}, {name: "alias event", eventField: "type", value: "invoice.payment_succeeded"},
		{name: "initial period", invoiceField: "billing_reason", value: "subscription_create"}, {name: "explicit paid", invoiceField: "paid", value: true},
		{name: "trusted live mode", liveMode: true}, {name: "configured quantity", quantity: 2},
		{name: "wrong event", eventField: "type", value: "checkout.session.completed", wantError: true},
		{name: "wrong event ID", eventField: "id", value: "evt_other", wantError: true},
		{name: "wrong mode", eventField: "livemode", value: true, wantError: true},
		{name: "wrong object", invoiceField: "object", value: "payment_intent", wantError: true},
		{name: "wrong invoice prefix", invoiceField: "id", value: "pi_other", wantError: true},
		{name: "unpaid status", invoiceField: "status", value: "open", wantError: true},
		{name: "explicit unpaid", invoiceField: "paid", value: false, wantError: true},
		{name: "wrong currency", invoiceField: "currency", value: "usd", wantError: true},
		{name: "wrong customer", invoiceField: "customer", value: "cus_other", wantError: true},
		{name: "wrong subscription", invoiceField: "subscription", value: "sub_other", wantError: true},
		{name: "wrong price", lineField: "price", value: "price_other", wantError: true},
		{name: "wrong quantity", lineField: "quantity", value: 2, wantError: true},
		{name: "line proration", lineField: "proration", value: true, wantError: true},
		{name: "update invoice", invoiceField: "billing_reason", value: "subscription_update", wantError: true},
		{name: "truncated lines", special: "truncated", wantError: true}, {name: "multiple lines", special: "multiple", wantError: true},
		{name: "empty lines", special: "empty", wantError: true}, {name: "parent proration", special: "parent proration", modern: true, wantError: true},
		{name: "wrong parent subscription", special: "parent subscription", modern: true, wantError: true},
		{name: "legacy subscription wins", invoiceField: "subscription", value: "sub_other", modern: true, wantError: true},
		{name: "legacy price wins", lineField: "price", value: "price_other", modern: true, wantError: true},
		{name: "zero period start", special: "zero start", wantError: true}, {name: "equal period ends", special: "equal ends", wantError: true},
		{name: "negative period start", special: "negative start", wantError: true},
		{name: "reversed period", special: "reversed", wantError: true}, {name: "malformed payload", special: "malformed", wantError: true},
		{name: "nil IDs are not evidence", invoiceField: "customer", value: nil, wantError: true},
		{name: "malformed ID value", invoiceField: "customer", value: true, wantError: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			event, invoice, line, cfg := paidPeriodFixture(test.modern)
			if test.liveMode {
				cfg.LiveMode = true
				event["livemode"] = true
			}
			if test.quantity > 0 {
				cfg.Quantity = test.quantity
				line["quantity"] = test.quantity
			}
			if test.eventField != "" {
				event[test.eventField] = test.value
			}
			if test.invoiceField != "" {
				invoice[test.invoiceField] = test.value
			}
			if test.lineField != "" {
				line[test.lineField] = test.value
			}
			lines := invoice["lines"].(map[string]any)
			period := line["period"].(map[string]any)
			switch test.special {
			case "truncated":
				lines["has_more"] = true
			case "multiple":
				lines["data"] = []any{line, line}
			case "empty":
				lines["data"] = []any{}
			case "parent proration":
				line["parent"].(map[string]any)["subscription_item_details"].(map[string]any)["proration"] = true
			case "parent subscription":
				line["parent"].(map[string]any)["subscription_item_details"].(map[string]any)["subscription"] = "sub_other"
			case "zero start":
				period["start"] = 0
			case "negative start":
				period["start"] = -1
			case "equal ends":
				period["end"] = period["start"]
			case "reversed":
				period["end"] = period["start"].(int64) - 1
			}
			raw, err := json.Marshal(event)
			require.NoError(t, err)
			if test.special == "malformed" {
				raw = []byte("private malformed evidence {")
			}
			actual, err := ParseStripePaidServicePeriod(raw, cfg)
			if test.wantError {
				require.ErrorIs(t, err, ErrStripePaidServicePeriodInvalid)
				require.Equal(t, StripePaidServicePeriod{}, actual)
				require.NotContains(t, err.Error(), "private malformed")
				return
			}
			require.NoError(t, err)
			require.Equal(t, StripePaidServicePeriod{InvoiceID: "in_paid", SubscriptionID: "sub_expected", CustomerID: "cus_expected", PriceID: "price_expected", Currency: "gbp", Quantity: cfg.Quantity, StartsAt: time.Unix(1791288000, 0).UTC(), ExpiresAt: time.Unix(1791892800, 0).UTC()}, actual)
		})
	}
}
func TestStripePaidServicePeriodIncompleteExpectations(t *testing.T) {
	type expectationCase struct{ name, field string }
	for _, test := range []expectationCase{{"event ID", "event"}, {"subscription", "subscription"}, {"customer", "customer"}, {"price", "price"}, {"currency", "currency"}, {"quantity", "quantity"}, {"negative quantity", "negative quantity"}} {
		t.Run(test.name, func(t *testing.T) {
			event, _, _, cfg := paidPeriodFixture(false)
			switch test.field {
			case "event":
				cfg.ExpectedEventID = ""
			case "subscription":
				cfg.SubscriptionID = ""
			case "customer":
				cfg.CustomerID = ""
			case "price":
				cfg.PriceID = ""
			case "currency":
				cfg.Currency = ""
			case "quantity":
				cfg.Quantity = 0
			case "negative quantity":
				cfg.Quantity = -1
			}
			raw, err := json.Marshal(event)
			require.NoError(t, err)
			actual, err := ParseStripePaidServicePeriod(raw, cfg)
			require.ErrorIs(t, err, ErrStripePaidServicePeriodConfigInvalid)
			require.Equal(t, StripePaidServicePeriod{}, actual)
			require.False(t, strings.Contains(err.Error(), "expected"))
		})
	}
}
