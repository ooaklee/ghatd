package router_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/ooaklee/ghatd/external/router"
	"github.com/stretchr/testify/require"
)

func TestCompiledPolicyShadows(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, first, second, secondMethod string
		invalid                           bool
		shadowed                          []string
	}{
		{"renamed parameter", "/items/{id}", "/items/{item}", "GET", true, []string{"GET", "OPTIONS"}},
		{"wildcard before literal", "/items/{id}", "/items/current", "GET", true, []string{"GET", "OPTIONS"}},
		{"regex before covered literal", "/items/{id:[0-9]+}", "/items/123", "GET", true, []string{"GET", "OPTIONS"}},
		{"regex excludes literal", "/items/{id:[0-9]+}", "/items/current", "GET", false, nil},
		{"literal before parameter", "/items/current", "/items/{id}", "GET", false, nil},
		{"different methods shared OPTIONS", "/items/{id}", "/items/current", "POST", false, []string{"OPTIONS"}},
		{"empty path means group prefix", "", "/items", "GET", false, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := router.NewRouter(nil, nil)
			g := r.NewRouteGroup("/api", router.Public, nil)
			g.Handle(router.RouteDefinition{Path: tc.first, Methods: []string{"GET", "OPTIONS"}, Operation: "first"}, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) })
			g.Handle(router.RouteDefinition{Path: tc.second, Methods: []string{tc.secondMethod, "OPTIONS"}, Operation: "second", ShadowedMethods: []string{"caller-forgery"}}, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(202) })
			if tc.invalid {
				require.ErrorIs(t, r.ValidateRoutePolicies(), router.ErrRouteConfiguration)
			} else {
				require.NoError(t, r.ValidateRoutePolicies())
			}
			inventory := r.RouteInventory()
			require.Len(t, inventory, 2)
			require.Equal(t, tc.shadowed, inventory[1].ShadowedMethods)
			if len(tc.shadowed) > 0 {
				inventory[1].ShadowedMethods[0] = "mutated"
				require.Equal(t, tc.shadowed, r.RouteInventory()[1].ShadowedMethods)
			}
			if tc.first == "" {
				rec := httptest.NewRecorder()
				r.GetRouter().ServeHTTP(rec, httptest.NewRequest("GET", "/api", nil))
				require.Equal(t, 204, rec.Code)
			}
		})
	}
}

// TestRouteGroupPrefixShadows checks full Mux paths across separate subrouters.
// Equal relative paths under disjoint prefixes must not invalidate the registry.
func TestRouteGroupPrefixShadows(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, firstPrefix, secondPrefix string
		invalid                         bool
	}{
		{"disjoint namespaces", "/api", "/admin", false},
		{"nested disjoint paths", "/api", "/api/admin", false},
		{"duplicate groups", "/api", "/api", true},
		{"dynamic prefix covers literal", "/api/{scope}", "/api/public", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := router.NewRouter(nil, nil)
			for i, prefix := range []string{tc.firstPrefix, tc.secondPrefix} {
				status := http.StatusNoContent + i
				r.NewRouteGroup(prefix, router.Public, nil).Handle(router.RouteDefinition{
					Path: "/items/current", Methods: []string{http.MethodGet}, Operation: "items.read",
				}, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(status) })
			}
			if tc.invalid {
				require.ErrorIs(t, r.ValidateRoutePolicies(), router.ErrRouteConfiguration)
				require.Equal(t, []string{http.MethodGet}, r.RouteInventory()[1].ShadowedMethods)
			} else {
				require.NoError(t, r.ValidateRoutePolicies())
				require.Empty(t, r.RouteInventory()[1].ShadowedMethods)
			}
			rec := httptest.NewRecorder()
			r.GetRouter().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tc.secondPrefix+"/items/current", nil))
			if tc.invalid {
				require.Equal(t, http.StatusServiceUnavailable, rec.Code)
			} else {
				require.Equal(t, http.StatusResetContent, rec.Code)
			}
		})
	}
}

