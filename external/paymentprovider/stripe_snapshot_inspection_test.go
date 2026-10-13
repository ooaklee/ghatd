package paymentprovider

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// The inspection is checked against a fingerprint produced by the native
// signed-delivery verifier, then subjected to altered evidence. No API call or
// newly accepted delivery is part of the inspected operation.
func TestStripeSnapshotInspectionExistingFormats(t *testing.T) {
	cases := []struct {
		name           string
		legacy, refund bool
		want           error
	}{
		{"current_invoice", false, false, nil},
		{"legacy_invoice", true, false, nil},
		{"current_amount_changed", false, false, ErrRevenueUnassessable},
		{"wrong_scope", false, false, ErrRevenueUnassessable},
		{"invalid_current_digest", false, false, ErrPaymentProviderInvalidPayload},
		{"unknown_version", false, false, ErrPaymentProviderInvalidPayload},
		{"current_refund_receipt_url_changed", false, true, nil},
		{"legacy_refund_receipt_url_changed", true, true, ErrRevenueUnassessable},
		{"current_refund_economics_changed", false, true, ErrRevenueUnassessable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { calls.Add(1); w.WriteHeader(503) }))
			t.Cleanup(server.Close)
			p, err := NewStripeProvider(&Config{APIKey: "fixture-key", WebhookSecret: "fixture-secret", APIBaseURL: server.URL, Revenue: &RevenueConfig{AccountID: "acct_primary", CurrencyExponents: map[string]int{"GBP": 2}}})
			require.NoError(t, err)
			kind := "invoice.paid"
			object := map[string]any{"id": "in_original", "object": "invoice", "amount_paid": 1000}
			if tc.refund {
				kind = "charge.refunded"
				object = map[string]any{"id": "ch_original", "object": "charge", "amount_refunded": 100, "receipt_url": "https://receipt.example/original"}
			}
			event := map[string]any{"id": "evt_original", "type": kind, "created": 1700000001, "livemode": false, "data": map[string]any{"object": object}}
			original, err := json.Marshal(event)
			require.NoError(t, err)
			signed := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(string(original)))
			signed.Header.Set(stripeSignatureHeader, stripeTestSignature("fixture-secret", time.Now().Unix(), original))
			identity, err := p.VerifyRevenueDelivery(t.Context(), signed)
			require.NoError(t, err)
			fingerprint := identity.SourceFingerprint
			if tc.legacy {
				fingerprint = identity.LegacySourceFingerprint
			}
			scope := identity.Scope
			switch tc.name {
			case "current_amount_changed":
				object["amount_paid"] = 1001
			case "wrong_scope":
				scope.AccountID = "acct_other"
			case "invalid_current_digest":
				fingerprint = stripeRevenueDigestV2 + strings.Repeat("z", 64)
			case "unknown_version":
				fingerprint = "stripe-revenue-v99:" + identity.LegacySourceFingerprint
			case "current_refund_receipt_url_changed", "legacy_refund_receipt_url_changed":
				object["receipt_url"] = "https://receipt.example/new-render"
			case "current_refund_economics_changed":
				object["amount_refunded"] = 101
			}
			raw, err := json.Marshal(event)
			require.NoError(t, err)
			request := RevenueSnapshotRequest{Scope: scope, EnvelopeID: identity.EnvelopeID, OriginalFingerprint: fingerprint, OriginalSnapshot: raw}
			got, err := p.InspectRetainedRevenueSnapshot(t.Context(), request)
			if tc.want != nil {
				require.ErrorIs(t, err, tc.want)
				require.Equal(t, RevenueSnapshotIdentity{}, got)
			} else {
				require.NoError(t, err)
				require.Equal(t, identity.SourceFingerprint, got.CanonicalFingerprint)
			}
			_, strictErr := p.VerifyRetainedRevenueSnapshot(t.Context(), request)
			if !tc.legacy {
				require.ErrorIs(t, strictErr, ErrPaymentProviderInvalidPayload)
			} else if tc.want == nil {
				require.NoError(t, strictErr)
			} else {
				require.ErrorIs(t, strictErr, tc.want)
			}
			require.Zero(t, calls.Load())
		})
	}
}
