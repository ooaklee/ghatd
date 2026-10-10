package billingmanager

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/billing/revenuestore"
	"github.com/ooaklee/ghatd/external/encryption"
	"github.com/ooaklee/ghatd/external/paymentprovider"
	"github.com/ooaklee/ghatd/external/repository/recordstore"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

type snapshotNativeClock struct{ at time.Time }

func (c snapshotNativeClock) Now() time.Time { return c.at }

type snapshotNativeAuthority struct{ denied atomic.Bool }

func (a *snapshotNativeAuthority) AuthorizeRevenueReconciliation(context.Context, string) error {
	if a.denied.Load() {
		return billing.ErrRevenueUnavailable
	}
	return nil
}

func snapshotNativeFeed(t *testing.T) (*billing.RevenueService, context.Context) {
	t.Helper()
	uri := os.Getenv("GHATD_TEST_MONGO_URI")
	if uri == "" {
		t.Skip("set GHATD_TEST_MONGO_URI to an isolated replica set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)
	client, err := mongo.Connect(options.Client().ApplyURI(uri))
	require.NoError(t, err)
	db := client.Database("revenue_snapshot_test_" + strings.ToLower(rand.Text()))
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		require.NoError(t, db.Drop(ctx))
		require.NoError(t, client.Disconnect(ctx))
	})
	cipher, err := encryption.NewPayloadCipher([]byte("01234567890123456789012345678901"))
	require.NoError(t, err)
	store, err := recordstore.NewMongoStoreFromDatabase(db, cipher)
	require.NoError(t, err)
	require.NoError(t, store.EnsureIndexes(ctx))
	require.NoError(t, store.Probe(ctx))
	repo, err := revenuestore.NewRepository(store)
	require.NoError(t, err)
	feed, err := billing.NewRevenueService(repo, snapshotNativeClock{at: time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC)})
	require.NoError(t, err)
	return feed, ctx
}

func snapshotSignedRequest(t *testing.T, body []byte) *http.Request {
	t.Helper()
	at := time.Now().Unix()
	hash := hmac.New(sha256.New, []byte("snapshot-fixture-secret"))
	_, err := fmt.Fprintf(hash, "%d.%s", at, body)
	require.NoError(t, err)
	r := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(string(body)))
	r.Header.Set("Stripe-Signature", fmt.Sprintf("t=%d,v1=%s", at, hex.EncodeToString(hash.Sum(nil))))
	return r
}

