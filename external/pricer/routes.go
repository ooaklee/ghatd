package pricer

import (
	"net/http"

	"github.com/gorilla/mux"
	"github.com/ooaklee/ghatd/external/router"
)

// PriceHandler interface defines expected methods for valid pricer handler.
type PriceHandler interface {
	CreatePricePlan(w http.ResponseWriter, r *http.Request)
	UpdatePricePlan(w http.ResponseWriter, r *http.Request)
	GetPricePlanByID(w http.ResponseWriter, r *http.Request)
	GetPricePlanBySlug(w http.ResponseWriter, r *http.Request)
	GetPricePlans(w http.ResponseWriter, r *http.Request)
	ValidatePriceSlug(w http.ResponseWriter, r *http.Request)
	PublishPricePlan(w http.ResponseWriter, r *http.Request)
	ArchivePricePlan(w http.ResponseWriter, r *http.Request)
	DeletePricePlan(w http.ResponseWriter, r *http.Request)
	CreateFeature(w http.ResponseWriter, r *http.Request)
	UpdateFeature(w http.ResponseWriter, r *http.Request)
	GetFeatures(w http.ResponseWriter, r *http.Request)
	DeleteFeature(w http.ResponseWriter, r *http.Request)
}

// APIPricesV1Prefix base URI prefix for all v1 price routes.
const APIPricesV1Prefix = "/api/v1/pricing"

// AttachRoutesRequest holds everything needed to attach pricer routes to router.
type AttachRoutesRequest struct {
	// Router main router being served by API.
	Router *router.Router

	// Handler valid pricer handler.
	Handler PriceHandler

	// AdminOnlyMiddleware middleware used to lock write endpoints down to admin only
	AdminOnlyMiddleware mux.MiddlewareFunc
}

// AttachRoutes attaches pricer handler to corresponding routes on router.
func AttachRoutes(request *AttachRoutesRequest) {
	httpRouter := request.Router.GetRouter()

	groupsAdminOnlyRoutes := httpRouter.PathPrefix(APIPricesV1Prefix).Subrouter()
	groupsAdminOnlyRoutes.HandleFunc("/plans/{id}/publish", request.Handler.PublishPricePlan).Methods(http.MethodPost, http.MethodOptions)
	groupsAdminOnlyRoutes.HandleFunc("/plans/{id}/archive", request.Handler.ArchivePricePlan).Methods(http.MethodPost, http.MethodOptions)
	groupsAdminOnlyRoutes.HandleFunc("/plans/{id:[0-9a-fA-F-]{36}}", request.Handler.GetPricePlanByID).Methods(http.MethodGet, http.MethodOptions)
	groupsAdminOnlyRoutes.HandleFunc("/plans/{slug:[A-Za-z0-9][A-Za-z0-9_-]*}", request.Handler.GetPricePlanBySlug).Methods(http.MethodGet, http.MethodOptions)
	groupsAdminOnlyRoutes.HandleFunc("/plans/{id}", request.Handler.UpdatePricePlan).Methods(http.MethodPut, http.MethodOptions)
	groupsAdminOnlyRoutes.HandleFunc("/plans/{id}", request.Handler.DeletePricePlan).Methods(http.MethodDelete, http.MethodOptions)
	groupsAdminOnlyRoutes.HandleFunc("/plans", request.Handler.GetPricePlans).Methods(http.MethodGet, http.MethodOptions)
	groupsAdminOnlyRoutes.HandleFunc("/plans", request.Handler.CreatePricePlan).Methods(http.MethodPost, http.MethodOptions)
	groupsAdminOnlyRoutes.HandleFunc("/validate-slug", request.Handler.ValidatePriceSlug).Methods(http.MethodGet, http.MethodOptions)
	groupsAdminOnlyRoutes.HandleFunc("/features/{id}", request.Handler.UpdateFeature).Methods(http.MethodPut, http.MethodOptions)
	groupsAdminOnlyRoutes.HandleFunc("/features/{id}", request.Handler.DeleteFeature).Methods(http.MethodDelete, http.MethodOptions)
	groupsAdminOnlyRoutes.HandleFunc("/features", request.Handler.GetFeatures).Methods(http.MethodGet, http.MethodOptions)
	groupsAdminOnlyRoutes.HandleFunc("/features", request.Handler.CreateFeature).Methods(http.MethodPost, http.MethodOptions)

	if request.AdminOnlyMiddleware != nil {
		groupsAdminOnlyRoutes.Use(request.AdminOnlyMiddleware)
	}

}
