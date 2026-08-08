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
	"time"

	"github.com/gorilla/mux"
	accessmanagerhelpers "github.com/ooaklee/ghatd/external/accessmanager/helpers"
	"github.com/ooaklee/ghatd/external/common"
	"github.com/ooaklee/ghatd/external/paymentprovider"
	"github.com/ooaklee/ghatd/external/pricer"
	"github.com/ooaklee/ghatd/external/response"
	ghatdRouter "github.com/ooaklee/ghatd/external/router"
	user "github.com/ooaklee/ghatd/external/user/v2"
)

const checkoutTestReturnURL = "https://app.example.test/app/plan?stripe=success&session_id={CHECKOUT_SESSION_ID}"

type checkoutProviderStub struct {
	requests  []*paymentprovider.CheckoutSessionRequest
	session   *paymentprovider.CheckoutSession
	err       error
	configErr error
	returnURL string
}

func (s *checkoutProviderStub) CreateCheckoutSession(_ context.Context, req *paymentprovider.CheckoutSessionRequest) (*paymentprovider.CheckoutSession, error) {
	s.requests = append(s.requests, req)
	if s.err != nil {
		return nil, s.err
	}
	if s.session != nil {
		return s.session, nil
	}
	return &paymentprovider.CheckoutSession{ID: "cs_test_123", ClientSecret: "cs_secret_123", PublishableKey: "pk_test_123"}, nil
}

func (s *checkoutProviderStub) GetCheckoutReturnURL() string {
	if s == nil {
		return ""
	}
	return s.returnURL
}

func (s *checkoutProviderStub) ValidateCheckoutConfig() error {
	if s == nil {
		return paymentprovider.ErrPaymentProviderInvalidConfiguration
	}
	return s.configErr
}

type checkoutRegistryStub struct {
	provider paymentprovider.CheckoutProvider
	err      error
	name     string
}

func (s *checkoutRegistryStub) GetCheckoutProvider(name string) (paymentprovider.CheckoutProvider, error) {
	s.name = name
	if s.err != nil {
		return nil, s.err
	}
	return s.provider, nil
}

type checkoutPricerStub struct {
	getPricePlans func(context.Context, *pricer.GetPricePlansRequest) (*pricer.GetPricePlansResponse, error)
}

func (s *checkoutPricerStub) GetPricePlans(ctx context.Context, req *pricer.GetPricePlansRequest) (*pricer.GetPricePlansResponse, error) {
	return s.getPricePlans(ctx, req)
}

func (*checkoutPricerStub) GetPricePlanBySlug(context.Context, *pricer.GetPricePlanBySlugRequest) (*pricer.GetPricePlanBySlugResponse, error) {
	return &pricer.GetPricePlanBySlugResponse{}, nil
}

func (*checkoutPricerStub) GetFeatures(context.Context, *pricer.GetFeaturesRequest) (*pricer.GetFeaturesResponse, error) {
	return &pricer.GetFeaturesResponse{}, nil
}

type checkoutUserServiceStub struct {
	response *user.GetUserByIDResponse
	err      error
}

func (*checkoutUserServiceStub) GetUserByEmail(context.Context, *user.GetUserByEmailRequest) (*user.GetUserByEmailResponse, error) {
	return nil, user.ErrUserNotFound
}

func (s *checkoutUserServiceStub) GetUserByID(context.Context, *user.GetUserByIDRequest) (*user.GetUserByIDResponse, error) {
	return s.response, s.err
}

func checkoutPublishedPlan(cadence pricer.PriceBillingCadence) pricer.PricePlan {
	return pricer.PricePlan{
		ID:          "plan_123",
		Slug:        "pro",
		Name:        "Pro",
		Status:      pricer.PricePlanStatusPublished,
		PublishedAt: time.Now().UTC().Add(-time.Hour).Format(time.RFC3339),
		Costs: []pricer.PriceCost{{
			ID:             "cost_123",
			Amount:         9900,
			Currency:       "GBP",
			BillingCadence: cadence,
			ProviderRefs: []pricer.PriceProviderRef{{
				Provider:        pricer.PriceProviderStripe,
				ProviderPriceID: "price_123",
			}},
		}},
	}
}

func checkoutServiceForPlan(plan pricer.PricePlan, provider *checkoutProviderStub) *Service {
	if provider != nil && provider.returnURL == "" {
		provider.returnURL = checkoutTestReturnURL
	}
	return &Service{
		CheckoutProviderRegistry: &checkoutRegistryStub{provider: provider},
		PricerService: &checkoutPricerStub{getPricePlans: func(_ context.Context, req *pricer.GetPricePlansRequest) (*pricer.GetPricePlansResponse, error) {
			return checkoutPricePlansPage(req, []pricer.PricePlan{plan}, 1), nil
		}},
		UserService: &checkoutUserServiceStub{response: &user.GetUserByIDResponse{User: &user.UniversalUser{ID: "user_123", Email: "authoritative@example.test"}}},
	}
}

