package router_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"
	"github.com/ooaklee/ghatd/external/router"
	"github.com/stretchr/testify/require"
)

func TestPolicyRegistrationValidation(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name        string
		prefix      string
		access      router.AccessMode
		middleware  bool
		authorizer  bool
		definition  router.RouteDefinition
		nilHandler  bool
		wantInvalid bool
	}{
		{name: "public", access: router.Public},
		{name: "protected", access: router.ActiveSession, middleware: true},
		{name: "protected missing middleware", access: router.Session, wantInvalid: true},
		{name: "rate limit missing middleware", access: router.PublicRateLimited, wantInvalid: true},
		{name: "optional missing middleware", access: router.OptionalActive, wantInvalid: true},
		{name: "unknown access", access: "sessoin", middleware: true, wantInvalid: true},
		{name: "prefix without slash", prefix: "api", access: router.Public, wantInvalid: true},
		{name: "prefix trailing slash", prefix: "/api/", access: router.Public, wantInvalid: true},
		{name: "prefix query", prefix: "/api?x=y", access: router.Public, wantInvalid: true},
		{name: "nil handler", access: router.Public, nilHandler: true, wantInvalid: true},
		{name: "proof boundary", access: router.Public, definition: router.RouteDefinition{Access: router.HandlerVerified, Policy: router.RoutePolicy{Proof: "signed-webhook"}}},
		{name: "proof missing", access: router.Public, definition: router.RouteDefinition{Access: router.HandlerVerified}, wantInvalid: true},
		{name: "protected downgrade", access: router.Session, middleware: true, definition: router.RouteDefinition{Access: router.Public}, wantInvalid: true},
		{name: "requirements need evaluator", access: router.Session, middleware: true, definition: router.RouteDefinition{Policy: router.RoutePolicy{Scopes: []string{"read"}}}, wantInvalid: true},
		{name: "requirements with evaluator", access: router.Session, middleware: true, authorizer: true, definition: router.RouteDefinition{Policy: router.RoutePolicy{Scopes: []string{"read"}}}},
		{name: "public requirements cannot precede proof", access: router.Public, authorizer: true, definition: router.RouteDefinition{Policy: router.RoutePolicy{Scopes: []string{"read"}}}, wantInvalid: true},
		{name: "handler requirements cannot precede proof", access: router.Public, authorizer: true, definition: router.RouteDefinition{Access: router.HandlerVerified, Policy: router.RoutePolicy{Proof: "signed-webhook", Permissions: []string{"write"}}}, wantInvalid: true},
		{name: "rate limited is not authenticated", access: router.PublicRateLimited, middleware: true, authorizer: true, definition: router.RouteDefinition{Policy: router.RoutePolicy{UsageMetric: "requests"}}, wantInvalid: true},
		{name: "optional can require authentication additively", access: router.OptionalActive, middleware: true, authorizer: true, definition: router.RouteDefinition{Policy: router.RoutePolicy{Scopes: []string{"read"}}}},
		{name: "usage needs evaluator", access: router.Session, middleware: true, definition: router.RouteDefinition{Policy: router.RoutePolicy{UsageMetric: "requests"}}, wantInvalid: true},
		{name: "empty scope", access: router.Public, authorizer: true, definition: router.RouteDefinition{Policy: router.RoutePolicy{Scopes: []string{""}}}, wantInvalid: true},
		{name: "wildcard permission", access: router.Public, authorizer: true, definition: router.RouteDefinition{Policy: router.RoutePolicy{Permissions: []string{"*"}}}, wantInvalid: true},
		{name: "duplicate type", access: router.Public, authorizer: true, definition: router.RouteDefinition{Policy: router.RoutePolicy{UserTypes: []string{"web", "web"}}}, wantInvalid: true},
		{name: "operation whitespace", access: router.Public, definition: router.RouteDefinition{Operation: "read items"}, wantInvalid: true},
		{name: "unknown method", access: router.Public, definition: router.RouteDefinition{Methods: []string{"READ"}}, wantInvalid: true},
		{name: "duplicate method", access: router.Public, definition: router.RouteDefinition{Methods: []string{"GET", "GET"}}, wantInvalid: true},
		{name: "malformed mux pattern", access: router.Public, definition: router.RouteDefinition{Path: "/{id"}, wantInvalid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := router.NewRouter(nil, nil)
			var mw mux.MiddlewareFunc
			if tc.middleware {
				mw = func(next http.Handler) http.Handler { return next }
			}
			if tc.authorizer {
				require.NoError(t, r.SetRouteAuthorizer(func(context.Context, *http.Request, router.RouteDefinition) error { return nil }))
			}
			prefix := tc.prefix
			if prefix == "" {
				prefix = "/api"
			}
			def := tc.definition
			if def.Operation == "" {
				def.Operation = "items.read"
			}
			if def.Path == "" {
				def.Path = "/items"
			}
			if def.Methods == nil {
				def.Methods = []string{http.MethodGet}
			}
			handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) })
			if tc.nilHandler {
				handler = nil
			}
			r.NewRouteGroup(prefix, tc.access, mw).Handle(def, handler)
			if tc.wantInvalid {
				require.ErrorIs(t, r.ValidateRoutePolicies(), router.ErrRouteConfiguration)
			} else {
				require.NoError(t, r.ValidateRoutePolicies())
			}
		})
	}
}

