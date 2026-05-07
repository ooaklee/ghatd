package contacter

import (
	"net/http"

	"github.com/gorilla/mux"
	"github.com/ooaklee/ghatd/external/router"
)

// AttachRoutesRequest holds everything needed to attach contacter routes to router
type AttachRoutesRequest struct {
	// Router main router being served by API
	Router *router.Router

	// Handler valid contacter handler
	Handler *Handler

	// AdminOnlyMiddleware middleware used to lock endpoints down to admin only
	AdminOnlyMiddleware mux.MiddlewareFunc
}

// AttachRoutes attaches contacter handler to corresponding routes on router
func AttachRoutes(request *AttachRoutesRequest) {
	httpRouter := request.Router.GetRouter()

	// Admin-only routes for comms management
	commsAdminOnlyRoutes := httpRouter.PathPrefix("/api/v1/ums/comms").Subrouter()
	commsAdminOnlyRoutes.HandleFunc("/stats", request.Handler.GetCommsStats).Methods(http.MethodGet, http.MethodOptions)
	if request.AdminOnlyMiddleware != nil {
		commsAdminOnlyRoutes.Use(request.AdminOnlyMiddleware)
	}
}
