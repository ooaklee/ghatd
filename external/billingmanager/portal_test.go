package billingmanager

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gorilla/mux"
	accessmanagerhelpers "github.com/ooaklee/ghatd/external/accessmanager/helpers"
	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/paymentprovider"
	"github.com/ooaklee/ghatd/external/response"
	ghatdRouter "github.com/ooaklee/ghatd/external/router"
)

const portalTestReturnURL = "https://app.example.test/app/settings#billing"

type portalProviderStub struct {
	requests  []*paymentprovider.CustomerPortalSessionRequest
	session   *paymentprovider.CustomerPortalSession
	err       error
	configErr error
	returnURL string
}

func (s *portalProviderStub) CreateCustomerPortalSession(_ context.Context, req *paymentprovider.CustomerPortalSessionRequest) (*paymentprovider.CustomerPortalSession, error) {
	s.requests = append(s.requests, req)
	if s.err != nil {
		return nil, s.err
	}
	if s.session != nil {
		return s.session, nil
	}
	return &paymentprovider.CustomerPortalSession{ID: "bps_123", URL: "https://portal.example.test/session/123"}, nil
}

func (s *portalProviderStub) GetCustomerPortalReturnURL() string {
	if s == nil {
		return ""
	}
	return s.returnURL
}

func (s *portalProviderStub) ValidateCustomerPortalConfig() error {
	if s == nil {
		return paymentprovider.ErrPaymentProviderInvalidConfiguration
	}
	return s.configErr
}

func (s *portalProviderStub) ValidateCustomerPortalSessionURL(value string) error {
	if s == nil {
		return paymentprovider.ErrPaymentProviderInvalidConfiguration
	}
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || !strings.EqualFold(parsed.Scheme, "https") || !strings.EqualFold(parsed.Host, "portal.example.test") || parsed.User != nil {
		return paymentprovider.ErrPaymentProviderAPIResponseInvalid
	}
	return nil
}

type portalRegistryStub struct {
	provider paymentprovider.CustomerPortalProvider
	err      error
	name     string
}

type portalProviderWithoutSessionURLValidator struct{ returnURL string }

func (*portalProviderWithoutSessionURLValidator) CreateCustomerPortalSession(context.Context, *paymentprovider.CustomerPortalSessionRequest) (*paymentprovider.CustomerPortalSession, error) {
	return &paymentprovider.CustomerPortalSession{ID: "bps_unsafe", URL: "https://attacker.example.test/session"}, nil
}
func (s *portalProviderWithoutSessionURLValidator) GetCustomerPortalReturnURL() string {
	return s.returnURL
}

func (s *portalRegistryStub) GetCustomerPortalProvider(name string) (paymentprovider.CustomerPortalProvider, error) {
	s.name = name
	return s.provider, s.err
}

type portalBillingServiceStub struct {
	BillingService
	requests []*billing.GetSubscriptionsRequest
	get      func(*billing.GetSubscriptionsRequest) (*billing.GetSubscriptionsResponse, error)
}

func (s *portalBillingServiceStub) GetSubscriptions(_ context.Context, req *billing.GetSubscriptionsRequest) (*billing.GetSubscriptionsResponse, error) {
	copyReq := *req
	copyReq.ForUserIDs = append([]string(nil), req.ForUserIDs...)
	s.requests = append(s.requests, &copyReq)
	return s.get(req)
}

func portalRequest() *ProcessBillingProviderPortalRequest {
	return &ProcessBillingProviderPortalRequest{
		UserID:       "user_123",
		ProviderName: "stripe",
		Origin:       "https://app.example.test",
		SecFetchSite: "same-origin",
	}
}

func portalEmptyPage(page int) *billing.GetSubscriptionsResponse {
	return &billing.GetSubscriptionsResponse{Page: page, PerPage: portalSubscriptionPageSize}
}

