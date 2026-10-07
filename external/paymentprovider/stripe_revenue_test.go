package paymentprovider

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Audit disposition: new named-case coverage for the optional authenticated
// revenue capability. Existing access/checkout tests remain unchanged; their
// outcomes do not establish financial evidence or provider allocation support.
func revenueFixture() map[string]map[string]any {
	return map[string]map[string]any{
		"/v1/account":                 {"id": "acct_primary"},
		"/v1/invoices/in_paid":        {"id": "in_paid", "livemode": false, "status": "paid", "currency": "gbp", "customer": "cus_payer", "amount_paid": 840, "total": 840, "total_excluding_tax": 700, "amount_remaining": 0, "starting_balance": 0, "pre_payment_credit_notes_amount": 0, "status_transitions": map[string]any{"paid_at": 1700000000}, "parent": map[string]any{"subscription_details": map[string]any{"subscription": "sub_recurring"}}},
		"/v1/invoice_payments":        {"object": "list", "has_more": false, "data": []any{map[string]any{"id": "inpay_one", "invoice": "in_paid", "livemode": false, "currency": "gbp", "status": "paid", "amount_paid": 840, "payment": map[string]any{"type": "payment_intent", "payment_intent": "pi_paid"}}}},
		"/v1/payment_intents/pi_paid": {"id": "pi_paid", "livemode": false, "status": "succeeded", "currency": "gbp", "amount_received": 840, "customer": "cus_payer"},
		"/v1/invoices/in_paid/lines":  {"object": "list", "has_more": false, "data": []any{map[string]any{"id": "il_subscription", "livemode": false, "currency": "gbp", "subtotal": 1000, "pretax_credit_amounts": []any{map[string]any{"amount": 200, "type": "discount"}, map[string]any{"amount": 100, "type": "credit_balance_transaction"}}, "discount_amounts": []any{map[string]any{"amount": 200}}, "parent": map[string]any{"subscription_item_details": map[string]any{"subscription": "sub_recurring"}}, "pricing": map[string]any{"price_details": map[string]any{"price": "price_frozen"}}}}},
	}
}

func fixturePayment(m map[string]map[string]any) map[string]any {
	return m["/v1/invoice_payments"]["data"].([]any)[0].(map[string]any)
}
func fixtureLine(m map[string]map[string]any) map[string]any {
	return m["/v1/invoices/in_paid/lines"]["data"].([]any)[0].(map[string]any)
}

