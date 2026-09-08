// Package routecontext shares matched route templates with middleware outside
// a router, without matching requests a second time or collecting raw paths.
package routecontext

import (
	"context"
	"net/http"

	"github.com/gorilla/mux"
)

type contextKey struct{}

type routeState struct{ template string }

// Begin returns a request carrying shared route state. Call it outside the
// router, before dispatch. Existing state is retained for nested middleware;
// an already-matched request initializes the state from its route template.
// The caller's request is not modified.
func Begin(request *http.Request) *http.Request {
	if _, ok := request.Context().Value(contextKey{}).(*routeState); ok {
		return request
	}
	state := &routeState{template: matchedTemplate(request)}
	return request.WithContext(context.WithValue(request.Context(), contextKey{}, state))
}

// ObserveMiddleware captures Mux's matched template before dispatching to the
// next handler. Install it before any Mux middleware that can short-circuit a
// request, such as caching or authorization. It never calls Router.Match.
func ObserveMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if state, ok := request.Context().Value(contextKey{}).(*routeState); ok {
			state.template = matchedTemplate(request)
		}
		next.ServeHTTP(writer, request)
	})
}

// Template returns the template captured during dispatch, or the route on an
// already-matched request. Unmatched requests return an empty string; their raw
// paths are never substituted. Shared state lets outer middleware call this
// after Mux has dispatched a cloned request containing the matched route.
func Template(request *http.Request) string {
	if request == nil {
		return ""
	}
	if state, ok := request.Context().Value(contextKey{}).(*routeState); ok {
		return state.template
	}
	return matchedTemplate(request)
}

func matchedTemplate(request *http.Request) string {
	if route := mux.CurrentRoute(request); route != nil {
		if template, err := route.GetPathTemplate(); err == nil {
			return template
		}
	}
	return request.Pattern
}
