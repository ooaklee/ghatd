package accesspolicymanager

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"
	"github.com/ooaklee/ghatd/external/accessmanager"
	amiddleware "github.com/ooaklee/ghatd/external/accessmanager/middleware"
	"github.com/ooaklee/ghatd/external/accesspolicy"
	"github.com/ooaklee/ghatd/external/auth"
	"github.com/ooaklee/ghatd/external/router"
	userv2 "github.com/ooaklee/ghatd/external/user/v2"
	"github.com/ooaklee/reply/v2"
	"github.com/stretchr/testify/require"
)

type managementHTTPStub struct {
	err      error
	preview  accesspolicy.TokenLimitPreview
	grant    accesspolicy.Grant
	calls    int
	target   string
	expected int64
	limits   accesspolicy.TokenLimits
}

func (s *managementHTTPStub) Preview(_ context.Context, id string, limits accesspolicy.TokenLimits) (accesspolicy.TokenLimitPreview, error) {
	s.calls++
	s.target = id
	s.limits = limits
	return s.preview, s.err
}
func (s *managementHTTPStub) Apply(_ context.Context, id string, expected int64, limits accesspolicy.TokenLimits) (accesspolicy.Grant, error) {
	s.calls++
	s.target = id
	s.expected = expected
	s.limits = limits
	return s.grant, s.err
}

func TestManagementHTTPBoundary(t *testing.T) {
	for _, tc := range []struct {
		name, method, variant string
		status, calls         int
		tag                   string
	}{
		{"missing preview grant", "POST", "", 200, 1, `"0"`},
		{"existing preview grant", "POST", "existing", 200, 1, `"7"`},
		{"known apply success", "PUT", "", 200, 1, `"8"`},
		{"missing revision", "PUT", "no-revision", 428, 0, ""},
		{"noncanonical revision", "PUT", "bad-revision", 400, 0, ""},
		{"malformed body", "POST", "body", 400, 0, ""},
		{"conflict canonicalized", "PUT", "conflict", 412, 1, ""},
		{"denied", "POST", "denied", 403, 1, ""},
		{"missing target", "POST", "missing", 404, 1, ""},
		{"missing actor", "POST", "actor", 401, 1, ""},
		{"backend configuration", "PUT", "config", 503, 1, ""},
		{"dependency diagnostics hidden", "PUT", "unknown", 500, 1, ""},
		{"host override", "PUT", "override", 409, 1, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stub := &managementHTTPStub{grant: accesspolicy.Grant{Revision: 8}}
			var overrides []reply.ErrorManifest
			body := completeLimits
			switch tc.variant {
			case "existing":
				stub.preview.Before = &accesspolicy.Grant{Revision: 7}
			case "body":
				body = `{"permanent":3}`
			case "conflict", "override":
				stub.err = fmt.Errorf("private details: %w", accesspolicy.ErrConflict)
			case "denied":
				stub.err = accesspolicy.ErrDenied
			case "missing":
				stub.err = ErrUserNotFound
			case "actor":
				stub.err = userv2.ErrUserNotFound
			case "config":
				stub.err = accesspolicy.ErrConfiguration
			case "unknown":
				stub.err = errors.New("private dependency password diagnostic")
			}
			if tc.variant == "override" {
				overrides = append(overrides, reply.ErrorManifest{accesspolicy.ErrConflict: {Title: "Review changed", Detail: "Review again", StatusCode: 409, Code: "HOST-1"}})
			}
			h, err := NewHandler(stub, overrides...)
			require.NoError(t, err)
			// Startup construction owns manifest entries independently of callers.
			if tc.variant == "override" {
				entry := overrides[0][accesspolicy.ErrConflict]
				entry.StatusCode = 418
				overrides[0][accesspolicy.ErrConflict] = entry
			}
			r := httptest.NewRequest(tc.method, "/", strings.NewReader(body))
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("If-Match", `"7"`)
			if tc.variant == "no-revision" {
				r.Header.Del("If-Match")
			}
			if tc.variant == "bad-revision" {
				r.Header.Set("If-Match", `"07"`)
			}
			r = mux.SetURLVars(r, map[string]string{"userID": "chosen"})
			w := httptest.NewRecorder()
			if tc.method == "POST" {
				h.PreviewTokenLimits(w, r)
			} else {
				h.ApplyTokenLimits(w, r)
			}
			require.Equal(t, tc.status, w.Code, w.Body.String())
			require.Equal(t, tc.calls, stub.calls)
			require.Equal(t, tc.tag, w.Header().Get("ETag"))
			require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
			require.Contains(t, w.Header().Get("Content-Type"), "json")
			require.NotContains(t, w.Body.String(), "private")
			if tc.calls > 0 {
				require.Equal(t, "chosen", stub.target)
				require.Equal(t, int64(2), stub.limits.Permanent)
			}
			if tc.method == "PUT" && tc.calls > 0 {
				require.Equal(t, int64(7), stub.expected)
			}
		})
	}
}

