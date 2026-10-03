package blueprint

import (
	"net/http"

	"github.com/gorilla/mux"
	"github.com/ooaklee/ghatd/external/common"
	"github.com/ooaklee/ghatd/external/router"
)

// blueprintHandler expected methods for valid blueprint handler
type blueprintHandler interface {
	CreateBlueprint(w http.ResponseWriter, r *http.Request)
	GetBlueprintByID(w http.ResponseWriter, r *http.Request)
	GetBlueprints(w http.ResponseWriter, r *http.Request)
}

const (
	// ApiBlueprintPrefix base URI prefix for all blueprint routes
	ApiBlueprintPrefix = common.ApiV1UriPrefix + "/blueprints"
)

// AttachRoutesRequest holds everything needed to attach blueprint
// routes to router
type AttachRoutesRequest struct {
	// Router main router being served by Api
	Router *router.Router

	// Handler valid blueprint handler
	Handler blueprintHandler

	// AdminOnlyMiddleware middleware used to lock management endpoints down to admin only.
	AdminOnlyMiddleware mux.MiddlewareFunc

	// AuthenticatedMiddleware middleware used for authenticated user endpoints.
	AuthenticatedMiddleware mux.MiddlewareFunc
}

// AttachRoutes registers administrator writes and authenticated-session reads.
// Both adapters are required; validate the registry before serving. Domain
// ownership checks remain the handler/service's responsibility.
func AttachRoutes(request *AttachRoutesRequest) {

	blueprintAdminRoutes := request.Router.NewRouteGroup(ApiBlueprintPrefix, router.AdminSession, request.AdminOnlyMiddleware)
	blueprintAdminRoutes.Handle(router.RouteDefinition{Path: "", Operation: "blueprint.CreateBlueprint", Methods: []string{http.MethodPost, http.MethodOptions}}, request.Handler.CreateBlueprint)

	blueprintAuthenticatedRoutes := request.Router.NewRouteGroup(ApiBlueprintPrefix, router.Session, request.AuthenticatedMiddleware)
	blueprintAuthenticatedRoutes.Handle(router.RouteDefinition{Path: "", Operation: "blueprint.GetBlueprints", Methods: []string{http.MethodGet, http.MethodOptions}}, request.Handler.GetBlueprints)
	blueprintAuthenticatedRoutes.Handle(router.RouteDefinition{Path: "/{blueprintId}", Operation: "blueprint.GetBlueprintByID", Methods: []string{http.MethodGet, http.MethodOptions}}, request.Handler.GetBlueprintByID)

}