func TestProcessBillingProviderPortalUsesServerOwnedRecurringCustomer(t *testing.T) {
	firstPage := make([]billing.Subscription, portalSubscriptionPageSize)
	firstPage[0] = billing.Subscription{
		UserID: "user_123", Integrator: "stripe", Status: billing.StatusCancelled,
		BillingKind: "recurring", IntegratorCustomerID: "customer_fallback",
	}
	for index := 1; index < len(firstPage); index++ {
		firstPage[index] = billing.Subscription{
			UserID: "user_123", Integrator: "stripe", Status: billing.StatusActive,
			BillingKind: "one_time", IsOneOff: true, IntegratorCustomerID: "customer_one_time",
		}
	}
	active := billing.Subscription{
		UserID: "user_123", Integrator: "STRIPE", Status: billing.StatusTrialing,
		BillingKind: "recurring", IntegratorCustomerID: " customer_active ",
	}
	billingService := &portalBillingServiceStub{get: func(req *billing.GetSubscriptionsRequest) (*billing.GetSubscriptionsResponse, error) {
		switch req.Page {
		case 1:
			return &billing.GetSubscriptionsResponse{Subscriptions: firstPage, Total: 101, TotalPages: 2, PerPage: 100, Page: 1}, nil
		case 2:
			return &billing.GetSubscriptionsResponse{Subscriptions: []billing.Subscription{active}, Total: 101, TotalPages: 2, PerPage: 100, Page: 2}, nil
		default:
			t.Fatalf("unexpected page %d", req.Page)
			return nil, nil
		}
	}}
	provider := &portalProviderStub{returnURL: portalTestReturnURL}
	service := NewService(nil, billingService).WithCustomerPortalProviderRegistry(&portalRegistryStub{provider: provider})

	response, err := service.ProcessBillingProviderPortal(context.Background(), portalRequest())
	if err != nil {
		t.Fatalf("ProcessBillingProviderPortal() error = %v", err)
	}
	if response == nil || response.Session == nil || response.Session.ID != "bps_123" {
		t.Fatalf("response = %#v", response)
	}
	if len(provider.requests) != 1 || provider.requests[0].CustomerID != "customer_active" || provider.requests[0].ReturnURL != portalTestReturnURL {
		t.Fatalf("provider requests = %#v", provider.requests)
	}
	if len(billingService.requests) != 2 {
		t.Fatalf("billing requests = %#v", billingService.requests)
	}
	for _, request := range billingService.requests {
		if request.IntegratorName != "stripe" || len(request.ForUserIDs) != 1 || request.ForUserIDs[0] != "user_123" || request.Order != "created_at_desc" || request.PerPage != 100 {
			t.Fatalf("server-owned billing filter = %#v", request)
		}
	}
}

func TestProcessBillingProviderPortalRetainsNonPreferredLifecycleCustomer(t *testing.T) {
	provider := &portalProviderStub{returnURL: portalTestReturnURL}
	billingService := &portalBillingServiceStub{get: func(_ *billing.GetSubscriptionsRequest) (*billing.GetSubscriptionsResponse, error) {
		return &billing.GetSubscriptionsResponse{
			Subscriptions: []billing.Subscription{
				{UserID: "user_123", Integrator: "stripe", Status: "past_due", BillingKind: "recurring", IntegratorCustomerID: "customer_newest"},
				{UserID: "user_123", Integrator: "stripe", Status: billing.StatusCancelled, BillingKind: "recurring", IntegratorCustomerID: "customer_newest"},
			},
			Total: 2, TotalPages: 1, PerPage: 100, Page: 1,
		}, nil
	}}
	service := NewService(nil, billingService).WithCustomerPortalProviderRegistry(&portalRegistryStub{provider: provider})
	if _, err := service.ProcessBillingProviderPortal(context.Background(), portalRequest()); err != nil {
		t.Fatal(err)
	}
	if len(provider.requests) != 1 || provider.requests[0].CustomerID != "customer_newest" {
		t.Fatalf("provider requests = %#v", provider.requests)
	}
}

