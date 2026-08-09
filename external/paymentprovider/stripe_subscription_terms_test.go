package paymentprovider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestStripeSubscriptionLifecycleParsesLiveCommercialTermsShape(t *testing.T) {
	body := `{"id":"evt_subscription","type":"customer.subscription.created","created":1700000000,"data":{"object":{"id":"sub_1","customer":"cus_1","status":"trialing","currency":"usd","trial_end":1701209600,"metadata":{"user_reference":"user_1","plan_id":"plan_1","cost_id":"cost_1","provider_price_id":"price_1"},"items":{"data":[{"quantity":1,"current_period_end":1701209600,"price":{"id":"price_1","object":"price","active":true,"billing_scheme":"per_unit","currency":"usd","type":"recurring","unit_amount":1800,"unit_amount_decimal":"1800","recurring":{"interval":"month","interval_count":1,"usage_type":"licensed"}}}]}}}}`
	provider, err := NewStripeProvider(&Config{WebhookSecret: "whsec_test"})
	if err != nil {
		t.Fatal(err)
	}
	payload, err := provider.ParsePayload(context.Background(), httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(body)))
	if err != nil {
		t.Fatalf("ParsePayload() error = %v", err)
	}
	terms := payload.SubscriptionTerms
	if !payload.SubscriptionStateAuthoritative || !terms.Observed || terms.Amount == nil || *terms.Amount != 1800 {
		t.Fatalf("commercial terms = %#v, payload = %#v", terms, payload)
	}
	if terms.Currency == nil || *terms.Currency != "USD" || terms.BillingInterval == nil || *terms.BillingInterval != "month" ||
		terms.BillingIntervalCount == nil || *terms.BillingIntervalCount != 1 || terms.Quantity == nil || *terms.Quantity != 1 {
		t.Fatalf("commercial cadence = %#v", terms)
	}
	if terms.ProviderPriceID == nil || *terms.ProviderPriceID != "price_1" || payload.ProviderPriceID != "price_1" {
		t.Fatalf("price identity = %#v, payload price = %q", terms, payload.ProviderPriceID)
	}
	if payload.Amount != 0 || payload.Currency != "USD" {
		t.Fatalf("subscription lifecycle fabricated ledger total: amount=%d currency=%q", payload.Amount, payload.Currency)
	}
	if payload.TrialEndsAt != stripeUnixDate(1701209600) {
		t.Fatalf("TrialEndsAt = %q", payload.TrialEndsAt)
	}
}

func TestStripeSubscriptionTermsResolveKnownFreeQuantityAndExactMetadataMatch(t *testing.T) {
	tests := []struct {
		name       string
		object     map[string]any
		metadata   map[string]any
		wantAmount *int64
		wantPrice  string
		observed   bool
	}{
		{
			name:       "known free",
			object:     stripeSubscriptionObject(stripeSubscriptionItem("price_free", 0, "0", 1, "licensed", "per_unit")),
			metadata:   map[string]any{"provider_price_id": "price_free"},
			wantAmount: int64Pointer(0), wantPrice: "price_free", observed: true,
		},
		{
			name:       "licensed quantity",
			object:     stripeSubscriptionObject(stripeSubscriptionItem("price_team", 1800, "1800.000000000000", 3, "licensed", "per_unit")),
			metadata:   map[string]any{"provider_price_id": "price_team"},
			wantAmount: int64Pointer(5400), wantPrice: "price_team", observed: true,
		},
		{
			name: "matching item among many",
			object: stripeSubscriptionObject(
				stripeSubscriptionItem("price_addon", 500, "500", 1, "licensed", "per_unit"),
				stripeSubscriptionItem("price_main", 1800, "1800", 1, "licensed", "per_unit"),
			),
			metadata:   map[string]any{"provider_price_id": "price_main"},
			wantAmount: int64Pointer(1800), wantPrice: "price_main", observed: true,
		},
		{
			name:       "sole item without preferred price",
			object:     stripeSubscriptionObject(stripeSubscriptionItem("price_single", 900, "900", 1, "licensed", "per_unit")),
			wantAmount: int64Pointer(900), wantPrice: "price_single", observed: true,
		},
		{
			name:       "preferred price mismatch fails closed",
			object:     stripeSubscriptionObject(stripeSubscriptionItem("price_new", 2400, "2400", 1, "licensed", "per_unit")),
			metadata:   map[string]any{"provider_price_id": "price_old"},
			wantAmount: nil, observed: true,
		},
		{
			name: "ambiguous items fail closed",
			object: stripeSubscriptionObject(
				stripeSubscriptionItem("price_a", 100, "100", 1, "licensed", "per_unit"),
				stripeSubscriptionItem("price_b", 200, "200", 1, "licensed", "per_unit"),
			),
			wantAmount: nil, observed: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			terms := stripeSubscriptionTerms(test.object, test.metadata)
			if terms.Observed != test.observed {
				t.Fatalf("Observed = %v, want %v", terms.Observed, test.observed)
			}
			if test.wantAmount == nil {
				if terms.Amount != nil {
					t.Fatalf("Amount = %d, want unknown", *terms.Amount)
				}
			} else if terms.Amount == nil || *terms.Amount != *test.wantAmount {
				t.Fatalf("Amount = %v, want %d", terms.Amount, *test.wantAmount)
			}
			if test.wantPrice != "" && (terms.ProviderPriceID == nil || *terms.ProviderPriceID != test.wantPrice) {
				t.Fatalf("ProviderPriceID = %v, want %q", terms.ProviderPriceID, test.wantPrice)
			}
		})
	}
}

