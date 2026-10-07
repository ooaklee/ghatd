package paymentprovider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Audit disposition: named authenticated HTTP cases cover optional checkout
// evidence and complete collection reads; no request reaches a real provider.
func revenueCheckoutSessionFixture() map[string]any {
	return map[string]any{
		"id": "cs_original", "object": "checkout.session", "livemode": false, "subscription": "sub_recurring", "customer": "cus_payer", "mode": "subscription", "status": "complete", "currency": "gbp", "client_reference_id": "owning_principal", "created": int64(1700000000), "metadata": map[string]any{"checkout_intent_id": "checkout_local", "plan_id": "untrusted_provider_plan"}, "client_secret": "fixture_secret",
	}
}
func revenueCheckoutLineFixture() map[string]any {
	return map[string]any{
		"id": "li_original", "object": "item", "quantity": int64(1), "currency": "gbp", "price": map[string]any{"id": "price_frozen", "object": "price", "livemode": false, "type": "recurring", "currency": "gbp", "unit_amount": int64(1000), "recurring": map[string]any{"interval": "month", "interval_count": int64(1)}},
	}
}
func TestStripeHistoricalRevenueCheckoutEvidence(t *testing.T) {
	type testCase struct {
		name         string
		session      func(map[string]any)
		line         func(map[string]any)
		account      string
		sessionCount int
		failure      string
		want         error
	}
	cases := []testCase{
		{name: "complete_authenticated_original_checkout", sessionCount: 1},
		{name: "missing_server_intent_pointer_is_legacy_unassessable", sessionCount: 1, session: func(s map[string]any) { delete(s, "metadata") }, want: ErrRevenueUnassessable},
		{name: "session_live_mode_does_not_match", sessionCount: 1, session: func(s map[string]any) { s["livemode"] = true }, want: ErrRevenueUnassessable},
		{name: "missing_mode_does_not_mean_test", sessionCount: 1, session: func(s map[string]any) { delete(s, "livemode") }, want: ErrRevenueUnassessable},
		{name: "wrong_subscription_in_filtered_response", sessionCount: 1, session: func(s map[string]any) { s["subscription"] = "sub_other" }, want: ErrRevenueUnassessable},
		{name: "session_not_complete", sessionCount: 1, session: func(s map[string]any) { s["status"] = "open" }, want: ErrRevenueUnassessable},
		{name: "multiple_sessions_not_guessed", sessionCount: 2, want: ErrRevenueUnassessable},
		{name: "no_session_is_not_email_joined", want: ErrRevenueUnassessable},
		{name: "original_line_currency_mismatch", sessionCount: 1, line: func(l map[string]any) { l["currency"] = "eur" }, want: ErrRevenueUnassessable},
		{name: "quantity_more_than_one_needs_explicit_allocation", sessionCount: 1, line: func(l map[string]any) { l["quantity"] = int64(2) }, want: ErrRevenueUnassessable},
		{name: "original_price_mode_mismatch", sessionCount: 1, line: func(l map[string]any) { l["price"].(map[string]any)["livemode"] = true }, want: ErrRevenueUnassessable},
		{name: "missing_original_unit_amount", sessionCount: 1, line: func(l map[string]any) { delete(l["price"].(map[string]any), "unit_amount") }, want: ErrRevenueUnassessable},
		{name: "multi_interval_original_price_not_assumed", sessionCount: 1, line: func(l map[string]any) {
			l["price"].(map[string]any)["recurring"].(map[string]any)["interval_count"] = int64(2)
		}, want: ErrRevenueUnassessable},
		{name: "authenticated_merchant_mismatch", sessionCount: 1, account: "acct_other", want: ErrRevenueUnassessable},
		{name: "collection_outage_remains_retryable", sessionCount: 1, failure: "/v1/checkout/sessions", want: ErrPaymentProviderAPIRequestFailed},
		{name: "original_lines_outage_remains_retryable", sessionCount: 1, failure: "/v1/checkout/sessions/cs_original/line_items", want: ErrPaymentProviderAPIRequestFailed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			session := revenueCheckoutSessionFixture()
			line := revenueCheckoutLineFixture()
			if tc.session != nil {
				tc.session(session)
			}
			if tc.line != nil {
				tc.line(line)
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if !assert.Equal(t, "Bearer sk_fixture", r.Header.Get("Authorization")) {
					return
				}
				if !assert.Equal(t, StripeDefaultAPIVersion, r.Header.Get("Stripe-Version")) {
					return
				}
				if !assert.Empty(t, r.Header.Get("Stripe-Account")) {
					return
				}
				if r.URL.Path == tc.failure {
					w.WriteHeader(http.StatusServiceUnavailable)
					return
				}
				var response any
				switch r.URL.Path {
				case "/v1/account":
					account := tc.account
					if account == "" {
						account = "acct_primary"
					}
					response = map[string]any{"id": account}
				case "/v1/checkout/sessions":
					if !assert.Equal(t, "sub_recurring", r.URL.Query().Get("subscription")) {
						return
					}
					if !assert.Equal(t, "100", r.URL.Query().Get("limit")) {
						return
					}
					data := []any{}
					for i := 0; i < tc.sessionCount; i++ {
						value := map[string]any{}
						for k, v := range session {
							value[k] = v
						}
						if i > 0 {
							value["id"] = "cs_second"
						}
						data = append(data, value)
					}
					response = map[string]any{"object": "list", "has_more": false, "data": data}
				case "/v1/checkout/sessions/cs_original/line_items":
					response = map[string]any{"object": "list", "has_more": false, "data": []any{line}}
				default:
					t.Errorf("unexpected current profile/catalogue lookup %s", r.URL.Path)
					w.WriteHeader(404)
					return
				}
				if !assert.NoError(t, json.NewEncoder(w).Encode(response)) {
					return
				}
			}))
			t.Cleanup(server.Close)
			provider, err := NewStripeProvider(&Config{APIKey: "sk_fixture", WebhookSecret: "whsec_fixture", APIBaseURL: server.URL, Revenue: &RevenueConfig{AccountID: "acct_primary", CurrencyExponents: map[string]int{"GBP": 2}}})
			require.NoError(t, err)
			evidence, err := provider.LookupRevenueCheckout(context.Background(), RevenueScope{Provider: "stripe", AccountID: "acct_primary"}, "sub_recurring")
			if tc.want != nil {
				require.ErrorIs(t, err, tc.want)
				require.Empty(t, evidence.IntentID)
				return
			}
			require.NoError(t, err)
			require.Equal(t, "checkout_local", evidence.IntentID)
			require.Equal(t, "owning_principal", evidence.ClientReferenceID)
			require.Equal(t, "price_frozen", evidence.PriceID)
			require.Equal(t, "GBP", evidence.Currency)
			require.Equal(t, int64(1000), evidence.UnitAmountMinor)
			require.Equal(t, "month", evidence.BillingCadence)
			require.True(t, evidence.CreatedAt.Equal(time.Unix(1700000000, 0).UTC()))
		})
	}
}
func TestStripeRevenueCheckoutRequiresCompletePages(t *testing.T) {
	cases := []struct {
		name                 string
		laterOutage, stalled bool
		want                 error
	}{{"second_page_is_not_ignored", false, false, ErrRevenueUnassessable}, {"later_page_outage_is_not_partial_evidence", true, false, ErrPaymentProviderAPIRequestFailed}, {"stalled_cursor_is_not_partial_evidence", false, true, ErrRevenueUnassessable}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pages := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/v1/account" {
					if !assert.NoError(t, json.NewEncoder(w).Encode(map[string]any{"id": "acct_primary"})) {
						return
					}
					return
				}
				if !assert.Equal(t, "/v1/checkout/sessions", r.URL.Path) {
					return
				}
				pages++
				session := revenueCheckoutSessionFixture()
				if pages == 1 {
					if !assert.Empty(t, r.URL.Query().Get("starting_after")) {
						return
					}
					if !assert.NoError(t, json.NewEncoder(w).Encode(map[string]any{"object": "list", "has_more": true, "data": []any{session}})) {
						return
					}
					return
				}
				if !assert.Equal(t, "cs_original", r.URL.Query().Get("starting_after")) {
					return
				}
				if tc.laterOutage {
					w.WriteHeader(503)
					return
				}
				if !tc.stalled {
					session["id"] = "cs_second"
				}
				if !assert.NoError(t, json.NewEncoder(w).Encode(map[string]any{"object": "list", "has_more": false, "data": []any{session}})) {
					return
				}
			}))
			t.Cleanup(server.Close)
			provider, err := NewStripeProvider(&Config{APIKey: "sk_fixture", WebhookSecret: "whsec_fixture", APIBaseURL: server.URL, Revenue: &RevenueConfig{AccountID: "acct_primary", CurrencyExponents: map[string]int{"GBP": 2}}})
			require.NoError(t, err)
			_, err = provider.LookupRevenueCheckout(context.Background(), RevenueScope{Provider: "stripe", AccountID: "acct_primary"}, "sub_recurring")
			require.ErrorIs(t, err, tc.want)
			require.Equal(t, 2, pages)
		})
	}
}
func TestStripeRevenueCheckoutScopeAndRetrieve(t *testing.T) {
	cases := []struct {
		name                 string
		account              string
		connected, wrongMode bool
		want                 error
	}{{name: "authenticated_primary_scope_and_session", account: "acct_primary"}, {name: "authenticated_connected_scope_and_session", account: "acct_connected", connected: true}, {name: "unlisted_account_refused", account: "acct_unknown", want: ErrRevenueUnassessable}, {name: "retrieved_session_wrong_live_mode", account: "acct_primary", wrongMode: true, want: ErrRevenueUnassessable}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if tc.connected {
					if !assert.Equal(t, "acct_connected", r.Header.Get("Stripe-Account")) {
						return
					}
				} else {
					if !assert.Empty(t, r.Header.Get("Stripe-Account")) {
						return
					}
				}
				if r.URL.Path == "/v1/account" {
					if !assert.NoError(t, json.NewEncoder(w).Encode(map[string]any{"id": tc.account})) {
						return
					}
					return
				}
				if !assert.Equal(t, "/v1/checkout/sessions/cs_original", r.URL.Path) {
					return
				}
				if !assert.Equal(t, http.MethodGet, r.Method) {
					return
				}
				session := revenueCheckoutSessionFixture()
				if tc.wrongMode {
					session["livemode"] = true
				}
				if !assert.NoError(t, json.NewEncoder(w).Encode(session)) {
					return
				}
			}))
			t.Cleanup(server.Close)
			provider, err := NewStripeProvider(&Config{APIKey: "sk_fixture", WebhookSecret: "whsec_fixture", PublishableKey: "pk_fixture", APIBaseURL: server.URL, Revenue: &RevenueConfig{AccountID: "acct_primary", ConnectedAccountIDs: []string{"acct_connected"}, CurrencyExponents: map[string]int{"GBP": 2}}})
			require.NoError(t, err)
			session, err := provider.RetrieveRevenueCheckoutSession(context.Background(), RevenueScope{Provider: "stripe", AccountID: tc.account}, "cs_original")
			if tc.want != nil {
				require.ErrorIs(t, err, tc.want)
				require.Nil(t, session)
			} else {
				require.NoError(t, err)
				require.Equal(t, "fixture_secret", session.ClientSecret)
				require.Equal(t, "pk_fixture", session.PublishableKey)
			}
			if tc.account == "acct_unknown" {
				require.Zero(t, calls)
			}
		})
	}
}

