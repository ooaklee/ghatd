package middleware

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ooaklee/ghatd/external/accessmanager"
	"github.com/ooaklee/ghatd/external/auth"
	"github.com/ooaklee/ghatd/external/errormanifest/bundles"
	"github.com/ooaklee/reply/v2"
	"github.com/stretchr/testify/require"
)

func TestMeEndpointResponseModes(t *testing.T) {
	for _, mode := range []MeEndpointResponseMode{MeEndpointLegacyAccepted, MeEndpointAcceptedEmpty, MeEndpointUnauthorized} {
		t.Run("mode="+string(mode), func(t *testing.T) {
			for _, tc := range []struct {
				name    string
				failure error
				api     bool
				status  int
				missing bool
			}{
				{"missing cookies", nil, false, 0, true},
				{"exact missing identity", accessmanager.ErrUnauthorizedUnableToAttainRequestorID, true, 0, true},
				{"wrapped missing identity", fmt.Errorf("private diagnostic: %w", accessmanager.ErrUnauthorizedUnableToAttainRequestorID), true, 0, true},
				{"lookalike missing identity", errors.New(accessmanager.ErrUnauthorizedUnableToAttainRequestorID.Error()), true, 500, false},
				{"mixed missing and outage", errors.Join(accessmanager.ErrUnauthorizedUnableToAttainRequestorID, errors.New("private outage")), true, 500, false},
				{"known invalid credential", auth.ErrUnauthorized, true, 401, false},
				{"expired credential", auth.ErrUnauthorizedParsedStringTokenExpired, true, 401, false},
				{"permission denied uses host override", accessmanager.ErrForbiddenUnableToAction, true, 409, false},
				{"known outage", accessmanager.ErrSessionVerificationUnavailable, true, 503, false},
				{"unknown outage", errors.New("private outage"), true, 500, false},
				{"valid identity", nil, true, 200, false},
			} {
				t.Run(tc.name, func(t *testing.T) {
					calls, dispatched := 0, 0
					service := &mockAccessManagerService{middlewareValidAPITokenRequiredFunc: func(*http.Request) (*accessmanager.MiddlewareAuthedUserResponse, error) {
						calls++
						return mockAuthedResp("owner", "ACTIVE", []string{"USER"}), tc.failure
					}}
					base := append(bundles.AuthMiddleware(), reply.ErrorManifest{
						accessmanager.ErrUnauthorizedUnableToAttainRequestorID: {Title: "Custom", StatusCode: 418, Code: "CUSTOM"},
						accessmanager.ErrForbiddenUnableToAction:               {Title: "Custom denial", StatusCode: 409, Code: "CUSTOM_DENIAL"},
					})
					mw := NewMiddleware(&NewMiddlewareRequest{Service: service, ErrorMaps: base, CookiePrefixAuthToken: "auth", CookiePrefixRefreshToken: "refresh"})
					probe, err := mw.MeEndpointMiddleware(mode)
					require.NoError(t, err)
					r := httptest.NewRequest(http.MethodGet, "/api/v1/ums/me", nil)
					if tc.api {
						r.Header.Set("X-Api-Token", "test-credential")
					}
					w := httptest.NewRecorder()
					w.Header().Set("Cache-Control", "public, max-age=3600")
					w.Header().Set("X-Robots-Tag", "nofollow")
					probe(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
						dispatched++
						_, _ = w.Write([]byte(`{"data":{"id":"owner"}}`))
					})).ServeHTTP(w, r)
					status := tc.status
					if tc.missing {
						status = http.StatusAccepted
						if mode == MeEndpointUnauthorized {
							status = http.StatusUnauthorized
						}
					}
					require.Equal(t, status, w.Code, w.Body.String())
					require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
					require.Contains(t, w.Header().Values("X-Robots-Tag"), "noindex")
					require.Contains(t, w.Header().Values("X-Robots-Tag"), "nofollow")
					require.NotContains(t, w.Body.String(), "private")
					require.NotContains(t, w.Body.String(), "test-credential")
					if tc.missing && mode == MeEndpointAcceptedEmpty {
						require.Empty(t, w.Body.String())
						require.Empty(t, w.Header().Get("Content-Type"), "empty mode does not advertise a JSON document")
					} else if tc.missing {
						require.JSONEq(t, fmt.Sprintf(`{"errors":[{"title":"Unauthorized","status":"%d","code":"AM00-013"}]}`, status), w.Body.String())
					} else {
						require.NotEmpty(t, w.Body.String())
					}
					if tc.api {
						require.Equal(t, 1, calls)
					} else {
						require.Zero(t, calls)
					}
					if tc.status == 200 {
						require.Equal(t, 1, dispatched)
					} else {
						require.Zero(t, dispatched)
					}
					// A route-local selection must not mutate the shared base maps or
					// convert another protected endpoint to the probe's wire contract.
					require.Equal(t, 418, base[len(base)-1][accessmanager.ErrUnauthorizedUnableToAttainRequestorID].StatusCode)
					ordinary := httptest.NewRecorder()
					mw.ActiveValidApiTokenOrAuthenticated(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("anonymous dispatch") })).ServeHTTP(ordinary, httptest.NewRequest(http.MethodGet, "/private", nil))
					require.Equal(t, 418, ordinary.Code)
					require.NotEmpty(t, ordinary.Body.String())
					require.Empty(t, ordinary.Header().Get("X-Robots-Tag"))
				})
			}
		})
	}
}

