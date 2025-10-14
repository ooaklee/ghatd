package billingmanager

import (
	"net/http"

	"github.com/gorilla/mux"
	"github.com/ooaklee/ghatd/external/router"
)

// billingmanagerHandler expected methods for valid billingmanager handler
type billingmanagerHandler interface {
	ProcessBillingProviderWebhooks(w http.ResponseWriter, r *http.Request)
	GetUserBillingEvents(w http.ResponseWriter, r *http.Request)
	GetUserSubscriptionStatus(w http.ResponseWriter, r *http.Request)
	GetUserBillingDetail(w http.ResponseWriter, r *http.Request)
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

	// MiddlewareAdminOnlyMiddleware middleware used to lock endpoints down to admin only
	MiddlewareAdminOnlyMiddleware mux.MiddlewareFunc

	// MiddlewareActiveValidApiTokenOrJWTMiddleware is middleware that is used to lock
	// down endpoints to either tokens or JWT (active)
	MiddlewareActiveValidApiTokenOrJWTMiddleware mux.MiddlewareFunc
}

// AttachRoutes attaches billingmanager handler to corresponding
// routes on router
func AttachRoutes(request *AttachRoutesRequest) {
	httpRouter := request.Router.GetRouter()

	billingmanagerOpenRoutes := httpRouter.PathPrefix(APIBillingManagerV1Prefix).Subrouter()
	billingmanagerOpenRoutes.HandleFunc("/billings/{providerName}/webhooks", request.Handler.ProcessBillingProviderWebhooks).Methods(http.MethodPost, http.MethodOptions)

	billingmanagerActiveOnlyRoutes := httpRouter.PathPrefix(APIBillingManagerV1Prefix).Subrouter()
	billingmanagerActiveOnlyRoutes.HandleFunc("/billings/users/{userId}/events", request.Handler.GetUserBillingEvents).Methods(http.MethodGet, http.MethodOptions)
	billingmanagerActiveOnlyRoutes.HandleFunc("/users/{userId}/details/subscription", request.Handler.GetUserSubscriptionStatus).Methods(http.MethodGet, http.MethodOptions)
	billingmanagerActiveOnlyRoutes.HandleFunc("/users/{userId}/details/billing", request.Handler.GetUserBillingDetail).Methods(http.MethodGet, http.MethodOptions)
	billingmanagerActiveOnlyRoutes.Use(request.MiddlewareActiveValidApiTokenOrJWTMiddleware)

	billingmanagerAdminRoutes := httpRouter.PathPrefix(APIBillingManagerV1Prefix + "/admin").Subrouter()
	billingmanagerAdminRoutes.HandleFunc("/billings/users/{userId}/events", request.Handler.GetUserBillingEvents).Methods(http.MethodGet, http.MethodOptions)
	billingmanagerAdminRoutes.HandleFunc("/users/{userId}/details/subscription", request.Handler.GetUserSubscriptionStatus).Methods(http.MethodGet, http.MethodOptions)
	billingmanagerAdminRoutes.HandleFunc("/users/{userId}/details/billing", request.Handler.GetUserBillingDetail).Methods(http.MethodGet, http.MethodOptions)
	billingmanagerAdminRoutes.Use(request.MiddlewareAdminOnlyMiddleware)

}