func TestStripeCapturedCheckoutChecksModeAndIntent(t *testing.T) {
	cases := []struct {
		name                                                                            string
		wrongPriceMode, missingPriceMode, wrongSessionMode, wrongIntent, wrongReference bool
		want                                                                            error
	}{
		{name: "authorized_request_has_verified_test_mode_and_intent"},
		{name: "price_live_mode_mismatch", wrongPriceMode: true, want: ErrPaymentProviderPriceMismatch},
		{name: "price_mode_missing", missingPriceMode: true, want: ErrPaymentProviderPriceMismatch},
		{name: "returned_session_mode_mismatch", wrongSessionMode: true, want: ErrPaymentProviderAPIResponseInvalid},
		{name: "returned_session_intent_mismatch", wrongIntent: true, want: ErrPaymentProviderAPIResponseInvalid},
		{name: "returned_session_client_reference_mismatch", wrongReference: true, want: ErrPaymentProviderAPIResponseInvalid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			posts := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if !assert.Equal(t, "Bearer sk_fixture", r.Header.Get("Authorization")) {
					return
				}
				if !assert.Equal(t, StripeDefaultAPIVersion, r.Header.Get("Stripe-Version")) {
					return
				}
				if r.URL.Path == "/v1/prices/price_frozen" {
					price := revenueCheckoutLineFixture()["price"].(map[string]any)
					price["active"] = true
					if tc.wrongPriceMode {
						price["livemode"] = true
					}
					if tc.missingPriceMode {
						delete(price, "livemode")
					}
					if !assert.NoError(t, json.NewEncoder(w).Encode(price)) {
						return
					}
					return
				}
				if !assert.Equal(t, "/v1/checkout/sessions", r.URL.Path) {
					return
				}
				if !assert.Equal(t, http.MethodPost, r.Method) {
					return
				}
				posts++
				if !assert.NoError(t, r.ParseForm()) {
					return
				}
				if !assert.Equal(t, "checkout_local", r.Form.Get("metadata[checkout_intent_id]")) {
					return
				}
				if !assert.Equal(t, "checkout_local", r.Form.Get("subscription_data[metadata][checkout_intent_id]")) {
					return
				}
				if !assert.Equal(t, "owning_principal", r.Form.Get("client_reference_id")) {
					return
				}
				if !assert.Equal(t, "attempt_local", r.Header.Get("Idempotency-Key")) {
					return
				}
				session := revenueCheckoutSessionFixture()
				if tc.wrongSessionMode {
					session["livemode"] = true
				}
				if tc.wrongIntent {
					session["metadata"].(map[string]any)["checkout_intent_id"] = "checkout_other"
				}
				if tc.wrongReference {
					session["client_reference_id"] = "seat_other"
				}
				if !assert.NoError(t, json.NewEncoder(w).Encode(session)) {
					return
				}
			}))
			t.Cleanup(server.Close)
			provider, err := NewStripeProvider(&Config{APIKey: "sk_fixture", PublishableKey: "pk_fixture", WebhookSecret: "whsec_fixture", APIBaseURL: server.URL, Revenue: &RevenueConfig{AccountID: "acct_primary", CurrencyExponents: map[string]int{"GBP": 2}}})
			require.NoError(t, err)
			session, err := provider.CreateCheckoutSession(context.Background(), &CheckoutSessionRequest{PriceID: "price_frozen", UserID: "owning_principal", UserReference: "owning_principal", CustomerEmail: "original@example.test", Mode: CheckoutModeSubscription, ReturnURL: "https://app.example.test/checkout", ExpectedAmount: 1000, ExpectedCurrency: "GBP", ExpectedBillingCadence: "month", IdempotencyKey: "attempt_local", Metadata: map[string]string{"checkout_intent_id": "checkout_local"}})
			if tc.want != nil {
				require.ErrorIs(t, err, tc.want)
				require.Nil(t, session)
			} else {
				require.NoError(t, err)
				require.Equal(t, "cs_original", session.ID)
			}
			if tc.wrongPriceMode || tc.missingPriceMode {
				require.Zero(t, posts)
			} else {
				require.Equal(t, 1, posts)
			}
		})
	}
}
