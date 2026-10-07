package paymentprovider

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Audit disposition: new named authenticated original-event recovery cases;
// these calls do not fabricate signatures or submit provider side effects.
func TestStripeRevenueAuthenticatedEventRecovery(t *testing.T) {
	type testCase struct {
		name          string
		mutate        func(map[string]any)
		want          error
		digestChanged bool
	}
	cases := []testCase{{name: "immutable_signed_source_matches_authenticated_event"}, {name: "refetched_event_id_must_match", mutate: func(e map[string]any) { e["id"] = "evt_other" }, want: ErrPaymentProviderAPIResponseInvalid}, {name: "refetched_mode_must_match_persisted_scope", mutate: func(e map[string]any) { e["livemode"] = true }, want: ErrRevenueUnassessable}, {name: "refetched_account_must_match_persisted_scope", mutate: func(e map[string]any) { e["account"] = "acct_unknown" }, want: ErrRevenueUnassessable}, {name: "changed_original_snapshot_changes_private_digest", mutate: func(e map[string]any) { e["data"].(map[string]any)["object"].(map[string]any)["amount_paid"] = 999 }, digestChanged: true}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			responses := revenueFixture()
			event := map[string]any{"id": "evt_invoice", "type": "invoice.paid", "created": 1700000001, "livemode": false, "data": map[string]any{"object": map[string]any{"id": "in_paid", "amount_paid": 840}}}
			body, _ := json.Marshal(event)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer fixture-key" {
					t.Error("authenticated lookup missing key")
				}
				if r.URL.Path == "/v1/events/evt_invoice" {
					_ = json.NewEncoder(w).Encode(event)
					return
				}
				_ = json.NewEncoder(w).Encode(responses[r.URL.Path])
			}))
			t.Cleanup(server.Close)
			p, err := NewStripeProvider(&Config{WebhookSecret: "fixture-secret", APIKey: "fixture-key", APIBaseURL: server.URL, Revenue: &RevenueConfig{AccountID: "acct_primary", CurrencyExponents: map[string]int{"GBP": 2}}})
			if err != nil {
				t.Fatal(err)
			}
			req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(string(body)))
			req.Header.Set(stripeSignatureHeader, stripeTestSignature("fixture-secret", time.Now().Unix(), body))
			original, err := p.ResolveRevenueWebhook(context.Background(), req)
			if err != nil {
				t.Fatal(err)
			}
			if tc.mutate != nil {
				tc.mutate(event)
			}
			recovered, err := p.ReconcileRevenueEvent(context.Background(), original.Scope, original.EnvelopeID)
			if tc.want != nil {
				if !errors.Is(err, tc.want) || recovered != nil {
					t.Fatalf("recovered=%+v error=%v", recovered, err)
				}
				return
			}
			if err != nil || recovered == nil || recovered.Invoice == nil {
				t.Fatalf("recovered=%+v error=%v", recovered, err)
			}
			if original.SourceFingerprint == "" || (recovered.SourceFingerprint != original.SourceFingerprint) != tc.digestChanged {
				t.Fatal("source fingerprint did not reflect immutable event")
			}
		})
	}
}
