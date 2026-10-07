package paymentprovider

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Audit disposition: new named refund allocation and complete-page cases. All
// provider responses are controlled fixtures; no charge or refund is submitted.
func refundRevenueFixture() map[string]map[string]any {
	m := revenueFixture()
	m["/v1/payment_intents/pi_paid"]["latest_charge"] = "ch_paid"
	m["/v1/charges/ch_paid"] = map[string]any{"id": "ch_paid", "livemode": false, "amount": 840, "amount_refunded": 420, "payment_intent": "pi_paid", "customer": "cus_payer", "currency": "gbp"}
	m["/v1/refunds"] = map[string]any{"object": "list", "has_more": false, "data": []any{map[string]any{"id": "re_partial", "livemode": false, "status": "succeeded", "amount": 420, "charge": "ch_paid", "payment_intent": "pi_paid", "currency": "gbp"}}}
	m["/v1/credit_notes"] = map[string]any{"object": "list", "has_more": false, "data": []any{map[string]any{"id": "cn_partial", "livemode": false, "status": "issued", "type": "post_payment", "invoice": "in_paid", "currency": "gbp", "pre_payment_amount": 0, "post_payment_amount": 420, "total_excluding_tax": 350, "refunds": []any{map[string]any{"refund": "re_partial", "amount_refunded": 420}}}}}
	m["/v1/credit_notes/cn_partial/lines"] = map[string]any{"object": "list", "has_more": false, "data": []any{map[string]any{"id": "cnli_partial", "livemode": false, "type": "invoice_line_item", "invoice_line_item": "il_subscription", "amount": 500, "pretax_credit_amounts": []any{map[string]any{"amount": 150}}, "discount_amounts": []any{map[string]any{"amount": 100}}, "taxes": []any{map[string]any{"amount": 70, "tax_behavior": "exclusive"}}}}}
	return m
}
func fixtureRefund(m map[string]map[string]any) map[string]any {
	return m["/v1/refunds"]["data"].([]any)[0].(map[string]any)
}
func fixtureCredit(m map[string]map[string]any) map[string]any {
	return m["/v1/credit_notes"]["data"].([]any)[0].(map[string]any)
}
func fixtureCreditLine(m map[string]map[string]any) map[string]any {
	return m["/v1/credit_notes/cn_partial/lines"]["data"].([]any)[0].(map[string]any)
}
func TestStripeRevenueRefundEvidence(t *testing.T) {
	type testCase struct {
		name    string
		gross   int64
		net     int64
		mutate  func(map[string]map[string]any)
		failure string
		want    error
	}
	cases := []testCase{
		{name: "partial_refund_uses_credit_note_net_line_excluding_tax", gross: 420, net: 350},
		{name: "inclusive_tax_is_removed_once", gross: 420, net: 350, mutate: func(m map[string]map[string]any) {
			l := fixtureCreditLine(m)
			l["amount"] = 570
			l["taxes"] = []any{map[string]any{"amount": 70, "tax_behavior": "inclusive"}}
		}},
		{name: "full_refund_reverses_original_net_not_gross", gross: 840, net: 700, mutate: func(m map[string]map[string]any) {
			m["/v1/charges/ch_paid"]["amount_refunded"] = 840
			fixtureRefund(m)["amount"] = 840
		}},
		{name: "unallocated_partial_refund_is_quarantined", gross: 420, mutate: func(m map[string]map[string]any) { m["/v1/credit_notes"]["data"] = []any{} }, want: ErrRevenueUnassessable},
		{name: "changed_cumulative_snapshot_is_not_reinterpreted", gross: 400, want: ErrRevenueUnassessable},
		{name: "unrelated_refund_cannot_supply_credit_note_evidence", gross: 420, mutate: func(m map[string]map[string]any) {
			fixtureCredit(m)["refunds"] = []any{map[string]any{"refund": "re_other", "amount_refunded": 420}}
		}, want: ErrRevenueUnassessable},
		{name: "refund_pending_is_not_realized_revenue_reversal", gross: 420, mutate: func(m map[string]map[string]any) { fixtureRefund(m)["status"] = "pending" }, want: ErrRevenueUnassessable},
		{name: "credit_to_customer_balance_is_not_assumed_cash_refund", gross: 420, mutate: func(m map[string]map[string]any) { fixtureCredit(m)["refunds"] = []any{} }, want: ErrRevenueUnassessable},
		{name: "credit_note_line_must_reconcile_net_total", gross: 420, mutate: func(m map[string]map[string]any) { fixtureCredit(m)["total_excluding_tax"] = 351 }, want: ErrRevenueUnassessable},
		{name: "unrelated_invoice_line_is_denied", gross: 420, mutate: func(m map[string]map[string]any) { fixtureCreditLine(m)["invoice_line_item"] = "il_other" }, want: ErrRevenueUnassessable},
		{name: "authenticated_refund_mode_must_match", gross: 420, mutate: func(m map[string]map[string]any) { fixtureRefund(m)["livemode"] = true }, want: ErrRevenueUnassessable},
		{name: "credit_line_api_outage_is_retryable", gross: 420, failure: "/v1/credit_notes/cn_partial/lines", want: ErrPaymentProviderAPIRequestFailed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := refundRevenueFixture()
			if tc.mutate != nil {
				tc.mutate(m)
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == tc.failure {
					w.WriteHeader(503)
					return
				}
				v, ok := m[r.URL.Path]
				if !ok {
					t.Errorf("unexpected path %s", r.URL.Path)
					w.WriteHeader(404)
					return
				}
				_ = json.NewEncoder(w).Encode(v)
			}))
			t.Cleanup(server.Close)
			p, err := NewStripeProvider(&Config{WebhookSecret: "fixture-secret", APIKey: "fixture-key", APIBaseURL: server.URL, Revenue: &RevenueConfig{AccountID: "acct_primary", CurrencyExponents: map[string]int{"GBP": 2}}})
			if err != nil {
				t.Fatal(err)
			}
			invoice, err := p.LookupRevenueInvoice(context.Background(), RevenueInvoiceRequest{Scope: RevenueScope{Provider: "stripe", AccountID: "acct_primary"}, InvoiceID: "in_paid", PaymentID: "pi_paid", IncludeRefunds: true, ExpectedCumulativeRefundedGrossMinor: tc.gross})
			if tc.want != nil {
				if !errors.Is(err, tc.want) || invoice != nil {
					t.Fatalf("invoice=%+v error=%v want=%v", invoice, err, tc.want)
				}
				return
			}
			if err != nil || invoice == nil || len(invoice.Lines) != 1 || invoice.Lines[0].CumulativeRefundedMinor != tc.net {
				t.Fatalf("invoice=%+v error=%v", invoice, err)
			}
		})
	}
}