func checkoutPricePlansPage(req *pricer.GetPricePlansRequest, plans []pricer.PricePlan, total int) *pricer.GetPricePlansResponse {
	totalPages := total / checkoutCataloguePageSize
	if total%checkoutCataloguePageSize != 0 {
		totalPages++
	}
	return &pricer.GetPricePlansResponse{
		PricePlans: plans,
		Total:      total,
		TotalPages: totalPages,
		Page:       req.Page,
		PerPage:    req.PerPage,
	}
}

func checkoutRequest() *ProcessBillingProviderCheckoutRequest {
	return &ProcessBillingProviderCheckoutRequest{
		UserID:         "user_123",
		ProviderName:   "stripe",
		PriceID:        "price_123",
		IdempotencyKey: "browser-attempt-123",
		Origin:         "https://app.example.test",
		SecFetchSite:   "same-origin",
	}
}

func TestProcessBillingProviderCheckoutBuildsTrustedProviderRequestAcrossCataloguePages(t *testing.T) {
	provider := &checkoutProviderStub{returnURL: checkoutTestReturnURL}
	plan := checkoutPublishedPlan(pricer.PriceBillingCadenceMonthly)
	plan.Costs[0].TrialPeriodDays = 731 // provider-specific limits do not belong in BMS
	fillerPlan := checkoutPublishedPlan(pricer.PriceBillingCadenceMonthly)
	fillerPlan.Costs = nil
	firstPage := make([]pricer.PricePlan, checkoutCataloguePageSize)
	for index := range firstPage {
		firstPage[index] = fillerPlan
	}
	var requests []*pricer.GetPricePlansRequest
	service := (&Service{
		CheckoutProviderRegistry: &checkoutRegistryStub{provider: provider},
		PricerService: &checkoutPricerStub{getPricePlans: func(_ context.Context, req *pricer.GetPricePlansRequest) (*pricer.GetPricePlansResponse, error) {
			copyOfRequest := *req
			requests = append(requests, &copyOfRequest)
			if req.Page == 1 {
				return checkoutPricePlansPage(req, firstPage, checkoutCataloguePageSize+1), nil
			}
			return checkoutPricePlansPage(req, []pricer.PricePlan{plan}, checkoutCataloguePageSize+1), nil
		}},
		UserService: &checkoutUserServiceStub{response: &user.GetUserByIDResponse{User: &user.UniversalUser{ID: "user_123", Email: " authoritative@example.test "}}},
	})

	first, err := service.ProcessBillingProviderCheckout(context.Background(), checkoutRequest())
	if err != nil {
		t.Fatalf("ProcessBillingProviderCheckout() error = %v", err)
	}
	second, err := service.ProcessBillingProviderCheckout(context.Background(), checkoutRequest())
	if err != nil {
		t.Fatalf("second ProcessBillingProviderCheckout() error = %v", err)
	}
	if first == nil || first.Session == nil || second == nil || len(provider.requests) != 2 {
		t.Fatalf("unexpected checkout responses or provider calls: first=%#v second=%#v calls=%d", first, second, len(provider.requests))
	}
	if len(requests) != 4 {
		t.Fatalf("catalogue requests = %d, want 4", len(requests))
	}
	for _, request := range requests {
		if request.PerPage != 100 || request.WithStatus != string(pricer.PricePlanStatusPublished) || !request.IsPublished || !request.IsNotDeleted || !request.IncludeCosts || !request.IncludeProviders {
			t.Fatalf("catalogue request did not fail closed: %#v", request)
		}
	}

	got := provider.requests[0]
	if got.PriceID != "price_123" || got.PlanID != "plan_123" || got.PlanSlug != "pro" || got.PlanName != "Pro" || got.CostID != "cost_123" {
		t.Fatalf("stable checkout metadata = %#v", got)
	}
	if got.UserID != "user_123" || got.UserReference != "user_123" || got.CustomerEmail != "authoritative@example.test" {
		t.Fatalf("authoritative user fields = %#v", got)
	}
	if got.Mode != paymentprovider.CheckoutModeSubscription || got.ExpectedAmount != 9900 || got.ExpectedCurrency != "GBP" || got.ExpectedBillingCadence != "month" || got.TrialPeriodDays != 731 {
		t.Fatalf("trusted price fields = %#v", got)
	}
	if got.IdempotencyKey == "browser-attempt-123" || !strings.HasPrefix(got.IdempotencyKey, "bms-checkout-") || got.IdempotencyKey != provider.requests[1].IdempotencyKey {
		t.Fatalf("scoped idempotency keys = %q and %q", got.IdempotencyKey, provider.requests[1].IdempotencyKey)
	}
	if got.Metadata["plan_id"] != "plan_123" || got.Metadata["cost_id"] != "cost_123" || got.Metadata["provider_price_id"] != "price_123" {
		t.Fatalf("provider metadata = %#v", got.Metadata)
	}
	if !strings.Contains(got.ReturnURL, "{CHECKOUT_SESSION_ID}") {
		t.Fatalf("ReturnURL lost provider placeholder: %q", got.ReturnURL)
	}
	parsed, err := url.Parse(strings.ReplaceAll(got.ReturnURL, "{CHECKOUT_SESSION_ID}", "session"))
	if err != nil {
		t.Fatalf("parse enriched ReturnURL: %v", err)
	}
	if parsed.Query().Get("plan_id") != "plan_123" || parsed.Query().Get("plan_slug") != "pro" || parsed.Query().Get("cost_id") != "cost_123" {
		t.Fatalf("enriched ReturnURL query = %v", parsed.Query())
	}
}

