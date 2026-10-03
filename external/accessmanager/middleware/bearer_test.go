package middleware

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ooaklee/ghatd/external/accessmanager"
	helpers "github.com/ooaklee/ghatd/external/accessmanager/helpers"
	"github.com/ooaklee/ghatd/external/auth"
	userv2 "github.com/ooaklee/ghatd/external/user/v2"
	"github.com/stretchr/testify/require"
)

// bearerServiceStub exposes only explicit session verification. Calling cookie
// refresh or API verification would panic through the unimplemented interface.
type bearerServiceStub struct {
	accessManagerService
	result *accessmanager.MiddlewareAuthedUserResponse
	err    error
	calls  int
	header string
	cancel context.CancelFunc
}

func (s *bearerServiceStub) MiddlewareJWTRequired(r *http.Request) (*accessmanager.MiddlewareAuthedUserResponse, error) {
	s.calls++
	s.header = r.Header.Get("Authorization")
	if s.cancel != nil {
		s.cancel()
	}
	return s.result, s.err
}

func TestBearerSessionCredentialSelection(t *testing.T) {
	for _, tc := range []struct {
		name    string
		headers []string
		api     bool
		variant string
		status  int
		calls   int
	}{
		{"explicit bearer", []string{"Bearer selected"}, false, "", 204, 1},
		{"cookie alone", nil, false, "", 401, 0},
		{"duplicate bearer", []string{"Bearer selected", "Bearer another"}, false, "", 401, 0},
		{"API alone", nil, true, "", 401, 0},
		{"competing API", []string{"Bearer selected"}, true, "", 401, 0},
		{"wrong scheme", []string{"Basic selected"}, false, "", 401, 0},
		{"empty bearer", []string{"Bearer "}, false, "", 401, 0},
		{"merged bearer", []string{"Bearer selected,another"}, false, "", 401, 0},
		{"oversized bearer", []string{"Bearer " + strings.Repeat("x", 16384)}, false, "", 401, 0},
		{"anonymous result", []string{"Bearer selected"}, false, "anonymous", 401, 1},
		{"missing claims", []string{"Bearer selected"}, false, "no-claims", 401, 1},
		{"missing session ID", []string{"Bearer selected"}, false, "no-session", 401, 1},
		{"wrong owner", []string{"Bearer selected"}, false, "wrong-owner", 401, 1},
		{"wrapped denial", []string{"Bearer selected"}, false, "denied", 401, 1},
		{"dependency diagnostics hidden", []string{"Bearer selected"}, false, "dependency", 500, 1},
		{"missing service", []string{"Bearer selected"}, false, "nil-service", 503, 0},
		{"nil middleware", []string{"Bearer selected"}, false, "nil-middleware", 503, 0},
		{"canceled entry", []string{"Bearer selected"}, false, "canceled", 500, 0},
		{"canceled verification", []string{"Bearer selected"}, false, "cancel-verify", 500, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stub := &bearerServiceStub{result: &accessmanager.MiddlewareAuthedUserResponse{Authenticated: true, UserID: "operator", User: &userv2.UniversalUser{ID: "operator"}, Token: &auth.TokenAccessDetails{UserID: "operator", AccessUUID: "session", TokenUse: auth.TokenUseAccess}}}
			switch tc.variant {
			case "anonymous":
				stub.result.Authenticated = false
			case "no-claims":
				stub.result.Token = nil
			case "no-session":
				stub.result.Token.AccessUUID = ""
			case "wrong-owner":
				stub.result.UserID = "another"
			case "denied":
				stub.err = fmt.Errorf("private verification context: %w", auth.ErrUnauthorized)
			case "dependency":
				stub.err = errors.New("private dependency password diagnostic")
			}
			mw := NewMiddleware(&NewMiddlewareRequest{Service: stub})
			ctx, cancel := context.WithCancel(context.Background())
			t.Cleanup(cancel)
			switch tc.variant {
			case "nil-service":
				mw.service = nil
			case "nil-middleware":
				mw = nil
			case "canceled":
				cancel()
			case "cancel-verify":
				stub.cancel = cancel
			}
			r := httptest.NewRequest(http.MethodPost, "/admin", nil).WithContext(ctx)
			for _, header := range tc.headers {
				r.Header.Add("Authorization", header)
			}
			if tc.api {
				r.Header.Set("X-Api-Token", "api-credential")
			}
			r.AddCookie(&http.Cookie{Name: "__aauth", Value: "unselected-cookie"})
			r.AddCookie(&http.Cookie{Name: "__rauth", Value: "unselected-refresh"})
			w := httptest.NewRecorder()
			mw.BearerSessionRequired(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, "operator", helpers.AcquireAuthenticatedUserIDFrom(r.Context()))
				require.NotNil(t, helpers.AcquireSessionFrom(r.Context()))
				require.True(t, IsExplicitBearerSession(r.Context()))
				w.WriteHeader(204)
			})).ServeHTTP(w, r)
			require.Equal(t, tc.status, w.Code)
			require.Equal(t, tc.calls, stub.calls)
			require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
			require.Empty(t, w.Header().Values("Set-Cookie"))
			require.NotContains(t, w.Body.String(), "private dependency")
			require.NotContains(t, w.Body.String(), "unselected")
			if tc.calls > 0 {
				require.Equal(t, "Bearer selected", stub.header)
			}
		})
	}
}

func TestExplicitBearerOriginBinding(t *testing.T) {
	for _, variant := range []string{"nil", "ordinary context", "cookie session", "matching bearer", "replaced session", "replaced user", "mixed credential"} {
		t.Run(variant, func(t *testing.T) {
			ctx := context.Background()
			result := &accessmanager.MiddlewareAuthedUserResponse{Authenticated: true, UserID: "owner", User: &userv2.UniversalUser{ID: "owner"}, Token: &auth.TokenAccessDetails{UserID: "owner", AccessUUID: "session", TokenUse: auth.TokenUseAccess}}
			switch variant {
			case "nil":
				ctx = nil
			case "ordinary context":
			default:
				var err error
				ctx, err = ContextWithAuthentication(ctx, result)
				require.NoError(t, err)
				if variant != "cookie session" {
					ctx = context.WithValue(ctx, explicitBearerKey{}, explicitBearerOrigin{userID: "owner", sessionID: "session"})
				}
				if variant == "replaced session" {
					ctx = helpers.TransitSessionWith(ctx, &auth.TokenAccessDetails{UserID: "owner", AccessUUID: "other"})
				}
				if variant == "replaced user" {
					ctx = helpers.TransitWith(ctx, "other")
				}
				if variant == "mixed credential" {
					ctx = helpers.TransitAuthenticatedWith(ctx, false)
				}
			}
			require.Equal(t, variant == "matching bearer", IsExplicitBearerSession(ctx))
		})
	}
}