func TestStripeRevenueCompletePagination(t *testing.T) {
	type testCase struct {
		name         string
		secondStatus int
		duplicate    bool
		empty        bool
		want         error
	}
	cases := []testCase{{name: "complete_two_pages"}, {name: "later_page_failure_has_no_partial_result", secondStatus: 503, want: ErrPaymentProviderAPIRequestFailed}, {name: "stalled_duplicate_cursor_is_denied", duplicate: true, want: ErrRevenueUnassessable}, {name: "empty_more_page_is_denied", empty: true, want: ErrRevenueUnassessable}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pages := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				pages++
				id := "item_one"
				more := true
				if pages == 2 {
					if r.URL.Query().Get("starting_after") != "item_one" {
						t.Error("cursor missing")
					}
					if tc.secondStatus != 0 {
						w.WriteHeader(tc.secondStatus)
						return
					}
					more = false
					if !tc.duplicate {
						id = "item_two"
					}
				}
				if pages > 2 {
					t.Error("pagination loop continued")
					w.WriteHeader(503)
					return
				}
				data := []any{map[string]any{"id": id}}
				if tc.empty {
					data = nil
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"object": "list", "has_more": more, "data": data})
			}))
			t.Cleanup(server.Close)
			p, err := NewStripeProvider(&Config{WebhookSecret: "fixture-secret", APIKey: "fixture-key", APIBaseURL: server.URL, Revenue: &RevenueConfig{AccountID: "acct_primary", CurrencyExponents: map[string]int{"GBP": 2}}})
			if err != nil {
				t.Fatal(err)
			}
			rows, err := p.revenueList(context.Background(), RevenueScope{Provider: "stripe", AccountID: "acct_primary"}, "/v1/source")
			if tc.want != nil {
				if !errors.Is(err, tc.want) || rows != nil {
					t.Fatalf("partial=%+v error=%v want=%v", rows, err, tc.want)
				}
				return
			}
			if err != nil || len(rows) != 2 || pages != 2 {
				t.Fatalf("rows=%+v pages=%d error=%v", rows, pages, err)
			}
		})
	}
}