func TestProcessBillingProviderCheckoutMapsOneTimeAndRecurringModes(t *testing.T) {
	tests := []struct {
		name    string
		cadence pricer.PriceBillingCadence
		mode    string
	}{
		{name: "one time", cadence: pricer.PriceBillingCadenceOneTime, mode: paymentprovider.CheckoutModePayment},
		{name: "weekly", cadence: pricer.PriceBillingCadenceWeekly, mode: paymentprovider.CheckoutModeSubscription},
		{name: "yearly", cadence: pricer.PriceBillingCadenceYearly, mode: paymentprovider.CheckoutModeSubscription},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			provider := &checkoutProviderStub{}
			service := checkoutServiceForPlan(checkoutPublishedPlan(test.cadence), provider)
			if _, err := service.ProcessBillingProviderCheckout(context.Background(), checkoutRequest()); err != nil {
				t.Fatalf("ProcessBillingProviderCheckout() error = %v", err)
			}
			if len(provider.requests) != 1 || provider.requests[0].Mode != test.mode {
				t.Fatalf("provider mode = %#v, want %q", provider.requests, test.mode)
			}
		})
	}
}

func TestProcessBillingProviderCheckoutRejectsUnsupportedCatalogueTerms(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*pricer.PricePlan)
	}{
		{name: "missing plan id", mutate: func(plan *pricer.PricePlan) { plan.ID = "" }},
		{name: "missing plan slug", mutate: func(plan *pricer.PricePlan) { plan.Slug = "" }},
		{name: "missing cost id", mutate: func(plan *pricer.PricePlan) { plan.Costs[0].ID = "" }},
		{name: "zero amount", mutate: func(plan *pricer.PricePlan) { plan.Costs[0].Amount = 0 }},
		{name: "setup fee", mutate: func(plan *pricer.PricePlan) { plan.Costs[0].SetupFeeAmount = 100 }},
		{name: "discount", mutate: func(plan *pricer.PricePlan) {
			plan.Discounts = []pricer.PriceDiscount{{Type: pricer.PriceDiscountTypePercent, PercentBps: 100}}
		}},
		{name: "payment terms", mutate: func(plan *pricer.PricePlan) { plan.PaymentTerms = &pricer.PricePaymentTerms{} }},
		{name: "unsupported cadence", mutate: func(plan *pricer.PricePlan) { plan.Costs[0].BillingCadence = "day" }},
		{name: "negative trial", mutate: func(plan *pricer.PricePlan) { plan.Costs[0].TrialPeriodDays = -1 }},
		{name: "one-time trial", mutate: func(plan *pricer.PricePlan) {
			plan.Costs[0].BillingCadence = pricer.PriceBillingCadenceOneTime
			plan.Costs[0].TrialPeriodDays = 1
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			plan := checkoutPublishedPlan(pricer.PriceBillingCadenceMonthly)
			test.mutate(&plan)
			provider := &checkoutProviderStub{}
			_, err := checkoutServiceForPlan(plan, provider).ProcessBillingProviderCheckout(context.Background(), checkoutRequest())
			if !errors.Is(err, ErrBillingManagerCheckoutTermsUnsupported) {
				t.Fatalf("error = %v, want checkout terms unsupported", err)
			}
			if len(provider.requests) != 0 {
				t.Fatalf("provider called for unsupported catalogue: %d", len(provider.requests))
			}
		})
	}
}