func TestPolicyRuntimeAndReplyErrors(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name               string
		denyAuthentication bool
		missingMiddleware  bool
		authorizationError error
		wantStatus         int
		wantCode           string
		wantEvents         []string
	}{
		{name: "allowed order", wantStatus: 204, wantEvents: []string{"authenticate", "authorize", "handler"}},
		{name: "authentication stops policy", denyAuthentication: true, wantStatus: 401, wantEvents: []string{"authenticate"}},
		{name: "missing middleware fails closed", missingMiddleware: true, wantStatus: 503, wantCode: "ROUTE_CONFIGURATION"},
		{name: "unknown error is unavailable", authorizationError: errors.New("secret diagnostic"), wantStatus: 503, wantCode: "ROUTE_UNAVAILABLE", wantEvents: []string{"authenticate", "authorize"}},
		{name: "wrapped denial", authorizationError: fmt.Errorf("secret diagnostic: %w", router.ErrRouteDenied), wantStatus: 403, wantCode: "ROUTE_DENIED", wantEvents: []string{"authenticate", "authorize"}},
		{name: "configuration error", authorizationError: router.ErrRouteConfiguration, wantStatus: 503, wantCode: "ROUTE_CONFIGURATION", wantEvents: []string{"authenticate", "authorize"}},
		{name: "dependency unavailable", authorizationError: router.ErrRouteUnavailable, wantStatus: 503, wantCode: "ROUTE_UNAVAILABLE", wantEvents: []string{"authenticate", "authorize"}},
		{name: "verified identity absent", authorizationError: router.ErrRouteUnauthenticated, wantStatus: 401, wantCode: "ROUTE_AUTHENTICATION", wantEvents: []string{"authenticate", "authorize"}},
		{name: "revision missing", authorizationError: router.ErrRoutePrecondition, wantStatus: 428, wantCode: "ROUTE_REVISION_REQUIRED", wantEvents: []string{"authenticate", "authorize"}},
		{name: "revision malformed", authorizationError: router.ErrRouteInvalidRevision, wantStatus: 400, wantCode: "ROUTE_REVISION_INVALID", wantEvents: []string{"authenticate", "authorize"}},
		{name: "quota exceeded", authorizationError: router.ErrRouteLimitReached, wantStatus: 429, wantCode: "ROUTE_LIMIT_REACHED", wantEvents: []string{"authenticate", "authorize"}},
		{name: "denial and private failure", authorizationError: errors.Join(router.ErrRouteDenied, errors.New("secret diagnostic")), wantStatus: 503, wantCode: "ROUTE_UNAVAILABLE", wantEvents: []string{"authenticate", "authorize"}},
		{name: "authentication and private failure", authorizationError: errors.Join(router.ErrRouteUnauthenticated, errors.New("secret diagnostic")), wantStatus: 503, wantCode: "ROUTE_UNAVAILABLE", wantEvents: []string{"authenticate", "authorize"}},
		{name: "known joined failures", authorizationError: errors.Join(router.ErrRouteDenied, router.ErrRouteLimitReached), wantStatus: 503, wantCode: "ROUTE_UNAVAILABLE", wantEvents: []string{"authenticate", "authorize"}},
		{name: "singleton join remains ambiguous", authorizationError: errors.Join(router.ErrRouteDenied), wantStatus: 503, wantCode: "ROUTE_UNAVAILABLE", wantEvents: []string{"authenticate", "authorize"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var events []string
			r := router.NewRouter(nil, nil)
			require.NoError(t, r.SetRouteAuthorizer(func(ctx context.Context, request *http.Request, d router.RouteDefinition) error {
				events = append(events, "authorize")
				require.Equal(t, "/api/items", d.Path)
				require.Equal(t, "read", mux.Vars(request)["id"])
				return tc.authorizationError
			}))
			var mw mux.MiddlewareFunc = func(next http.Handler) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					events = append(events, "authenticate")
					if tc.denyAuthentication {
						w.WriteHeader(401)
						return
					}
					next.ServeHTTP(w, mux.SetURLVars(r, map[string]string{"id": "read"}))
				})
			}
			if tc.missingMiddleware {
				mw = nil
			}
			r.NewRouteGroup("/api", router.Session, mw).Handle(router.RouteDefinition{Path: "/items", Operation: "items.read", Methods: []string{"GET"}}, func(w http.ResponseWriter, r *http.Request) { events = append(events, "handler"); w.WriteHeader(204) })
			rec := httptest.NewRecorder()
			r.GetRouter().ServeHTTP(rec, httptest.NewRequest("GET", "/api/items", nil))
			require.Equal(t, tc.wantStatus, rec.Code)
			require.Equal(t, tc.wantEvents, events)
			if tc.wantCode != "" {
				var response struct {
					Errors []struct {
						Code string `json:"code"`
					} `json:"errors"`
				}
				require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
				require.Len(t, response.Errors, 1)
				require.Equal(t, tc.wantCode, response.Errors[0].Code)
				require.NotContains(t, rec.Body.String(), "secret diagnostic")
				require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
				require.Equal(t, "nosniff", rec.Header().Get("X-Content-Type-Options"))
			}
		})
	}
}

