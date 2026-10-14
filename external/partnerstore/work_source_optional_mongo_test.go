package partnerstore

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/billing/revenuestore"
	"github.com/ooaklee/ghatd/external/billingmanager"
	"github.com/ooaklee/ghatd/external/logger"
	"github.com/ooaklee/ghatd/external/partnermanager"
	"github.com/ooaklee/ghatd/external/paymentprovider"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

type optionalSourceRegistry struct {
	provider paymentprovider.RevenueProvider
}

func (r optionalSourceRegistry) GetRevenueProvider(string) (paymentprovider.RevenueProvider, error) {
	return r.provider, nil
}

type optionalSourceAuthority struct{}

func (optionalSourceAuthority) AuthorizeRevenueReconciliation(context.Context, string) error {
	return nil
}

// The real Stripe revenue adapter reads only this controlled local endpoint.
// Its complete legacy checkout deliberately lacks an owning intent pointer.
func optionalSourceProvider(t *testing.T) *paymentprovider.StripeProvider {
	t.Helper()
	list := func(rows ...any) map[string]any {
		return map[string]any{"object": "list", "has_more": false, "data": rows}
	}
	responses := map[string]map[string]any{
		"/v1/account":                     {"id": "acct_optional_fixture"},
		"/v1/events/evt_optional":         {"id": "evt_optional", "type": "invoice.paid", "livemode": false, "created": 1700000100, "data": map[string]any{"object": map[string]any{"id": "in_optional"}}},
		"/v1/invoices/in_optional":        {"id": "in_optional", "livemode": false, "status": "paid", "currency": "gbp", "customer": "cus_optional", "amount_paid": 1200, "total": 1200, "total_excluding_tax": 1000, "amount_remaining": 0, "starting_balance": 0, "pre_payment_credit_notes_amount": 0, "status_transitions": map[string]any{"paid_at": 1700000100}, "parent": map[string]any{"subscription_details": map[string]any{"subscription": "sub_optional"}}},
		"/v1/invoice_payments":            list(map[string]any{"id": "inpay_optional", "invoice": "in_optional", "livemode": false, "currency": "gbp", "status": "paid", "amount_paid": 1200, "payment": map[string]any{"type": "payment_intent", "payment_intent": "pi_optional"}}),
		"/v1/payment_intents/pi_optional": {"id": "pi_optional", "livemode": false, "status": "succeeded", "currency": "gbp", "amount_received": 1200, "customer": "cus_optional"},
		"/v1/invoices/in_optional/lines":  list(map[string]any{"id": "il_optional", "livemode": false, "currency": "gbp", "subtotal": 1000, "pretax_credit_amounts": []any{}, "parent": map[string]any{"subscription_item_details": map[string]any{"subscription": "sub_optional"}}, "pricing": map[string]any{"price_details": map[string]any{"price": "price_optional"}}}),
		"/v1/checkout/sessions":           list(map[string]any{"id": "cs_optional", "object": "checkout.session", "livemode": false, "subscription": "sub_optional", "customer": "cus_optional", "mode": "subscription", "status": "complete", "currency": "gbp", "client_reference_id": "fixture-original-payer", "created": 1700000000}),
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.Header.Get("Authorization") != "Bearer sk_optional_fixture" || r.Header.Get("Stripe-Version") != paymentprovider.StripeDefaultAPIVersion {
			t.Error("unexpected provider method or fixture authentication")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		response, ok := responses[r.URL.Path]
		if !ok {
			t.Errorf("unexpected provider path %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if err := json.NewEncoder(w).Encode(response); err != nil {
			t.Errorf("encode fixture provider response: %v", err)
		}
	}))
	t.Cleanup(server.Close)
	provider, err := paymentprovider.NewStripeProvider(&paymentprovider.Config{APIKey: "sk_optional_fixture", WebhookSecret: "whsec_optional_fixture", APIBaseURL: server.URL, Revenue: &paymentprovider.RevenueConfig{AccountID: "acct_optional_fixture", CurrencyExponents: map[string]int{"GBP": 2}}})
	require.NoError(t, err)
	return provider
}

