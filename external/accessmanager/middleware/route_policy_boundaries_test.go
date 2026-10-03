package middleware

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ooaklee/ghatd/external/accesspolicy"
	"github.com/ooaklee/ghatd/external/router"
	"github.com/ooaklee/reply/v2"
	"github.com/stretchr/testify/require"
)

// boundaryPolicyPort deliberately ignores cancellation to prove orchestration
// stops before the next adapter even when an injected dependency does not.
type boundaryPolicyPort struct {
	authorizeErr, consumeErr     error
	afterAuthorize, afterConsume func()
	calls                        []string
}

func (p *boundaryPolicyPort) Authorize(context.Context, accesspolicy.Subject, []string, []string) error {
	p.calls = append(p.calls, "authorize")
	if p.afterAuthorize != nil {
		p.afterAuthorize()
	}
	return p.authorizeErr
}
func (p *boundaryPolicyPort) ConsumeAuthorized(context.Context, accesspolicy.Consumption) (accesspolicy.Usage, error) {
	p.calls = append(p.calls, "consume")
	if p.afterConsume != nil {
		p.afterConsume()
	}
	return accesspolicy.Usage{}, p.consumeErr
}

func TestPolicyGuardCancellation(t *testing.T) {
	for _, phase := range []string{"entry", "authorize", "resource", "consume"} {
		t.Run(phase, func(t *testing.T) {
			ctx, cancel := context.WithCancel(policyContext(t, "session"))
			t.Cleanup(cancel)
			port := &boundaryPolicyPort{}
			if phase == "entry" {
				cancel()
			}
			if phase == "authorize" {
				port.afterAuthorize = cancel
			}
			if phase == "consume" {
				port.afterConsume = cancel
			}
			guard, err := NewRoutePolicyGuard("service", port, map[string]ResourceCheck{"owner": func(context.Context, *http.Request, string) error {
				port.calls = append(port.calls, "resource")
				if phase == "resource" {
					cancel()
				}
				return nil
			}})
			require.NoError(t, err)
			err = guard.Authorize(ctx, httptest.NewRequest("GET", "/", nil).WithContext(ctx), router.RouteDefinition{Access: router.Session, Operation: "read", Policy: router.RoutePolicy{Scopes: []string{"read"}, ResourceCheck: "owner", UsageMetric: "requests"}})
			require.ErrorIs(t, err, context.Canceled)
			want := map[string][]string{"entry": nil, "authorize": {"authorize"}, "resource": {"authorize", "resource"}, "consume": {"authorize", "resource", "consume"}}
			require.Equal(t, want[phase], port.calls)
		})
	}
}

func TestPolicyGuardEntryBoundaries(t *testing.T) {
	for _, mode := range []router.AccessMode{router.Public, router.OptionalActive, router.Session} {
		for _, variant := range []string{"nil context", "nil request", "canceled", "nil guard"} {
			t.Run(string(mode)+"/"+variant, func(t *testing.T) {
				guard, err := NewRoutePolicyGuard("service", &boundaryPolicyPort{}, nil)
				require.NoError(t, err)
				ctx := context.Background()
				request := httptest.NewRequest("GET", "/", nil)
				want := router.ErrRouteConfiguration
				switch variant {
				case "nil context":
					ctx = nil
				case "nil request":
					request = nil
				case "nil guard":
					guard = nil
				case "canceled":
					c, cancel := context.WithCancel(ctx)
					cancel()
					ctx = c
					want = context.Canceled
				}
				require.NotPanics(t, func() { require.ErrorIs(t, guard.Authorize(ctx, request, router.RouteDefinition{Access: mode}), want) })
			})
		}
	}
}

func TestPolicyGuardNativeFailureResponses(t *testing.T) {
	outage := errors.New("private storage failure")
	for _, tc := range []struct {
		name    string
		failure error
		status  int
		code    string
	}{
		{"native denial", accesspolicy.ErrDenied, 403, "ACP0-001"}, {"wrapped quota", fmt.Errorf("private diagnostic: %w", accesspolicy.ErrLimitReached), 429, "ACP0-004"},
		{"configuration", accesspolicy.ErrConfiguration, 503, "ACP0-002"}, {"unknown", outage, 503, "ROUTE_UNAVAILABLE"}, {"mixed", errors.Join(accesspolicy.ErrDenied, outage), 503, "ROUTE_UNAVAILABLE"},
	} {
		for _, boundary := range []string{"grant", "resource", "consume"} {
			t.Run(tc.name+"/"+boundary, func(t *testing.T) {
				port := &boundaryPolicyPort{}
				if boundary == "grant" {
					port.authorizeErr = tc.failure
				}
				if boundary == "consume" {
					port.consumeErr = tc.failure
				}
				guard, err := NewRoutePolicyGuard("service", port, map[string]ResourceCheck{"owner": func(context.Context, *http.Request, string) error {
					if boundary == "resource" {
						return tc.failure
					}
					return nil
				}})
				require.NoError(t, err)
				def := router.RouteDefinition{Path: "/items", Methods: []string{"GET"}, Access: router.Session, Operation: "items.read", Policy: router.RoutePolicy{Scopes: []string{"read"}, ResourceCheck: "owner", UsageMetric: "requests"}}
				ctx := policyContext(t, "session")
				request := httptest.NewRequest("GET", "/api/items", nil).WithContext(ctx)
				require.True(t, guard.Authorize(ctx, request, def) == tc.failure, "guard must preserve the native failure")
				r := router.NewRouter(nil, nil)
				require.NoError(t, guard.Install(r))
				called := false
				r.NewRouteGroup("/api", router.Session, func(h http.Handler) http.Handler { return h }).Handle(def, func(http.ResponseWriter, *http.Request) { called = true })
				require.NoError(t, r.ValidateRoutePolicies())
				w := httptest.NewRecorder()
				r.GetRouter().ServeHTTP(w, request)
				require.Equal(t, tc.status, w.Code)
				require.Contains(t, w.Body.String(), tc.code)
				require.NotContains(t, w.Body.String(), "private")
				require.False(t, called)
			})
		}
	}
}

func TestPolicyGuardHostManifestOverride(t *testing.T) {
	for _, override := range []bool{false, true} {
		t.Run(fmt.Sprint(override), func(t *testing.T) {
			g, err := NewRoutePolicyGuard("service", &boundaryPolicyPort{authorizeErr: accesspolicy.ErrDenied}, nil)
			require.NoError(t, err)
			r := router.NewRouter(nil, nil)
			var manifests []reply.ErrorManifest
			if override {
				manifests = []reply.ErrorManifest{{accesspolicy.ErrDenied: {StatusCode: 422, Code: "HOST-POLICY"}}}
			}
			require.NoError(t, g.Install(r, manifests...))
			called := false
			r.NewRouteGroup("/api", router.Session, func(h http.Handler) http.Handler { return h }).Handle(router.RouteDefinition{Path: "/items", Methods: []string{"GET"}, Operation: "items.read", Policy: router.RoutePolicy{Scopes: []string{"read"}}}, func(http.ResponseWriter, *http.Request) { called = true })
			w := httptest.NewRecorder()
			r.GetRouter().ServeHTTP(w, httptest.NewRequest("GET", "/api/items", nil).WithContext(policyContext(t, "session")))
			if override {
				require.Equal(t, 422, w.Code)
				require.Contains(t, w.Body.String(), "HOST-POLICY")
			} else {
				require.Equal(t, 403, w.Code)
				require.Contains(t, w.Body.String(), "ACP0-001")
			}
			require.False(t, called)
		})
	}
}