func TestPolicyMethodsAndShadowing(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name          string
		secondMethod  string
		requestMethod string
		wantInvalid   bool
		wantStatus    int
		wantHandler   string
	}{
		{"explicit get", "POST", "GET", false, 200, "first"},
		{"explicit post", "POST", "POST", false, 200, "second"},
		{"options keeps first match", "POST", "OPTIONS", false, 200, "first"},
		{"head not implicit", "POST", "HEAD", false, 405, ""},
		{"duplicate closes earlier shadow", "GET", "GET", true, 503, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := router.NewRouter(nil, nil)
			g := r.NewRouteGroup("/api", router.Public, nil)
			called := ""
			g.Handle(router.RouteDefinition{Path: "/items", Operation: "items.read", Methods: []string{"GET", "OPTIONS"}}, func(w http.ResponseWriter, r *http.Request) { called = "first" })
			g.Handle(router.RouteDefinition{Path: "/items", Operation: "items.write", Methods: []string{tc.secondMethod, "OPTIONS"}}, func(w http.ResponseWriter, r *http.Request) { called = "second" })
			if tc.wantInvalid {
				require.ErrorIs(t, r.ValidateRoutePolicies(), router.ErrRouteConfiguration)
			} else {
				require.NoError(t, r.ValidateRoutePolicies())
			}
			rec := httptest.NewRecorder()
			r.GetRouter().ServeHTTP(rec, httptest.NewRequest(tc.requestMethod, "/api/items", nil))
			require.Equal(t, tc.wantStatus, rec.Code)
			require.Equal(t, tc.wantHandler, called)
		})
	}
}

func TestPolicyMetadataSnapshots(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name            string
		mutateInput     bool
		mutateInventory bool
		mutateEvaluator bool
	}{
		{"caller mutation", true, false, false}, {"inventory mutation", false, true, false}, {"evaluator mutation", false, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := router.NewRouter(nil, nil)
			def := router.RouteDefinition{Path: "/items", Operation: "items.read", Methods: []string{"GET"}, Policy: router.RoutePolicy{Scopes: []string{"read"}, Permissions: []string{"view"}, UserTypes: []string{"web"}}}
			calls := 0
			require.NoError(t, r.SetRouteAuthorizer(func(_ context.Context, _ *http.Request, d router.RouteDefinition) error {
				calls++
				require.Equal(t, []string{"read"}, d.Policy.Scopes)
				require.Equal(t, []string{"view"}, d.Policy.Permissions)
				require.Equal(t, []string{"web"}, d.Policy.UserTypes)
				if tc.mutateEvaluator {
					d.Policy.Scopes[0] = "write"
					d.Policy.Permissions[0] = "delete"
					d.Policy.UserTypes[0] = "api"
				}
				return nil
			}))
			r.NewRouteGroup("/api", router.Session, func(next http.Handler) http.Handler { return next }).Handle(def, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })
			if tc.mutateInput {
				def.Methods[0] = "DELETE"
				def.Policy.Scopes[0] = "write"
				def.Policy.Permissions[0] = "delete"
				def.Policy.UserTypes[0] = "api"
			}
			if tc.mutateInventory {
				snapshot := r.RouteInventory()
				snapshot[0].Path = "/elsewhere"
				snapshot[0].Methods[0] = "DELETE"
				snapshot[0].Policy.Scopes[0] = "write"
				snapshot[0].Policy.Permissions[0] = "delete"
				snapshot[0].Policy.UserTypes[0] = "api"
			}
			for i := 0; i < 2; i++ {
				rec := httptest.NewRecorder()
				r.GetRouter().ServeHTTP(rec, httptest.NewRequest("GET", "/api/items", nil))
				require.Equal(t, 204, rec.Code)
			}
			require.Equal(t, 2, calls)
			require.Equal(t, "/api/items", r.RouteInventory()[0].Path)
		})
	}
}

func TestRouteAuthorizerFreezesAtGroupCreation(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name          string
		createGroup   bool
		registerRoute bool
		wantError     bool
	}{
		{"before configuration", false, false, false}, {"after empty group", true, false, true}, {"after route", true, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := router.NewRouter(nil, nil)
			if tc.createGroup {
				g := r.NewRouteGroup("/api", router.Public, nil)
				if tc.registerRoute {
					g.Handle(router.RouteDefinition{Path: "/items", Operation: "items.read", Methods: []string{"GET"}}, func(http.ResponseWriter, *http.Request) {})
				}
			}
			err := r.SetRouteAuthorizer(nil)
			if tc.wantError {
				require.ErrorIs(t, err, router.ErrRouteConfiguration)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
