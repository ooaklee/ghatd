package middleware

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ooaklee/ghatd/external/accessmanager"
	helpers "github.com/ooaklee/ghatd/external/accessmanager/helpers"
	"github.com/ooaklee/ghatd/external/apitoken"
	"github.com/ooaklee/ghatd/external/auth"
	userv2 "github.com/ooaklee/ghatd/external/user/v2"
	"github.com/ooaklee/reply/v2"
	"github.com/stretchr/testify/require"
)

// TestCookieLifecycle exercises every cookie adapter at verification, rotation
// and retry boundaries. Unknown failures never become anonymous authorization.
func TestCookieLifecycle(t *testing.T) {
	outage := errors.New("private-database-diagnostic")
	expired := auth.ErrUnauthorizedParsedStringTokenExpired
	invalid := auth.ErrUnauthorized
	cases := []struct {
		name                     string
		initial, refresh, retry  error
		cancelAt, badResult      string
		verifications, rotations int
		clear, replace, allowed  bool
		status                   int
	}{
		{name: "valid", verifications: 1, allowed: true, status: 200},
		{name: "expired rotates", initial: expired, verifications: 2, rotations: 1, replace: true, allowed: true, status: 200},
		{name: "wrapped expiry rotates", initial: fmt.Errorf("parse: %w", expired), verifications: 2, rotations: 1, replace: true, allowed: true, status: 200},
		{name: "missing access record rotates", initial: accessmanager.ErrUnauthorizedTokenNotFoundInStore, verifications: 2, rotations: 1, replace: true, allowed: true, status: 200},
		{name: "initial outage", initial: outage, verifications: 1, status: 500},
		{name: "initial lookalike expiry", initial: errors.New(expired.Error()), verifications: 1, status: 500},
		{name: "initial joined", initial: errors.Join(expired, outage), verifications: 1, status: 500},
		{name: "initial nil result", badResult: "initial nil", verifications: 1, status: 503},
		{name: "initial public result", badResult: "initial public", verifications: 1, status: 503},
		{name: "initial invalid", initial: invalid, verifications: 1, clear: true, status: 401},
		{name: "initial status denial", initial: accessmanager.ErrUnauthorizedNonActiveStatus, verifications: 1, status: 401},
		{name: "initial admin denial", initial: accessmanager.ErrUnauthorizedAdminAccessAttempted, verifications: 1, status: 401},
		{name: "refresh outage", initial: expired, refresh: outage, verifications: 1, rotations: 1, status: 500},
		{name: "refresh timeout", initial: expired, refresh: accessmanager.ErrRefreshTemporarilyUnavailable, verifications: 1, rotations: 1, status: 503},
		{name: "refresh invalid", initial: expired, refresh: accessmanager.ErrInvalidRefreshToken, verifications: 1, rotations: 1, clear: true, status: 400},
		{name: "refresh joined", initial: expired, refresh: errors.Join(invalid, outage), verifications: 1, rotations: 1, status: 500},
		{name: "refresh nil result", initial: expired, badResult: "nil tokens", verifications: 1, rotations: 1, status: 503},
		{name: "refresh partial result", initial: expired, badResult: "empty access", verifications: 1, rotations: 1, status: 503},
		{name: "retry outage", initial: expired, retry: outage, verifications: 2, rotations: 1, status: 500},
		{name: "retry invalid", initial: expired, retry: invalid, verifications: 2, rotations: 1, clear: true, status: 401},
		{name: "retry expiry", initial: expired, retry: expired, verifications: 2, rotations: 1, clear: true, status: 401},
		{name: "retry policy denial", initial: expired, retry: accessmanager.ErrUnauthorizedNonActiveStatus, verifications: 2, rotations: 1, status: 401},
		{name: "retry nil identity", initial: expired, badResult: "nil identity", verifications: 2, rotations: 1, status: 503},
		{name: "retry public identity", initial: expired, badResult: "public identity", verifications: 2, rotations: 1, status: 503},
		{name: "cancel before", cancelAt: "entry", status: 500},
		{name: "cancel initial success", cancelAt: "initial", verifications: 1, status: 500},
		{name: "cancel initial expiry", initial: expired, cancelAt: "initial", verifications: 1, status: 500},
		{name: "cancel rotation", initial: expired, cancelAt: "refresh", verifications: 1, rotations: 1, status: 500},
		{name: "cancel rotation with credential error", initial: expired, refresh: invalid, cancelAt: "refresh", verifications: 1, rotations: 1, status: 500},
		{name: "cancel retry", initial: expired, cancelAt: "retry", verifications: 2, rotations: 1, status: 500},
		{name: "cancel retry with credential error", initial: expired, retry: invalid, cancelAt: "retry", verifications: 2, rotations: 1, status: 500},
	}
	modes := []struct {
		name     string
		wrap     func(*Middleware, http.Handler) http.Handler
		optional bool
	}{
		{"standard", (*Middleware).JWTRequired, false},
		{"active", (*Middleware).ActiveJWTRequired, false},
		{"admin", (*Middleware).AdminJWTRequired, false},
		{"mixed standard", (*Middleware).ActiveValidApiTokenOrAuthenticated, false},
		{"mixed active", (*Middleware).ActiveValidApiTokenOrJWTRequired, false},
		{"mixed admin", (*Middleware).AdminApiTokenOrJWTRequired, false},
		{"optional", (*Middleware).RateLimitOrActiveJWTRequired, true},
	}
	for _, mode := range modes {
		for _, tc := range cases {
			t.Run(mode.name+"/"+tc.name, func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				t.Cleanup(cancel)
				verifyCalls, refreshCalls, publicCalls, handlerCalls := 0, 0, 0, 0
				verify := func(r *http.Request) (*accessmanager.MiddlewareAuthedUserResponse, error) {
					if r.Header.Get("Authorization") == "" {
						publicCalls++
						return mockPublicResp("anonymous"), nil
					}
					verifyCalls++
					phase, failure := "initial", tc.initial
					if verifyCalls > 1 {
						phase, failure = "retry", tc.retry
						require.Equal(t, "Bearer new-access-token", r.Header.Get("Authorization"))
					}
					if tc.cancelAt == phase {
						cancel()
					}
					if failure != nil {
						return nil, failure
					}
					if phase == "initial" {
						if tc.badResult == "initial nil" {
							return nil, nil
						}
						if tc.badResult == "initial public" {
							return mockPublicResp("anonymous"), nil
						}
					}
					if phase == "retry" {
						if tc.badResult == "nil identity" {
							return nil, nil
						}
						if tc.badResult == "public identity" {
							return mockPublicResp("anonymous"), nil
						}
					}
					return mockAuthedResp("user-123", userv2.AccountStatusKeyActive, []string{userv2.UserRoleAdmin}), nil
				}
				stub := &mockAccessManagerService{
					middlewareJWTRequiredFunc: verify, middlewareActiveJWTRequiredFunc: verify,
					middlewareAdminJWTRequiredFunc: verify, middlewareRateLimitOrActiveJWTRequiredFunc: verify,
					refreshTokenFunc: func(_ context.Context, request *accessmanager.RefreshTokenRequest) (*accessmanager.RefreshTokenResponse, error) {
						refreshCalls++
						require.Equal(t, "presented-refresh", request.RefreshToken)
						if tc.cancelAt == "refresh" {
							cancel()
						}
						if tc.refresh != nil {
							return nil, tc.refresh
						}
						if tc.badResult == "nil tokens" {
							return nil, nil
						}
						result := &accessmanager.RefreshTokenResponse{AccessToken: "new-access-token", RefreshToken: "new-refresh-token", AccessTokenExpiresAt: 2000000000, RefreshTokenExpiresAt: 2000003600}
						if tc.badResult == "empty access" {
							result.AccessToken = ""
						}
						return result, nil
					},
				}
				mw := createTestMiddleware(stub)
				mw.errorMaps = []reply.ErrorManifest{accessmanager.AccessmanagerErrorMap, auth.AuthErrorMap}
				handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					handlerCalls++
					if tc.replace {
						require.Equal(t, "Bearer new-access-token", r.Header.Get("Authorization"))
						access, err := r.Cookie("test_auth")
						require.NoError(t, err)
						require.Equal(t, "new-access-token", access.Value)
						refresh, err := r.Cookie("test_refresh")
						require.NoError(t, err)
						require.Equal(t, "new-refresh-token", refresh.Value)
					}
					w.WriteHeader(http.StatusOK)
				})
				request := httptest.NewRequest(http.MethodGet, "/resource", nil).WithContext(ctx)
				request.Header.Set("Authorization", "Bearer original-header")
				request.AddCookie(&http.Cookie{Name: "test_auth", Value: "presented-access"})
				request.AddCookie(&http.Cookie{Name: "test_refresh", Value: "presented-refresh"})
				originalCookies := request.Header.Get("Cookie")
				if tc.cancelAt == "entry" {
					cancel()
				}
				recorder := httptest.NewRecorder()
				mode.wrap(mw, handler).ServeHTTP(recorder, request)
				fallback := mode.optional && tc.clear
				wantStatus := tc.status
				if fallback {
					wantStatus = http.StatusOK
				}
				require.Equal(t, wantStatus, recorder.Code, recorder.Body.String())
				require.Equal(t, tc.verifications, verifyCalls)
				require.Equal(t, tc.rotations, refreshCalls)
				require.Equal(t, fallback, publicCalls == 1)
				require.LessOrEqual(t, publicCalls, 1)
				require.Equal(t, tc.allowed || fallback, handlerCalls == 1)
				require.LessOrEqual(t, handlerCalls, 1)
				require.Equal(t, "Bearer original-header", request.Header.Get("Authorization"))
				require.Equal(t, originalCookies, request.Header.Get("Cookie"))
				require.NotContains(t, recorder.Body.String(), outage.Error())
				cookies := recorder.Result().Cookies()
				for _, cookie := range []struct{ name, value string }{{"test_auth", "new-access-token"}, {"test_refresh", "new-refresh-token"}} {
					require.Equal(t, tc.clear, responseHasCookieRemoval(cookies, cookie.name))
					require.Equal(t, tc.replace, responseHasCookieValue(cookies, cookie.name, cookie.value))
				}
				if !tc.clear && !tc.replace {
					require.Empty(t, recorder.Header().Values("Set-Cookie"))
				}
			})
		}
	}
}