func TestProcessBillingProviderPortalRejectsAmbiguousCustomerIdentityWithinSelectedTier(t *testing.T) {
	tests := []struct {
		name          string
		subscriptions []billing.Subscription
	}{
		{name: "multiple preferred customers", subscriptions: []billing.Subscription{
			{UserID: "user_123", Integrator: "stripe", Status: billing.StatusActive, BillingKind: "recurring", IntegratorCustomerID: "customer_active_1"},
			{UserID: "user_123", Integrator: "stripe", Status: billing.StatusTrialing, BillingKind: "recurring", IntegratorCustomerID: "customer_active_2"},
		}},
		{name: "multiple fallback customers", subscriptions: []billing.Subscription{
			{UserID: "user_123", Integrator: "stripe", Status: "past_due", BillingKind: "recurring", IntegratorCustomerID: "customer_fallback_1"},
			{UserID: "user_123", Integrator: "stripe", Status: billing.StatusCancelled, BillingKind: "recurring", IntegratorCustomerID: "customer_fallback_2"},
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			provider := &portalProviderStub{returnURL: portalTestReturnURL}
			billingService := &portalBillingServiceStub{get: func(_ *billing.GetSubscriptionsRequest) (*billing.GetSubscriptionsResponse, error) {
				return &billing.GetSubscriptionsResponse{Subscriptions: test.subscriptions, Total: len(test.subscriptions), TotalPages: 1, PerPage: 100, Page: 1}, nil
			}}
			service := NewService(nil, billingService).WithCustomerPortalProviderRegistry(&portalRegistryStub{provider: provider})
			_, err := service.ProcessBillingProviderPortal(context.Background(), portalRequest())
			if !errors.Is(err, ErrBillingManagerPortalCustomerAmbiguous) || len(provider.requests) != 0 {
				t.Fatalf("error=%v provider calls=%d", err, len(provider.requests))
			}
		})
	}
}

func TestProcessBillingProviderPortalRejectsUntrustedInputsBeforeBillingLookup(t *testing.T) {
	tests := []struct {
		name        string
		request     *ProcessBillingProviderPortalRequest
		returnURL   string
		configErr   error
		registryErr error
		want        error
	}{
		{name: "missing user", request: &ProcessBillingProviderPortalRequest{ProviderName: "stripe"}, returnURL: portalTestReturnURL, want: ErrInvalidBillingManagerRequestPayload},
		{name: "malformed provider", request: &ProcessBillingProviderPortalRequest{UserID: "user_123", ProviderName: "stripe/other"}, returnURL: portalTestReturnURL, want: ErrInvalidBillingManagerRequestPayload},
		{name: "provider unavailable", request: portalRequest(), returnURL: portalTestReturnURL, registryErr: paymentprovider.ErrPaymentProviderNotFound, want: ErrBillingManagerPortalProviderUnavailable},
		{name: "provider config invalid", request: portalRequest(), returnURL: portalTestReturnURL, configErr: paymentprovider.ErrPaymentProviderInvalidConfiguration, want: ErrBillingManagerPortalConfigurationInvalid},
		{name: "missing return URL", request: portalRequest(), want: ErrBillingManagerPortalConfigurationInvalid},
		{name: "mismatched origin", request: &ProcessBillingProviderPortalRequest{UserID: "user_123", ProviderName: "stripe", Origin: "https://attacker.example.test"}, returnURL: portalTestReturnURL, want: ErrBillingManagerPortalOriginRejected},
		{name: "cross-site", request: &ProcessBillingProviderPortalRequest{UserID: "user_123", ProviderName: "stripe", Origin: "https://app.example.test", SecFetchSite: "cross-site"}, returnURL: portalTestReturnURL, want: ErrBillingManagerPortalOriginRejected},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			billingService := &portalBillingServiceStub{get: func(req *billing.GetSubscriptionsRequest) (*billing.GetSubscriptionsResponse, error) {
				return portalEmptyPage(req.Page), nil
			}}
			provider := &portalProviderStub{returnURL: test.returnURL, configErr: test.configErr}
			service := NewService(nil, billingService).WithCustomerPortalProviderRegistry(&portalRegistryStub{provider: provider, err: test.registryErr})
			_, err := service.ProcessBillingProviderPortal(context.Background(), test.request)
			if !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want %v", err, test.want)
			}
			if len(billingService.requests) != 0 || len(provider.requests) != 0 {
				t.Fatalf("untrusted input reached billing/provider: billing=%d provider=%d", len(billingService.requests), len(provider.requests))
			}
		})
	}
}

