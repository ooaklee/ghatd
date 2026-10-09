package billingmanager

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"
	accessmanagerhelpers "github.com/ooaklee/ghatd/external/accessmanager/helpers"
	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/paymentprovider"
	"github.com/stretchr/testify/require"
)

// Audit disposition: table cases compose actual CheckoutService and native
// reverse/forward codecs on isolated owning snapshots. Provider I/O is a fixture;
// encrypted Mongo provenance and real authenticated HTTP are tested separately.
type checkoutReadPayer struct {
	calls, denyAt int
	denied        error
}

func TestOriginalCheckoutStatusHandlerIdentityAndNoStore(t *testing.T) {
	for _, tc := range []struct {
		name, actor, query string
		want               int
		reads              int
	}{
		{"verified_actor_overrides_query_spoof", "original-payer", "session_id=cs_original&ActorID=another-member&plan_id=spoof&cost_id=spoof&amount=1", 200, 1},
		{"no_authentication", "", "session_id=cs_original&ActorID=original-payer", 401, 0},
		{"other_verified_actor", "another-member", "session_id=cs_original", 503, 0},
		{"duplicate_session_selection", "original-payer", "session_id=cs_original&session_id=cs_other", 400, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, owner, _, _, _, i := completionManagerFixture(t)
			_, err := s.WithCheckoutRevenueCapture(owner, &checkoutReadPayer{})
			require.NoError(t, err)
			p := &checkoutReadProvider{revenueCheckoutFixture: &revenueCheckoutFixture{checkoutProviderStub: &checkoutProviderStub{}, scope: paymentprovider.RevenueScope{Provider: i.Scope.Provider, AccountID: i.Scope.AccountID}}}
			p.expectedSession = i.SessionID
			p.e = paymentprovider.CheckoutStatusEvidence{Scope: p.scope, SessionID: i.SessionID, IntentID: i.ID, ClientReferenceID: i.Request.UserID, PriceID: i.Request.PriceID, Currency: "GBP", Mode: i.Request.Mode, SessionStatus: "complete", PaymentStatus: "paid", UnitAmountMinor: i.Request.ExpectedAmount, AmountTotalMinor: i.Request.ExpectedAmount, AmountTotalKnown: true, IntervalCount: 1, BillingCadence: "month", CreatedAt: i.CreatedAt}
			s.CheckoutProviderRegistry = &checkoutRegistryStub{provider: p}
			r := httptest.NewRequest(http.MethodGet, "/api/v1/bms/billings/stripe/checkout/status?"+tc.query, nil)
			r = mux.SetURLVars(r, map[string]string{"providerName": "stripe"})
			if tc.actor != "" {
				ctx := accessmanagerhelpers.TransitWith(r.Context(), tc.actor)
				ctx = accessmanagerhelpers.TransitAuthenticatedWith(ctx, true)
				r = r.WithContext(ctx)
			}
			w := httptest.NewRecorder()
			NewHandler(s, nil).GetBillingProviderCheckoutStatus(w, r)
			require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
			require.Equal(t, tc.want, w.Code, w.Body.String())
			require.Equal(t, tc.reads, p.reads)
			require.Empty(t, p.requests)
			if tc.want == 200 {
				var body struct {
					Data GetBillingProviderCheckoutStatusResponse `json:"data"`
				}
				require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
				require.Equal(t, "paid", body.Data.State)
				require.Equal(t, i.Request.PlanID, body.Data.PlanID)
				require.Equal(t, i.Request.CostID, body.Data.CostID)
				require.NotContains(t, w.Body.String(), "original-payer")
				require.NotContains(t, w.Body.String(), "client_secret")
			}
		})
	}
}

func (a *checkoutReadPayer) AuthorizeCheckoutPayer(ctx context.Context, actor string) error {
	a.calls++
	if err := ctx.Err(); err != nil {
		return err
	}
	if a.calls == a.denyAt {
		return a.denied
	}
	return nil
}

type checkoutReadProvider struct {
	*revenueCheckoutFixture
	e               paymentprovider.CheckoutStatusEvidence
	err             error
	reads           int
	expectedSession string
	onRead          func()
}

func (p *checkoutReadProvider) LookupCheckoutStatus(_ context.Context, scope paymentprovider.RevenueScope, id string) (paymentprovider.CheckoutStatusEvidence, error) {
	p.reads++
	if p.onRead != nil {
		p.onRead()
	}
	if scope != p.scope || id != p.expectedSession {
		panic("lookup did not retain original session")
	}
	return p.e, p.err
}

