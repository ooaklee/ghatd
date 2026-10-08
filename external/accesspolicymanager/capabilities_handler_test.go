package accesspolicymanager

import (
	"context"
	"github.com/ooaklee/ghatd/external/accessmanager"
	amiddleware "github.com/ooaklee/ghatd/external/accessmanager/middleware"
	"github.com/ooaklee/ghatd/external/accesspolicy"
	"github.com/ooaklee/ghatd/external/auth"
	"github.com/ooaklee/ghatd/external/router"
	user "github.com/ooaklee/ghatd/external/user/v2"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// capabilityHTTPStub retains the independent token-only handler contract.
type capabilityHTTPStub struct {
	managementHTTPStub
	patch accesspolicy.Capabilities
}

func (s *capabilityHTTPStub) ReviewCapabilities(_ context.Context, id string) (*accesspolicy.Grant, error) {
	s.calls++
	s.target = id
	if s.err != nil {
		return nil, s.err
	}
	return &s.grant, nil
}
func (s *capabilityHTTPStub) ApplyCapabilities(_ context.Context, id string, expected int64, patch accesspolicy.Capabilities) (accesspolicy.Grant, error) {
	s.calls++
	s.target = id
	s.expected = expected
	s.patch = patch
	return s.grant, s.err
}

func TestCapabilityRoutesExplicitBearerAndRevision(t *testing.T) {
	for _, tc := range []struct {
		name, variant, method string
		status                int
	}{
		{"administrator review", "", "GET", 200},
		{"administrator explicit replacement", "", "PUT", 200},
		{"anonymous cannot inspect grants", "anonymous", "GET", 401},
		{"ordinary member cannot inspect grants", "member", "GET", 403},
		{"suspended administrator cannot inspect grants", "suspended", "GET", 403},
		{"cookie context cannot become explicit bearer", "cookie", "PUT", 401},
		{"missing revision", "no-revision", "PUT", 428},
		{"ambiguous revision", "bad-revision", "PUT", 400},
		{"token body cannot enter capability write", "body", "PUT", 400},
		{"current service denial", "denied", "PUT", 403},
		{"stale owning revision", "conflict", "PUT", 412},
		{"private dependency diagnostic hidden", "outage", "PUT", 500},
	} {
		t.Run(tc.name, func(t *testing.T) {
			routes := router.NewRouter(nil, nil)
			policy, err := accesspolicy.NewService(&managementStore{}, nil)
			require.NoError(t, err)
			guard, err := amiddleware.NewRoutePolicyGuard("platform", policy, nil)
			require.NoError(t, err)
			require.NoError(t, guard.Install(routes))
			stub := &capabilityHTTPStub{managementHTTPStub: managementHTTPStub{grant: accesspolicy.Grant{Revision: 4}}}
			switch tc.variant {
			case "denied":
				stub.err = accesspolicy.ErrDenied
			case "conflict":
				stub.err = accesspolicy.ErrConflict
			case "outage":
				stub.err = context.DeadlineExceeded
			}
			h, err := NewHandler(stub)
			require.NoError(t, err)
			operator := &user.UniversalUser{ID: "operator", Status: user.AccountStatusKeyActive, Roles: []string{"ADMIN"}}
			if tc.variant == "member" {
				operator.Roles = nil
			}
			if tc.variant == "suspended" {
				operator.Status = user.AccountStatusKeySuspended
			}
			result := &accessmanager.MiddlewareAuthedUserResponse{Authenticated: true, UserID: operator.ID, User: operator, Token: &auth.TokenAccessDetails{UserID: operator.ID, AccessUUID: "session", TokenUse: auth.TokenUseAccess}}
			mw := amiddleware.NewMiddleware(&amiddleware.NewMiddlewareRequest{Service: &routeAuthenticator{result: result}})
			adapter := mw.BearerSessionRequired
			if tc.variant == "cookie" {
				adapter = func(next http.Handler) http.Handler {
					return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						ctx, e := amiddleware.ContextWithAuthentication(r.Context(), result)
						require.NoError(t, e)
						next.ServeHTTP(w, r.WithContext(ctx))
					})
				}
			}
			require.NoError(t, AttachRoutes(routes, h, adapter))
			require.NoError(t, AttachCapabilityRoutes(routes, h, adapter))
			require.Len(t, routes.RouteInventory(), 4)
			for _, route := range routes.RouteInventory() {
				require.Equal(t, router.AdminSession, route.Access)
			}
			body := completeCapabilities
			if tc.variant == "body" {
				body = completeLimits
			}
			r := httptest.NewRequest(tc.method, BasePath+"/users/selected/capabilities", strings.NewReader(body))
			r.Header.Set("Content-Type", "application/json")
			if tc.variant != "anonymous" {
				r.Header.Set("Authorization", "Bearer fixture")
			}
			if tc.variant != "no-revision" {
				r.Header.Set("If-Match", `"3"`)
			}
			if tc.variant == "bad-revision" {
				r.Header.Set("If-Match", `"03"`)
			}
			w := httptest.NewRecorder()
			routes.GetRouter().ServeHTTP(w, r)
			require.Equal(t, tc.status, w.Code, w.Body.String())
			require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
			require.NotContains(t, w.Body.String(), "deadline exceeded")
			wantCalls := 0
			if tc.status == 200 || tc.variant == "denied" || tc.variant == "conflict" || tc.variant == "outage" {
				wantCalls = 1
			}
			require.Equal(t, wantCalls, stub.calls)
			if tc.status == 200 {
				require.Equal(t, 1, stub.calls)
				require.Equal(t, "selected", stub.target)
				require.Equal(t, `"4"`, w.Header().Get("ETag"))
				if tc.method == "PUT" {
					require.Equal(t, int64(3), stub.expected)
					require.True(t, stub.patch.Enabled)
				}
			}
		})
	}
}

func TestCapabilityRoutesRequireExplicitOptIn(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodPut} {
		t.Run(method, func(t *testing.T) {
			routes := router.NewRouter(nil, nil)
			policy, err := accesspolicy.NewService(&managementStore{}, nil)
			require.NoError(t, err)
			guard, err := amiddleware.NewRoutePolicyGuard("platform", policy, nil)
			require.NoError(t, err)
			require.NoError(t, guard.Install(routes))
			stub := &capabilityHTTPStub{}
			h, err := NewHandler(stub)
			require.NoError(t, err)
			adapter := func(next http.Handler) http.Handler { return next }
			require.NoError(t, AttachRoutes(routes, h, adapter))
			require.Len(t, routes.RouteInventory(), 2)
			w := httptest.NewRecorder()
			routes.GetRouter().ServeHTTP(w, httptest.NewRequest(method, BasePath+"/users/selected/capabilities", nil))
			require.Equal(t, http.StatusNotFound, w.Code)
			require.Zero(t, stub.calls)
			tokenOnly, err := NewHandler(&managementHTTPStub{})
			require.NoError(t, err)
			require.ErrorIs(t, AttachCapabilityRoutes(routes, tokenOnly, adapter), accesspolicy.ErrConfiguration)
		})
	}
}