func TestProcessBillingProviderCheckoutFailsClosedForCatalogueAmbiguityAndBounds(t *testing.T) {
	t.Run("duplicate price", func(t *testing.T) {
		plan := checkoutPublishedPlan(pricer.PriceBillingCadenceMonthly)
		duplicate := plan
		duplicate.ID = "plan_duplicate"
		duplicate.Slug = "duplicate"
		duplicate.Costs = append([]pricer.PriceCost(nil), plan.Costs...)
		duplicate.Costs[0].ID = "cost_duplicate"
		service := checkoutServiceForPlan(plan, &checkoutProviderStub{})
		service.PricerService = &checkoutPricerStub{getPricePlans: func(_ context.Context, req *pricer.GetPricePlansRequest) (*pricer.GetPricePlansResponse, error) {
			return checkoutPricePlansPage(req, []pricer.PricePlan{plan, duplicate}, 2), nil
		}}
		_, err := service.ProcessBillingProviderCheckout(context.Background(), checkoutRequest())
		if !errors.Is(err, ErrBillingManagerCheckoutPriceAmbiguous) {
			t.Fatalf("error = %v, want ambiguous price", err)
		}
	})

	t.Run("future publication is unavailable", func(t *testing.T) {
		plan := checkoutPublishedPlan(pricer.PriceBillingCadenceMonthly)
		plan.PublishedAt = time.Now().UTC().Add(time.Hour).Format(time.RFC3339)
		provider := &checkoutProviderStub{}
		_, err := checkoutServiceForPlan(plan, provider).ProcessBillingProviderCheckout(context.Background(), checkoutRequest())
		if !errors.Is(err, ErrBillingManagerCheckoutPriceUnavailable) || len(provider.requests) != 0 {
			t.Fatalf("error = %v provider calls = %d, want unavailable price with no provider call", err, len(provider.requests))
		}
	})

	t.Run("catalogue page limit", func(t *testing.T) {
		calls := 0
		provider := &checkoutProviderStub{}
		service := checkoutServiceForPlan(checkoutPublishedPlan(pricer.PriceBillingCadenceMonthly), provider)
		service.PricerService = &checkoutPricerStub{getPricePlans: func(_ context.Context, req *pricer.GetPricePlansRequest) (*pricer.GetPricePlansResponse, error) {
			calls++
			return checkoutPricePlansPage(req, make([]pricer.PricePlan, checkoutCataloguePageSize), checkoutCataloguePageSize*checkoutCatalogueMaxPages+1), nil
		}}
		_, err := service.ProcessBillingProviderCheckout(context.Background(), checkoutRequest())
		if !errors.Is(err, ErrBillingManagerCheckoutCatalogueUnavailable) || calls != 1 || len(provider.requests) != 0 {
			t.Fatalf("error = %v catalogue calls = %d provider calls = %d", err, calls, len(provider.requests))
		}
	})

	t.Run("inconsistent pagination cannot hide a duplicate", func(t *testing.T) {
		plan := checkoutPublishedPlan(pricer.PriceBillingCadenceMonthly)
		provider := &checkoutProviderStub{}
		service := checkoutServiceForPlan(plan, provider)
		service.PricerService = &checkoutPricerStub{getPricePlans: func(_ context.Context, req *pricer.GetPricePlansRequest) (*pricer.GetPricePlansResponse, error) {
			return &pricer.GetPricePlansResponse{
				PricePlans: []pricer.PricePlan{plan},
				Total:      checkoutCataloguePageSize + 1,
				TotalPages: 1,
				Page:       req.Page,
				PerPage:    req.PerPage,
			}, nil
		}}
		_, err := service.ProcessBillingProviderCheckout(context.Background(), checkoutRequest())
		if !errors.Is(err, ErrBillingManagerCheckoutCatalogueUnavailable) || len(provider.requests) != 0 {
			t.Fatalf("error = %v provider calls = %d", err, len(provider.requests))
		}
	})
}

