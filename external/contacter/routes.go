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

	// AdminOnlyMiddleware must authenticate and authorize an administrator.
	// Missing middleware invalidates the registry instead of exposing statistics.
	AdminOnlyMiddleware mux.MiddlewareFunc
	// AdminAccess is AdminSession (also the empty default) or AdminSessionOrAPI.
	// It must match the enforcing middleware. Other modes invalidate registration;
	// declaring an access mode does not authenticate or authorize callers.
	AdminAccess router.AccessMode
}

// AttachRoutes registers public capability discovery and protected statistics.
// Hosts must check ValidateRoutePolicies after all route attachment and before
// serving; an invalid registry denies every descriptor-backed route.
func AttachRoutes(request *AttachRoutesRequest) {
	// Public capability discovery. This exposes labels and accepted values only;
	// comms records and statistics remain protected below.
	public := request.Router.NewRouteGroup("/api/v1/comms", router.Public, nil)
	public.Handle(router.RouteDefinition{Path: "/types", Operation: "contacter.GetAvailableCommsTypes", Methods: []string{http.MethodGet, http.MethodOptions}}, request.Handler.GetAvailableCommsTypes)

	// Admin-only routes for comms management
	access := request.AdminAccess
	switch access {
	case "":
		access = router.AdminSession
	case router.AdminSession, router.AdminSessionOrAPI:
	default:
		access = "" // Unknown mode triggers the registry's closed backstop.
	}
	admin := request.Router.NewRouteGroup("/api/v1/comms", access, request.AdminOnlyMiddleware)
	admin.Handle(router.RouteDefinition{Path: "/stats", Operation: "contacter.GetCommsStats", Methods: []string{http.MethodGet, http.MethodOptions}}, request.Handler.GetCommsStats)
}