func TestProcessBillingProviderPortalRequiresProviderSessionURLAllowlistCapability(t *testing.T) {
	billingService := &portalBillingServiceStub{get: func(req *billing.GetSubscriptionsRequest) (*billing.GetSubscriptionsResponse, error) {
		return portalEmptyPage(req.Page), nil
	}}
	service := NewService(nil, billingService).WithCustomerPortalProviderRegistry(&portalRegistryStub{
		provider: &portalProviderWithoutSessionURLValidator{returnURL: portalTestReturnURL},
	})
	_, err := service.ProcessBillingProviderPortal(context.Background(), portalRequest())
	if !errors.Is(err, ErrBillingManagerPortalConfigurationInvalid) || len(billingService.requests) != 0 {
		t.Fatalf("error=%v billing calls=%d", err, len(billingService.requests))
	}
}

func TestProcessBillingProviderPortalFailsClosedForOwnershipEligibilityAndPagination(t *testing.T) {
	tests := []struct {
		name     string
		response *billing.GetSubscriptionsResponse
		want     error
	}{
		{name: "empty", response: portalEmptyPage(1), want: ErrBillingManagerPortalSubscriptionUnavailable},
		{name: "other user", response: &billing.GetSubscriptionsResponse{Subscriptions: []billing.Subscription{{UserID: "other", Integrator: "stripe", BillingKind: "recurring", IntegratorCustomerID: "customer_other"}}, Total: 1, TotalPages: 1, PerPage: 100, Page: 1}, want: ErrBillingManagerPortalSubscriptionUnavailable},
		{name: "other provider", response: &billing.GetSubscriptionsResponse{Subscriptions: []billing.Subscription{{UserID: "user_123", Integrator: "other", BillingKind: "recurring", IntegratorCustomerID: "customer_other"}}, Total: 1, TotalPages: 1, PerPage: 100, Page: 1}, want: ErrBillingManagerPortalSubscriptionUnavailable},
		{name: "one time", response: &billing.GetSubscriptionsResponse{Subscriptions: []billing.Subscription{{UserID: "user_123", Integrator: "stripe", BillingKind: "one_time", IsOneOff: true, IntegratorCustomerID: "customer_one_time"}}, Total: 1, TotalPages: 1, PerPage: 100, Page: 1}, want: ErrBillingManagerPortalSubscriptionUnavailable},
		{name: "missing customer", response: &billing.GetSubscriptionsResponse{Subscriptions: []billing.Subscription{{UserID: "user_123", Integrator: "stripe", BillingKind: "recurring"}}, Total: 1, TotalPages: 1, PerPage: 100, Page: 1}, want: ErrBillingManagerPortalSubscriptionUnavailable},
		{name: "wrong page", response: &billing.GetSubscriptionsResponse{Page: 2, PerPage: 100}, want: ErrBillingManagerPortalBillingUnavailable},
		{name: "wrong cardinality", response: &billing.GetSubscriptionsResponse{Total: 2, TotalPages: 1, PerPage: 100, Page: 1, Subscriptions: []billing.Subscription{{}}}, want: ErrBillingManagerPortalBillingUnavailable},
		{name: "over page cap", response: &billing.GetSubscriptionsResponse{Total: 10001, TotalPages: 101, PerPage: 100, Page: 1, Subscriptions: make([]billing.Subscription, 100)}, want: ErrBillingManagerPortalBillingUnavailable},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			provider := &portalProviderStub{returnURL: portalTestReturnURL}
			billingService := &portalBillingServiceStub{get: func(_ *billing.GetSubscriptionsRequest) (*billing.GetSubscriptionsResponse, error) {
				return test.response, nil
			}}
			service := NewService(nil, billingService).WithCustomerPortalProviderRegistry(&portalRegistryStub{provider: provider})
			_, err := service.ProcessBillingProviderPortal(context.Background(), portalRequest())
			if !errors.Is(err, test.want) || len(provider.requests) != 0 {
				t.Fatalf("error = %v, want %v; provider calls=%d", err, test.want, len(provider.requests))
			}
		})
	}
}