func TestProcessBillingProviderCheckoutRejectsUntrustedOriginsBeforeProviderCalls(t *testing.T) {
	tests := []struct {
		name        string
		origin      string
		secFetch    string
		returnURL   string
		wantError   error
		shouldStart bool
	}{
		{name: "matching origin", origin: "https://app.example.test", returnURL: checkoutTestReturnURL, shouldStart: true},
		{name: "matching origin normalises case", origin: "HTTPS://APP.EXAMPLE.TEST", returnURL: checkoutTestReturnURL, shouldStart: true},
		{name: "canonical default port", origin: "https://app.example.test", returnURL: "https://app.example.test:443/app?session={SESSION_ID}", shouldStart: true},
		{name: "mismatched origin", origin: "https://attacker.example.test", returnURL: checkoutTestReturnURL, wantError: ErrBillingManagerCheckoutOriginRejected},
		{name: "origin with path", origin: "https://app.example.test/not-an-origin", returnURL: checkoutTestReturnURL, wantError: ErrBillingManagerCheckoutOriginRejected},
		{name: "cross-site fetch metadata", origin: "https://app.example.test", secFetch: "cross-site", returnURL: checkoutTestReturnURL, wantError: ErrBillingManagerCheckoutOriginRejected},
		{name: "invalid trusted URL", origin: "https://app.example.test", returnURL: "javascript:alert(1)", wantError: ErrBillingManagerCheckoutConfigurationInvalid},
		{name: "nil trusted config", origin: "https://app.example.test", returnURL: "", wantError: ErrBillingManagerCheckoutConfigurationInvalid},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			provider := &checkoutProviderStub{returnURL: test.returnURL}
			service := checkoutServiceForPlan(checkoutPublishedPlan(pricer.PriceBillingCadenceMonthly), provider)
			provider.returnURL = test.returnURL
			request := checkoutRequest()
			request.Origin = test.origin
			request.SecFetchSite = test.secFetch
			_, err := service.ProcessBillingProviderCheckout(context.Background(), request)
			if test.wantError != nil && !errors.Is(err, test.wantError) {
				t.Fatalf("error = %v, want %v", err, test.wantError)
			}
			if test.wantError == nil && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if test.shouldStart != (len(provider.requests) == 1) {
				t.Fatalf("provider calls = %d, shouldStart = %v", len(provider.requests), test.shouldStart)
			}
		})
	}
}

type checkoutProviderWithoutReturnURLStub struct {
	checkout *checkoutProviderStub
}

func (s *checkoutProviderWithoutReturnURLStub) CreateCheckoutSession(ctx context.Context, req *paymentprovider.CheckoutSessionRequest) (*paymentprovider.CheckoutSession, error) {
	return s.checkout.CreateCheckoutSession(ctx, req)
}

func TestProcessBillingProviderCheckoutProviderOwnedReturnURLAndLegacyFallback(t *testing.T) {
	t.Run("provider-owned URL overrides deprecated fallback", func(t *testing.T) {
		provider := &checkoutProviderStub{returnURL: checkoutTestReturnURL}
		service := checkoutServiceForPlan(checkoutPublishedPlan(pricer.PriceBillingCadenceMonthly), provider).
			WithCheckoutProviderConfig("stripe", &CheckoutProviderConfig{ReturnURL: "https://legacy.example.test/return"})
		if _, err := service.ProcessBillingProviderCheckout(context.Background(), checkoutRequest()); err != nil {
			t.Fatalf("ProcessBillingProviderCheckout() error = %v", err)
		}
		if len(provider.requests) != 1 || !strings.HasPrefix(provider.requests[0].ReturnURL, "https://app.example.test/") {
			t.Fatalf("provider-owned ReturnURL not used: %#v", provider.requests)
		}
	})

	t.Run("deprecated fallback preserves custom provider compatibility", func(t *testing.T) {
		checkout := &checkoutProviderStub{}
		service := checkoutServiceForPlan(checkoutPublishedPlan(pricer.PriceBillingCadenceMonthly), checkout)
		service.CheckoutProviderRegistry = &checkoutRegistryStub{provider: &checkoutProviderWithoutReturnURLStub{checkout: checkout}}
		service.WithCheckoutProviderConfig("stripe", &CheckoutProviderConfig{ReturnURL: checkoutTestReturnURL})
		if _, err := service.ProcessBillingProviderCheckout(context.Background(), checkoutRequest()); err != nil {
			t.Fatalf("ProcessBillingProviderCheckout() error = %v", err)
		}
		if len(checkout.requests) != 1 {
			t.Fatalf("legacy provider calls = %d", len(checkout.requests))
		}
	})

	t.Run("typed-nil checkout provider fails closed", func(t *testing.T) {
		service := checkoutServiceForPlan(checkoutPublishedPlan(pricer.PriceBillingCadenceMonthly), &checkoutProviderStub{})
		var provider *checkoutProviderStub
		service.CheckoutProviderRegistry = &checkoutRegistryStub{provider: provider}
		_, err := service.ProcessBillingProviderCheckout(context.Background(), checkoutRequest())
		if !errors.Is(err, ErrBillingManagerCheckoutProviderUnavailable) {
			t.Fatalf("error = %v, want checkout provider unavailable", err)
		}
	})
}

