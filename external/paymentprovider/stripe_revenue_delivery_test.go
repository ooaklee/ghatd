package paymentprovider

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Audit disposition: new named cases isolate fresh signed delivery identity
// from economic lookup. The server always fails; no Stripe side effects occur.
func TestStripeRevenueDeliveryVerificationWithoutAPI(t *testing.T) {
	type testCase struct {
		name             string
		mutate           func(map[string]any)
		signature        bool
		missingSignature bool
		connected        bool
		want             error
	}
	cases := []testCase{
		{name: "fresh_signed_identity_survives_provider_outage", signature: true},
		{name: "allowlisted_connected_account_is_in_delivery_scope", signature: true, connected: true, mutate: func(e map[string]any) { e["account"] = "acct_connected" }},
		{name: "invalid_signature_cannot_acknowledge_an_old_delivery", want: ErrPaymentProviderInvalidWebhookSignature},
		{name: "missing_signature_cannot_acknowledge_an_old_delivery", missingSignature: true, want: ErrPaymentProviderMissingSignature},
		{name: "unsigned_mode_is_not_inferred", signature: true, mutate: func(e map[string]any) { delete(e, "livemode") }, want: ErrPaymentProviderInvalidPayload},
		{name: "signed_mode_must_match_explicit_mode", signature: true, mutate: func(e map[string]any) { e["livemode"] = true }, want: ErrRevenueUnassessable},
		{name: "unlisted_connected_account_cannot_replay_other_scope", signature: true, mutate: func(e map[string]any) { e["account"] = "acct_unknown" }, want: ErrRevenueUnassessable},
		{name: "nonfinancial_events_have_no_revenue_identity", signature: true, mutate: func(e map[string]any) { e["type"] = "customer.created" }, want: ErrRevenueEventNotRelevant},
		{name: "invalid_envelope_identity_is_rejected", signature: true, mutate: func(e map[string]any) { e["id"] = "bad/id" }, want: ErrPaymentProviderInvalidPayload},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int64
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.WriteHeader(http.StatusServiceUnavailable)
			}))
			t.Cleanup(server.Close)
			cfg := &RevenueConfig{AccountID: "acct_primary", CurrencyExponents: map[string]int{"GBP": 2}}
			if tc.connected {
				cfg.ConnectedAccountIDs = []string{"acct_connected"}
			}
			p, err := NewStripeProvider(&Config{APIKey: "fixture-key", WebhookSecret: "fixture-secret", APIBaseURL: server.URL, Revenue: cfg})
			require.NoError(t, err)
			event := map[string]any{"id": "evt_original", "type": "invoice.paid", "created": 1700000001, "livemode": false, "data": map[string]any{"object": map[string]any{"id": "in_paid"}}}
			if tc.mutate != nil {
				tc.mutate(event)
			}
			body, err := json.Marshal(event)
			require.NoError(t, err)
			req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(string(body)))
			if tc.signature {
				req.Header.Set(stripeSignatureHeader, stripeTestSignature("fixture-secret", time.Now().Unix(), body))
			} else if !tc.missingSignature {
				req.Header.Set(stripeSignatureHeader, stripeTestSignature("incorrect-secret", time.Now().Unix(), body))
			}
			identity, err := p.VerifyRevenueDelivery(context.Background(), req)
			if tc.want != nil {
				require.ErrorIs(t, err, tc.want)
				require.Empty(t, identity.EnvelopeID)
			} else {
				require.NoError(t, err)
				require.Equal(t, "evt_original", identity.EnvelopeID)
				require.Len(t, identity.SourceFingerprint, 64)
				account := "acct_primary"
				if tc.connected {
					account = "acct_connected"
				}
				require.Equal(t, RevenueScope{Provider: "stripe", AccountID: account}, identity.Scope)
				restored, err := io.ReadAll(req.Body)
				require.NoError(t, err)
				require.Equal(t, body, restored)
			}
			require.Zero(t, calls.Load())
		})
	}
}

func TestStripeRevenueDeliveryCanonicalDigest(t *testing.T) {
	type testCase struct {
		name, body string
		changed    bool
	}
	cases := []testCase{
		{name: "key_order_and_whitespace_preserve_native_snapshot", body: `{ "data":{"object":{"amount_paid":840,"id":"in_paid"}}, "livemode":false,"created":1700000001,"type":"invoice.paid","id":"evt_original" }`},
		{name: "changed_native_amount_changes_evidence", changed: true, body: `{"id":"evt_original","type":"invoice.paid","created":1700000001,"livemode":false,"data":{"object":{"id":"in_paid","amount_paid":841}}}`},
	}
	p, err := NewStripeProvider(&Config{APIKey: "fixture-key", WebhookSecret: "fixture-secret", Revenue: &RevenueConfig{AccountID: "acct_primary", CurrencyExponents: map[string]int{"GBP": 2}}})
	require.NoError(t, err)
	verify := func(body string) RevenueDeliveryIdentity {
		req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(body))
		req.Header.Set(stripeSignatureHeader, stripeTestSignature("fixture-secret", time.Now().Unix(), []byte(body)))
		identity, err := p.VerifyRevenueDelivery(context.Background(), req)
		require.NoError(t, err)
		return identity
	}
	original := verify(`{"id":"evt_original","type":"invoice.paid","created":1700000001,"livemode":false,"data":{"object":{"id":"in_paid","amount_paid":840}}}`)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			identity := verify(tc.body)
			require.Equal(t, tc.changed, identity.SourceFingerprint != original.SourceFingerprint)
		})
	}
}
