package billingmanager

import (
	"net/http"

	"github.com/gorilla/mux"
	"github.com/ooaklee/ghatd/external/router"
)

// billingmanagerHandler expected methods for valid billingmanager handler
type billingmanagerHandler interface {
	// ProcessBillingProviderWebhooks serves the billing provider webhook HTTP
	// endpoint, mapping the request and delegating processing to the billing
	// manager service.
	ProcessBillingProviderWebhooks(w http.ResponseWriter, r *http.Request)
	// GetUserBillingEvents serves the user billing events HTTP endpoint, mapping
	// the request and returning the selected user's event summaries with optional
	// pagination metadata.
	GetUserBillingEvents(w http.ResponseWriter, r *http.Request)
	// GetUserSubscriptionStatus serves the user subscription status HTTP endpoint
	// required by billingmanagerHandler. Implementations validate the request,
	// delegate to the service, and write the subscription status or an error
	// response; w receives the HTTP response for request r.
	GetUserSubscriptionStatus(w http.ResponseWriter, r *http.Request)
	// GetUserBillingDetail serves the user billing detail HTTP endpoint required by
	// billingmanagerHandler. Implementations validate the request, delegate to the
	// service, and write the billing detail or an error response; w receives the
	// HTTP response for request r.
	GetUserBillingDetail(w http.ResponseWriter, r *http.Request)
	// GetPricingPlans serves the pricing plans listing HTTP endpoint required by
	// billingmanagerHandler. Implementations map and validate the request, delegate
	// to the service, and write the price plans, optionally enriched with response
	// metadata, to w.
	GetPricingPlans(w http.ResponseWriter, r *http.Request)
	// GetPricePlanBySlug serves the single pricing plan HTTP endpoint required by
	// billingmanagerHandler. Implementations map and validate the request, delegate
	// to the service, and write the requested plan or an error response to w.
	GetPricePlanBySlug(w http.ResponseWriter, r *http.Request)
	// GetPricingFeatures serves the pricing feature catalogue HTTP endpoint
	// required by billingmanagerHandler. Implementations map and validate the
	// request, delegate to the service, and write the feature list, optionally
	// enriched with response metadata, to w.
	GetPricingFeatures(w http.ResponseWriter, r *http.Request)
}

// billingmanagerCheckoutHandler is an optional routing capability so custom
// legacy route handlers keep satisfying billingmanagerHandler.
type billingmanagerCheckoutHandler interface {
	// ProcessBillingProviderCheckout serves the provider checkout creation endpoint
	// exposed through the optional billingmanagerCheckoutHandler routing
	// capability. Implementations answer OPTIONS preflight, validate the request,
	// delegate to the checkout service, and write the created session with 201 to
	// w.
	ProcessBillingProviderCheckout(w http.ResponseWriter, r *http.Request)
}

// billingmanagerCheckoutStatusHandler keeps legacy custom handlers compatible.
type billingmanagerCheckoutStatusHandler interface {
	// GetBillingProviderCheckoutStatus serves the checkout status endpoint exposed
	// through the optional billingmanagerCheckoutStatusHandler compatibility
	// capability. Implementations bind the actor from authenticated context,
	// validate provider and session parameters, and write the checkout status or an
	// error response to w.
	GetBillingProviderCheckoutStatus(http.ResponseWriter, *http.Request)
}

// billingmanagerPortalHandler is optional so legacy route handlers remain
// compatible when hosted customer-portal support is not implemented.
type billingmanagerPortalHandler interface {
	// ProcessBillingProviderPortal serves the hosted billing portal endpoint
	// exposed through the optional billingmanagerPortalHandler routing capability.
	// Implementations answer OPTIONS preflight, validate the request, delegate to
	// the portal service, and write the created portal session with 201 to w.
	ProcessBillingProviderPortal(w http.ResponseWriter, r *http.Request)
}

const (
	// APIBillingManagerV1Prefix base URI prefix for all billing manager v1 routes
	APIBillingManagerV1Prefix = "/api/v1/bms"
)

// AttachRoutesRequest holds everything needed to attach billingmanager
// routes to router
type AttachRoutesRequest struct {
	// Router main router being served by Api
	Router *router.Router

	// Handler valid billingmanager handler
	Handler billingmanagerHandler

	// MiddlewareAdminOnlyMiddleware is retained for source compatibility;
	// this attachment does not consume it.
	MiddlewareAdminOnlyMiddleware mux.MiddlewareFunc

	// MiddlewareActiveValidApiTokenOrJWTMiddleware is middleware that is used to lock
	// down endpoints to either tokens or JWT (active)
	MiddlewareActiveValidApiTokenOrJWTMiddleware mux.MiddlewareFunc
}

