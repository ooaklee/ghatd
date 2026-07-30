package vision

import (
	"net/http"

	"github.com/gorilla/mux"
	"github.com/ooaklee/ghatd/external/common"
	"github.com/ooaklee/ghatd/external/router"
)

// visionHandler expected methods for valid vision handler
type visionHandler interface {
	CreateVision(w http.ResponseWriter, r *http.Request)
	GetVisionByID(w http.ResponseWriter, r *http.Request)
	GetVisions(w http.ResponseWriter, r *http.Request)
}

const (
	// ApiVisionPrefix base URI prefix for all vision routes
	ApiVisionPrefix = common.ApiV1UriPrefix + "/visions"
)

// AttachRoutesRequest holds everything needed to attach vision
// routes to router
type AttachRoutesRequest struct {
	// Router main router being served by Api
	Router *router.Router

	// Handler valid vision handler
	Handler visionHandler

	// AdminOnlyMiddleware middleware used to lock management endpoints down to admin only.
	AdminOnlyMiddleware mux.MiddlewareFunc

	// AuthenticatedMiddleware middleware used for authenticated user endpoints.
	AuthenticatedMiddleware mux.MiddlewareFunc
}

// AttachRoutes attaches vision handler to corresponding
// routes on router
func AttachRoutes(request *AttachRoutesRequest) {
	httpRouter := request.Router.GetRouter()

	visionAdminRoutes := httpRouter.PathPrefix(ApiVisionPrefix).Subrouter()
	visionAdminRoutes.HandleFunc("", request.Handler.CreateVision).Methods(http.MethodPost, http.MethodOptions)

	if request.AdminOnlyMiddleware != nil {
		visionAdminRoutes.Use(request.AdminOnlyMiddleware)
	}

	visionAuthenticatedRoutes := httpRouter.PathPrefix(ApiVisionPrefix).Subrouter()
	visionAuthenticatedRoutes.HandleFunc("", request.Handler.GetVisions).Methods(http.MethodGet, http.MethodOptions)
	visionAuthenticatedRoutes.HandleFunc("/{visionID}", request.Handler.GetVisionByID).Methods(http.MethodGet, http.MethodOptions)

	if request.AuthenticatedMiddleware != nil {
		visionAuthenticatedRoutes.Use(request.AuthenticatedMiddleware)
	}
}
