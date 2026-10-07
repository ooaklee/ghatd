package billingmanager

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/paymentprovider"
	"github.com/ooaklee/ghatd/external/pricer"
	user "github.com/ooaklee/ghatd/external/user/v2"
	"github.com/stretchr/testify/require"
)

// Audit disposition: named tests check manager hook ordering and current actor
// boundaries. The owning service has independent real-Mongo atomic proof.
type checkoutCaptureFixture struct {
	intent                                 *billing.CheckoutIntent
	findErr, prepareErr, ackErr, submitErr error
	prepared, acknowledged                 int
}

func (f *checkoutCaptureFixture) FindCheckoutIntent(context.Context, billing.RevenueScope, string) (billing.CheckoutIntent, error) {
	if f.findErr != nil {
		return billing.CheckoutIntent{}, f.findErr
	}
	if f.intent == nil {
		return billing.CheckoutIntent{}, billing.ErrRevenueNotFound
	}
	return *f.intent, nil
}
func (f *checkoutCaptureFixture) PrepareCheckout(_ context.Context, scope billing.RevenueScope, r paymentprovider.CheckoutSessionRequest) (billing.CheckoutIntent, error) {
	f.prepared++
	if f.prepareErr != nil {
		return billing.CheckoutIntent{}, f.prepareErr
	}
	copied := map[string]string{}
	for k, v := range r.Metadata {
		copied[k] = v
	}
	r.Metadata = copied
	r.Metadata["checkout_intent_id"] = "intent_frozen"
	v := billing.CheckoutIntent{ID: "intent_frozen", Scope: scope, Request: r, CreatedAt: time.Now().UTC()}
	f.intent = &v
	return v, nil
}
func (f *checkoutCaptureFixture) AcknowledgeCheckout(_ context.Context, _ billing.CheckoutIntent, session string) error {
	f.acknowledged++
	if f.ackErr != nil {
		return f.ackErr
	}
	f.intent.SessionID = session
	return nil
}
func (f *checkoutCaptureFixture) CanSubmitCheckout(context.Context, billing.CheckoutIntent) error {
	return f.submitErr
}

type revenueCheckoutFixture struct {
	*checkoutProviderStub
	onRetrieve, onCreate  func()
	capture               *checkoutCaptureFixture
	scope                 paymentprovider.RevenueScope
	scopeErr, retrieveErr error
	retrieves             int
}

