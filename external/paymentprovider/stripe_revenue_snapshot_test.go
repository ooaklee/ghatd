package paymentprovider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

// Audit disposition: named retained-proof cases require an exact legacy hash,
// selected source scope and bounded private input without any provider request.
func TestStripeRetainedRevenueSnapshotProof(t *testing.T) {
	cases := []struct {
		name      string
		mutate    func(*RevenueSnapshotRequest)
		limitBody bool
		want      error
	}{
		{name: "exact_retained_snapshot_derives_stable_identity"},
		{name: "missing_snapshot", mutate: func(r *RevenueSnapshotRequest) { r.OriginalSnapshot = nil }, want: ErrPaymentProviderInvalidPayload},
		{name: "oversized_snapshot", limitBody: true, want: ErrPaymentProviderInvalidPayload},
		{name: "malformed_json", mutate: func(r *RevenueSnapshotRequest) { r.OriginalSnapshot = []byte("{") }, want: ErrPaymentProviderInvalidPayload},
		{name: "missing_legacy_hash", mutate: func(r *RevenueSnapshotRequest) { r.OriginalFingerprint = "" }, want: ErrPaymentProviderInvalidPayload},
		{name: "versioned_hash_is_not_a_legacy_proof", mutate: func(r *RevenueSnapshotRequest) { r.OriginalFingerprint = stripeRevenueDigestV2 + r.OriginalFingerprint }, want: ErrPaymentProviderInvalidPayload},
		{name: "changed_legacy_hash", mutate: func(r *RevenueSnapshotRequest) { r.OriginalFingerprint = strings.Repeat("0", 64) }, want: ErrRevenueUnassessable},
		{name: "changed_original_receipt_url_is_not_an_exact_proof", mutate: func(r *RevenueSnapshotRequest) {
			r.OriginalSnapshot = []byte(strings.ReplaceAll(string(r.OriginalSnapshot), "/original", "/changed"))
		}, want: ErrRevenueUnassessable},
		{name: "changed_economics_is_not_an_exact_proof", mutate: func(r *RevenueSnapshotRequest) {
			r.OriginalSnapshot = []byte(strings.ReplaceAll(string(r.OriginalSnapshot), `"amount_refunded":100`, `"amount_refunded":200`))
		}, want: ErrRevenueUnassessable},
		{name: "different_selected_event", mutate: func(r *RevenueSnapshotRequest) { r.EnvelopeID = "evt_other" }, want: ErrPaymentProviderInvalidPayload},
		{name: "different_selected_account", mutate: func(r *RevenueSnapshotRequest) { r.Scope.AccountID = "acct_other" }, want: ErrRevenueUnassessable},
		{name: "different_selected_mode", mutate: func(r *RevenueSnapshotRequest) { r.Scope.LiveMode = true }, want: ErrRevenueUnassessable},
		{name: "different_selected_provider", mutate: func(r *RevenueSnapshotRequest) { r.Scope.Provider = "other" }, want: ErrRevenueUnassessable},
		{name: "unlisted_snapshot_account", mutate: func(r *RevenueSnapshotRequest) {
			r.OriginalSnapshot = []byte(strings.ReplaceAll(string(r.OriginalSnapshot), "acct_primary", "acct_other"))
		}, want: ErrRevenueUnassessable},
		{name: "missing_snapshot_mode", mutate: func(r *RevenueSnapshotRequest) {
			r.OriginalSnapshot = []byte(strings.ReplaceAll(string(r.OriginalSnapshot), `"livemode":false,`, ""))
		}, want: ErrPaymentProviderInvalidPayload},
		{name: "unrelated_event", mutate: func(r *RevenueSnapshotRequest) {
			r.OriginalSnapshot = []byte(strings.ReplaceAll(string(r.OriginalSnapshot), "charge.refunded", "customer.created"))
		}, want: ErrRevenueEventNotRelevant},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int64
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				calls.Add(1)
				w.WriteHeader(http.StatusServiceUnavailable)
			}))
			t.Cleanup(server.Close)
			p, err := NewStripeProvider(&Config{APIKey: "fixture-key", WebhookSecret: "fixture-secret", APIBaseURL: server.URL, Revenue: &RevenueConfig{AccountID: "acct_primary", CurrencyExponents: map[string]int{"GBP": 2}}})
			require.NoError(t, err)
			req := RevenueSnapshotRequest{Scope: RevenueScope{Provider: "stripe", AccountID: "acct_primary"}, EnvelopeID: "evt_refund", OriginalFingerprint: stripeRefundLegacyFixtureDigest, OriginalSnapshot: []byte(stripeRefundDigestFixture)}
			if tc.mutate != nil {
				tc.mutate(&req)
			}
			if tc.limitBody {
				p.maxWebhookBodySize = int64(len(req.OriginalSnapshot) - 1)
			}
			identity, err := p.VerifyRetainedRevenueSnapshot(context.Background(), req)
			if tc.want != nil {
				require.ErrorIs(t, err, tc.want)
				require.Empty(t, identity.EnvelopeID)
			} else {
				require.NoError(t, err)
				require.Equal(t, req.Scope, identity.Scope)
				require.Equal(t, req.EnvelopeID, identity.EnvelopeID)
				require.Equal(t, req.OriginalFingerprint, identity.OriginalFingerprint)
				require.True(t, strings.HasPrefix(identity.CanonicalFingerprint, stripeRevenueDigestV2))
				encoded, err := json.Marshal(identity)
				require.NoError(t, err)
				require.NotContains(t, string(encoded), identity.OriginalFingerprint)
				require.NotContains(t, string(encoded), identity.CanonicalFingerprint)
				encoded, err = json.Marshal(req)
				require.NoError(t, err)
				require.NotContains(t, string(encoded), "receipt_url")
			}
			require.Zero(t, calls.Load())
		})
	}
}
