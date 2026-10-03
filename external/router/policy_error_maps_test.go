package router_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ooaklee/ghatd/external/router"
	"github.com/ooaklee/reply/v2"
	"github.com/stretchr/testify/require"
)

func TestNativeRouteErrorManifests(t *testing.T) {
	t.Parallel()
	native := errors.New("private domain classification")
	for _, tc := range []struct {
		name     string
		failure  error
		override bool
		status   int
		code     string
	}{
		{"direct native", native, false, 403, "DOMAIN-DENIED"},
		{"wrapped native", fmt.Errorf("private diagnostic: %w", native), false, 403, "DOMAIN-DENIED"},
		{"host override", native, true, 429, "HOST-DENIED"},
		{"unmapped outage", errors.New("private outage"), false, 503, "ROUTE_UNAVAILABLE"},
		{"joined outage", errors.Join(native, errors.New("private outage")), false, 503, "ROUTE_UNAVAILABLE"},
		{"joined mapped", errors.Join(native, router.ErrRouteDenied), false, 503, "ROUTE_UNAVAILABLE"},
		{"default route map retained", router.ErrRouteDenied, false, 403, "ROUTE_DENIED"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := router.NewRouter(nil, nil)
			manifest := reply.ErrorManifest{native: {StatusCode: 403, Code: "DOMAIN-DENIED"}}
			overrides := []reply.ErrorManifest{manifest}
			if tc.override {
				overrides = append(overrides, reply.ErrorManifest{native: {StatusCode: 429, Code: "HOST-DENIED"}})
			}
			require.NoError(t, r.ConfigureRoutePolicy(func(context.Context, *http.Request, router.RouteDefinition) error { return tc.failure }, func(router.RouteDefinition) error { return nil }, overrides...))
			for _, m := range overrides {
				m[native] = reply.ErrorManifest{native: {StatusCode: 418, Code: "MUTATED"}}[native]
			}
			called := false
			r.NewRouteGroup("/api", router.Session, func(next http.Handler) http.Handler { return next }).Handle(router.RouteDefinition{Path: "/items", Operation: "items.read", Methods: []string{"GET"}}, func(http.ResponseWriter, *http.Request) { called = true })
			require.NoError(t, r.ValidateRoutePolicies())
			require.ErrorIs(t, r.ConfigureRoutePolicy(func(context.Context, *http.Request, router.RouteDefinition) error { return nil }, func(router.RouteDefinition) error { return nil }), router.ErrRouteConfiguration)
			w := httptest.NewRecorder()
			r.GetRouter().ServeHTTP(w, httptest.NewRequest("GET", "/api/items", nil))
			require.Equal(t, tc.status, w.Code)
			require.Contains(t, w.Body.String(), tc.code)
			require.NotContains(t, w.Body.String(), "private")
			require.NotContains(t, w.Body.String(), "MUTATED")
			require.False(t, called)
		})
	}
}

func TestRouteErrorOverrideValidation(t *testing.T) {
	for _, code := range []int{0, 200, 302, 399, 400, 503, 599, 600} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			r := router.NewRouter(nil, nil)
			err := r.ConfigureRoutePolicy(func(context.Context, *http.Request, router.RouteDefinition) error { return router.ErrRouteDenied }, func(router.RouteDefinition) error { return nil }, reply.ErrorManifest{router.ErrRouteDenied: {StatusCode: code}})
			if code < 400 || code > 599 {
				require.ErrorIs(t, err, router.ErrRouteConfiguration)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestPolicyIdentifierUTF8(t *testing.T) {
	for _, identifier := range []string{"items.read", "items.读取", "items.\xff"} {
		t.Run(fmt.Sprintf("%q", identifier), func(t *testing.T) {
			r := router.NewRouter(nil, nil)
			r.NewRouteGroup("/api", router.Public, nil).Handle(router.RouteDefinition{Path: "/items", Operation: identifier, Methods: []string{"GET"}}, func(http.ResponseWriter, *http.Request) {})
			if identifier == "items.\xff" {
				require.Error(t, r.ValidateRoutePolicies())
			} else {
				require.NoError(t, r.ValidateRoutePolicies())
			}
		})
	}
}
