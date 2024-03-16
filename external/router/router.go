package router

import (
	"net/http"

	"github.com/gorilla/mux"
)

// Router handles routing on lambda
type Router struct {
	httpRouter *mux.Router
}

// NewRouter creates a Router
func NewRouter(default404Handler func(w http.ResponseWriter, r *http.Request), defaultHealthcheckHandler func(w http.ResponseWriter, r *http.Request), mwf ...mux.MiddlewareFunc) *Router {
	httpRouter := mux.NewRouter()

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