func TestStripeRevenueVerifiedInvoice(t *testing.T) {
	type testCase struct {
		name       string
		mutate     func(map[string]map[string]any)
		eventType  string
		apiFailure string
		quarantine bool
		wantErr    error
	}
	cases := []testCase{
		{name: "first_subscription_payment_uses_net_after_discounts_and_credits"},
		{name: "renewal_invoice_uses_same_fiscal_contract", eventType: "invoice.payment_succeeded"},
		{name: "legacy_explicit_ex_tax_and_discount_fields", mutate: func(m map[string]map[string]any) {
			l := fixtureLine(m)
			delete(l, "subtotal")
			delete(l, "pretax_credit_amounts")
			l["amount_excluding_tax"] = 900
		}},
		{name: "invoice_total_is_not_net", mutate: func(m map[string]map[string]any) { m["/v1/invoices/in_paid"]["total_excluding_tax"] = 840 }, quarantine: true},
		{name: "partial_paid_invoice_is_not_allocated_proportionally", mutate: func(m map[string]map[string]any) { m["/v1/invoices/in_paid"]["amount_paid"] = 400 }, quarantine: true},
		{name: "off_stripe_payment_does_not_establish_authenticated_payment", mutate: func(m map[string]map[string]any) { m["/v1/invoices/in_paid"]["amount_paid_off_stripe"] = 840 }, quarantine: true},
		{name: "unallocated_customer_balance_is_quarantined", mutate: func(m map[string]map[string]any) { m["/v1/invoices/in_paid"]["starting_balance"] = -100 }, quarantine: true},
		{name: "pre_payment_credit_note_requires_explicit_allocation", mutate: func(m map[string]map[string]any) { m["/v1/invoices/in_paid"]["pre_payment_credit_notes_amount"] = 100 }, quarantine: true},
		{name: "paid_status_without_actual_provider_payment_is_quarantined", mutate: func(m map[string]map[string]any) { m["/v1/invoice_payments"]["data"] = []any{} }, quarantine: true},
		{name: "multiple_successful_payment_allocations_are_not_assumed", mutate: func(m map[string]map[string]any) {
			second := map[string]any{}
			for k, v := range fixturePayment(m) {
				second[k] = v
			}
			second["id"] = "inpay_two"
			m["/v1/invoice_payments"]["data"] = append(m["/v1/invoice_payments"]["data"].([]any), second)
		}, quarantine: true},
		{name: "authenticated_mode_must_match_signed_mode", mutate: func(m map[string]map[string]any) { m["/v1/payment_intents/pi_paid"]["livemode"] = true }, quarantine: true},
		{name: "authenticated_account_must_match_explicit_account", mutate: func(m map[string]map[string]any) { m["/v1/account"]["id"] = "acct_other" }, quarantine: true},
		{name: "payment_customer_must_match_invoice_customer", mutate: func(m map[string]map[string]any) { m["/v1/payment_intents/pi_paid"]["customer"] = "cus_other" }, quarantine: true},
		{name: "line_subscription_must_match_invoice", mutate: func(m map[string]map[string]any) {
			fixtureLine(m)["parent"] = map[string]any{"subscription_item_details": map[string]any{"subscription": "sub_other"}}
		}, quarantine: true},
		{name: "negative_line_has_no_invented_credit_allocation", mutate: func(m map[string]map[string]any) { fixtureLine(m)["subtotal"] = -100 }, quarantine: true},
		{name: "missing_explicit_ex_tax_amount_is_quarantined", mutate: func(m map[string]map[string]any) { delete(fixtureLine(m), "subtotal"); fixtureLine(m)["amount"] = 840 }, quarantine: true},
		{name: "missing_provider_mode_is_not_test_mode", mutate: func(m map[string]map[string]any) { delete(fixtureLine(m), "livemode") }, quarantine: true},
		{name: "transport_outage_must_not_be_acknowledged_as_quarantine", apiFailure: "/v1/invoices/in_paid/lines", wantErr: ErrPaymentProviderAPIRequestFailed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			responses := revenueFixture()
			if tc.mutate != nil {
				tc.mutate(responses)
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer sk_fixture" || r.Header.Get("Stripe-Version") != StripeDefaultAPIVersion || r.Header.Get("Stripe-Account") != "" {
					t.Error("provider authentication/scope headers differ")
				}
				if r.URL.Path == "/v1/invoice_payments" && r.URL.Query().Get("invoice") != "in_paid" {
					t.Error("invoice-payment query is not bound to source")
				}
				if r.URL.Path == tc.apiFailure {
					w.WriteHeader(http.StatusServiceUnavailable)
					return
				}
				value, ok := responses[r.URL.Path]
				if !ok {
					t.Errorf("unexpected current price/access lookup: %s", r.URL.Path)
					w.WriteHeader(404)
					return
				}
				_ = json.NewEncoder(w).Encode(value)
			}))
			t.Cleanup(server.Close)
			p, err := NewStripeProvider(&Config{APIKey: "sk_fixture", WebhookSecret: "whsec_fixture", APIBaseURL: server.URL, Revenue: &RevenueConfig{AccountID: "acct_primary", CurrencyExponents: map[string]int{"GBP": 2}}})
			if err != nil {
				t.Fatal(err)
			}
			eventType := tc.eventType
			if eventType == "" {
				eventType = "invoice.paid"
			}
			body, _ := json.Marshal(map[string]any{"id": "evt_invoice", "type": eventType, "created": 1700000001, "livemode": false, "data": map[string]any{"object": map[string]any{"id": "in_paid"}}})
			req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(string(body)))
			req.Header.Set(stripeSignatureHeader, stripeTestSignature("whsec_fixture", time.Now().Unix(), body))
			evidence, err := p.ResolveRevenueWebhook(context.Background(), req)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("error=%v want=%v", err, tc.wantErr)
				}
				if evidence != nil {
					t.Fatal("outage returned apparently accepted evidence")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			restored, _ := io.ReadAll(req.Body)
			if string(restored) != string(body) {
				t.Fatal("webhook body was consumed")
			}
			if tc.quarantine {
				if evidence.QuarantineReason == "" || evidence.Invoice != nil {
					t.Fatalf("ambiguous invoice=%+v", evidence)
				}
				return
			}
			invoice := evidence.Invoice
			if invoice == nil || invoice.GrossPaidMinor != 840 || invoice.PaymentID != "pi_paid" || invoice.Currency != "GBP" || len(invoice.Lines) != 1 || invoice.Lines[0].NetPaidMinor != 700 || invoice.Lines[0].PriceID != "price_frozen" || !invoice.PaidAt.Equal(time.Unix(1700000000, 0).UTC()) {
				t.Fatalf("invoice=%+v", invoice)
			}
		})
	}
}