func TestPreparedRefreshPublication(t *testing.T) {
	for _, canceled := range []bool{false, true} {
		t.Run(fmt.Sprintf("canceled_before_publication=%t", canceled), func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			t.Cleanup(cancel)
			tokens := &accessmanager.RefreshTokenResponse{AccessToken: "replacement", RefreshToken: "replacement-refresh", AccessTokenExpiresAt: 2000000000, RefreshTokenExpiresAt: 2000003600}
			stub := &mockAccessManagerService{refreshTokenFunc: func(context.Context, *accessmanager.RefreshTokenRequest) (*accessmanager.RefreshTokenResponse, error) {
				return tokens, nil
			}}
			mw := createTestMiddleware(stub)
			req := httptest.NewRequest("GET", "/", nil).WithContext(ctx)
			req.Header.Set("Authorization", "Bearer original")
			prepared, err := mw.attemptTokenRefresh(req, &http.Cookie{Value: "refresh"}, func(r *http.Request) (*accessmanager.MiddlewareAuthedUserResponse, error) {
				require.Equal(t, "Bearer replacement", r.Header.Get("Authorization"))
				return mockAuthedResp("member", "ACTIVE", nil), nil
			})
			require.NoError(t, err)
			require.Equal(t, "Bearer original", req.Header.Get("Authorization"))
			tokens.AccessToken = "mutated adapter response"
			if canceled {
				cancel()
			}
			called := false
			w := httptest.NewRecorder()
			mw.serveRefreshed(w, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				called = true
				require.Equal(t, "member", helpers.AcquireAuthenticatedUserIDFrom(r.Context()))
			}), prepared)
			require.Equal(t, !canceled, called)
			if canceled {
				require.Empty(t, w.Header().Values("Set-Cookie"))
			} else {
				require.True(t, responseHasCookieValue(w.Result().Cookies(), "test_auth", "replacement"))
			}
		})
	}
}