// Audit disposition: native encrypted transactions, real Stripe adapter and
// manager boundaries use controlled GET-only provider responses. Named cases
// prove automatic new-source recovery, explicit legacy proof, concurrent
// record-once recovery and signed replay without rewriting the quarantine.
func TestNativeStripeSnapshotRecovery(t *testing.T) {
	cases := []struct {
		name                                                               string
		legacy, proof, unchanged, changedAmount, tamperedProof, concurrent bool
		want                                                               error
	}{
		{name: "versioned_quarantine_recovers_across_rendered_url_without_manual_proof"},
		{name: "legacy_quarantine_recovers_with_exact_retained_snapshot", legacy: true, proof: true},
		{name: "legacy_unchanged_event_recovers_without_manual_proof", legacy: true, unchanged: true},
		{name: "legacy_changed_event_without_proof_remains_pending", legacy: true, want: billing.ErrRevenueInvalid},
		{name: "tampered_original_snapshot_cannot_bridge_legacy_source", legacy: true, proof: true, tamperedProof: true, want: paymentprovider.ErrRevenueUnassessable},
		{name: "changed_remote_amount_cannot_bridge_legacy_source", legacy: true, proof: true, changedAmount: true, want: billing.ErrRevenueInvalid},
		{name: "concurrent_legacy_recovery_commits_one_refund_and_resolution", legacy: true, proof: true, concurrent: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			feed, ctx := snapshotNativeFeed(t)
			var outage atomic.Bool
			var reads atomic.Int64
			originalBody := []byte(`{"id":"evt_refund","type":"charge.refunded","created":1700000001,"livemode":false,"data":{"object":{"id":"ch_paid","object":"charge","amount_refunded":840,"currency":"gbp","payment_intent":"pi_paid","receipt_url":"https://receipts.example.test/original"}}}`)
			remoteBody := originalBody
			if !tc.unchanged {
				remoteBody = []byte(strings.ReplaceAll(string(remoteBody), "/original", "/event-rendered"))
			}
			if tc.changedAmount {
				remoteBody = []byte(strings.ReplaceAll(string(remoteBody), `"amount_refunded":840`, `"amount_refunded":800`))
			}
			responses := map[string]string{
				"/v1/account":                 `{"id":"acct_primary"}`,
				"/v1/events/evt_refund":       string(remoteBody),
				"/v1/invoices/in_paid":        `{"id":"in_paid","livemode":false,"status":"paid","currency":"gbp","customer":"cus_payer","amount_paid":840,"total":840,"total_excluding_tax":700,"amount_remaining":0,"starting_balance":0,"pre_payment_credit_notes_amount":0,"status_transitions":{"paid_at":1700000000},"parent":{"subscription_details":{"subscription":"sub_recurring"}}}`,
				"/v1/invoice_payments":        `{"object":"list","has_more":false,"data":[{"id":"inpay_one","invoice":"in_paid","livemode":false,"currency":"gbp","status":"paid","amount_paid":840,"payment":{"type":"payment_intent","payment_intent":"pi_paid"}}]}`,
				"/v1/payment_intents/pi_paid": `{"id":"pi_paid","livemode":false,"status":"succeeded","currency":"gbp","amount_received":840,"customer":"cus_payer","latest_charge":"ch_paid"}`,
				"/v1/invoices/in_paid/lines":  `{"object":"list","has_more":false,"data":[{"id":"il_subscription","livemode":false,"currency":"gbp","subtotal":1000,"pretax_credit_amounts":[{"amount":200,"type":"discount"},{"amount":100,"type":"credit_balance_transaction"}],"discount_amounts":[{"amount":200}],"parent":{"subscription_item_details":{"subscription":"sub_recurring"}},"pricing":{"price_details":{"price":"price_frozen"}}}]}`,
				"/v1/charges/ch_paid":         `{"id":"ch_paid","livemode":false,"amount":840,"amount_refunded":840,"payment_intent":"pi_paid","customer":"cus_payer","currency":"gbp"}`,
				"/v1/refunds":                 `{"object":"list","has_more":false,"data":[{"id":"re_full","status":"succeeded","amount":840,"charge":"ch_paid","payment_intent":"pi_paid","currency":"gbp"}]}`,
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					t.Error("recovery attempted a provider mutation")
					w.WriteHeader(http.StatusMethodNotAllowed)
					return
				}
				reads.Add(1)
				if r.Header.Get("Authorization") != "Bearer snapshot-fixture-key" || r.Header.Get("Stripe-Version") != paymentprovider.StripeDefaultAPIVersion {
					t.Error("authenticated pinned provider lookup required")
				}
				if outage.Load() {
					w.WriteHeader(http.StatusServiceUnavailable)
					return
				}
				body, ok := responses[r.URL.Path]
				if !ok {
					t.Errorf("unexpected lookup %s", r.URL.Path)
					w.WriteHeader(http.StatusNotFound)
					return
				}
				_, _ = w.Write([]byte(body))
			}))
			t.Cleanup(server.Close)
			provider, err := paymentprovider.NewStripeProvider(&paymentprovider.Config{APIKey: "snapshot-fixture-key", WebhookSecret: "snapshot-fixture-secret", APIBaseURL: server.URL, Revenue: &paymentprovider.RevenueConfig{AccountID: "acct_primary", CurrencyExponents: map[string]int{"GBP": 2}}})
			require.NoError(t, err)
			identity, err := provider.VerifyRevenueDelivery(ctx, snapshotSignedRequest(t, originalBody))
			require.NoError(t, err)
			scope := billingRevenueScope(identity.Scope)
			invoice, err := provider.LookupRevenueInvoice(ctx, paymentprovider.RevenueInvoiceRequest{Scope: identity.Scope, InvoiceID: "in_paid", PaymentID: "pi_paid"})
			require.NoError(t, err)
			line := invoice.Lines[0]
			payment, err := feed.AcceptVerified(ctx, billing.VerifiedRevenueRequest{Scope: scope, EnvelopeID: "evt_paid", Facts: []billing.RevenueFact{{Scope: scope, Kind: billing.RevenuePayment, PaymentID: invoice.PaymentID, InvoiceID: invoice.InvoiceID, AllocationID: line.ID, PrincipalID: "original-payer", SubscriptionID: line.SubscriptionID, PlanID: "original-plan", CostID: "original-cost", ProviderPriceID: line.PriceID, ProviderCustomerID: invoice.CustomerID, Currency: invoice.Currency, CurrencyExponent: invoice.CurrencyExponent, PaidMinor: line.NetPaidMinor, EffectiveAt: invoice.PaidAt}}})
			require.NoError(t, err)
			sourceHash := identity.SourceFingerprint
			if tc.legacy {
				sourceHash = identity.LegacySourceFingerprint
			}
			original, err := feed.AcceptVerified(ctx, billing.VerifiedRevenueRequest{Scope: scope, EnvelopeID: identity.EnvelopeID, SourceFingerprint: sourceHash, QuarantineReason: "invoice_economics_unassessable"})
			require.NoError(t, err)
			authority := &snapshotNativeAuthority{}
			association := &revenueBoundaryAssociation{}
			manager, err := (&Service{}).WithRevenueServices(revenueBoundaryRegistry{p: provider}, feed, association)
			require.NoError(t, err)
			_, err = manager.WithRevenueReconciliationAuthority(authority)
			require.NoError(t, err)
			req := ReconcileRevenueSourceRequest{ObservationID: original.ID, ExpectedFingerprint: original.Fingerprint, Reason: "verified-source-recovered", ActorID: "current-worker"}
			if tc.proof {
				req.OriginalSnapshot = originalBody
			}
			if tc.tamperedProof {
				req.OriginalSnapshot = []byte(strings.ReplaceAll(string(originalBody), "/original", "/tampered"))
			}
			if tc.concurrent {
				start := make(chan struct{})
				results := make(chan error, 8)
				var workers sync.WaitGroup
				for i := 0; i < 8; i++ {
					workers.Add(1)
					go func() { defer workers.Done(); <-start; _, e := manager.ReconcileRevenueSource(ctx, req); results <- e }()
				}
				close(start)
				workers.Wait()
				close(results)
				for err := range results {
					require.NoError(t, err)
				}
			}
			resolution, err := manager.ReconcileRevenueSource(ctx, req)
			if tc.want != nil {
				require.ErrorIs(t, err, tc.want)
				require.Empty(t, resolution.ID)
				_, err = feed.GetRevenueSourceResolution(ctx, original.ID)
				require.ErrorIs(t, err, billing.ErrRevenueNotFound)
				facts, err := feed.PendingRevenueFacts(ctx, "test-consumer", 200)
				require.NoError(t, err)
				require.Len(t, facts, 1)
			} else {
				require.NoError(t, err)
				require.Equal(t, original.ID, resolution.ResolutionOf)
				require.Equal(t, original.SourceFingerprint, resolution.SourceFingerprint)
				require.Equal(t, identity.SourceFingerprint, resolution.RecoveryFingerprint)
				require.Len(t, resolution.FactIDs, 1)
				refund, err := feed.GetRevenueFact(ctx, resolution.FactIDs[0])
				require.NoError(t, err)
				require.Equal(t, billing.RevenueRefund, refund.Kind)
				require.EqualValues(t, 700, refund.CumulativeRefundedMinor)
				require.Equal(t, "original-payer", refund.PrincipalID)
				require.Equal(t, "original-plan", refund.PlanID)
				require.Empty(t, association.requests)
				originalFacts, err := feed.FindPaymentRevenueFacts(ctx, scope, "pi_paid")
				require.NoError(t, err)
				require.Len(t, originalFacts, 1)
				require.Equal(t, payment.FactIDs[0], originalFacts[0].ID)
				outage.Store(true)
				before := reads.Load()
				replay, err := manager.ReconcileRevenueSource(ctx, req)
				require.NoError(t, err)
				require.Equal(t, resolution, replay)
				workerReq := req
				workerReq.OriginalSnapshot = nil
				replay, err = manager.ReconcileRevenueSource(ctx, workerReq)
				require.NoError(t, err)
				require.Equal(t, resolution, replay)
				redelivery := []byte(strings.ReplaceAll(string(originalBody), "/original", "/redelivered"))
				accepted, err := manager.acceptRevenueWebhook(ctx, "stripe", snapshotSignedRequest(t, redelivery))
				require.NoError(t, err)
				require.True(t, accepted)
				require.Equal(t, before, reads.Load(), "committed recovery and signed replay cannot depend on provider API")
				facts, err := feed.PendingRevenueFacts(ctx, "test-consumer", 200)
				require.NoError(t, err)
				require.Len(t, facts, 2, "one payment and one refund after all retries")
				authority.denied.Store(true)
				_, err = manager.ReconcileRevenueSource(ctx, workerReq)
				require.ErrorIs(t, err, billing.ErrRevenueUnavailable)
				if tc.proof {
					authority.denied.Store(false)
					changed := req
					changed.OriginalSnapshot = []byte(strings.ReplaceAll(string(originalBody), "/original", "/tampered"))
					_, err = manager.ReconcileRevenueSource(ctx, changed)
					require.ErrorIs(t, err, paymentprovider.ErrRevenueUnassessable)
				}
				encoded, err := json.Marshal(resolution)
				require.NoError(t, err)
				require.NotContains(t, string(encoded), resolution.RecoveryFingerprint)
				require.NotContains(t, string(encoded), resolution.SourceFingerprint)
			}
			retained, err := feed.GetRevenueObservation(ctx, original.ID)
			require.NoError(t, err)
			require.Equal(t, original, retained)
		})
	}
}