func TestProcessBillingProviderCheckoutBoundsDirectTransportInputs(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*ProcessBillingProviderCheckoutRequest)
	}{
		{name: "malformed provider", mutate: func(req *ProcessBillingProviderCheckoutRequest) { req.ProviderName = "stripe/other" }},
		{name: "oversized price", mutate: func(req *ProcessBillingProviderCheckoutRequest) {
			req.PriceID = strings.Repeat("p", checkoutMaxPriceIDLength+1)
		}},
		{name: "oversized key", mutate: func(req *ProcessBillingProviderCheckoutRequest) {
			req.IdempotencyKey = strings.Repeat("k", checkoutMaxClientKeyLength+1)
		}},
		{name: "oversized origin", mutate: func(req *ProcessBillingProviderCheckoutRequest) {
			req.Origin = "https://" + strings.Repeat("o", checkoutMaxOriginLength)
		}},
		{name: "oversized fetch site", mutate: func(req *ProcessBillingProviderCheckoutRequest) {
			req.SecFetchSite = strings.Repeat("s", checkoutMaxFetchSiteLength+1)
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			provider := &checkoutProviderStub{}
			request := checkoutRequest()
			test.mutate(request)
			_, err := checkoutServiceForPlan(checkoutPublishedPlan(pricer.PriceBillingCadenceMonthly), provider).
				ProcessBillingProviderCheckout(context.Background(), request)
			if !errors.Is(err, ErrInvalidBillingManagerRequestPayload) || len(provider.requests) != 0 {
				t.Fatalf("error = %v provider calls = %d", err, len(provider.requests))
			}
		})
	}
}