func TestMongoSourceWorkerOptionalReadsPreserveQuarantine(t *testing.T) {
	cases := []struct {
		name   string
		passes int
	}{
		{"one_pending_source_pass", 1},
		{"repeated_due_source_passes", 3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := owningWorkerFixture(t)
			provider := optionalSourceProvider(t)
			scope := paymentprovider.RevenueScope{Provider: "stripe", AccountID: "acct_optional_fixture"}
			evidence, err := provider.ReconcileRevenueEvent(f.ctx, scope, "evt_optional")
			require.NoError(t, err)
			require.Empty(t, evidence.QuarantineReason)
			source, err := f.revenue.AcceptVerified(f.ctx, billing.VerifiedRevenueRequest{Scope: billing.RevenueScope{Provider: scope.Provider, AccountID: scope.AccountID}, EnvelopeID: evidence.EnvelopeID, SourceFingerprint: evidence.SourceFingerprint, QuarantineReason: "historical_payer_plan_pending"})
			require.NoError(t, err)
			repo, err := revenuestore.NewRepository(f.store)
			require.NoError(t, err)
			checkout, err := billing.NewCheckoutService(repo, f.clock, provider)
			require.NoError(t, err)
			reconciler, err := billingmanager.NewService(nil, nil).WithRevenueServices(optionalSourceRegistry{provider}, f.revenue, checkout)
			require.NoError(t, err)
			reconciler, err = reconciler.WithRevenueReconciliationAuthority(optionalSourceAuthority{})
			require.NoError(t, err)
			queue := f.queue(t, f.queueRepo)
			worker, err := partnermanager.NewWorker(f.manager(t, false), queue, f.users, f.revenue, reconciler, partnermanager.WorkerConfig{ActorID: "fixture-current-worker", ConsumerID: "partner-financial-consumer", PageSize: 100, BatchSize: 100})
			require.NoError(t, err)
			core, logs := observer.New(zap.DebugLevel)
			ctx := logger.TransitWith(f.ctx, zap.New(core))
			for pass := 0; pass < tc.passes; pass++ {
				report, err := worker.RunOnce(ctx)
				require.ErrorIs(t, err, billing.ErrRevenueUnassessable)
				require.Zero(t, report.Completed)
				require.Equal(t, 1, report.Retried)
				require.Equal(t, []partnermanager.WorkerIssue{{Kind: partnermanager.WorkRevenueSource, Code: "review_pending"}}, report.Issues)
				f.clock.now = f.clock.now.Add(time.Hour)
			}
			original, err := f.revenue.GetRevenueObservation(ctx, source.ID)
			require.NoError(t, err)
			require.Equal(t, source, original, "the original quarantine must remain immutable")
			work, err := queue.Get(ctx, partnermanager.WorkRevenueSource, source.ID)
			require.NoError(t, err)
			require.Equal(t, partnermanager.WorkPending, work.State)
			require.Equal(t, "review_pending", work.LastErrorCode)
			require.Nil(t, work.Decision)
			for kind, want := range map[string]int64{"billing_revenue_observation": 1, "billing_revenue_fact": 0, "billing_checkout_association": 0, "billing_legacy_checkout_migration": 0} {
				count, err := f.db.Collection("ghatd_owned_records").CountDocuments(ctx, bson.M{"kind": kind})
				require.NoError(t, err)
				require.Equal(t, want, count, "optional reads must not create owning state: %s", kind)
			}
			for message, cause := range map[string]string{"stripe-checkout-revenue-evidence-refused": "checkout_intent_reference_missing", "revenue-association-evidence-refused": "provider_checkout_unassessable", "revenue-source-reconciliation-failed": "revenue_unassessable"} {
				entries := logs.FilterMessage(message).All()
				require.Len(t, entries, tc.passes)
				for _, entry := range entries {
					require.Equal(t, cause, entry.ContextMap()["cause_code"])
					require.NotContains(t, entry.ContextMap(), "error")
				}
			}
			for _, entry := range logs.FilterMessage("revenue-source-reconciliation-failed").All() {
				require.Equal(t, "historical_payer_plan_pending", entry.ContextMap()["original_quarantine_reason"])
				require.Equal(t, "historical_payer_plan_pending", entry.ContextMap()["current_quarantine_reason"])
			}
			failures := logs.FilterMessage("mongo-operation-failed").All()
			owners := map[string]int{}
			for _, entry := range failures {
				owner, _ := entry.ContextMap()["owner_operation"].(string)
				owners[owner]++
			}
			t.Logf("source passes=%d: transaction errors=%d, owner counts=%v; unchanged quarantine warnings=%d, accepted facts=0", tc.passes, len(failures), owners, logs.FilterMessage("revenue-source-reconciliation-failed").Len())
			require.Empty(t, failures, "optional resolution/association absence must not become a database failure")
		})
	}
}
