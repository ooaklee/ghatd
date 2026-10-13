package billinglifecycle

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/billing/revenuestore"
	"github.com/ooaklee/ghatd/external/billingmanager"
	"github.com/ooaklee/ghatd/external/logger"
	"github.com/ooaklee/ghatd/external/paymentprovider"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

// The actual Stripe HTTP adapter feeds native owning joins, worker authority,
// and encrypted preparation/capture. The provider server and identity are local
// fixtures; these cases neither make a payment nor qualify a deployed worker.
func TestCheckoutExecutionProviderStatesEncrypted(t *testing.T) {
	for _, tc := range []struct {
		name, status, payment, missingKind string
		noSubscription, wrongMode          bool
		want                               error
	}{
		{name: "complete_paid", status: "complete", payment: "paid"},
		{name: "complete_trial_is_not_revenue", status: "complete", payment: "no_payment_required"},
		{name: "open_unpaid_without_subscription", status: "open", payment: "unpaid", noSubscription: true, want: billing.ErrRevenueUnassessable},
		{name: "expired_unpaid_without_subscription", status: "expired", payment: "unpaid", noSubscription: true, want: billing.ErrRevenueUnassessable},
		{name: "complete_missing_subscription", status: "complete", payment: "paid", noSubscription: true, want: billing.ErrRevenueUnassessable},
		{name: "complete_wrong_scope", status: "complete", payment: "paid", wrongMode: true, want: billing.ErrRevenueUnassessable},
		{name: "missing_original_prevents_provider", missingKind: "billing_checkout_intent", want: billing.ErrRevenueUnavailable},
		{name: "missing_acknowledgement_prevents_provider", missingKind: "billing_checkout_acknowledgement", want: billing.ErrRevenueUnavailable},
		{name: "missing_reverse_prevents_provider", missingKind: "billing_checkout_session", want: billing.ErrRevenueUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, r, clock, job := nativeCheckoutBindingFixture(t)
			i := f.intent
			session := map[string]any{
				"object": "checkout.session", "id": i.SessionID, "livemode": tc.wrongMode,
				"mode": "subscription", "status": tc.status, "payment_status": tc.payment,
				"subscription": "sub_original", "customer": "cus_original", "created": i.CreatedAt.Unix(),
				"client_reference_id": i.Request.UserReference, "currency": "gbp",
				"metadata": map[string]string{"checkout_intent_id": i.ID},
			}
			if tc.noSubscription {
				session["subscription"], session["customer"] = nil, nil
			}
			line := map[string]any{
				"id": "li_original", "object": "item", "quantity": 1, "currency": "gbp",
				"price": map[string]any{
					"object": "price", "id": i.Request.PriceID, "livemode": false, "type": "recurring",
					"currency": "gbp", "unit_amount": i.Request.ExpectedAmount,
					"recurring": map[string]any{"interval": "month", "interval_count": 1},
				},
			}
			var calls, writes atomic.Int64
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				calls.Add(1)
				if req.Method != http.MethodGet {
					writes.Add(1)
					w.WriteHeader(http.StatusMethodNotAllowed)
					return
				}
				if req.Header.Get("Authorization") != "Bearer sk_fixture" || req.Header.Get("Stripe-Version") != paymentprovider.StripeDefaultAPIVersion || req.Header.Get("Stripe-Account") != "" {
					w.WriteHeader(http.StatusUnauthorized)
					return
				}
				var out any
				switch req.URL.Path {
				case "/v1/account":
					out = map[string]any{"id": i.Scope.AccountID}
				case "/v1/checkout/sessions/" + i.SessionID:
					out = session
				case "/v1/checkout/sessions/" + i.SessionID + "/line_items":
					out = map[string]any{"object": "list", "has_more": false, "data": []any{line}}
				default:
					w.WriteHeader(http.StatusNotFound)
					return
				}
				_ = json.NewEncoder(w).Encode(out)
			}))
			t.Cleanup(server.Close)
			provider, err := paymentprovider.NewStripeProvider(&paymentprovider.Config{APIKey: "sk_fixture", WebhookSecret: "whsec_fixture", APIBaseURL: server.URL, Revenue: &paymentprovider.RevenueConfig{AccountID: i.Scope.AccountID, CurrencyExponents: map[string]int{"GBP": 2}}})
			require.NoError(t, err)
			repo, err := revenuestore.NewRepository(f.reply)
			require.NoError(t, err)
			f.native, err = billing.NewCheckoutService(repo, f.clock, provider)
			require.NoError(t, err)
			binder, ctx := nativeCheckoutBinder(t, f, r, clock, "worker-original")
			owner := binder.validator.(*billingmanager.Service)
			outbox, err := NewCheckoutOutbox(f.reply, owner)
			require.NoError(t, err)
			execution, err := NewCheckoutExecution(binder.scheduler, r, outbox)
			require.NoError(t, err)
			if tc.missingKind != "" {
				deleted, err := f.db.Collection("ghatd_owned_records").DeleteOne(ctx, bson.M{"kind": tc.missingKind})
				require.NoError(t, err)
				require.EqualValues(t, 1, deleted.DeletedCount)
			}
			core, observed := observer.New(zap.DebugLevel)
			ctx = logger.TransitWith(ctx, zap.New(core))
			out, err := execution.Observe(ctx, JobLease(job))
			require.ErrorIs(t, err, tc.want)
			require.Zero(t, writes.Load())
			stored, readErr := r.ReadJob(ctx, job.Source)
			require.NoError(t, readErr)
			if tc.missingKind != "" {
				require.Zero(t, calls.Load())
				require.False(t, stored.CheckoutPrepared)
			} else {
				require.True(t, stored.CheckoutPrepared)
				input, err := outbox.Find(ctx, "worker-original", i)
				require.NoError(t, err)
				require.Equal(t, i, input.Intent)
				if tc.want != nil {
					require.Nil(t, input.Evidence)
					require.EqualValues(t, 2, calls.Load(), "refusal happens before line lookup")
					entries := observed.FilterMessage("stripe-checkout-revenue-evidence-refused").All()
					require.Len(t, entries, 1)
					require.Equal(t, "checkout_session_shape_or_scope_invalid", entries[0].ContextMap()["cause_code"])
				} else {
					require.NotNil(t, input.Evidence)
					require.EqualValues(t, 3, calls.Load())
					require.Equal(t, i.ID, out.Receipt.IntentID)
				}
			}
			if tc.want != nil {
				require.Equal(t, CheckoutObservation{}, out)
			}
			for _, kind := range []string{"billing_checkout_lifecycle_receipt", "billing_checkout_lifecycle_anchor", "billing_revenue_fact", "billing_checkout_association", "partners_lifecycle_completion"} {
				count, err := f.db.Collection("ghatd_owned_records").CountDocuments(ctx, bson.M{"kind": kind})
				require.NoError(t, err)
				if tc.want == nil && (kind == "billing_checkout_lifecycle_receipt" || kind == "billing_checkout_lifecycle_anchor") {
					require.EqualValues(t, 1, count, kind)
				} else {
					require.Zero(t, count, kind)
				}
			}
		})
	}
}