func TestStripeRevenueTrustBoundaries(t *testing.T) {
	type testCase struct {
		name      string
		account   string
		mode      any
		signature bool
		connected bool
		wantErr   error
	}
	cases := []testCase{
		{name: "invalid_signature_prevents_lookup", mode: false, wantErr: ErrPaymentProviderInvalidWebhookSignature},
		{name: "missing_signed_mode_is_invalid", mode: nil, signature: true, wantErr: ErrPaymentProviderInvalidPayload},
		{name: "signed_live_mode_cannot_be_inferred_from_test_environment", mode: true, signature: true, wantErr: ErrRevenueUnassessable},
		{name: "unlisted_connect_account_is_denied", mode: false, account: "acct_unknown", signature: true, wantErr: ErrRevenueUnassessable},
		{name: "configured_connect_account_is_verified_with_scoped_api", mode: false, account: "acct_connected", signature: true, connected: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int64
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.Header.Get("Stripe-Account") != "acct_connected" {
					t.Error("connected scope header missing")
				}
				m := revenueFixture()
				m["/v1/account"]["id"] = "acct_connected"
				_ = json.NewEncoder(w).Encode(m[r.URL.Path])
			}))
			t.Cleanup(server.Close)
			p, err := NewStripeProvider(&Config{APIKey: "sk_fixture", WebhookSecret: "whsec_fixture", APIBaseURL: server.URL, Environment: "test", Revenue: &RevenueConfig{AccountID: "acct_primary", ConnectedAccountIDs: []string{"acct_connected"}, CurrencyExponents: map[string]int{"GBP": 2}}})
			if err != nil {
				t.Fatal(err)
			}
			body, _ := json.Marshal(map[string]any{"id": "evt_invoice", "type": "invoice.paid", "created": 1700000001, "account": tc.account, "livemode": tc.mode, "data": map[string]any{"object": map[string]any{"id": "in_paid"}}})
			req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(string(body)))
			req.Header.Set(stripeSignatureHeader, stripeTestSignature("wrong_secret", time.Now().Unix(), body))
			if tc.signature {
				req.Header.Set(stripeSignatureHeader, stripeTestSignature("whsec_fixture", time.Now().Unix(), body))
			}
			evidence, err := p.ResolveRevenueWebhook(context.Background(), req)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("error=%v want=%v", err, tc.wantErr)
				}
				if calls.Load() != 0 {
					t.Fatal("untrusted envelope made provider API call")
				}
				return
			}
			if err != nil || evidence.Invoice == nil || evidence.Scope.AccountID != "acct_connected" || calls.Load() == 0 {
				t.Fatalf("evidence=%+v error=%v", evidence, err)
			}
		})
	}
}

func TestStripeRevenueLineArithmetic(t *testing.T) {
	type testCase struct {
		name    string
		body    string
		want    int64
		invalid bool
	}
	cases := []testCase{
		{name: "modern_discount_in_pretax_is_not_subtracted_twice", body: `{"subtotal":1000,"pretax_credit_amounts":[{"amount":200},{"amount":100}],"discount_amounts":[{"amount":200}]}`, want: 700},
		{name: "legacy_explicit_ex_tax", body: `{"amount_excluding_tax":1000,"discount_amounts":[{"amount":200}]}`, want: 800},
		{name: "zero_net_after_full_credit", body: `{"subtotal":1000,"pretax_credit_amounts":[{"amount":1000}]}`, want: 0},
		{name: "credit_cannot_exceed_line", body: `{"subtotal":1000,"pretax_credit_amounts":[{"amount":1001}]}`, invalid: true},
		{name: "negative_credit_is_invalid", body: `{"subtotal":1000,"pretax_credit_amounts":[{"amount":-1}]}`, invalid: true},
		{name: "large_credit_sum_does_not_overflow", body: `{"subtotal":9223372036854775807,"pretax_credit_amounts":[{"amount":9223372036854775807},{"amount":9223372036854775807}]}`, invalid: true},
		{name: "max_base_is_exact", body: `{"subtotal":9223372036854775807,"pretax_credit_amounts":[]}`, want: math.MaxInt64},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var line map[string]json.RawMessage
			_ = json.Unmarshal([]byte(tc.body), &line)
			value, err := stripeRevenueLineNet(line)
			if tc.invalid {
				if !errors.Is(err, ErrRevenueUnassessable) {
					t.Fatalf("value=%d error=%v", value, err)
				}
				return
			}
			if err != nil || value != tc.want {
				t.Fatalf("value=%d error=%v", value, err)
			}
		})
	}
}