func TestAnonymousFallbackCredentialBoundary(t *testing.T) {
	for _, variant := range []string{"public", "authenticated", "claims", "API metadata", "nil", "canceled"} {
		t.Run(variant, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			t.Cleanup(cancel)
			ctx = helpers.TransitAuthenticatedWith(helpers.TransitWith(ctx, "inherited"), true)
			stub := &mockAccessManagerService{middlewareRateLimitOrActiveJWTRequiredFunc: func(r *http.Request) (*accessmanager.MiddlewareAuthedUserResponse, error) {
				require.Empty(t, r.Header.Get("Authorization"))
				require.Empty(t, r.Header.Get("X-Api-Token"))
				require.Empty(t, helpers.AcquireFrom(r.Context()))
				for _, name := range []string{"test_auth", "test_refresh"} {
					_, err := r.Cookie(name)
					require.ErrorIs(t, err, http.ErrNoCookie)
				}
				locale, err := r.Cookie("locale")
				require.NoError(t, err)
				require.Equal(t, "en", locale.Value)
				result := mockPublicResp("anonymous")
				switch variant {
				case "authenticated":
					result = mockAuthedResp("member", "ACTIVE", nil)
				case "claims":
					result.Token = &auth.TokenAccessDetails{UserID: "member"}
				case "API metadata":
					result.APIToken = &apitoken.CredentialDetails{UserID: "member", TokenID: "token"}
				case "nil":
					result = nil
				case "canceled":
					cancel()
				}
				return result, nil
			}}
			mw := createTestMiddleware(stub)
			req := httptest.NewRequest("GET", "/", nil).WithContext(ctx)
			req.Header.Set("Authorization", "Bearer presented")
			req.Header.Set("X-Api-Token", "presented-api")
			req.AddCookie(&http.Cookie{Name: "test_auth", Value: "presented"})
			req.AddCookie(&http.Cookie{Name: "test_refresh", Value: "presented-refresh"})
			req.AddCookie(&http.Cookie{Name: "locale", Value: "en"})
			called := false
			w := httptest.NewRecorder()
			mw.handleRateLimitOrActiveUnauthenticated(w, req, http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				called = true
				require.Empty(t, helpers.AcquireAuthenticatedUserIDFrom(r.Context()))
			}), false, "test", nil, nil, nil)
			require.Equal(t, variant == "public", called)
			require.Empty(t, w.Header().Values("Set-Cookie"))
			require.Equal(t, "Bearer presented", req.Header.Get("Authorization"))
			if variant != "public" {
				require.GreaterOrEqual(t, w.Code, 500)
			}
		})
	}
}