func (p *revenueCheckoutFixture) CheckoutRevenueScope(context.Context) (paymentprovider.RevenueScope, error) {
	return p.scope, p.scopeErr
}
func (p *revenueCheckoutFixture) LookupRevenueCheckout(context.Context, paymentprovider.RevenueScope, string) (paymentprovider.RevenueCheckoutEvidence, error) {
	return paymentprovider.RevenueCheckoutEvidence{}, paymentprovider.ErrRevenueUnassessable
}
func (p *revenueCheckoutFixture) RetrieveRevenueCheckoutSession(_ context.Context, _ paymentprovider.RevenueScope, id string) (*paymentprovider.CheckoutSession, error) {
	p.retrieves++
	if p.onRetrieve != nil {
		p.onRetrieve()
	}
	if p.retrieveErr != nil {
		return nil, p.retrieveErr
	}
	return &paymentprovider.CheckoutSession{ID: id, ClientSecret: "recovered_secret"}, nil
}
func (p *revenueCheckoutFixture) CreateCheckoutSession(ctx context.Context, r *paymentprovider.CheckoutSessionRequest) (*paymentprovider.CheckoutSession, error) {
	if p.capture.intent == nil || p.capture.intent.Request.IdempotencyKey != r.IdempotencyKey || r.Metadata["checkout_intent_id"] != p.capture.intent.ID {
		return nil, errors.New("submission preceded durable authorization")
	}
	result, err := p.checkoutProviderStub.CreateCheckoutSession(ctx, r)
	if p.onCreate != nil {
		p.onCreate()
	}
	return result, err
}
func TestCheckoutRevenueCaptureOrderingAndFrozenRecovery(t *testing.T) {
	cases := []struct {
		name                    string
		providerError, ackError error
		wantFirst               error
		retrieval               bool
	}{
		{name: "acknowledged_session_retrieved_after_catalogue_and_profile_change", retrieval: true},
		{name: "lost_provider_response_retains_frozen_request", providerError: paymentprovider.ErrPaymentProviderAPIRequestFailed, wantFirst: ErrBillingManagerCheckoutProviderRequestFailed},
		{name: "lost_local_ack_retains_frozen_authorization", ackError: billing.ErrRevenueUncertain, wantFirst: billing.ErrRevenueUncertain},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			capture := &checkoutCaptureFixture{ackErr: tc.ackError}
			base := &checkoutProviderStub{returnURL: checkoutTestReturnURL, err: tc.providerError}
			provider := &revenueCheckoutFixture{checkoutProviderStub: base, capture: capture, scope: paymentprovider.RevenueScope{Provider: "stripe", AccountID: "acct_primary"}}
			service := checkoutServiceForPlan(checkoutPublishedPlan(pricer.PriceBillingCadenceMonthly), base)
			service.CheckoutProviderRegistry = &checkoutRegistryStub{provider: provider}
			_, err := service.WithCheckoutRevenueCapture(capture, checkoutPayerFixture{})
			require.NoError(t, err)
			_, err = service.ProcessBillingProviderCheckout(context.Background(), checkoutRequest())
			if tc.wantFirst != nil {
				require.ErrorIs(t, err, tc.wantFirst)
			} else {
				require.NoError(t, err)
			}
			require.NotNil(t, capture.intent)
			require.Len(t, base.requests, 1)
			require.Equal(t, 1, capture.prepared)
			first := capture.intent.Request
			// Only existing frozen intent is available on retry; new catalogue results
			// would fail, and a changed current email must not change POST parameters.
			service.PricerService = &checkoutPricerStub{getPricePlans: func(context.Context, *pricer.GetPricePlansRequest) (*pricer.GetPricePlansResponse, error) {
				t.Fatal("retried checkout consulted mutable catalogue")
				return nil, nil
			}}
			service.UserService = &checkoutUserServiceStub{response: &user.GetUserByIDResponse{User: &user.UniversalUser{ID: "user_123", Email: "changed@example.test"}}}
			base.err = nil
			capture.ackErr = nil
			second, err := service.ProcessBillingProviderCheckout(context.Background(), checkoutRequest())
			require.NoError(t, err)
			require.Equal(t, "cs_test_123", second.Session.ID)
			require.Equal(t, 1, capture.prepared)
			if tc.retrieval {
				require.Len(t, base.requests, 1)
				require.Equal(t, 1, provider.retrieves)
			} else {
				require.Len(t, base.requests, 2)
				require.Equal(t, first, *base.requests[1])
				require.Equal(t, 0, provider.retrieves)
			}
		})
	}
}
func TestCheckoutRevenueCaptureRefusesUnsafeSubmission(t *testing.T) {
	cases := []struct {
		name                                     string
		findErr, prepareErr, scopeErr, submitErr error
		scope                                    paymentprovider.RevenueScope
		missingCapability                        bool
		want                                     error
	}{
		{name: "owning_store_outage", findErr: billing.ErrRevenueUnavailable, want: billing.ErrRevenueUnavailable},
		{name: "joined_absence_and_outage", findErr: errors.Join(billing.ErrRevenueNotFound, billing.ErrRevenueUnavailable), want: billing.ErrRevenueUnavailable},
		{name: "intent_commit_uncertain", prepareErr: billing.ErrRevenueUncertain, want: billing.ErrRevenueUncertain},
		{name: "authenticated_merchant_lookup_fails", scopeErr: paymentprovider.ErrPaymentProviderAPIRequestFailed, want: paymentprovider.ErrPaymentProviderAPIRequestFailed},
		{name: "wrong_provider_scope", scope: paymentprovider.RevenueScope{Provider: "other", AccountID: "acct_primary"}, want: billing.ErrRevenueInvalid},
		{name: "provider_missing_evidence_capability", missingCapability: true, want: billing.ErrRevenueUnavailable},
		{name: "expired_idempotency_window_requires_reconciliation", submitErr: billing.ErrRevenueUnassessable, want: billing.ErrRevenueUnassessable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			capture := &checkoutCaptureFixture{findErr: tc.findErr, prepareErr: tc.prepareErr, submitErr: tc.submitErr}
			base := &checkoutProviderStub{returnURL: checkoutTestReturnURL}
			scope := tc.scope
			if scope.Provider == "" {
				scope = paymentprovider.RevenueScope{Provider: "stripe", AccountID: "acct_primary"}
			}
			provider := &revenueCheckoutFixture{checkoutProviderStub: base, capture: capture, scope: scope, scopeErr: tc.scopeErr}
			service := checkoutServiceForPlan(checkoutPublishedPlan(pricer.PriceBillingCadenceMonthly), base)
			if !tc.missingCapability {
				service.CheckoutProviderRegistry = &checkoutRegistryStub{provider: provider}
			}
			_, err := service.WithCheckoutRevenueCapture(capture, checkoutPayerFixture{})
			require.NoError(t, err)
			_, err = service.ProcessBillingProviderCheckout(context.Background(), checkoutRequest())
			require.ErrorIs(t, err, tc.want)
			require.Empty(t, base.requests)
			require.Zero(t, capture.acknowledged)
		})
	}
}
func TestCheckoutRevenueCaptureReplayNeedsCurrentIdentity(t *testing.T) {
	cases := []struct {
		name    string
		actor   string
		userErr error
		want    error
	}{
		{name: "current_identity_revoked", actor: "user_123", userErr: user.ErrUserNotFound, want: ErrBillingManagerCheckoutUserUnavailable},
		{name: "another_actor_cannot_use_original_intent", actor: "another_user", want: billing.ErrRevenueConflict},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			capture := &checkoutCaptureFixture{}
			base := &checkoutProviderStub{returnURL: checkoutTestReturnURL}
			provider := &revenueCheckoutFixture{checkoutProviderStub: base, capture: capture, scope: paymentprovider.RevenueScope{Provider: "stripe", AccountID: "acct_primary"}}
			service := checkoutServiceForPlan(checkoutPublishedPlan(pricer.PriceBillingCadenceMonthly), base)
			service.CheckoutProviderRegistry = &checkoutRegistryStub{provider: provider}
			_, err := service.WithCheckoutRevenueCapture(capture, checkoutPayerFixture{})
			require.NoError(t, err)
			_, err = service.ProcessBillingProviderCheckout(context.Background(), checkoutRequest())
			require.NoError(t, err)
			request := checkoutRequest()
			request.ActorID = tc.actor
			service.UserService = &checkoutUserServiceStub{err: tc.userErr, response: &user.GetUserByIDResponse{User: &user.UniversalUser{ID: tc.actor, Email: "active@example.test"}}}
			_, err = service.ProcessBillingProviderCheckout(context.Background(), request)
			require.ErrorIs(t, err, tc.want)
			require.Len(t, base.requests, 1)
			require.Zero(t, provider.retrieves)
		})
	}
}