func TestMeEndpointFailureBoundaries(t *testing.T) {
	for _, mode := range []MeEndpointResponseMode{MeEndpointLegacyAccepted, MeEndpointAcceptedEmpty, MeEndpointUnauthorized} {
		t.Run("mode="+string(mode), func(t *testing.T) {
			for _, tc := range []struct {
				name   string
				status int
			}{
				{"nil successful identity", 503},
				{"mismatched successful identity", 503},
				{"canceled before cookie selection", 500},
				{"enclosed early response", 429},
			} {
				t.Run(tc.name, func(t *testing.T) {
					service := &mockAccessManagerService{middlewareValidAPITokenRequiredFunc: func(*http.Request) (*accessmanager.MiddlewareAuthedUserResponse, error) {
						if tc.name == "nil successful identity" {
							return nil, nil
						}
						result := mockAuthedResp("owner", "ACTIVE", []string{"USER"})
						if tc.name == "mismatched successful identity" {
							result.User.ID = "different-owner"
						}
						return result, nil
					}}
					mw := NewMiddleware(&NewMiddlewareRequest{Service: service, ErrorMaps: bundles.AuthMiddleware()})
					probe, err := mw.MeEndpointMiddleware(mode)
					require.NoError(t, err)
					r := httptest.NewRequest(http.MethodGet, "/me", nil)
					r.Header.Set("X-Api-Token", "test-credential")
					if tc.name == "canceled before cookie selection" {
						r.Header.Del("X-Api-Token")
						ctx, cancel := context.WithCancel(r.Context())
						cancel()
						r = r.WithContext(ctx)
					}
					dispatched := 0
					w := httptest.NewRecorder()
					probe(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
						dispatched++
						w.WriteHeader(429)
					})).ServeHTTP(w, r)
					require.Equal(t, tc.status, w.Code, w.Body.String())
					require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
					require.Contains(t, w.Header().Values("X-Robots-Tag"), "noindex")
					if tc.status == 429 {
						require.Equal(t, 1, dispatched)
					} else {
						require.Zero(t, dispatched)
						require.NotEmpty(t, w.Body.String())
						require.NotContains(t, w.Body.String(), "AM00-013")
					}
				})
			}
		})
	}
}

func TestMeEndpointNilReceiver(t *testing.T) {
	for _, mode := range []MeEndpointResponseMode{MeEndpointLegacyAccepted, MeEndpointAcceptedEmpty, MeEndpointUnauthorized, "invalid"} {
		t.Run("mode="+string(mode), func(t *testing.T) {
			var mw *Middleware
			probe, err := mw.MeEndpointMiddleware(mode)
			require.Nil(t, probe)
			require.ErrorIs(t, err, ErrNilRequest)
		})
	}
}

func TestMeEndpointSuiteConfiguration(t *testing.T) {
	for _, tc := range []struct {
		name    string
		mode    MeEndpointResponseMode
		status  int
		empty   bool
		invalid bool
	}{
		{"legacy default", MeEndpointLegacyAccepted, 202, false, false},
		{"empty accepted", MeEndpointAcceptedEmpty, 202, true, false},
		{"structured unauthorized", MeEndpointUnauthorized, 401, false, false},
		{"invalid", "typo", 0, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			suite, err := NewSuite(&NewSuiteRequest{Service: &mockAccessManagerService{}, EphemeralStore: &mockHardenedRateLimitStore{}, MeEndpointResponseMode: tc.mode})
			if tc.invalid {
				require.ErrorIs(t, err, ErrInvalidMeEndpointResponseMode)
				require.Nil(t, suite)
				return
			}
			require.NoError(t, err)
			w := httptest.NewRecorder()
			suite.CustomMeEndpointValidApiTokenOrJWT(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("anonymous dispatch") })).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/me", nil))
			require.Equal(t, tc.status, w.Code)
			require.Equal(t, tc.empty, w.Body.Len() == 0)
		})
	}
}
