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

// AttachRoutes attaches blueprint handler to corresponding
// routes on router
func AttachRoutes(request *AttachRoutesRequest) {
	httpRouter := request.Router.GetRouter()

	blueprintAdminRoutes := httpRouter.PathPrefix(ApiBlueprintPrefix).Subrouter()
	blueprintAdminRoutes.HandleFunc("", request.Handler.CreateBlueprint).Methods(http.MethodPost, http.MethodOptions)

	if request.AdminOnlyMiddleware != nil {
		blueprintAdminRoutes.Use(request.AdminOnlyMiddleware)
	}

	blueprintAuthenticatedRoutes := httpRouter.PathPrefix(ApiBlueprintPrefix).Subrouter()
	blueprintAuthenticatedRoutes.HandleFunc("", request.Handler.GetBlueprints).Methods(http.MethodGet, http.MethodOptions)
	blueprintAuthenticatedRoutes.HandleFunc("/{blueprintId}", request.Handler.GetBlueprintByID).Methods(http.MethodGet, http.MethodOptions)

	if request.AuthenticatedMiddleware != nil {
		blueprintAuthenticatedRoutes.Use(request.AuthenticatedMiddleware)
	}
}