// routeAuthenticator exposes only the shared adapter's verifier port; all cookie
// or refresh methods remain unimplemented and would panic if accidentally used.
type routeAuthenticator struct {
	*accessmanager.Service
	result *accessmanager.MiddlewareAuthedUserResponse
}

func (a *routeAuthenticator) MiddlewareJWTRequired(*http.Request) (*accessmanager.MiddlewareAuthedUserResponse, error) {
	return a.result, nil
}

func TestManagementRouteGuard(t *testing.T) {
	for _, tc := range []struct {
		name, variant string
		status        int
	}{
		{"current administrator", "", 200}, {"anonymous", "anonymous", 401}, {"ordinary member", "member", 403}, {"disabled administrator", "disabled", 403}, {"missing revision", "no-revision", 428}, {"missing evaluator fails startup", "no-guard", 0}, {"cookie adapter miswire", "cookie-adapter", 401},
	} {
		t.Run(tc.name, func(t *testing.T) {
			routes := router.NewRouter(nil, nil)
			if tc.variant != "no-guard" {
				policy, err := accesspolicy.NewService(&managementStore{}, nil)
				require.NoError(t, err)
				guard, err := amiddleware.NewRoutePolicyGuard("service", policy, nil)
				require.NoError(t, err)
				require.NoError(t, guard.Install(routes))
			}
			stub := &managementHTTPStub{grant: accesspolicy.Grant{Revision: 1}}
			h, err := NewHandler(stub)
			require.NoError(t, err)
			user := &userv2.UniversalUser{ID: "operator", Status: userv2.AccountStatusKeyActive, Roles: []string{"ADMIN"}}
			if tc.variant == "member" {
				user.Roles = nil
			}
			if tc.variant == "disabled" {
				user.Status = userv2.AccountStatusKeySuspended
			}
			result := &accessmanager.MiddlewareAuthedUserResponse{Authenticated: true, UserID: user.ID, User: user, Token: &auth.TokenAccessDetails{UserID: user.ID, AccessUUID: "session", TokenUse: auth.TokenUseAccess}}
			mw := amiddleware.NewMiddleware(&amiddleware.NewMiddlewareRequest{Service: &routeAuthenticator{result: result}})
			adapter := mw.BearerSessionRequired
			if tc.variant == "cookie-adapter" {
				adapter = func(next http.Handler) http.Handler {
					return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						ctx, err := amiddleware.ContextWithAuthentication(r.Context(), result)
						require.NoError(t, err)
						next.ServeHTTP(w, r.WithContext(ctx))
					})
				}
			}
			err = AttachRoutes(routes, h, adapter)
			if tc.variant == "no-guard" {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			inventory := routes.RouteInventory()
			require.Len(t, inventory, 2)
			for _, route := range inventory {
				require.Equal(t, router.AdminSession, route.Access)
			}
			r := httptest.NewRequest("PUT", BasePath+"/users/chosen/token-limits", strings.NewReader(completeLimits))
			r.Header.Set("Content-Type", "application/json")
			if tc.variant != "anonymous" {
				r.Header.Set("Authorization", "Bearer selected")
			}
			if tc.variant != "no-revision" {
				r.Header.Set("If-Match", `"0"`)
			}
			w := httptest.NewRecorder()
			routes.GetRouter().ServeHTTP(w, r)
			require.Equal(t, tc.status, w.Code, w.Body.String())
			if tc.status == 200 {
				require.Equal(t, 1, stub.calls)
			} else {
				require.Zero(t, stub.calls)
			}
		})
	}
}
