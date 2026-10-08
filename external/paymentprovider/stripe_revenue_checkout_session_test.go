package paymentprovider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Fresh authenticated HTTP fixtures prove read-only scope/evidence boundaries,
// not a native billing association, paid charge, trial status or live provider.
func TestStripeRetainedCheckoutSessionEvidence(t *testing.T) {
	for _, tc := range []struct {
		name, input, context, account, failure string
		pagination                             string
		session, line                          func(map[string]any)
		multipleLines, noHTTP                  bool
		want                                   error
	}{
		{name: "completed_subscription_session"},
		{name: "completed_unpaid_trial_checkout_is_only_association_evidence", session: func(s map[string]any) {
			s["amount_total"], s["payment_status"] = 0, "no_payment_required"
		}},
		{name: "wrong_returned_session", session: func(s map[string]any) { s["id"] = "cs_other" }, want: ErrRevenueUnassessable},
		{name: "missing_subscription", session: func(s map[string]any) { delete(s, "subscription") }, want: ErrRevenueUnassessable},
		{name: "invalid_subscription_identity", session: func(s map[string]any) { s["subscription"] = "cus_other" }, want: ErrRevenueUnassessable},
		{name: "missing_intent_pointer", session: func(s map[string]any) { delete(s, "metadata") }, want: ErrRevenueUnassessable},
		{name: "incomplete_checkout", session: func(s map[string]any) { s["status"] = "open" }, want: ErrRevenueUnassessable},
		{name: "one_time_checkout", session: func(s map[string]any) { s["mode"] = "payment" }, want: ErrRevenueUnassessable},
		{name: "wrong_session_object", session: func(s map[string]any) { s["object"] = "subscription" }, want: ErrRevenueUnassessable},
		{name: "wrong_session_mode", session: func(s map[string]any) { s["livemode"] = true }, want: ErrRevenueUnassessable},
		{name: "missing_mode_is_not_test", session: func(s map[string]any) { delete(s, "livemode") }, want: ErrRevenueUnassessable},
		{name: "missing_customer", session: func(s map[string]any) { delete(s, "customer") }, want: ErrRevenueUnassessable},
		{name: "missing_payer_reference", session: func(s map[string]any) { delete(s, "client_reference_id") }, want: ErrRevenueUnassessable},
		{name: "malformed_three_character_currency", session: func(s map[string]any) { s["currency"] = "1gb" }, line: func(l map[string]any) {
			l["currency"], l["price"].(map[string]any)["currency"] = "1gb", "1gb"
		}, want: ErrRevenueUnassessable},
		{name: "original_line_currency_conflict", line: func(l map[string]any) { l["currency"] = "eur" }, want: ErrRevenueUnassessable},
		{name: "original_price_mode_conflict", line: func(l map[string]any) { l["price"].(map[string]any)["livemode"] = true }, want: ErrRevenueUnassessable},
		{name: "missing_original_unit_amount", line: func(l map[string]any) { delete(l["price"].(map[string]any), "unit_amount") }, want: ErrRevenueUnassessable},
		{name: "unsupported_quantity", line: func(l map[string]any) { l["quantity"] = 2 }, want: ErrRevenueUnassessable},
		{name: "multiple_original_lines_not_guessed", multipleLines: true, want: ErrRevenueUnassessable},
		{name: "second_line_page_outage_is_not_partial_evidence", pagination: "outage", want: ErrPaymentProviderAPIRequestFailed},
		{name: "duplicate_line_across_pages_fails_closed", pagination: "duplicate", want: ErrRevenueUnassessable},
		{name: "two_paginated_lines_are_not_one_original_line", pagination: "two-lines", want: ErrRevenueUnassessable},
		{name: "merchant_mismatch", account: "acct_other", want: ErrRevenueUnassessable},
		{name: "session_outage", failure: "/v1/checkout/sessions/cs_original", want: ErrPaymentProviderAPIRequestFailed},
		{name: "line_outage", failure: "/v1/checkout/sessions/cs_original/line_items", want: ErrPaymentProviderAPIRequestFailed},
		{name: "invalid_input_never_calls_provider", input: "cs_../other", noHTTP: true, want: ErrRevenueUnassessable},
		{name: "wrong_input_kind_never_calls_provider", input: "sub_recurring", noHTTP: true, want: ErrRevenueUnassessable},
		{name: "nil_context_never_calls_provider", context: "nil", noHTTP: true, want: ErrPaymentProviderInvalidPayload},
		{name: "cancelled_context_never_calls_provider", context: "cancelled", noHTTP: true, want: context.Canceled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			session, line := revenueCheckoutSessionFixture(), revenueCheckoutLineFixture()
			if tc.session != nil {
				tc.session(session)
			}
			if tc.line != nil {
				tc.line(line)
			}
			var calls atomic.Int64
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if !assert.Equal(t, http.MethodGet, r.Method) || !assert.Equal(t, "Bearer sk_fixture", r.Header.Get("Authorization")) || !assert.Equal(t, StripeDefaultAPIVersion, r.Header.Get("Stripe-Version")) || !assert.Empty(t, r.Header.Get("Stripe-Account")) {
					w.WriteHeader(http.StatusBadRequest)
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
				case "/v1/checkout/sessions/cs_original":
					response = session
				case "/v1/checkout/sessions/cs_original/line_items":
					if !assert.Equal(t, "100", r.URL.Query().Get("limit")) {
						w.WriteHeader(http.StatusBadRequest)
						return
					}
					lines := []any{line}
					hasMore := tc.pagination != ""
					if cursor := r.URL.Query().Get("starting_after"); cursor != "" {
						if !assert.Equal(t, "li_original", cursor) {
							w.WriteHeader(http.StatusBadRequest)
							return
						}
						if tc.pagination == "outage" {
							w.WriteHeader(http.StatusServiceUnavailable)
							return
						}
						hasMore = false
						if tc.pagination == "two-lines" {
							other := make(map[string]any, len(line))
							for key, value := range line {
								other[key] = value
							}
							other["id"] = "li_other"
							lines = []any{other}
						}
					}
					if tc.multipleLines {
						lines = append(lines, line)
						lines[1] = map[string]any{"id": "li_other", "object": "item"}
					}
					response = map[string]any{"object": "list", "has_more": hasMore, "data": lines}
				default:
					t.Errorf("unexpected provider request %s", r.URL.Path)
					w.WriteHeader(http.StatusNotFound)
					return
				}
				if !assert.NoError(t, json.NewEncoder(w).Encode(response)) {
					return
				}
			}))
			t.Cleanup(server.Close)
			provider, err := NewStripeProvider(&Config{APIKey: "sk_fixture", WebhookSecret: "whsec_fixture", APIBaseURL: server.URL, Revenue: &RevenueConfig{AccountID: "acct_primary", CurrencyExponents: map[string]int{"GBP": 2}}})
			require.NoError(t, err)
			input := tc.input
			if input == "" {
				input = "cs_original"
			}
			ctx := context.Background()
			if tc.context == "nil" {
				ctx = nil
			} else if tc.context == "cancelled" {
				cancelled, cancel := context.WithCancel(ctx)
				cancel()
				ctx = cancelled
			}
			scope := RevenueScope{Provider: "stripe", AccountID: "acct_primary"}
			evidence, err := provider.LookupRevenueCheckoutSessionEvidence(ctx, scope, input)
			if tc.noHTTP {
				require.Zero(t, calls.Load())
			}
			if tc.pagination != "" {
				require.EqualValues(t, 4, calls.Load(), "both line pages must be observed before returning")
			}
			if tc.want != nil {
				require.ErrorIs(t, err, tc.want)
				require.Empty(t, evidence.IntentID)
				return
			}
			require.NoError(t, err)
			require.EqualValues(t, 3, calls.Load(), "merchant, exact session and complete line evidence only")
			require.Equal(t, scope, evidence.Scope)
			require.Equal(t, "cs_original", evidence.SessionID)
			require.Equal(t, "sub_recurring", evidence.SubscriptionID)
			require.Equal(t, "cus_payer", evidence.CustomerID)
			require.Equal(t, "checkout_local", evidence.IntentID)
			require.Equal(t, "owning_principal", evidence.ClientReferenceID)
			require.Equal(t, "price_frozen", evidence.PriceID)
			require.EqualValues(t, 1000, evidence.UnitAmountMinor, "original price, never an attested paid charge")
		})
	}
}
