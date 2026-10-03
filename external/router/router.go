package router

import (
	"net/http"

	"github.com/gorilla/mux"
	"github.com/ooaklee/ghatd/external/router/routecontext"
)

// Router composes Mux routing, default handlers and opt-in route policy metadata.
// Configure it before serving; registrations must not race with requests.
type Router struct {
	// httpRouter retains the underlying Mux dispatch and middleware order.
	httpRouter *mux.Router
	// policyRoutes and policyErrors are assembled before serving, like Mux routes.
	policyRoutes []RouteDefinition
	policyErrors []error
	// policyMatchers supports duplicate-equivalence and earlier-pattern checks.
	policyMatchers []policyMatcher
	// authorizer is fixed before route registration, never swapped during requests.
	authorizer RouteAuthorizer
	// policyValidator checks adapter-owned names/dependencies during registration.
	policyValidator func(RouteDefinition) error
	// policyStarted freezes evaluator configuration when the first group is created.
	policyStarted bool
}

// NewRouter creates a Mux-backed router with optional fallback and health handlers.
// Route observation precedes supplied middleware. Policy adoption is explicit.
func NewRouter(default404Handler func(w http.ResponseWriter, r *http.Request), defaultHealthcheckHandler func(w http.ResponseWriter, r *http.Request), mwf ...mux.MiddlewareFunc) *Router {
	httpRouter := mux.NewRouter()
	// Capture route templates before a supplied middleware can respond without
	// invoking the route handler. Outer observers can then include cache hits
	// and authorization rejections without matching the request again.
	httpRouter.Use(routecontext.ObserveMiddleware)

	if len(mwf) > 0 {
		httpRouter.Use(mwf...)
	}

	if default404Handler != nil {
		httpRouter.NotFoundHandler = http.HandlerFunc(default404Handler)
	}

	if defaultHealthcheckHandler != nil {
		httpRouter.HandleFunc(healthCheckEndpoint, defaultHealthcheckHandler)
	}

	return &Router{
		httpRouter: httpRouter,
	}
}

// GetRouter returns http router
func (r *Router) GetRouter() *mux.Router {
	return r.httpRouter
}
