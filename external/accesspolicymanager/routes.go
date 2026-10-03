package accesspolicymanager

import (
	"net/http"

	"github.com/gorilla/mux"
	amiddleware "github.com/ooaklee/ghatd/external/accessmanager/middleware"
	"github.com/ooaklee/ghatd/external/accesspolicy"
	"github.com/ooaklee/ghatd/external/auth"
	"github.com/ooaklee/ghatd/external/router"
)

// BasePath is the framework's explicit policy-management namespace, independent
// of the product API and of the policy Subject.System configured in the manager.
const BasePath = "/api/v1/access-policies"

// AttachRoutes declares administrator/session-only endpoints. BearerSession must
// be the explicit-session adapter, not cookie-refresh or API-token middleware.
// Configure a RoutePolicyGuard before attachment; startup validation rejects an
// absent revision evaluator. The manager rechecks live authority independently.
func AttachRoutes(r *router.Router, h *Handler, bearerSession mux.MiddlewareFunc) error {
	if r == nil || h == nil || h.service == nil || bearerSession == nil {
		return accesspolicy.ErrConfiguration
	}
	// Do not rely on a host choosing the correct adapter: require the private
	// origin marker minted by the shared explicit-bearer middleware too.
	explicitSession := func(next http.Handler) http.Handler {
		return bearerSession(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
			w.Header().Set("Cache-Control", "no-store")
			if !amiddleware.IsExplicitBearerSession(request.Context()) {
				h.fail(w, auth.ErrUnauthorized)
				return
			}
			next.ServeHTTP(w, request)
		}))
	}
	group := r.NewRouteGroup(BasePath, router.AdminSession, explicitSession)
	group.Handle(router.RouteDefinition{Path: "/users/{userID}/token-limits/preview", Methods: []string{http.MethodPost}, Operation: "accesspolicymanager.PreviewTokenLimits"}, h.PreviewTokenLimits)
	group.Handle(router.RouteDefinition{Path: "/users/{userID}/token-limits", Methods: []string{http.MethodPut}, Operation: "accesspolicymanager.ApplyTokenLimits", Policy: router.RoutePolicy{RevisionRequired: true}}, h.ApplyTokenLimits)
	return r.ValidateRoutePolicies()
}