func TestProcessBillingProviderPortalValidatesEveryAdvertisedPage(t *testing.T) {
	firstPage := make([]billing.Subscription, 100)
	firstPage[0] = billing.Subscription{UserID: "user_123", Integrator: "stripe", Status: billing.StatusActive, BillingKind: "recurring", IntegratorCustomerID: "customer_active"}
	provider := &portalProviderStub{returnURL: portalTestReturnURL}
	billingService := &portalBillingServiceStub{get: func(req *billing.GetSubscriptionsRequest) (*billing.GetSubscriptionsResponse, error) {
		if req.Page == 1 {
			return &billing.GetSubscriptionsResponse{Subscriptions: firstPage, Total: 101, TotalPages: 2, PerPage: 100, Page: 1}, nil
		}
		return &billing.GetSubscriptionsResponse{Subscriptions: []billing.Subscription{{}}, Total: 100, TotalPages: 1, PerPage: 100, Page: 2}, nil
	}}
	service := NewService(nil, billingService).WithCustomerPortalProviderRegistry(&portalRegistryStub{provider: provider})
	_, err := service.ProcessBillingProviderPortal(context.Background(), portalRequest())
	if !errors.Is(err, ErrBillingManagerPortalBillingUnavailable) || len(provider.requests) != 0 || len(billingService.requests) != 2 {
		t.Fatalf("error=%v billing=%d provider=%d", err, len(billingService.requests), len(provider.requests))
	}
}

func TestPortalReturnURLOriginValidation(t *testing.T) {
	got, err := portalReturnURLOrigin("HTTPS://APP.EXAMPLE.TEST:0443/settings#billing")
	if err != nil || got != "https://app.example.test" {
		t.Fatalf("origin=%q error=%v", got, err)
	}
	for _, value := range []string{
		"", "/settings", "javascript:alert(1)", "https://user@app.example.test/settings",
		"https://app.example.test:70000/settings", "https://{tenant}.example.test/settings",
	} {
		if _, err := portalReturnURLOrigin(value); !errors.Is(err, ErrBillingManagerPortalConfigurationInvalid) {
			t.Errorf("portalReturnURLOrigin(%q) error=%v", value, err)
		}
	}
}