func TestOriginalCheckoutStatusAuthorityAndBinding(t *testing.T) {
	denied := errors.New("current payer revoked")
	for _, tc := range []struct {
		name, state, actor                                                string
		denyAt                                                            int
		mutate                                                            func(*paymentprovider.CheckoutStatusEvidence)
		providerErr                                                       error
		missingOwner, wrongSession, wrongScope, cancelDuring, paymentMode bool
		want                                                              error
		wantReads                                                         int
	}{
		{name: "paid_original", state: "paid", wantReads: 1},
		{name: "paid_one_time_original", state: "paid", paymentMode: true, wantReads: 1},
		{name: "trial_is_not_paid", state: "no_payment_required", wantReads: 1},
		{name: "stripe_paid_zero_trial_is_not_payment", state: "no_payment_required", mutate: func(e *paymentprovider.CheckoutStatusEvidence) { e.PaymentStatus = "paid" }, wantReads: 1},
		{name: "zero_one_time_is_not_authorized", paymentMode: true, mutate: func(e *paymentprovider.CheckoutStatusEvidence) { e.AmountTotalMinor = 0 }, want: billing.ErrRevenueUnavailable, wantReads: 1},
		{name: "unknown_total_withholds_result", mutate: func(e *paymentprovider.CheckoutStatusEvidence) { e.AmountTotalKnown = false }, want: billing.ErrRevenueUnavailable, wantReads: 1},
		{name: "open_original_unpaid", state: "unpaid", wantReads: 1},
		{name: "async_original_pending", state: "pending", wantReads: 1},
		{name: "expired_original", state: "expired", wantReads: 1},
		{name: "other_actor_never_looks_up_target", actor: "another-member", want: billing.ErrRevenueUnavailable},
		{name: "missing_owner_fails_closed", missingOwner: true, want: billing.ErrRevenueUnavailable},
		{name: "unrelated_session", wrongSession: true, want: billing.ErrRevenueUnavailable},
		{name: "wrong_merchant", wrongScope: true, want: billing.ErrRevenueUnavailable},
		{name: "denied_before_lookup", denyAt: 1, want: denied},
		{name: "revoked_before_provider", denyAt: 2, want: denied},
		{name: "revoked_before_disclosure", denyAt: 3, want: denied, wantReads: 1},
		{name: "revocation_wins_over_outage", denyAt: 3, providerErr: paymentprovider.ErrPaymentProviderAPIRequestFailed, want: denied, wantReads: 1},
		{name: "provider_outage_is_not_payment_failure", providerErr: paymentprovider.ErrPaymentProviderAPIRequestFailed, want: billing.ErrRevenueUnavailable, wantReads: 1},
		{name: "joined_absence_and_outage_does_not_confirm", providerErr: errors.Join(billing.ErrRevenueNotFound, paymentprovider.ErrPaymentProviderAPIRequestFailed), want: billing.ErrRevenueUnavailable, wantReads: 1},
		{name: "cancellation_withholds_result", cancelDuring: true, want: context.Canceled, wantReads: 1},
		{name: "changed_session", mutate: func(e *paymentprovider.CheckoutStatusEvidence) { e.SessionID = "cs_other" }, want: billing.ErrRevenueUnavailable, wantReads: 1},
		{name: "changed_intent", mutate: func(e *paymentprovider.CheckoutStatusEvidence) { e.IntentID = "checkout_other" }, want: billing.ErrRevenueUnavailable, wantReads: 1},
		{name: "changed_payer_reference", mutate: func(e *paymentprovider.CheckoutStatusEvidence) { e.ClientReferenceID = "other" }, want: billing.ErrRevenueUnavailable, wantReads: 1},
		{name: "changed_price", mutate: func(e *paymentprovider.CheckoutStatusEvidence) { e.PriceID = "price_other" }, want: billing.ErrRevenueUnavailable, wantReads: 1},
		{name: "changed_currency", mutate: func(e *paymentprovider.CheckoutStatusEvidence) { e.Currency = "USD" }, want: billing.ErrRevenueUnavailable, wantReads: 1},
		{name: "changed_amount", mutate: func(e *paymentprovider.CheckoutStatusEvidence) { e.UnitAmountMinor++ }, want: billing.ErrRevenueUnavailable, wantReads: 1},
		{name: "changed_cadence", mutate: func(e *paymentprovider.CheckoutStatusEvidence) { e.BillingCadence = "year" }, want: billing.ErrRevenueUnavailable, wantReads: 1},
		{name: "changed_interval", mutate: func(e *paymentprovider.CheckoutStatusEvidence) { e.IntervalCount = 2 }, want: billing.ErrRevenueUnavailable, wantReads: 1},
		{name: "changed_mode", mutate: func(e *paymentprovider.CheckoutStatusEvidence) { e.Mode = "payment" }, want: billing.ErrRevenueUnavailable, wantReads: 1},
		{name: "changed_live_mode", mutate: func(e *paymentprovider.CheckoutStatusEvidence) { e.Scope.LiveMode = true }, want: billing.ErrRevenueUnavailable, wantReads: 1},
		{name: "checkout_before_frozen_intent", mutate: func(e *paymentprovider.CheckoutStatusEvidence) { e.CreatedAt = e.CreatedAt.Add(-1) }, want: billing.ErrRevenueUnavailable, wantReads: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			service, owner, _, _, records, i := completionManagerFixture(t)
			if tc.paymentMode {
				q := i.Request
				q.IdempotencyKey = "one-time-original"
				q.Mode = "payment"
				q.ExpectedBillingCadence = "one_time"
				q.TrialPeriodDays = 0
				proposed, err := owner.PrepareCheckout(t.Context(), i.Scope, q)
				require.NoError(t, err)
				require.NoError(t, owner.AcknowledgeCheckout(t.Context(), proposed, "cs_one_time"))
				i, err = owner.FindCheckoutIntent(t.Context(), i.Scope, q.IdempotencyKey)
				require.NoError(t, err)
			}

			authority := &checkoutReadPayer{denyAt: tc.denyAt, denied: denied}
			_, setupErr := service.WithCheckoutRevenueCapture(owner, authority)
			require.NoError(t, setupErr)
			provider := &checkoutReadProvider{revenueCheckoutFixture: &revenueCheckoutFixture{checkoutProviderStub: &checkoutProviderStub{}, scope: paymentprovider.RevenueScope{Provider: i.Scope.Provider, AccountID: i.Scope.AccountID}}, err: tc.providerErr}
			provider.expectedSession = i.SessionID
			provider.e = paymentprovider.CheckoutStatusEvidence{Scope: provider.scope, SessionID: i.SessionID, IntentID: i.ID, ClientReferenceID: i.Request.UserID, PriceID: i.Request.PriceID, Currency: "GBP", Mode: i.Request.Mode, SessionStatus: "complete", PaymentStatus: "paid", UnitAmountMinor: i.Request.ExpectedAmount, AmountTotalMinor: i.Request.ExpectedAmount, AmountTotalKnown: true, IntervalCount: 1, BillingCadence: "month", CreatedAt: i.CreatedAt}
			if tc.paymentMode {
				provider.e.IntervalCount = 0
				provider.e.BillingCadence = "one_time"
			}
			switch tc.state {
			case "no_payment_required":
				provider.e.PaymentStatus = tc.state
				provider.e.AmountTotalMinor = 0
				require.Equal(t, 14, i.Request.TrialPeriodDays)
			case "unpaid":
				provider.e.SessionStatus = "open"
				provider.e.PaymentStatus = "unpaid"
			case "pending":
				provider.e.PaymentStatus = "unpaid"
			case "expired":
				provider.e.SessionStatus = "expired"
				provider.e.PaymentStatus = "unpaid"
			}
			if tc.mutate != nil {
				tc.mutate(&provider.e)
			}
			if tc.wrongScope {
				provider.scope.AccountID = "other-merchant"
			}
			service.CheckoutProviderRegistry = &checkoutRegistryStub{provider: provider}
			if tc.missingOwner {
				service.checkoutRevenueCapture = nil
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if tc.cancelDuring {
				provider.onRead = cancel
			}
			actor := i.Request.UserID
			if tc.actor != "" {
				actor = tc.actor
			}
			session := i.SessionID
			if tc.wrongSession {
				session = "cs_unknown"
			}
			before, err := json.Marshal(records.records)
			require.NoError(t, err)
			out, err := service.GetBillingProviderCheckoutStatus(ctx, &GetBillingProviderCheckoutStatusRequest{ActorID: actor, ProviderName: "stripe", SessionID: session})
			if tc.want != nil {
				require.ErrorIs(t, err, tc.want)
				require.Nil(t, out)
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.state, out.State)
				require.Equal(t, i.Request.PlanID, out.PlanID)
				require.Equal(t, i.Request.CostID, out.CostID)
				require.Equal(t, i.SessionID, out.SessionID)
			}
			require.Equal(t, tc.wantReads, provider.reads)
			require.Empty(t, provider.requests)
			after, err := json.Marshal(records.records)
			require.NoError(t, err)
			require.Equal(t, string(before), string(after), "status read must not write or grant access")
		})
	}
}