// AttachRoutes registers pricing, provider-proof and active-account endpoints.
// Handlers own resource authorization and webhook proof checks. Hosts must
// validate the registry before serving; a missing active adapter fails closed.
func AttachRoutes(request *AttachRoutesRequest) {

	billingmanagerPricingOpenRoutes := request.Router.NewRouteGroup(APIBillingManagerV1Prefix+"/pricing", router.Public, nil)
	billingmanagerPricingOpenRoutes.Handle(router.RouteDefinition{Path: "/plans", Operation: "billingmanager.GetPricingPlans", Methods: []string{http.MethodGet, http.MethodOptions}}, request.Handler.GetPricingPlans)
	billingmanagerPricingOpenRoutes.Handle(router.RouteDefinition{Path: "/plans/{slug}", Operation: "billingmanager.GetPricePlanBySlug", Methods: []string{http.MethodGet, http.MethodOptions}}, request.Handler.GetPricePlanBySlug)
	billingmanagerPricingOpenRoutes.Handle(router.RouteDefinition{Path: "/features", Operation: "billingmanager.GetPricingFeatures", Methods: []string{http.MethodGet, http.MethodOptions}}, request.Handler.GetPricingFeatures)

	// The provider handler, not route metadata or a user session, must verify
	// the webhook signature before processing events.
	billingmanagerOpenRoutes := request.Router.NewRouteGroup(APIBillingManagerV1Prefix, router.Public, nil)
	billingmanagerOpenRoutes.Handle(router.RouteDefinition{Path: "/billings/{providerName}/webhooks", Operation: "billingmanager.ProcessBillingProviderWebhooks", Methods: []string{http.MethodPost, http.MethodOptions}, Access: router.HandlerVerified, Policy: router.RoutePolicy{Proof: "provider-webhook-signature"}}, request.Handler.ProcessBillingProviderWebhooks)

	billingmanagerActiveOnlyRoutes := request.Router.NewRouteGroup(APIBillingManagerV1Prefix, router.ActiveSessionOrAPI, request.MiddlewareActiveValidApiTokenOrJWTMiddleware)
	if checkoutHandler, ok := request.Handler.(billingmanagerCheckoutHandler); ok {
		billingmanagerActiveOnlyRoutes.Handle(router.RouteDefinition{Path: "/billings/{providerName}/checkout", Operation: "billingmanager.ProcessBillingProviderCheckout", Methods: []string{http.MethodPost, http.MethodOptions}}, checkoutHandler.ProcessBillingProviderCheckout)
	}
	if statusHandler, ok := request.Handler.(billingmanagerCheckoutStatusHandler); ok {
		billingmanagerActiveOnlyRoutes.Handle(router.RouteDefinition{Path: "/billings/{providerName}/checkout/status", Operation: "billingmanager.GetBillingProviderCheckoutStatus", Methods: []string{http.MethodGet, http.MethodOptions}}, statusHandler.GetBillingProviderCheckoutStatus)
	}
	if portalHandler, ok := request.Handler.(billingmanagerPortalHandler); ok {
		billingmanagerActiveOnlyRoutes.Handle(router.RouteDefinition{Path: "/billings/{providerName}/portal", Operation: "billingmanager.ProcessBillingProviderPortal", Methods: []string{http.MethodPost, http.MethodOptions}}, portalHandler.ProcessBillingProviderPortal)
	}
	billingmanagerActiveOnlyRoutes.Handle(router.RouteDefinition{Path: "/billings/users/{userId}/events", Operation: "billingmanager.GetUserBillingEvents", Methods: []string{http.MethodGet, http.MethodOptions}}, request.Handler.GetUserBillingEvents)
	billingmanagerActiveOnlyRoutes.Handle(router.RouteDefinition{Path: "/users/{userId}/details/subscription", Operation: "billingmanager.GetUserSubscriptionStatus", Methods: []string{http.MethodGet, http.MethodOptions}}, request.Handler.GetUserSubscriptionStatus)
	billingmanagerActiveOnlyRoutes.Handle(router.RouteDefinition{Path: "/users/{userId}/details/billing", Operation: "billingmanager.GetUserBillingDetail", Methods: []string{http.MethodGet, http.MethodOptions}}, request.Handler.GetUserBillingDetail)

}