func TestProcessBillingProviderPortalMapsProviderFailuresToStableErrors(t *testing.T) {
	subscriptionPage := &billing.GetSubscriptionsResponse{
		Subscriptions: []billing.Subscription{{UserID: "user_123", Integrator: "stripe", Status: billing.StatusActive, BillingKind: "recurring", IntegratorCustomerID: "customer_123"}},
		Total:         1, TotalPages: 1, PerPage: 100, Page: 1,
	}
	tests := []struct {
		name     string
		provider *portalProviderStub
		want     error
	}{
		{name: "provider error", provider: &portalProviderStub{returnURL: portalTestReturnURL, err: errors.New("upstream secret")}, want: ErrBillingManagerPortalProviderRequestFailed},
		{name: "nil session", provider: &portalProviderStub{returnURL: portalTestReturnURL, session: &paymentprovider.CustomerPortalSession{}}, want: ErrBillingManagerPortalSessionInvalid},
		{name: "unsafe session URL", provider: &portalProviderStub{returnURL: portalTestReturnURL, session: &paymentprovider.CustomerPortalSession{ID: "bps_123", URL: "javascript:alert(1)"}}, want: ErrBillingManagerPortalSessionInvalid},
		{name: "untrusted HTTPS session URL", provider: &portalProviderStub{returnURL: portalTestReturnURL, session: &paymentprovider.CustomerPortalSession{ID: "bps_123", URL: "https://attacker.example.test/session"}}, want: ErrBillingManagerPortalSessionInvalid},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			billingService := &portalBillingServiceStub{get: func(_ *billing.GetSubscriptionsRequest) (*billing.GetSubscriptionsResponse, error) {
				return subscriptionPage, nil
			}}
			service := NewService(nil, billingService).WithCustomerPortalProviderRegistry(&portalRegistryStub{provider: test.provider})
			_, err := service.ProcessBillingProviderPortal(context.Background(), portalRequest())
			if !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestMapRequestToProcessBillingProviderPortalRequestUsesOnlyAuthenticatedTransportValues(t *testing.T) {
	request := authenticatedCheckoutHTTPRequest(http.MethodPost, "/api/v1/bms/billings/stripe/portal?customer_id=customer_attacker&return_url=https://attacker.example.test")
	request.Header.Set("Origin", " https://app.example.test ")
	request.Header.Set("Sec-Fetch-Site", " same-origin ")
	got, err := mapRequestToProcessBillingProviderPortalRequest(request, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.UserID != "user_123" || got.ProviderName != "stripe" || got.Origin != "https://app.example.test" || got.SecFetchSite != "same-origin" {
		t.Fatalf("mapped request = %#v", got)
	}

	missingAuth := httptest.NewRequest(http.MethodPost, "/portal", nil)
	missingAuth = mux.SetURLVars(missingAuth, map[string]string{"providerName": "stripe"})
	if _, err := mapRequestToProcessBillingProviderPortalRequest(missingAuth, nil); !errors.Is(err, ErrBillingManagerUnableToIdentifyUser) {
		t.Fatalf("missing auth error = %v", err)
	}
}

func TestBillingManagerPortalHandlerAndRouteReturnStandardNoStoreEnvelope(t *testing.T) {
	provider := &portalProviderStub{returnURL: portalTestReturnURL}
	billingService := &portalBillingServiceStub{get: func(_ *billing.GetSubscriptionsRequest) (*billing.GetSubscriptionsResponse, error) {
		return &billing.GetSubscriptionsResponse{
			Subscriptions: []billing.Subscription{{UserID: "user_123", Integrator: "stripe", Status: billing.StatusActive, BillingKind: "recurring", IntegratorCustomerID: "customer_123"}},
			Total:         1, TotalPages: 1, PerPage: 100, Page: 1,
		}, nil
	}}
	handler := NewHandler(NewService(nil, billingService).WithCustomerPortalProviderRegistry(&portalRegistryStub{provider: provider}), nil)
	httpRouter := ghatdRouter.NewRouter(response.GetResourceNotFoundError, response.GetDefault200Response)
	middleware := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := accessmanagerhelpers.TransitWith(r.Context(), "user_123")
			ctx = accessmanagerhelpers.TransitAuthenticatedWith(ctx, true)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
	AttachRoutes(&AttachRoutesRequest{Router: httpRouter, Handler: handler, MiddlewareActiveValidApiTokenOrJWTMiddleware: middleware})

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/bms/billings/stripe/portal?customer_id=attacker", nil)
	request.Header.Set("Origin", "https://app.example.test")
	httpRouter.GetRouter().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusCreated || recorder.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("status=%d cache=%q body=%s", recorder.Code, recorder.Header().Get("Cache-Control"), recorder.Body.String())
	}
	var body struct {
		Data paymentprovider.CustomerPortalSession `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil || body.Data.ID != "bps_123" || body.Data.URL == "" {
		t.Fatalf("response body=%s error=%v", recorder.Body.String(), err)
	}

	optionsRecorder := httptest.NewRecorder()
	httpRouter.GetRouter().ServeHTTP(optionsRecorder, httptest.NewRequest(http.MethodOptions, "/api/v1/bms/billings/stripe/portal", nil))
	if optionsRecorder.Code != http.StatusNoContent || len(provider.requests) != 1 || len(billingService.requests) != 1 {
		t.Fatalf("OPTIONS status=%d billing=%d provider=%d", optionsRecorder.Code, len(billingService.requests), len(provider.requests))
	}

	getRecorder := httptest.NewRecorder()
	httpRouter.GetRouter().ServeHTTP(getRecorder, httptest.NewRequest(http.MethodGet, "/api/v1/bms/billings/stripe/portal", nil))
	if getRecorder.Code == http.StatusCreated || getRecorder.Code == http.StatusOK {
		t.Fatalf("GET unexpectedly reached portal handler: %d", getRecorder.Code)
	}
}

type fullPortalProviderStub struct {
	*paymentprovider.MockProvider
	portal *portalProviderStub
}

var _ billingManagerPortalService = (*Service)(nil)
var _ billingmanagerPortalHandler = (*Handler)(nil)

func (*fullPortalProviderStub) GetProviderName() string { return "stripe" }
func (s *fullPortalProviderStub) CreateCustomerPortalSession(ctx context.Context, req *paymentprovider.CustomerPortalSessionRequest) (*paymentprovider.CustomerPortalSession, error) {
	return s.portal.CreateCustomerPortalSession(ctx, req)
}
func (s *fullPortalProviderStub) GetCustomerPortalReturnURL() string {
	return s.portal.GetCustomerPortalReturnURL()
}
func (s *fullPortalProviderStub) ValidateCustomerPortalConfig() error {
	return s.portal.ValidateCustomerPortalConfig()
}
func (s *fullPortalProviderStub) ValidateCustomerPortalSessionURL(value string) error {
	return s.portal.ValidateCustomerPortalSessionURL(value)
}

func TestNewServiceAutoDetectsCustomerPortalProviderRegistry(t *testing.T) {
	registry := paymentprovider.NewProviderRegistry()
	registry.Register(&fullPortalProviderStub{MockProvider: paymentprovider.NewMockProvider("stripe"), portal: &portalProviderStub{returnURL: portalTestReturnURL}})
	service := NewService(registry, nil)
	if service.CustomerPortalProviderRegistry == nil {
		t.Fatal("expected portal registry capability")
	}
	provider, err := service.CustomerPortalProviderRegistry.GetCustomerPortalProvider("stripe")
	if err != nil || isNilCheckoutCapability(provider) {
		t.Fatalf("portal provider=%#v error=%v", provider, err)
	}
}

func TestProcessBillingProviderPortalRejectsTypedNilCapability(t *testing.T) {
	var provider *portalProviderStub
	billingService := &portalBillingServiceStub{get: func(req *billing.GetSubscriptionsRequest) (*billing.GetSubscriptionsResponse, error) {
		return portalEmptyPage(req.Page), nil
	}}
	service := NewService(nil, billingService).WithCustomerPortalProviderRegistry(&portalRegistryStub{provider: provider})
	_, err := service.ProcessBillingProviderPortal(context.Background(), portalRequest())
	if !errors.Is(err, ErrBillingManagerPortalProviderUnavailable) || len(billingService.requests) != 0 {
		t.Fatalf("error=%v billing calls=%d", err, len(billingService.requests))
	}
}

func TestProcessBillingProviderPortalRejectsTypedNilDependencies(t *testing.T) {
	var registry *portalRegistryStub
	service := NewService(nil, &portalBillingServiceStub{}).WithCustomerPortalProviderRegistry(registry)
	if _, err := service.ProcessBillingProviderPortal(context.Background(), portalRequest()); !errors.Is(err, ErrBillingManagerPortalConfigurationInvalid) {
		t.Fatalf("typed nil registry error=%v", err)
	}

	var billingService *portalBillingServiceStub
	service = NewService(nil, billingService).WithCustomerPortalProviderRegistry(&portalRegistryStub{provider: &portalProviderStub{returnURL: portalTestReturnURL}})
	if _, err := service.ProcessBillingProviderPortal(context.Background(), portalRequest()); !errors.Is(err, ErrBillingManagerPortalConfigurationInvalid) {
		t.Fatalf("typed nil billing service error=%v", err)
	}
}

func TestPortalRequestBounds(t *testing.T) {
	tests := []*ProcessBillingProviderPortalRequest{
		{UserID: "user_123", ProviderName: strings.Repeat("p", 65)},
		{UserID: "user_123", ProviderName: "stripe", Origin: strings.Repeat("o", checkoutMaxOriginLength+1)},
		{UserID: "user_123", ProviderName: "stripe", SecFetchSite: strings.Repeat("s", checkoutMaxFetchSiteLength+1)},
	}
	for _, request := range tests {
		service := &Service{}
		if _, err := service.ProcessBillingProviderPortal(context.Background(), request); !errors.Is(err, ErrInvalidBillingManagerRequestPayload) {
			t.Errorf("request=%#v error=%v", request, err)
		}
	}
}