func TestStripeSubscriptionTermsRejectUnsupportedAmounts(t *testing.T) {
	tests := []struct {
		name string
		item map[string]any
	}{
		{name: "metered", item: stripeSubscriptionItem("price_1", 1800, "1800", 1, "metered", "per_unit")},
		{name: "tiered", item: stripeSubscriptionItem("price_1", 1800, "1800", 1, "licensed", "tiered")},
		{name: "fractional decimal", item: stripeSubscriptionItem("price_1", 1800, "1800.5", 1, "licensed", "per_unit")},
		{name: "decimal mismatch", item: stripeSubscriptionItem("price_1", 1800, "1801", 1, "licensed", "per_unit")},
		{name: "missing quantity", item: func() map[string]any {
			item := stripeSubscriptionItem("price_1", 1800, "1800", 1, "licensed", "per_unit")
			delete(item, "quantity")
			return item
		}()},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			terms := stripeSubscriptionTerms(stripeSubscriptionObject(test.item), map[string]any{"provider_price_id": "price_1"})
			if !terms.Observed || terms.Amount != nil {
				t.Fatalf("terms = %#v, want observed unknown amount", terms)
			}
		})
	}
}

func TestStripePaidTrialCheckoutRemainsTrialing(t *testing.T) {
	body := `{"id":"evt_trial_checkout","type":"checkout.session.completed","created":1700000000,"data":{"object":{"id":"cs_1","mode":"subscription","subscription":"sub_1","payment_status":"paid","amount_total":0,"currency":"usd","client_reference_id":"user_1","metadata":{"billing_kind":"recurring","trial_period_days":"14","plan_id":"plan_1","cost_id":"cost_1","provider_price_id":"price_1"}}}}`
	provider, err := NewStripeProvider(&Config{WebhookSecret: "whsec_test"})
	if err != nil {
		t.Fatal(err)
	}
	payload, err := provider.ParsePayload(context.Background(), httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(body)))
	if err != nil {
		t.Fatal(err)
	}
	if payload.Status != SubscriptionStatusTrialing || payload.PaymentStatus != PaymentStatusSucceeded || payload.SubscriptionStateAuthoritative {
		t.Fatalf("paid trial checkout = %#v", payload)
	}
}

func stripeSubscriptionObject(items ...map[string]any) map[string]any {
	data := make([]any, len(items))
	for index := range items {
		data[index] = items[index]
	}
	return map[string]any{"items": map[string]any{"data": data}}
}

func stripeSubscriptionItem(priceID string, unitAmount int64, decimal string, quantity int64, usageType, billingScheme string) map[string]any {
	return map[string]any{
		"quantity": quantity,
		"price": map[string]any{
			"id": priceID, "unit_amount": unitAmount, "unit_amount_decimal": decimal,
			"currency": "usd", "billing_scheme": billingScheme,
			"recurring": map[string]any{"interval": "month", "interval_count": int64(1), "usage_type": usageType},
		},
	}
}

func int64Pointer(value int64) *int64 { return &value }
