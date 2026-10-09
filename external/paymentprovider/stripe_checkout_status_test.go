package paymentprovider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

// Audit disposition: each case uses fresh HTTP fixtures; every request is an
// authenticated GET. No real provider, session creation or payment is invoked.
func TestStripeCheckoutStatusRead(t *testing.T) {
	for _, tc := range []struct {
		name, sessionStatus, paymentStatus, want string
		paymentMode                              bool
		mutate                                   func(map[string]any, map[string]any)
		failure                                  string
		badPage                                  bool
		cancel                                   bool
	}{
		{name: "paid_subscription", sessionStatus: "complete", paymentStatus: "paid", want: "paid"},
		{name: "paid_one_time", sessionStatus: "complete", paymentStatus: "paid", want: "paid", paymentMode: true},
		{name: "unpaid_open", sessionStatus: "open", paymentStatus: "unpaid", want: "unpaid"},
		{name: "async_payment_pending", sessionStatus: "complete", paymentStatus: "unpaid", want: "pending"},
		{name: "trial_is_not_paid", sessionStatus: "complete", paymentStatus: "no_payment_required", want: "no_payment_required"},
		{name: "stripe_paid_zero_trial_is_not_payment", sessionStatus: "complete", paymentStatus: "paid", want: "no_payment_required", mutate: func(s, l map[string]any) { s["amount_total"] = 0 }},
		{name: "unknown_total_is_not_paid", sessionStatus: "complete", paymentStatus: "paid", mutate: func(s, l map[string]any) { delete(s, "amount_total") }},
		{name: "negative_total_is_not_paid", sessionStatus: "complete", paymentStatus: "paid", mutate: func(s, l map[string]any) { s["amount_total"] = -1 }},
		{name: "positive_total_cannot_require_no_payment", sessionStatus: "complete", paymentStatus: "no_payment_required", mutate: func(s, l map[string]any) { s["amount_total"] = 1000 }},
		{name: "expired_unpaid", sessionStatus: "expired", paymentStatus: "unpaid", want: "expired"},
		{name: "open_cannot_be_paid", sessionStatus: "open", paymentStatus: "paid"},
		{name: "missing_payment_status", sessionStatus: "complete"},
		{name: "wrong_session", sessionStatus: "complete", paymentStatus: "paid", mutate: func(s, l map[string]any) { s["id"] = "cs_other" }},
		{name: "wrong_live_mode", sessionStatus: "complete", paymentStatus: "paid", mutate: func(s, l map[string]any) { s["livemode"] = true }},
		{name: "missing_intent", sessionStatus: "complete", paymentStatus: "paid", mutate: func(s, l map[string]any) { delete(s, "metadata") }},
		{name: "line_currency_mismatch", sessionStatus: "complete", paymentStatus: "paid", mutate: func(s, l map[string]any) { l["currency"] = "eur" }},
		{name: "price_mode_mismatch", sessionStatus: "complete", paymentStatus: "paid", mutate: func(s, l map[string]any) { l["price"].(map[string]any)["livemode"] = true }},
		{name: "unsupported_quantity", sessionStatus: "complete", paymentStatus: "paid", mutate: func(s, l map[string]any) { l["quantity"] = 2 }},
		{name: "unknown_original_unit_amount", sessionStatus: "complete", paymentStatus: "paid", mutate: func(s, l map[string]any) { delete(l["price"].(map[string]any), "unit_amount") }},
		{name: "missing_complete_subscription", sessionStatus: "complete", paymentStatus: "paid", mutate: func(s, l map[string]any) { s["subscription"] = nil }},
		{name: "merchant_mismatch", sessionStatus: "complete", paymentStatus: "paid", failure: "merchant"},
		{name: "provider_outage", sessionStatus: "complete", paymentStatus: "paid", failure: "session"},
		{name: "incomplete_page_never_guessed", sessionStatus: "complete", paymentStatus: "paid", badPage: true},
		{name: "cancellation_returns_no_evidence", sessionStatus: "complete", paymentStatus: "paid", cancel: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			session, line := revenueCheckoutSessionFixture(), revenueCheckoutLineFixture()
			session["status"], session["payment_status"] = tc.sessionStatus, tc.paymentStatus
			session["amount_total"] = 1000
			if tc.paymentStatus == "no_payment_required" {
				session["amount_total"] = 0
			}
			if tc.paymentMode {
				session["mode"] = "payment"
				session["subscription"] = nil
				price := line["price"].(map[string]any)
				price["type"] = "one_time"
				delete(price, "recurring")
			}
			if tc.mutate != nil {
				tc.mutate(session, line)
			}
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				require.Equal(t, http.MethodGet, r.Method)
				require.Equal(t, "Bearer sk_fixture", r.Header.Get("Authorization"))
				require.Equal(t, StripeDefaultAPIVersion, r.Header.Get("Stripe-Version"))
				var body any
				switch r.URL.Path {
				case "/v1/account":
					account := "acct_primary"
					if tc.failure == "merchant" {
						account = "acct_other"
					}
					body = map[string]any{"id": account}
				case "/v1/checkout/sessions/cs_original":
					if tc.failure == "session" {
						w.WriteHeader(503)
						return
					}
					body = session
				case "/v1/checkout/sessions/cs_original/line_items":
					body = map[string]any{"object": "list", "data": []any{line}, "has_more": tc.badPage}
					if tc.badPage && r.URL.Query().Get("starting_after") != "" {
						body = map[string]any{"object": "list", "data": []any{}, "has_more": true}
					}
				default:
					t.Errorf("unexpected provider operation: %s", r.URL.Path)
					w.WriteHeader(500)
					return
				}
				require.NoError(t, json.NewEncoder(w).Encode(body))
			}))
			defer server.Close()
			provider, err := NewStripeProvider(&Config{APIKey: "sk_fixture", WebhookSecret: "whsec_fixture", APIBaseURL: server.URL, Revenue: &RevenueConfig{AccountID: "acct_primary", CurrencyExponents: map[string]int{"GBP": 2}}})
			require.NoError(t, err)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if tc.cancel {
				cancel()
			}
			out, err := provider.LookupCheckoutStatus(ctx, RevenueScope{Provider: "stripe", AccountID: "acct_primary"}, "cs_original")
			if tc.want == "" {
				require.Error(t, err)
				require.Equal(t, CheckoutStatusEvidence{}, out)
				if tc.cancel {
					require.Zero(t, calls)
				}
				return
			}
			require.NoError(t, err)
			state, err := out.State()
			require.NoError(t, err)
			require.Equal(t, tc.want, state)
			require.Equal(t, "price_frozen", out.PriceID)
			require.Equal(t, int64(1000), out.UnitAmountMinor)
			require.Equal(t, 3, calls)
		})
	}
}
