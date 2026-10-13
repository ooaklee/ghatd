package paymentprovider

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStripeCheckoutInspectionPreservesStrictLookup(t *testing.T) {
	cases := []struct {
		name     string
		metadata any
		absent   bool
		want     error
	}{
		{"pointer_present", map[string]string{"checkout_intent_id": "checkout_local"}, false, nil},
		{"metadata_missing", nil, true, nil},
		{"metadata_null", nil, false, nil},
		{"empty_metadata", map[string]string{}, false, nil},
		{"malformed_metadata", []string{"private"}, false, ErrRevenueUnassessable},
		{"wrong_merchant", map[string]string{}, false, ErrRevenueUnassessable},
		{"wrong_mode", map[string]string{}, false, ErrRevenueUnassessable},
		{"line_outage", map[string]string{}, false, ErrPaymentProviderAPIRequestFailed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			session := revenueCheckoutSessionFixture()
			session["metadata"] = tc.metadata
			if tc.absent {
				delete(session, "metadata")
			}
			if tc.name == "wrong_mode" {
				session["livemode"] = true
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, http.MethodGet, r.Method)
				var response any
				switch r.URL.Path {
				case "/v1/account":
					account := "acct_primary"
					if tc.name == "wrong_merchant" {
						account = "acct_other"
					}
					response = map[string]any{"id": account}
				case "/v1/checkout/sessions":
					response = map[string]any{"object": "list", "data": []any{session}, "has_more": false}
				case "/v1/checkout/sessions/cs_original":
					response = session
				case "/v1/checkout/sessions/cs_original/line_items":
					if tc.name == "line_outage" {
						w.WriteHeader(503)
						return
					}
					response = map[string]any{"object": "list", "data": []any{revenueCheckoutLineFixture()}, "has_more": false}
				default:
					t.Errorf("unexpected path %s", r.URL.Path)
					w.WriteHeader(404)
					return
				}
				require.NoError(t, json.NewEncoder(w).Encode(response))
			}))
			t.Cleanup(server.Close)
			p, err := NewStripeProvider(&Config{APIKey: "sk_fixture", WebhookSecret: "whsec_fixture", APIBaseURL: server.URL, Revenue: &RevenueConfig{AccountID: "acct_primary", CurrencyExponents: map[string]int{"GBP": 2}}})
			require.NoError(t, err)
			scope := RevenueScope{Provider: "stripe", AccountID: "acct_primary"}
			for _, bySession := range []bool{false, true} {
				var e RevenueCheckoutEvidence
				if bySession {
					e, err = p.InspectRevenueCheckoutSession(t.Context(), scope, "cs_original")
				} else {
					e, err = p.InspectRevenueCheckout(t.Context(), scope, "sub_recurring")
				}
				if tc.want != nil {
					require.ErrorIs(t, err, tc.want)
					require.Empty(t, e.SessionID)
				} else {
					require.NoError(t, err)
					require.Equal(t, "cs_original", e.SessionID)
					require.Equal(t, "price_frozen", e.PriceID)
				}
			}
			_, err = p.LookupRevenueCheckout(t.Context(), scope, "sub_recurring")
			if tc.name == "pointer_present" {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, ErrRevenueUnassessable)
			}
		})
	}
}