func TestProcessBillingProviderCheckoutReturnsStableProviderAndUserErrors(t *testing.T) {
	t.Run("missing provider", func(t *testing.T) {
		service := checkoutServiceForPlan(checkoutPublishedPlan(pricer.PriceBillingCadenceMonthly), &checkoutProviderStub{})
		service.CheckoutProviderRegistry = &checkoutRegistryStub{err: paymentprovider.ErrPaymentProviderNotFound}
		_, err := service.ProcessBillingProviderCheckout(context.Background(), checkoutRequest())
		if !errors.Is(err, ErrBillingManagerCheckoutProviderUnavailable) {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("invalid provider checkout configuration", func(t *testing.T) {
		provider := &checkoutProviderStub{configErr: paymentprovider.ErrPaymentProviderInvalidConfiguration}
		_, err := checkoutServiceForPlan(checkoutPublishedPlan(pricer.PriceBillingCadenceMonthly), provider).
			ProcessBillingProviderCheckout(context.Background(), checkoutRequest())
		if !errors.Is(err, ErrBillingManagerCheckoutConfigurationInvalid) || len(provider.requests) != 0 {
			t.Fatalf("error = %v provider calls = %d", err, len(provider.requests))
		}
	})

	t.Run("authoritative user missing", func(t *testing.T) {
		provider := &checkoutProviderStub{}
		service := checkoutServiceForPlan(checkoutPublishedPlan(pricer.PriceBillingCadenceMonthly), provider)
		service.UserService = &checkoutUserServiceStub{response: &user.GetUserByIDResponse{User: &user.UniversalUser{ID: "other_user", Email: "spoof@example.test"}}}
		_, err := service.ProcessBillingProviderCheckout(context.Background(), checkoutRequest())
		if !errors.Is(err, ErrBillingManagerCheckoutUserUnavailable) || len(provider.requests) != 0 {
			t.Fatalf("error = %v calls = %d", err, len(provider.requests))
		}
	})

	t.Run("generic provider failure", func(t *testing.T) {
		provider := &checkoutProviderStub{err: paymentprovider.ErrPaymentProviderAPIRequestFailed}
		_, err := checkoutServiceForPlan(checkoutPublishedPlan(pricer.PriceBillingCadenceMonthly), provider).ProcessBillingProviderCheckout(context.Background(), checkoutRequest())
		if !errors.Is(err, ErrBillingManagerCheckoutProviderRequestFailed) {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("price mismatch remains actionable", func(t *testing.T) {
		provider := &checkoutProviderStub{err: paymentprovider.ErrPaymentProviderPriceMismatch}
		_, err := checkoutServiceForPlan(checkoutPublishedPlan(pricer.PriceBillingCadenceMonthly), provider).ProcessBillingProviderCheckout(context.Background(), checkoutRequest())
		if !errors.Is(err, paymentprovider.ErrPaymentProviderPriceMismatch) {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("nil session", func(t *testing.T) {
		provider := &checkoutProviderStub{session: &paymentprovider.CheckoutSession{}}
		_, err := checkoutServiceForPlan(checkoutPublishedPlan(pricer.PriceBillingCadenceMonthly), provider).ProcessBillingProviderCheckout(context.Background(), checkoutRequest())
		if !errors.Is(err, ErrBillingManagerCheckoutSessionInvalid) {
			t.Fatalf("error = %v", err)
		}
	})
}

func authenticatedCheckoutHTTPRequest(method, target string) *http.Request {
	request := httptest.NewRequest(method, target, nil)
	request = mux.SetURLVars(request, map[string]string{"providerName": "stripe"})
	ctx := accessmanagerhelpers.TransitWith(request.Context(), "user_123")
	ctx = accessmanagerhelpers.TransitAuthenticatedWith(ctx, true)
	return request.WithContext(ctx)
}

func TestMapRequestToProcessBillingProviderCheckoutRequestUsesOnlyAuthenticatedTransportValues(t *testing.T) {
	request := authenticatedCheckoutHTTPRequest(http.MethodPost, "/api/v1/bms/billings/stripe/checkout?price=price_123&UserID=spoof")
	request.Header.Set(common.IdempotencyKeyHttpHeader, " attempt_123 ")
	request.Header.Set("Origin", " https://app.example.test ")
	request.Header.Set("Sec-Fetch-Site", " same-origin ")

	got, err := mapRequestToProcessBillingProviderCheckoutRequest(request, nil)
	if err != nil {
		t.Fatalf("map checkout request: %v", err)
	}
	if got.UserID != "user_123" || got.ProviderName != "stripe" || got.PriceID != "price_123" || got.IdempotencyKey != "attempt_123" || got.Origin != "https://app.example.test" || got.SecFetchSite != "same-origin" {
		t.Fatalf("mapped request = %#v", got)
	}

	missingAuth := httptest.NewRequest(http.MethodPost, "/?price=price_123", nil)
	missingAuth = mux.SetURLVars(missingAuth, map[string]string{"providerName": "stripe"})
	if _, err := mapRequestToProcessBillingProviderCheckoutRequest(missingAuth, nil); !errors.Is(err, ErrBillingManagerUnableToIdentifyUser) {
		t.Fatalf("missing-auth error = %v", err)
	}
	missingPrice := authenticatedCheckoutHTTPRequest(http.MethodPost, "/api/v1/bms/billings/stripe/checkout")
	if _, err := mapRequestToProcessBillingProviderCheckoutRequest(missingPrice, nil); !errors.Is(err, ErrInvalidBillingManagerRequestPayload) {
		t.Fatalf("missing-price error = %v", err)
	}

	tests := []struct {
		name         string
		providerName string
		priceID      string
		clientKey    string
		origin       string
		secFetchSite string
	}{
		{name: "malformed provider", providerName: "stripe/other", priceID: "price_123"},
		{name: "oversized provider", providerName: strings.Repeat("a", 65), priceID: "price_123"},
		{name: "oversized price", providerName: "stripe", priceID: strings.Repeat("p", checkoutMaxPriceIDLength+1)},
		{name: "oversized idempotency key", providerName: "stripe", priceID: "price_123", clientKey: strings.Repeat("k", checkoutMaxClientKeyLength+1)},
		{name: "oversized origin", providerName: "stripe", priceID: "price_123", origin: "https://" + strings.Repeat("o", checkoutMaxOriginLength)},
		{name: "oversized fetch site", providerName: "stripe", priceID: "price_123", secFetchSite: strings.Repeat("s", checkoutMaxFetchSiteLength+1)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := authenticatedCheckoutHTTPRequest(http.MethodPost, "/?price="+url.QueryEscape(test.priceID))
			request = mux.SetURLVars(request, map[string]string{"providerName": test.providerName})
			request.Header.Set(common.IdempotencyKeyHttpHeader, test.clientKey)
			request.Header.Set("Origin", test.origin)
			request.Header.Set("Sec-Fetch-Site", test.secFetchSite)
			if _, err := mapRequestToProcessBillingProviderCheckoutRequest(request, nil); !errors.Is(err, ErrInvalidBillingManagerRequestPayload) {
				t.Fatalf("error = %v, want invalid payload", err)
			}
		})
	}
}

func TestBillingManagerCheckoutHandlerAndRouteReturnStandardNoStoreEnvelope(t *testing.T) {
	provider := &checkoutProviderStub{}
	handler := NewHandler(checkoutServiceForPlan(checkoutPublishedPlan(pricer.PriceBillingCadenceMonthly), provider), nil)
	httpRouter := ghatdRouter.NewRouter(response.GetResourceNotFoundError, response.GetDefault200Response)
	middleware := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := accessmanagerhelpers.TransitWith(r.Context(), "user_123")
			ctx = accessmanagerhelpers.TransitAuthenticatedWith(ctx, true)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
	AttachRoutes(&AttachRoutesRequest{
		Router:  httpRouter,
		Handler: handler,
		MiddlewareActiveValidApiTokenOrJWTMiddleware: middleware,
	})

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/bms/billings/stripe/checkout?price=price_123", nil)
	request.Header.Set("Origin", "https://app.example.test")
	httpRouter.GetRouter().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusCreated || recorder.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("status=%d cache=%q body=%s", recorder.Code, recorder.Header().Get("Cache-Control"), recorder.Body.String())
	}
	var body struct {
		Data paymentprovider.CheckoutSession `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil || body.Data.ID != "cs_test_123" || body.Data.ClientSecret != "cs_secret_123" {
		t.Fatalf("standard response body = %s error=%v", recorder.Body.String(), err)
	}

	optionsRecorder := httptest.NewRecorder()
	optionsRequest := httptest.NewRequest(http.MethodOptions, "/api/v1/bms/billings/stripe/checkout", nil)
	httpRouter.GetRouter().ServeHTTP(optionsRecorder, optionsRequest)
	if optionsRecorder.Code != http.StatusNoContent || len(provider.requests) != 1 {
		t.Fatalf("OPTIONS status=%d provider calls=%d body=%s", optionsRecorder.Code, len(provider.requests), optionsRecorder.Body.String())
	}

	getRecorder := httptest.NewRecorder()
	httpRouter.GetRouter().ServeHTTP(getRecorder, httptest.NewRequest(http.MethodGet, "/api/v1/bms/billings/stripe/checkout?price=price_123", nil))
	if getRecorder.Code == http.StatusCreated || getRecorder.Code == http.StatusOK {
		t.Fatalf("GET unexpectedly reached checkout handler with status %d", getRecorder.Code)
	}
}

type legacyBillingManagerServiceStub struct {
	BillingManagerService
}

var _ BillingManagerService = (*legacyBillingManagerServiceStub)(nil)
var _ billingManagerCheckoutService = (*Service)(nil)

type legacyBillingManagerHandlerStub struct {
	billingmanagerHandler
}

var _ billingmanagerHandler = (*legacyBillingManagerHandlerStub)(nil)
var _ billingmanagerCheckoutHandler = (*Handler)(nil)

func TestCheckoutHandlerKeepsLegacyBillingManagerServiceSourceCompatible(t *testing.T) {
	legacyService := &legacyBillingManagerServiceStub{}
	if _, ok := interface{}(legacyService).(billingManagerCheckoutService); ok {
		t.Fatal("legacy BillingManagerService unexpectedly exposes checkout capability")
	}

	handler := NewHandler(legacyService, nil)
	recorder := httptest.NewRecorder()
	request := authenticatedCheckoutHTTPRequest(http.MethodPost, "/api/v1/bms/billings/stripe/checkout?price=price_123")
	handler.ProcessBillingProviderCheckout(recorder, request)
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("legacy service checkout status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestCheckoutRouteKeepsLegacyBillingManagerHandlerSourceCompatible(t *testing.T) {
	legacyHandler := &legacyBillingManagerHandlerStub{}
	if _, ok := interface{}(legacyHandler).(billingmanagerCheckoutHandler); ok {
		t.Fatal("legacy billingmanagerHandler unexpectedly exposes checkout capability")
	}
}

type fullCheckoutProviderStub struct {
	*paymentprovider.MockProvider
	checkout *checkoutProviderStub
}

func (*fullCheckoutProviderStub) GetProviderName() string { return "stripe" }

func (s *fullCheckoutProviderStub) CreateCheckoutSession(ctx context.Context, req *paymentprovider.CheckoutSessionRequest) (*paymentprovider.CheckoutSession, error) {
	return s.checkout.CreateCheckoutSession(ctx, req)
}

func (s *fullCheckoutProviderStub) GetCheckoutReturnURL() string {
	return s.checkout.GetCheckoutReturnURL()
}

func TestNewServiceAutoDetectsCheckoutProviderRegistry(t *testing.T) {
	registry := paymentprovider.NewProviderRegistry()
	registry.Register(&fullCheckoutProviderStub{MockProvider: paymentprovider.NewMockProvider("stripe"), checkout: &checkoutProviderStub{returnURL: checkoutTestReturnURL}})
	service := NewService(registry, nil)
	if service.CheckoutProviderRegistry == nil {
		t.Fatal("expected NewService to detect checkout provider registry capability")
	}
	provider, err := service.CheckoutProviderRegistry.GetCheckoutProvider("stripe")
	if err != nil {
		t.Fatalf("GetCheckoutProvider() error = %v", err)
	}
	configured, ok := provider.(paymentprovider.CheckoutReturnURLProvider)
	if !ok || configured.GetCheckoutReturnURL() != checkoutTestReturnURL {
		t.Fatalf("provider checkout configuration = %#v", provider)
	}
}
