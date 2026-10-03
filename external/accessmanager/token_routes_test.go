package accessmanager_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"
	"github.com/ooaklee/ghatd/external/accessmanager"
	"github.com/ooaklee/ghatd/external/apitoken"
	"github.com/ooaklee/ghatd/external/router"
	"github.com/ooaklee/reply/v2"
	"github.com/stretchr/testify/require"
)

// credentialRouteHandler exposes only the routes under test. Other handlers
// remain nil so accidental dispatch fails rather than reporting false success.
type credentialRouteHandler struct {
	accessmanager.AccessmanagerHandler
	calls int
}

func (h *credentialRouteHandler) CreateUserAPIToken(w http.ResponseWriter, _ *http.Request) {
	h.calls++
	w.WriteHeader(204)
}
func (h *credentialRouteHandler) GetSpecificUserAPITokens(w http.ResponseWriter, _ *http.Request) {
	h.calls++
	w.WriteHeader(204)
}
func (h *credentialRouteHandler) GetUserAPITokenThreshold(w http.ResponseWriter, _ *http.Request) {
	h.calls++
	w.WriteHeader(204)
}
func (h *credentialRouteHandler) DeleteUserAPIToken(w http.ResponseWriter, _ *http.Request) {
	h.calls++
	w.WriteHeader(204)
}
func (h *credentialRouteHandler) ActivateUserAPIToken(w http.ResponseWriter, _ *http.Request) {
	h.calls++
	w.WriteHeader(204)
}
func (h *credentialRouteHandler) RevokeUserAPIToken(w http.ResponseWriter, _ *http.Request) {
	h.calls++
	w.WriteHeader(204)
}
func (h *credentialRouteHandler) LogoutUserOthers(w http.ResponseWriter, _ *http.Request) {
	h.calls++
	w.WriteHeader(204)
}

func TestCredentialRoutesRequireSessionMiddleware(t *testing.T) {
	for _, tc := range []struct{ name, method, path string }{
		{"create", "POST", "/users/owner/tokens"}, {"list", "GET", "/users/owner/tokens"}, {"thresholds", "GET", "/users/owner/tokens/thresholds"},
		{"delete", "DELETE", "/users/owner/tokens/token"}, {"activate", "PUT", "/users/owner/tokens/token/activate"}, {"revoke", "PUT", "/users/owner/tokens/token/revoke"},
		{"logout other sessions", "GET", "/logout/other-sessions"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := router.NewRouter(nil, nil)
			handler := &credentialRouteHandler{}
			pass := func(next http.Handler) http.Handler { return next }
			sessionCalls, mixedCalls := 0, 0
			session := func(next http.Handler) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
					sessionCalls++
					if req.Header.Get("Test-Session") != "verified" {
						w.WriteHeader(401)
						return
					}
					next.ServeHTTP(w, req)
				})
			}
			mixed := func(next http.Handler) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) { mixedCalls++; next.ServeHTTP(w, req) })
			}
			accessmanager.AttachRoutes(&accessmanager.AttachRoutesRequest{Router: r, Handler: handler, ActiveOnlyMiddleware: session, ActiveValidApiTokenOrJWTMiddleware: mixed, HardenedRateLimitMiddleware: pass})
			for _, hasSession := range []bool{false, true} {
				req := httptest.NewRequest(tc.method, accessmanager.APIAccessManagerPrefix+tc.path, nil)
				if hasSession {
					req.Header.Set("Test-Session", "verified")
				} else {
					req.Header.Set("X-Api-Token", "test-prefix.test-secret")
				}
				rec := httptest.NewRecorder()
				r.GetRouter().ServeHTTP(rec, req)
				if hasSession {
					require.Equal(t, 204, rec.Code)
				} else {
					require.Equal(t, 401, rec.Code)
				}
			}
			require.Equal(t, 2, sessionCalls)
			require.Zero(t, mixedCalls)
			require.Equal(t, 1, handler.calls)
			credentialRoutes := 0
			for _, route := range r.RouteInventory() {
				switch route.Operation {
				case "accessmanager.CreateUserAPIToken", "accessmanager.GetSpecificUserAPITokens", "accessmanager.GetUserAPITokenThreshold", "accessmanager.DeleteUserAPIToken", "accessmanager.ActivateUserAPIToken", "accessmanager.RevokeUserAPIToken", "accessmanager.LogoutUserOthers":
					credentialRoutes++
					require.Equal(t, router.ActiveSession, route.Access)
				}
			}
			require.Equal(t, 7, credentialRoutes)
		})
	}
}