type checkoutPayerFixture struct{ err error }

func (p checkoutPayerFixture) AuthorizeCheckoutPayer(context.Context, string) error { return p.err }

type changingCheckoutPayer struct{ revoked bool }

func (p *changingCheckoutPayer) AuthorizeCheckoutPayer(context.Context, string) error {
	if p.revoked {
		return ErrBillingManagerUserUnauthorisedToCarryOutOperation
	}
	return nil
}
func TestCapturedCheckoutDoesNotReturnSecretAfterInflightRevocation(t *testing.T) {
	cases := []struct {
		name       string
		onRetrieve bool
	}{{"revoked_during_acknowledged_session_lookup", true}, {"revoked_during_session_creation", false}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			capture := &checkoutCaptureFixture{}
			base := &checkoutProviderStub{returnURL: checkoutTestReturnURL}
			provider := &revenueCheckoutFixture{checkoutProviderStub: base, capture: capture, scope: paymentprovider.RevenueScope{Provider: "stripe", AccountID: "acct_primary"}}
			authority := &changingCheckoutPayer{}
			service := checkoutServiceForPlan(checkoutPublishedPlan(pricer.PriceBillingCadenceMonthly), base)
			service.CheckoutProviderRegistry = &checkoutRegistryStub{provider: provider}
			_, err := service.WithCheckoutRevenueCapture(capture, authority)
			require.NoError(t, err)
			if tc.onRetrieve {
				_, err = service.ProcessBillingProviderCheckout(context.Background(), checkoutRequest())
				require.NoError(t, err)
				provider.onRetrieve = func() { authority.revoked = true }
			} else {
				provider.onCreate = func() { authority.revoked = true }
			}
			response, err := service.ProcessBillingProviderCheckout(context.Background(), checkoutRequest())
			require.ErrorIs(t, err, ErrBillingManagerUserUnauthorisedToCarryOutOperation)
			require.Nil(t, response)
			// Provider receipt remains retained despite withholding the browser secret.
			require.Equal(t, "cs_test_123", capture.intent.SessionID)
		})
	}
}
func TestCapturedCheckoutRejectsWrongKeyFromOwningPort(t *testing.T) {
	cases := []struct {
		name      string
		changeKey bool
	}{{"another_browser_attempt", true}, {"same_attempt", false}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			capture := &checkoutCaptureFixture{}
			base := &checkoutProviderStub{returnURL: checkoutTestReturnURL}
			provider := &revenueCheckoutFixture{checkoutProviderStub: base, capture: capture, scope: paymentprovider.RevenueScope{Provider: "stripe", AccountID: "acct_primary"}}
			service := checkoutServiceForPlan(checkoutPublishedPlan(pricer.PriceBillingCadenceMonthly), base)
			service.CheckoutProviderRegistry = &checkoutRegistryStub{provider: provider}
			_, err := service.WithCheckoutRevenueCapture(capture, checkoutPayerFixture{})
			require.NoError(t, err)
			_, err = service.ProcessBillingProviderCheckout(context.Background(), checkoutRequest())
			require.NoError(t, err)
			// This deliberately faulty port returns an intent from a different lookup.
			request := checkoutRequest()
			if tc.changeKey {
				request.IdempotencyKey = "another_attempt"
			}
			_, err = service.ProcessBillingProviderCheckout(context.Background(), request)
			if tc.changeKey {
				require.ErrorIs(t, err, billing.ErrRevenueConflict)
				require.Zero(t, provider.retrieves)
			} else {
				require.NoError(t, err)
				require.Equal(t, 1, provider.retrieves)
			}
		})
	}
}
