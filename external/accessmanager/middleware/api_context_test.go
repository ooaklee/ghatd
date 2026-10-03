package middleware

import (
	"context"
	"testing"

	"github.com/ooaklee/ghatd/external/accessmanager"
	helpers "github.com/ooaklee/ghatd/external/accessmanager/helpers"
	"github.com/ooaklee/ghatd/external/apitoken"
	"github.com/ooaklee/ghatd/external/auth"
	"github.com/stretchr/testify/require"
)

func TestAPIContextPublication(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name          string
		authenticated bool
		owner         string
		tokenID       string
		withSession   bool
		wantError     bool
		wantAPI       bool
	}{
		{"verified API", true, "member", "credential", false, false, true},
		{"anonymous clears API", false, "member", "credential", false, false, false},
		{"owner mismatch", true, "other", "credential", false, true, false},
		{"missing credential ID", true, "member", "", false, true, false},
		{"competing session", true, "member", "credential", true, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			inherited := helpers.TransitAuthenticatedWith(context.Background(), true)
			inherited = helpers.TransitWith(inherited, "previous")
			inherited = helpers.TransitSessionWith(inherited, &auth.TokenAccessDetails{UserID: "previous", AccessUUID: "session"})
			inherited = helpers.TransitAPITokenWith(inherited, &apitoken.CredentialDetails{UserID: "previous", TokenID: "old"})
			result := mockAuthedResp("member", "ACTIVE", nil)
			result.Authenticated = tc.authenticated
			result.APIToken = &apitoken.CredentialDetails{TokenID: tc.tokenID, UserID: tc.owner}
			if tc.withSession {
				result.Token = &auth.TokenAccessDetails{UserID: "member", AccessUUID: "session"}
			}
			ctx, err := ContextWithAuthentication(inherited, result)
			if tc.wantError {
				require.Error(t, err)
				require.Empty(t, helpers.AcquireAuthenticatedUserIDFrom(ctx))
			} else {
				require.NoError(t, err)
			}
			require.Nil(t, helpers.AcquireSessionFrom(ctx))
			if tc.wantAPI {
				got := helpers.AcquireAPITokenFrom(ctx)
				require.Equal(t, result.APIToken, got)
				result.APIToken.TokenID = "mutated"
				got.TokenID = "also-mutated"
				require.Equal(t, "credential", helpers.AcquireAPITokenFrom(ctx).TokenID)
			} else {
				require.Nil(t, helpers.AcquireAPITokenFrom(ctx))
			}
		})
	}
}

func TestCredentialHelpersRejectUntrustedOrMixedContext(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name          string
		authenticated bool
		owner         string
		session       bool
		api           bool
		wantSession   bool
		wantAPI       bool
	}{
		{"session", true, "member", true, false, true, false},
		{"API", true, "member", false, true, false, true},
		{"both", true, "member", true, true, false, false},
		{"anonymous API", false, "member", false, true, false, false},
		{"wrong owner", true, "other", false, true, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := helpers.TransitWith(context.Background(), tc.owner)
			ctx = helpers.TransitAuthenticatedWith(ctx, tc.authenticated)
			if tc.session {
				ctx = helpers.TransitSessionWith(ctx, &auth.TokenAccessDetails{UserID: "member", AccessUUID: "session"})
			}
			if tc.api {
				ctx = helpers.TransitAPITokenWith(ctx, &apitoken.CredentialDetails{UserID: "member", TokenID: "credential"})
			}
			require.Equal(t, tc.wantSession, helpers.AcquireSessionFrom(ctx) != nil)
			require.Equal(t, tc.wantAPI, helpers.AcquireAPITokenFrom(ctx) != nil)
			cleared, err := ContextWithAuthentication(ctx, (*accessmanager.MiddlewareAuthedUserResponse)(nil))
			require.Error(t, err)
			require.Nil(t, helpers.AcquireSessionFrom(cleared))
			require.Nil(t, helpers.AcquireAPITokenFrom(cleared))
		})
	}
}
