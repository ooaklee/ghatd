package middleware

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/accessmanager"
	"github.com/ooaklee/ghatd/external/ephemeral"
	"github.com/ooaklee/reply/v2"
	"github.com/stretchr/testify/require"
)

func TestMiddlewareErrorResponses(t *testing.T) {
	for _, mode := range []string{"standard", "active", "admin", "api", "admin-api", "optional"} {
		t.Run(mode, func(t *testing.T) {
			for _, tc := range []struct {
				name, variant string
				err           error
				status        int
			}{
				{"mapped denial", "", accessmanager.ErrUnauthorizedAdminAccessAttempted, 401},
				{"wrapped denial", "", fmt.Errorf("private-diagnostic: %w", accessmanager.ErrUnauthorizedAdminAccessAttempted), 401},
				{"unknown failure", "", errors.New("private-diagnostic"), 500},
				{"joined outage and denial", "", errors.Join(accessmanager.ErrUnauthorizedAdminAccessAttempted, errors.New("private-diagnostic")), 500},
				{"host manifest override", "override", accessmanager.ErrUnauthorizedAdminAccessAttempted, 403},
				{"verification unavailable", "", accessmanager.ErrSessionVerificationUnavailable, 503},
				{"refresh temporarily unavailable", "", accessmanager.ErrRefreshTemporarilyUnavailable, 503},
				{"host verification override", "unavailable override", accessmanager.ErrSessionVerificationUnavailable, 502},
				{"explicit empty manifest", "empty", accessmanager.ErrUnauthorizedAdminAccessAttempted, 500},
			} {
				t.Run(tc.name, func(t *testing.T) {
					verify := func(*http.Request) (*accessmanager.MiddlewareAuthedUserResponse, error) { return nil, tc.err }
					stub := &mockAccessManagerService{
						middlewareJWTRequiredFunc: verify, middlewareActiveJWTRequiredFunc: verify, middlewareAdminJWTRequiredFunc: verify,
						middlewareValidAPITokenRequiredFunc: verify, middlewareAdminAPITokenRequiredFunc: verify, middlewareRateLimitOrActiveJWTRequiredFunc: verify,
						refreshTokenFunc: func(context.Context, *accessmanager.RefreshTokenRequest) (*accessmanager.RefreshTokenResponse, error) {
							return nil, tc.err
						},
					}
					maps := []reply.ErrorManifest{accessmanager.AccessmanagerErrorMap}
					if tc.variant == "override" {
						maps = append(maps, reply.ErrorManifest{accessmanager.ErrUnauthorizedAdminAccessAttempted: {Title: "Host denial", StatusCode: 403, Code: "HOST-DENIED"}})
					}
					if tc.variant == "unavailable override" {
						maps = append(maps, reply.ErrorManifest{accessmanager.ErrSessionVerificationUnavailable: {Title: "Host unavailable", StatusCode: 502, Code: "HOST-UNAVAILABLE"}})
					}
					if tc.variant == "empty" {
						maps = []reply.ErrorManifest{}
					}
					mw := NewMiddleware(&NewMiddlewareRequest{Service: stub, ErrorMaps: maps, CookiePrefixAuthToken: "access", CookiePrefixRefreshToken: "refresh"})
					r := httptest.NewRequest("GET", "/protected", nil)
					r.AddCookie(&http.Cookie{Name: "access", Value: "fixture-access"})
					r.AddCookie(&http.Cookie{Name: "refresh", Value: "fixture-refresh"})
					next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("failed authentication reached handler") })
					var handler http.Handler
					switch mode {
					case "standard":
						handler = mw.JWTRequired(next)
					case "active":
						handler = mw.ActiveJWTRequired(next)
					case "admin":
						handler = mw.AdminJWTRequired(next)
					case "api":
						handler = mw.ValidAPITokenRequired(next)
					case "admin-api":
						r.Header.Set("X-Api-Token", "fixture-api")
						handler = mw.AdminApiTokenOrJWTRequired(next)
					case "optional":
						handler = mw.RateLimitOrActiveJWTRequired(next)
					}
					w := httptest.NewRecorder()
					handler.ServeHTTP(w, r)
					require.Equal(t, tc.status, w.Code, w.Body.String())
					require.NotContains(t, w.Body.String(), "private-diagnostic")
					if tc.variant == "override" {
						require.Contains(t, w.Body.String(), "HOST-DENIED")
					}
				})
			}
		})
	}
}

func TestHardenedLimitDistinguishesOutageFromAbuse(t *testing.T) {
	for _, tc := range []struct {
		name                 string
		blocked              bool
		checkErr, trackErr   error
		status, tracks, bans int
	}{
		{"allowed", false, nil, nil, 204, 1, 0},
		{"already blocked", true, nil, nil, 429, 0, 0},
		{"block lookup failed", false, errors.New("private-diagnostic"), nil, 500, 0, 0},
		{"counter failed", false, nil, errors.New("private-diagnostic"), 500, 1, 0},
		{"counter canceled", false, nil, context.Canceled, 500, 1, 0},
		{"threshold reached", false, nil, ephemeral.ErrHardenedRateLimitExceeded, 429, 1, 1},
		{"wrapped threshold", false, nil, fmt.Errorf("private-diagnostic: %w", ephemeral.ErrHardenedRateLimitExceeded), 429, 1, 1},
		{"joined threshold and outage", false, nil, errors.Join(ephemeral.ErrHardenedRateLimitExceeded, errors.New("private-diagnostic")), 500, 1, 0},
		{"joined single threshold", false, nil, errors.Join(ephemeral.ErrHardenedRateLimitExceeded), 500, 1, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tracks, bans := 0, 0
			store := &mockHardenedRateLimitStore{
				isIPBlockedFunc:          func(context.Context, string) (bool, error) { return tc.blocked, tc.checkErr },
				trackHardenedAttemptFunc: func(context.Context, string, string, int, time.Duration) error { tracks++; return tc.trackErr },
				blockIPFunc:              func(context.Context, string, time.Duration) error { bans++; return nil },
			}
			mw := NewHardenedRateLimitProtection(&NewHardenedRateLimitProtectionRequest{EphemeralStore: store, ErrorMaps: []reply.ErrorManifest{ephemeral.EphemeralStoreErrorMap}})
			w := httptest.NewRecorder()
			mw.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) })).ServeHTTP(w, httptest.NewRequest("GET", "/verify?c=TESTCODE", nil))
			require.Equal(t, tc.status, w.Code)
			require.Equal(t, tc.tracks, tracks)
			require.Equal(t, tc.bans, bans)
			require.NotContains(t, w.Body.String(), "private-diagnostic")
		})
	}
}