func TestRoutePolicyAdapterConfiguration(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name                                    string
		nilAuthorize, nilValidate, late, reject bool
		configureError, registryError           bool
	}{
		{name: "valid"},
		{name: "nil authorize", nilAuthorize: true, configureError: true},
		{name: "nil validator", nilValidate: true, configureError: true},
		{name: "late configuration", late: true, configureError: true},
		{name: "adapter rejects definition", reject: true, registryError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := router.NewRouter(nil, nil)
			calls := 0
			authorize := router.RouteAuthorizer(func(context.Context, *http.Request, router.RouteDefinition) error { return nil })
			validate := func(d router.RouteDefinition) error {
				calls++
				require.Equal(t, "/api/items", d.Path)
				d.Policy.Proof = "mutated"
				if tc.reject {
					return router.ErrRouteConfiguration
				}
				return nil
			}
			if tc.nilAuthorize {
				authorize = nil
			}
			if tc.nilValidate {
				validate = nil
			}
			if tc.late {
				r.NewRouteGroup("/old", router.Public, nil)
			}
			err := r.ConfigureRoutePolicy(authorize, validate)
			if tc.configureError {
				require.ErrorIs(t, err, router.ErrRouteConfiguration)
			} else {
				require.NoError(t, err)
			}
			r.NewRouteGroup("/api", router.Public, nil).Handle(router.RouteDefinition{Path: "/items", Methods: []string{"GET"}, Operation: "items.read"}, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) })
			if tc.registryError {
				require.ErrorIs(t, r.ValidateRoutePolicies(), router.ErrRouteConfiguration)
			} else {
				require.NoError(t, r.ValidateRoutePolicies())
			}
			if tc.configureError {
				require.Zero(t, calls)
			} else {
				require.Equal(t, 1, calls)
			}
			require.Empty(t, r.RouteInventory()[0].Policy.Proof, "adapter must not mutate the enforcement snapshot")
		})
	}
}

func TestPolicyRegistryBoundary(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, path string
		invalid    bool
		want       int
	}{
		{"descriptor protected", "/api/items", false, 403},
		{"raw route is outside registry", "/raw", false, 202},
		{"invalid registry closes descriptor", "/api/items", true, 503},
		{"invalid registry does not certify raw route", "/raw", true, 202},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := router.NewRouter(nil, nil)
			require.NoError(t, r.SetRouteAuthorizer(func(context.Context, *http.Request, router.RouteDefinition) error { return router.ErrRouteDenied }))
			r.GetRouter().HandleFunc("/raw", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(202) })
			r.NewRouteGroup("/api", router.Public, nil).Handle(router.RouteDefinition{Path: "/items", Methods: []string{"GET"}, Operation: "items.read"}, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) })
			if tc.invalid {
				r.NewRouteGroup("/invalid", router.Session, nil)
			}
			require.Len(t, r.RouteInventory(), 1)
			rec := httptest.NewRecorder()
			r.GetRouter().ServeHTTP(rec, httptest.NewRequest("GET", tc.path, nil))
			require.Equal(t, tc.want, rec.Code)
		})
	}
}

func TestConcurrentPolicySnapshots(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		workers int
	}{{"single", 1}, {"concurrent", 32}} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := router.NewRouter(nil, nil)
			var calls, failures atomic.Int64
			require.NoError(t, r.SetRouteAuthorizer(func(_ context.Context, _ *http.Request, d router.RouteDefinition) error {
				calls.Add(1)
				if len(d.Policy.Scopes) != 1 || d.Policy.Scopes[0] != "read" {
					failures.Add(1)
				}
				d.Policy.Scopes[0] = "mutated"
				return nil
			}))
			r.NewRouteGroup("/api", router.Session, func(next http.Handler) http.Handler { return next }).Handle(router.RouteDefinition{Path: "/items", Methods: []string{"GET"}, Operation: "items.read", Policy: router.RoutePolicy{Scopes: []string{"read"}}}, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) })
			require.NoError(t, r.ValidateRoutePolicies())
			var wg sync.WaitGroup
			for i := 0; i < tc.workers; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					snapshot := r.RouteInventory()
					snapshot[0].Policy.Scopes[0] = "reader mutation"
					rec := httptest.NewRecorder()
					r.GetRouter().ServeHTTP(rec, httptest.NewRequest("GET", "/api/items", nil))
					if rec.Code != 204 {
						failures.Add(1)
					}
				}()
			}
			wg.Wait()
			require.Equal(t, int64(tc.workers), calls.Load())
			require.Zero(t, failures.Load())
			require.Equal(t, []string{"read"}, r.RouteInventory()[0].Policy.Scopes)
		})
	}
}
