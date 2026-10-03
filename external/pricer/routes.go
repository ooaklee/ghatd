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

	// AdminOnlyMiddleware verifies an administrator session for every endpoint,
	// including reads. A missing adapter invalidates the registry.
	AdminOnlyMiddleware mux.MiddlewareFunc
}

// AttachRoutes registers pricing administration, preserving UUID-before-slug
// and OPTIONS matching order. Validate the registry before serving.
func AttachRoutes(request *AttachRoutesRequest) {

	groupsAdminOnlyRoutes := request.Router.NewRouteGroup(APIPricesV1Prefix, router.AdminSession, request.AdminOnlyMiddleware)
	groupsAdminOnlyRoutes.Handle(router.RouteDefinition{Path: "/plans/{id}/publish", Operation: "pricer.PublishPricePlan", Methods: []string{http.MethodPost, http.MethodOptions}}, request.Handler.PublishPricePlan)
	groupsAdminOnlyRoutes.Handle(router.RouteDefinition{Path: "/plans/{id}/archive", Operation: "pricer.ArchivePricePlan", Methods: []string{http.MethodPost, http.MethodOptions}}, request.Handler.ArchivePricePlan)
	groupsAdminOnlyRoutes.Handle(router.RouteDefinition{Path: "/plans/{id:[0-9a-fA-F-]{36}}", Operation: "pricer.GetPricePlanByID", Methods: []string{http.MethodGet, http.MethodOptions}}, request.Handler.GetPricePlanByID)
	groupsAdminOnlyRoutes.Handle(router.RouteDefinition{Path: "/plans/{slug:[A-Za-z0-9][A-Za-z0-9_-]*}", Operation: "pricer.GetPricePlanBySlug", Methods: []string{http.MethodGet, http.MethodOptions}}, request.Handler.GetPricePlanBySlug)
	groupsAdminOnlyRoutes.Handle(router.RouteDefinition{Path: "/plans/{id}", Operation: "pricer.UpdatePricePlan", Methods: []string{http.MethodPut, http.MethodOptions}}, request.Handler.UpdatePricePlan)
	groupsAdminOnlyRoutes.Handle(router.RouteDefinition{Path: "/plans/{id}", Operation: "pricer.DeletePricePlan", Methods: []string{http.MethodDelete, http.MethodOptions}}, request.Handler.DeletePricePlan)
	groupsAdminOnlyRoutes.Handle(router.RouteDefinition{Path: "/plans", Operation: "pricer.GetPricePlans", Methods: []string{http.MethodGet, http.MethodOptions}}, request.Handler.GetPricePlans)
	groupsAdminOnlyRoutes.Handle(router.RouteDefinition{Path: "/plans", Operation: "pricer.CreatePricePlan", Methods: []string{http.MethodPost, http.MethodOptions}}, request.Handler.CreatePricePlan)
	groupsAdminOnlyRoutes.Handle(router.RouteDefinition{Path: "/validate-slug", Operation: "pricer.ValidatePriceSlug", Methods: []string{http.MethodGet, http.MethodOptions}}, request.Handler.ValidatePriceSlug)
	groupsAdminOnlyRoutes.Handle(router.RouteDefinition{Path: "/features/{id}", Operation: "pricer.UpdateFeature", Methods: []string{http.MethodPut, http.MethodOptions}}, request.Handler.UpdateFeature)
	groupsAdminOnlyRoutes.Handle(router.RouteDefinition{Path: "/features/{id}", Operation: "pricer.DeleteFeature", Methods: []string{http.MethodDelete, http.MethodOptions}}, request.Handler.DeleteFeature)
	groupsAdminOnlyRoutes.Handle(router.RouteDefinition{Path: "/features", Operation: "pricer.GetFeatures", Methods: []string{http.MethodGet, http.MethodOptions}}, request.Handler.GetFeatures)
	groupsAdminOnlyRoutes.Handle(router.RouteDefinition{Path: "/features", Operation: "pricer.CreateFeature", Methods: []string{http.MethodPost, http.MethodOptions}}, request.Handler.CreateFeature)

}
