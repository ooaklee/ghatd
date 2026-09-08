package router

import (
	"net/http"

	"github.com/gorilla/mux"
	"github.com/ooaklee/ghatd/external/router/routecontext"
)

// Router handles routing on lambda
type Router struct {
	httpRouter *mux.Router
}

// NewRouter creates a Router
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
