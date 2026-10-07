package paymentprovider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Audit disposition: named authenticated HTTP and no-I/O validation cases with
// isolated servers. All provider calls are fixtures; no real account is queried.
func TestStripeRevenueSubscriptionEvidence(t *testing.T) {
	type testCase struct {
		name, state, account, sub, missing, field string
		value                                     any
		scheduled, connected, redirect, oversized bool
		httpStatus                                int
		want                                      error
	}
	cases := []testCase{
		{name: "active", state: "active"},
		{name: "active_scheduled_end", state: "active", scheduled: true},
		{name: "trialing", state: "trialing"},
		{name: "incomplete", state: "incomplete"},
		{name: "incomplete_expired", state: "incomplete_expired"},
		{name: "past_due", state: "past_due"},
		{name: "unpaid", state: "unpaid"},
		{name: "canceled", state: "canceled"},
		{name: "paused", state: "paused"},
		{name: "unknown_status_unassessable", state: "new_status", want: ErrRevenueUnassessable},
		{name: "missing_status", missing: "status", want: ErrRevenueUnassessable},
		{name: "wrong_returned_subscription", field: "id", value: "sub_other", want: ErrRevenueUnassessable},
		{name: "wrong_returned_object", field: "object", value: "customer", want: ErrRevenueUnassessable},
		{name: "customer_absent", missing: "customer", want: ErrRevenueUnassessable},
		{name: "customer_is_not_customer", field: "customer", value: "pi_other", want: ErrRevenueUnassessable},
		{name: "expanded_customer", field: "customer", value: map[string]any{"id": "cus_original", "object": "customer"}},
		{name: "missing_mode_is_not_test", missing: "livemode", want: ErrRevenueUnassessable},
		{name: "wrong_mode", field: "livemode", value: true, want: ErrRevenueUnassessable},
		{name: "missing_cancellation_is_not_false", missing: "cancel_at_period_end", want: ErrRevenueUnassessable},
		{name: "null_cancellation_is_not_false", field: "cancel_at_period_end", want: ErrRevenueUnassessable},
		{name: "string_cancellation_rejected", field: "cancel_at_period_end", value: "false", want: ErrRevenueUnassessable},
		{name: "numeric_cancellation_rejected", field: "cancel_at_period_end", value: 0, want: ErrRevenueUnassessable},
		{name: "merchant_mismatch", account: "acct_other", want: ErrRevenueUnassessable},
		{name: "connected_account_is_explicit", connected: true},
		{name: "provider_outage_retryable", httpStatus: 503, want: ErrPaymentProviderAPIRequestFailed},
		{name: "redirect_is_not_followed", redirect: true, want: ErrPaymentProviderAPIRequestFailed},
		{name: "oversized_body_rejected", oversized: true, want: ErrPaymentProviderAPIResponseInvalid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			state := tc.state
			if state == "" {
				state = "active"
			}
			object := map[string]any{"id": "sub_original", "object": "subscription", "customer": "cus_original", "status": state, "livemode": false, "cancel_at_period_end": tc.scheduled}
			if tc.field != "" {
				object[tc.field] = tc.value
			}
			if tc.missing != "" {
				delete(object, tc.missing)
			}
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if !assert.Equal(t, "GET", r.Method) {
					return
				}
				if !assert.Equal(t, "Bearer sk_fixture", r.Header.Get("Authorization")) {
					return
				}
				if !assert.Equal(t, StripeDefaultAPIVersion, r.Header.Get("Stripe-Version")) {
					return
				}
				if tc.connected {
					if !assert.Equal(t, "acct_connected", r.Header.Get("Stripe-Account")) {
						return
					}
				} else {
					if !assert.Empty(t, r.Header.Get("Stripe-Account")) {
						return
					}
				}
				switch r.URL.Path {
				case "/v1/account":
					account := tc.account
					if account == "" {
						account = "acct_primary"
					}
					if tc.connected {
						account = "acct_connected"
					}
					if !assert.NoError(t, json.NewEncoder(w).Encode(map[string]any{"id": account})) {
						return
					}
				case "/v1/subscriptions/sub_original":
					if tc.httpStatus > 0 {
						w.WriteHeader(tc.httpStatus)
						return
					}
					if tc.redirect {
						w.Header().Set("Location", "/unexpected-target")
						w.WriteHeader(302)
						return
					}
					if tc.oversized {
						_, err := w.Write([]byte(strings.Repeat("x", (1<<20)+1)))
						if !assert.NoError(t, err) {
							return
						}
						return
					}
					if !assert.NoError(t, json.NewEncoder(w).Encode(object)) {
						return
					}
				default:
					t.Errorf("unexpected endpoint %s", r.URL.Path)
					w.WriteHeader(404)
				}
			}))
			t.Cleanup(server.Close)
			p, err := NewStripeProvider(&Config{APIKey: "sk_fixture", WebhookSecret: "whsec_fixture", APIBaseURL: server.URL, Revenue: &RevenueConfig{AccountID: "acct_primary", ConnectedAccountIDs: []string{"acct_connected"}, CurrencyExponents: map[string]int{"GBP": 2}}})
			require.NoError(t, err)
			scope := RevenueScope{Provider: "stripe", AccountID: "acct_primary"}
			if tc.connected {
				scope.AccountID = "acct_connected"
			}
			v, err := p.LookupRevenueSubscription(context.Background(), scope, "sub_original")
			if tc.want != nil {
				require.ErrorIs(t, err, tc.want)
				require.Zero(t, v)
			} else {
				require.NoError(t, err)
				require.Equal(t, scope, v.Scope)
				require.Equal(t, state, v.Status)
				require.Equal(t, "cus_original", v.CustomerID)
				require.Equal(t, tc.scheduled, v.CancellationScheduled)
				data, err := json.Marshal(v)
				require.NoError(t, err)
				require.JSONEq(t, `{}`, string(data))
			}
			if tc.account != "" {
				require.Equal(t, 1, calls)
			} else {
				require.Equal(t, 2, calls)
			}
		})
	}
}
func TestStripeRevenueSubscriptionNoIOForInvalidScope(t *testing.T) {
	type testCase struct {
		name, id                                  string
		scope                                     RevenueScope
		nilContext, cancel, nilProvider, disabled bool
		want                                      error
	}
	base := RevenueScope{Provider: "stripe", AccountID: "acct_primary"}
	cases := []testCase{
		{name: "nil_context", id: "sub_original", scope: base, nilContext: true, want: ErrPaymentProviderAPIRequestFailed},
		{name: "canceled_context", id: "sub_original", scope: base, cancel: true, want: context.Canceled},
		{name: "nil_provider", id: "sub_original", scope: base, nilProvider: true, want: ErrRevenueNotEnabled},
		{name: "revenue_disabled", id: "sub_original", scope: base, disabled: true, want: ErrRevenueNotEnabled},
		{name: "path_injection", id: "sub_original/other", scope: base, want: ErrRevenueUnassessable},
		{name: "customer_not_subscription", id: "cus_original", scope: base, want: ErrRevenueUnassessable},
		{name: "merchant_not_allowlisted", id: "sub_original", scope: RevenueScope{Provider: "stripe", AccountID: "acct_unknown"}, want: ErrRevenueUnassessable},
		{name: "mode_not_configured", id: "sub_original", scope: RevenueScope{Provider: "stripe", AccountID: "acct_primary", LiveMode: true}, want: ErrRevenueUnassessable},
		{name: "provider_mismatch", id: "sub_original", scope: RevenueScope{Provider: "other", AccountID: "acct_primary"}, want: ErrRevenueUnassessable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; w.WriteHeader(500) }))
			t.Cleanup(server.Close)
			config := &Config{APIKey: "sk_fixture", WebhookSecret: "whsec_fixture", APIBaseURL: server.URL, Revenue: &RevenueConfig{AccountID: "acct_primary", CurrencyExponents: map[string]int{"GBP": 2}}}
			if tc.disabled {
				config.Revenue = nil
			}
			p, err := NewStripeProvider(config)
			require.NoError(t, err)
			if tc.nilProvider {
				p = nil
			}
			ctx := context.Background()
			if tc.nilContext {
				ctx = nil
			}
			if tc.cancel {
				c, cancel := context.WithCancel(ctx)
				cancel()
				ctx = c
			}
			v, err := p.LookupRevenueSubscription(ctx, tc.scope, tc.id)
			require.ErrorIs(t, err, tc.want)
			require.Zero(t, v)
			require.Zero(t, calls)
		})
	}
}
