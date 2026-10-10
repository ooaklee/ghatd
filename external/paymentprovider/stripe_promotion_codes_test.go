package paymentprovider

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStripeCheckoutPromotionCodePolicy(t *testing.T) {
	for _, tc := range []struct {
		name                            string
		mode                            string
		allowed, mismatch, mutateConfig bool
	}{
		{name: "subscription_default_off", mode: CheckoutModeSubscription},
		{name: "subscription_explicit_opt_in", mode: CheckoutModeSubscription, allowed: true},
		{name: "payment_default_off", mode: CheckoutModePayment},
		{name: "payment_explicit_opt_in", mode: CheckoutModePayment, allowed: true},
		{name: "opt_in_does_not_bypass_price_validation", mode: CheckoutModeSubscription, allowed: true, mismatch: true},
		{name: "policy_is_captured_at_construction", mode: CheckoutModeSubscription, allowed: true, mutateConfig: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var priceCalls, checkoutCalls atomic.Int32
			api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "Bearer sk_test_fixture", r.Header.Get("Authorization"))
				assert.Equal(t, StripeDefaultAPIVersion, r.Header.Get("Stripe-Version"))
				w.Header().Set("Content-Type", "application/json")
				switch {
				case r.Method == http.MethodGet && r.URL.Path == "/v1/prices/price_fixture":
					priceCalls.Add(1)
					assert.Empty(t, r.URL.Query().Get("allow_promotion_codes"))
					amount := int64(1000)
					if tc.mismatch {
						amount++
					}
					price := map[string]any{"id": "price_fixture", "active": true, "currency": "gbp", "unit_amount": amount, "type": "one_time"}
					if tc.mode == CheckoutModeSubscription {
						price["type"] = "recurring"
						price["recurring"] = map[string]any{"interval": "month", "interval_count": 1, "usage_type": "licensed"}
					}
					assert.NoError(t, json.NewEncoder(w).Encode(price))
				case r.Method == http.MethodPost && r.URL.Path == "/v1/checkout/sessions":
					checkoutCalls.Add(1)
					if !assert.NoError(t, r.ParseForm()) {
						w.WriteHeader(400)
						return
					}
					if tc.allowed {
						assert.Equal(t, []string{"true"}, r.PostForm["allow_promotion_codes"])
					} else {
						assert.NotContains(t, r.PostForm, "allow_promotion_codes")
					}
					assert.Equal(t, tc.mode, r.PostForm.Get("mode"))
					assert.Equal(t, "embedded_page", r.PostForm.Get("ui_mode"))
					assert.Equal(t, "price_fixture", r.PostForm.Get("line_items[0][price]"))
					assert.Equal(t, "1", r.PostForm.Get("line_items[0][quantity]"))
					assert.Equal(t, "member_fixture", r.PostForm.Get("metadata[user_id]"))
					assert.Equal(t, "plan_fixture", r.PostForm.Get("metadata[plan_id]"))
					assert.Equal(t, "cost_fixture", r.PostForm.Get("metadata[cost_id]"))
					mirror := "payment_intent_data"
					if tc.mode == CheckoutModeSubscription {
						mirror = "subscription_data"
					}
					assert.Equal(t, "member_fixture", r.PostForm.Get(mirror+"[metadata][user_id]"))
					assert.Equal(t, "plan_fixture", r.PostForm.Get(mirror+"[metadata][plan_id]"))
					assert.Equal(t, "attempt_fixture", r.Header.Get("Idempotency-Key"))
					assert.NoError(t, json.NewEncoder(w).Encode(map[string]string{"id": "cs_fixture", "client_secret": "fixture_secret"}))
				default:
					t.Errorf("unexpected Stripe request %s %s", r.Method, r.URL.Path)
					w.WriteHeader(404)
				}
			}))
			t.Cleanup(api.Close)
			config := &Config{WebhookSecret: "whsec_fixture", APIKey: "sk_test_fixture", PublishableKey: "pk_test_fixture", APIBaseURL: api.URL, ReturnURL: "https://app.example.test/complete", HTTPClient: api.Client(), AllowPromotionCodes: tc.allowed}
			provider, err := NewStripeProvider(config)
			require.NoError(t, err)
			if tc.mutateConfig {
				config.AllowPromotionCodes = false
			}
			cadence := string(BillingKindOneTime)
			if tc.mode == CheckoutModeSubscription {
				cadence = "month"
			}
			_, err = provider.CreateCheckoutSession(t.Context(), &CheckoutSessionRequest{PriceID: "price_fixture", PlanID: "plan_fixture", CostID: "cost_fixture", UserReference: "member_fixture", CustomerEmail: "member@example.test", Mode: tc.mode, ExpectedAmount: 1000, ExpectedCurrency: "GBP", ExpectedBillingCadence: cadence, IdempotencyKey: "attempt_fixture"})
			require.Equal(t, int32(1), priceCalls.Load())
			if tc.mismatch {
				require.ErrorIs(t, err, ErrPaymentProviderPriceMismatch)
				require.Zero(t, checkoutCalls.Load())
			} else {
				require.NoError(t, err)
				require.Equal(t, int32(1), checkoutCalls.Load())
			}
		})
	}
}
