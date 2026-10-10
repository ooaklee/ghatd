package pricer

import (
	"net/http"

	"github.com/gorilla/mux"
	"github.com/ooaklee/ghatd/external/router"
)

// PriceHandler interface defines expected methods for valid pricer handler.
type PriceHandler interface {
	// CreatePricePlan serves HTTP price plan creation for PriceHandler: it maps and
	// validates the request, delegates to the service, and writes the created plan
	// with status 201 or an error response.
	CreatePricePlan(w http.ResponseWriter, r *http.Request)
	// UpdatePricePlan serves HTTP price plan updates for PriceHandler: it maps and
	// validates the request, delegates to the service, and writes the updated plan
	// or an error response.
	UpdatePricePlan(w http.ResponseWriter, r *http.Request)
	// GetPricePlanByID serves HTTP retrieval of a price plan by ID for
	// PriceHandler: it maps the request, delegates to the service, and writes the
	// plan or an error response.
	GetPricePlanByID(w http.ResponseWriter, r *http.Request)
	// GetPricePlanBySlug serves HTTP retrieval of a price plan by slug for
	// PriceHandler: it maps the request, delegates to the service, and writes the
	// plan or an error response.
	GetPricePlanBySlug(w http.ResponseWriter, r *http.Request)
	// GetPricePlans serves HTTP listing of price plans for PriceHandler: it maps
	// the request, delegates to the service, and writes the plans, including
	// pagination metadata when requested.
	GetPricePlans(w http.ResponseWriter, r *http.Request)
	// ValidatePriceSlug serves HTTP pricing slug validation for PriceHandler: it
	// maps the request, delegates to the service, and writes the normalised slug
	// with availability information without persisting anything.
	ValidatePriceSlug(w http.ResponseWriter, r *http.Request)
	// PublishPricePlan serves HTTP price plan publishing for PriceHandler: it maps
	// the request, delegates to the service, and writes the published plan or an
	// error response.
	PublishPricePlan(w http.ResponseWriter, r *http.Request)
	// ArchivePricePlan serves HTTP price plan archiving for PriceHandler: it maps
	// the request, delegates to the service, and writes the archived plan or an
	// error response.
	ArchivePricePlan(w http.ResponseWriter, r *http.Request)
	// DeletePricePlan serves HTTP soft deletion of a price plan for PriceHandler:
	// it maps the request, delegates to the service, and writes the deleted plan or
	// an error response.
	DeletePricePlan(w http.ResponseWriter, r *http.Request)
	// CreateFeature serves HTTP feature creation for PriceHandler: it maps and
	// validates the request, delegates to the service, and writes the created
	// feature with status 201 or an error response.
	CreateFeature(w http.ResponseWriter, r *http.Request)
	// UpdateFeature serves HTTP feature updates for PriceHandler: it maps and
	// validates the request, delegates to the service, and writes the updated
	// feature or an error response.
	UpdateFeature(w http.ResponseWriter, r *http.Request)
	// GetFeatures serves HTTP listing of feature catalog items for PriceHandler: it
	// maps the request, delegates to the service, and writes the features,
	// including pagination metadata when requested.
	GetFeatures(w http.ResponseWriter, r *http.Request)
	// DeleteFeature serves HTTP soft deletion of a feature catalog item for
	// PriceHandler: it maps the request, delegates to the service, and writes the
	// deleted feature or an error response.
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
