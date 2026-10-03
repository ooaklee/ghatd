package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/accessmanager"
	helpers "github.com/ooaklee/ghatd/external/accessmanager/helpers"
	"github.com/ooaklee/ghatd/external/auth"
	"github.com/stretchr/testify/require"
)

func TestAuthenticationContextCopiesClaimsAndRejectsMismatches(t *testing.T) {
	for _, tc := range []struct {
		name                   string
		want                   error
		authenticated, session bool
	}{
		{"copied claims", nil, true, true},
		{"mismatched claims", auth.ErrUnauthorized, false, false},
		{"anonymous placeholder", nil, false, false},
		{"legacy without claims", nil, true, false},
		{"nil result", auth.ErrUnauthorized, false, false},
		{"nil context", accessmanager.ErrSessionVerificationUnavailable, false, false},
		{"canceled context", context.Canceled, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result := mockAuthedResp("member", "ACTIVE", nil)
			result.Token = &auth.TokenAccessDetails{UserID: "member", AccessUUID: "session", Audience: []string{"mobile"}, AuthenticationTime: time.Now().Add(-time.Minute)}
			inherited, err := ContextWithAuthentication(context.Background(), result)
			require.NoError(t, err)
			switch tc.name {
			case "mismatched claims":
				result.Token.UserID = "other"
			case "anonymous placeholder":
				result.Authenticated = false
			case "legacy without claims":
				result.Token = nil
			case "nil result":
				result = nil
			case "nil context":
				inherited = nil
			case "canceled context":
				var cancel context.CancelFunc
				inherited, cancel = context.WithCancel(inherited)
				cancel()
			}
			ctx, err := ContextWithAuthentication(inherited, result)
			require.ErrorIs(t, err, tc.want)
			require.NotNil(t, ctx)
			if tc.authenticated {
				require.Equal(t, "member", helpers.AcquireAuthenticatedUserIDFrom(ctx))
			} else {
				require.Empty(t, helpers.AcquireAuthenticatedUserIDFrom(ctx))
			}
			if tc.want != nil {
				require.Empty(t, helpers.AcquireFrom(ctx))
			} else {
				require.Equal(t, "member", helpers.AcquireFrom(ctx))
			}
			if !tc.session {
				require.Nil(t, helpers.AcquireSessionFrom(ctx))
				return
			}
			require.Equal(t, "session", helpers.AcquireSessionFrom(ctx).AccessUUID)
			result.Token.Audience[0] = "changed"
			copy := helpers.AcquireSessionFrom(ctx)
			require.Equal(t, []string{"mobile"}, copy.Audience)
			copy.Audience[0] = "also-changed"
			require.Equal(t, []string{"mobile"}, helpers.AcquireSessionFrom(ctx).Audience)
		})
	}
}

func TestPreloadedMiddlewarePublishesClaimsAndStopsInconsistentResults(t *testing.T) {
	for _, tc := range []struct {
		name    string
		allowed bool
	}{
		{"valid", true}, {"mismatched", false}, {"nil", false}, {"canceled", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := &Middleware{}
			result := mockAuthedResp("member", "ACTIVE", nil)
			result.Token = &auth.TokenAccessDetails{UserID: "member", AccessUUID: "session"}
			req := httptest.NewRequest("GET", "/", nil)
			switch tc.name {
			case "mismatched":
				result.UserID = "different"
			case "nil":
				result = nil
			case "canceled":
				ctx, cancel := context.WithCancel(req.Context())
				cancel()
				req = req.WithContext(ctx)
			}
			called := false
			next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				called = true
				require.Equal(t, "session", helpers.AcquireSessionFrom(r.Context()).AccessUUID)
			})
			w := httptest.NewRecorder()
			m.serveAuthenticated(w, req, next, result)
			require.Equal(t, tc.allowed, called)
			if !tc.allowed {
				require.GreaterOrEqual(t, w.Code, 400)
			}
		})
	}
}