// A missing session verifier must not silently expose credential management,
// even when the deprecated mixed-credential verifier is supplied.
func TestCredentialRoutesMissingSessionVerifier(t *testing.T) {
	for _, tc := range []struct{ name, method, path string }{
		{"create", "POST", "/users/owner/tokens"}, {"list", "GET", "/users/owner/tokens"},
		{"thresholds", "GET", "/users/owner/tokens/thresholds"}, {"delete", "DELETE", "/users/owner/tokens/token"},
		{"activate", "PUT", "/users/owner/tokens/token/activate"}, {"revoke", "PUT", "/users/owner/tokens/token/revoke"},
		{"logout other sessions", "GET", "/logout/other-sessions"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := router.NewRouter(nil, nil)
			handler := &credentialRouteHandler{}
			mixedCalls := 0
			mixed := func(next http.Handler) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) { mixedCalls++; next.ServeHTTP(w, req) })
			}
			accessmanager.AttachRoutes(&accessmanager.AttachRoutesRequest{Router: r, Handler: handler, ActiveValidApiTokenOrJWTMiddleware: mixed})
			rec := httptest.NewRecorder()
			r.GetRouter().ServeHTTP(rec, httptest.NewRequest(tc.method, accessmanager.APIAccessManagerPrefix+tc.path, nil))
			require.Equal(t, http.StatusServiceUnavailable, rec.Code)
			require.Zero(t, handler.calls)
			require.Zero(t, mixedCalls)
		})
	}
}

func TestStatusMapperRetainsVerifiedTargetOwner(t *testing.T) {
	for _, tc := range []struct {
		name                 string
		activate, wrongOwner bool
	}{
		{"revoke owner", false, false}, {"activate owner", true, false}, {"revoke other owner denied", false, true}, {"activate other owner denied", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			owner := "f5a10f5e-6d67-4e10-b2be-4426e8122033"
			token := "a131e6fc-78db-4c0a-8b3a-68c28a92cb49"
			req := httptest.NewRequest("PUT", "/", nil)
			actor := owner
			if tc.wrongOwner {
				actor = "other-owner"
			}
			req = req.WithContext(tokenSessionContext(req.Context(), actor))
			req = mux.SetURLVars(req, map[string]string{accessmanager.UserURIVariableID: owner, accessmanager.APITokenURIVariableID: token})
			var got *accessmanager.UserAPITokenStatusRequest
			var err error
			if tc.activate {
				got, err = accessmanager.MapRequestToActivateUserAPITokenRequest(req, newTestValidator())
			} else {
				got, err = accessmanager.MapRequestToRevokeUserAPITokenRequest(req, newTestValidator())
			}
			if tc.wrongOwner {
				require.ErrorIs(t, err, accessmanager.ErrForbiddenUnableToAction)
				require.Nil(t, got)
			} else {
				require.NoError(t, err)
				require.Equal(t, owner, got.UserID)
				require.Equal(t, token, got.APITokenID)
			}
		})
	}
}

func TestTokenErrorsHaveStructuredDefaultReplies(t *testing.T) {
	for _, tc := range []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{"invalid lifetime", apitoken.ErrInvalidTokenTTL, 400, "APT0-010"}, {"invalid query", apitoken.ErrInvalidTokenQuery, 400, "APT0-011"},
		{"owner record absent", apitoken.ErrResourceNotFound, 404, "APT0-008"}, {"policy not configured safely", accessmanager.ErrTokenPolicyUnavailable, 503, "AM00-038"},
		{"inventory not prepared", apitoken.ErrInventoryUnavailable, 503, "APT0-012"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			handler := accessmanager.NewHandler(&accessmanager.NewHandlerRequest{})
			rec := httptest.NewRecorder()
			require.NoError(t, handler.NewHTTPErrorResponse(rec, fmt.Errorf("private diagnostic: %w", tc.err)))
			require.Equal(t, tc.status, rec.Code)
			require.Contains(t, rec.Body.String(), tc.code)
			require.NotContains(t, rec.Body.String(), "private diagnostic")
		})
	}
}

func TestTokenErrorHostOverridePrecedence(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
	}{
		{"direct", apitoken.ErrInvalidTokenTTL},
		{"wrapped", fmt.Errorf("private diagnostic: %w", apitoken.ErrInvalidTokenTTL)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			handler := accessmanager.NewHandler(&accessmanager.NewHandlerRequest{ErrorMaps: []reply.ErrorManifest{{apitoken.ErrInvalidTokenTTL: {StatusCode: 422, Title: "Invalid lifetime", Code: "HOST-LIFETIME"}}}})
			rec := httptest.NewRecorder()
			require.NoError(t, handler.NewHTTPErrorResponse(rec, tc.err))
			require.Equal(t, 422, rec.Code)
			require.Contains(t, rec.Body.String(), "HOST-LIFETIME")
			require.NotContains(t, rec.Body.String(), "APT0-010")
			require.NotContains(t, rec.Body.String(), "private diagnostic")
		})
	}
}
