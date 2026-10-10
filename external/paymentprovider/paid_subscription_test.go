package paymentprovider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// paidSubscriptionFixture adds current periods and charge evidence to the
// existing reconciled refund fixture. No real provider calls are performed.
func paidSubscriptionFixture() map[string]map[string]any {
	m := refundRevenueFixture()
	m["/v1/charges/ch_paid"]["amount_refunded"] = 0
	m["/v1/charges/ch_paid"]["disputed"] = false
	m["/v1/charges/ch_paid"]["paid"] = true
	fixtureLine(m)["period"] = map[string]any{"start": 1700000000, "end": 1700604800}
	m["/v1/subscriptions/sub_recurring"] = map[string]any{
		"object": "subscription", "id": "sub_recurring", "customer": "cus_payer", "status": "active", "livemode": false,
		"cancel_at_period_end": false, "latest_invoice": "in_paid", "pause_collection": nil,
		"items": map[string]any{"object": "list", "has_more": false, "data": []any{map[string]any{
			"id": "si_fixture", "current_period_start": 1700000000, "current_period_end": 1700604800,
			"price": map[string]any{"id": "price_frozen", "recurring": map[string]any{"interval": "week", "interval_count": 1}},
		}}},
	}
	return m
}

// TestStripePaidSubscriptionEvidence verifies payment/period relationships and
// the distinction between an assessed unpaid state and unavailable evidence.
func TestStripePaidSubscriptionEvidence(t *testing.T) {
	for _, tc := range []struct {
		name          string
		mutate        func(map[string]map[string]any)
		finalMutation func(map[string]any)
		failure       string
		net           int64
		want          error
	}{
		{name: "paid weekly", net: 700},
		{name: "canceled during evidence lookup", finalMutation: func(s map[string]any) { s["status"] = "canceled" }},
		{name: "collection paused during evidence lookup", finalMutation: func(s map[string]any) { s["pause_collection"] = map[string]any{"behavior": "void"} }},
		{name: "new invoice during evidence lookup", want: ErrRevenueUnassessable, finalMutation: func(s map[string]any) { s["latest_invoice"] = "in_new" }},
		{name: "plan changed during evidence lookup", want: ErrRevenueUnassessable, finalMutation: func(s map[string]any) {
			s["items"].(map[string]any)["data"].([]any)[0].(map[string]any)["price"].(map[string]any)["id"] = "price_new"
		}},
		{name: "missing collection pause evidence", want: ErrRevenueUnassessable, mutate: func(m map[string]map[string]any) { delete(m["/v1/subscriptions/sub_recurring"], "pause_collection") }},
		{name: "malformed collection pause evidence", want: ErrRevenueUnassessable, mutate: func(m map[string]map[string]any) { m["/v1/subscriptions/sub_recurring"]["pause_collection"] = false }},
		{name: "missing latest invoice", want: ErrRevenueUnassessable, mutate: func(m map[string]map[string]any) { delete(m["/v1/subscriptions/sub_recurring"], "latest_invoice") }},
		{name: "known absence of latest invoice", mutate: func(m map[string]map[string]any) { m["/v1/subscriptions/sub_recurring"]["latest_invoice"] = nil }},
		{name: "pending cancellation retains paid period", net: 700, mutate: func(m map[string]map[string]any) { m["/v1/subscriptions/sub_recurring"]["cancel_at_period_end"] = true }},
		{name: "trial", mutate: func(m map[string]map[string]any) { m["/v1/subscriptions/sub_recurring"]["status"] = "trialing" }},
		{name: "past due", mutate: func(m map[string]map[string]any) { m["/v1/subscriptions/sub_recurring"]["status"] = "past_due" }},
		{name: "unpaid", mutate: func(m map[string]map[string]any) { m["/v1/subscriptions/sub_recurring"]["status"] = "unpaid" }},
		{name: "cancelled", mutate: func(m map[string]map[string]any) { m["/v1/subscriptions/sub_recurring"]["status"] = "canceled" }},
		{name: "paused collection while active", mutate: func(m map[string]map[string]any) {
			m["/v1/subscriptions/sub_recurring"]["pause_collection"] = map[string]any{"behavior": "void"}
		}},
		{name: "free invoice", mutate: func(m map[string]map[string]any) { m["/v1/invoices/in_paid"]["amount_paid"] = 0 }},
		{name: "unpaid current invoice", mutate: func(m map[string]map[string]any) { m["/v1/invoices/in_paid"]["status"] = "open" }},
		{name: "full refund", mutate: func(m map[string]map[string]any) { m["/v1/charges/ch_paid"]["amount_refunded"] = 840 }},
		{name: "disputed payment", mutate: func(m map[string]map[string]any) { m["/v1/charges/ch_paid"]["disputed"] = true }},
		{name: "allocated partial refund", net: 350, mutate: func(m map[string]map[string]any) { m["/v1/charges/ch_paid"]["amount_refunded"] = 420 }},
		{name: "unallocated partial refund", want: ErrRevenueUnassessable, mutate: func(m map[string]map[string]any) {
			m["/v1/charges/ch_paid"]["amount_refunded"] = 420
			m["/v1/credit_notes"]["data"] = []any{}
		}},
		{name: "missing service period", want: ErrRevenueUnassessable, mutate: func(m map[string]map[string]any) { delete(fixtureLine(m), "period") }},
		{name: "old invoice period", want: ErrRevenueUnassessable, mutate: func(m map[string]map[string]any) {
			fixtureLine(m)["period"] = map[string]any{"start": 1699395200, "end": 1700000000}
		}},
		{name: "wrong invoice customer", want: ErrRevenueUnassessable, mutate: func(m map[string]map[string]any) { m["/v1/invoices/in_paid"]["customer"] = "cus_other" }},
		{name: "wrong invoice mode", want: ErrRevenueUnassessable, mutate: func(m map[string]map[string]any) { m["/v1/invoices/in_paid"]["livemode"] = true }},
		{name: "wrong charge customer", want: ErrRevenueUnassessable, mutate: func(m map[string]map[string]any) { m["/v1/charges/ch_paid"]["customer"] = "cus_other" }},
		{name: "unknown disputed state", want: ErrRevenueUnassessable, mutate: func(m map[string]map[string]any) { delete(m["/v1/charges/ch_paid"], "disputed") }},
		{name: "incomplete item pagination", want: ErrRevenueUnassessable, mutate: func(m map[string]map[string]any) {
			m["/v1/subscriptions/sub_recurring"]["items"].(map[string]any)["has_more"] = true
		}},
		{name: "provider outage", failure: "/v1/charges/ch_paid", want: ErrPaymentProviderAPIRequestFailed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture := paidSubscriptionFixture()
			if tc.mutate != nil {
				tc.mutate(fixture)
			}
			chargeObserved := false
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, http.MethodGet, r.Method)
				if r.URL.Path == tc.failure {
					w.WriteHeader(503)
					return
				}
				object, ok := fixture[r.URL.Path]
				if !ok {
					w.WriteHeader(404)
					return
				}
				if r.URL.Path == "/v1/charges/ch_paid" {
					chargeObserved = true
				}
				if chargeObserved && r.URL.Path == "/v1/subscriptions/sub_recurring" && tc.finalMutation != nil {
					tc.finalMutation(object)
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(object)
			}))
			t.Cleanup(server.Close)
			provider, err := NewStripeProvider(&Config{APIKey: "sk_fixture", WebhookSecret: "whsec_fixture", APIBaseURL: server.URL, Environment: "test", Revenue: &RevenueConfig{AccountID: "acct_primary", CurrencyExponents: map[string]int{"GBP": 2}}})
			require.NoError(t, err)
			proof, err := provider.LookupPaidSubscription(context.Background(), RevenueScope{Provider: "stripe", AccountID: "acct_primary"}, "sub_recurring")
			if tc.want != nil {
				require.ErrorIs(t, err, tc.want)
				require.Empty(t, proof.PaidLines)
				return
			}
			require.NoError(t, err)
			if tc.net == 0 {
				require.Empty(t, proof.PaidLines)
			} else {
				require.Len(t, proof.PaidLines, 1)
				line := proof.PaidLines[0]
				require.Equal(t, tc.net, line.NetPaidMinor)
				require.Equal(t, "price_frozen", line.PriceID)
				require.Equal(t, "week", line.Interval)
				require.EqualValues(t, 1, line.IntervalCount)
				require.Equal(t, time.Unix(1700000000, 0).UTC(), line.PeriodStart)
				require.Equal(t, time.Unix(1700604800, 0).UTC(), line.PeriodEnd)
			}
			data, err := json.Marshal(proof)
			require.NoError(t, err)
			require.JSONEq(t, `{}`, string(data))
		})
	}
}
