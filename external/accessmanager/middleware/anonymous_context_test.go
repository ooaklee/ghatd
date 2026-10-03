package middleware

import (
	"context"
	"testing"

	"github.com/ooaklee/ghatd/external/accessmanager"
	helpers "github.com/ooaklee/ghatd/external/accessmanager/helpers"
	"github.com/ooaklee/ghatd/external/apitoken"
	"github.com/ooaklee/ghatd/external/auth"
	user "github.com/ooaklee/ghatd/external/user/v2"
	"github.com/stretchr/testify/require"
)

// TestAnonymousContextWithoutPlaceholder covers the explicit anonymous shape,
// not missing verifier results. Every publication clears inherited authority.
func TestAnonymousContextWithoutPlaceholder(t *testing.T) {
	for _, tc := range []struct {
		name, id, userID                                    string
		user, authenticated, session, api, absent, canceled bool
		want                                                error
	}{
		{name: "empty anonymous result"},
		{name: "empty anonymous user", user: true},
		{name: "configured placeholder", id: "anonymous", userID: "anonymous", user: true},
		{name: "nil result", absent: true, want: auth.ErrUnauthorized},
		{name: "authenticated empty identity", authenticated: true, user: true, want: auth.ErrUnauthorized},
		{name: "anonymous empty identity with session", session: true, want: auth.ErrUnauthorized},
		{name: "anonymous empty identity with API credential", api: true, want: auth.ErrUnauthorized},
		{name: "anonymous empty identity with both credentials", session: true, api: true, want: auth.ErrUnauthorized},
		{name: "user without matching identity", user: true, userID: "member", want: auth.ErrUnauthorized},
		{name: "placeholder without user", id: "anonymous", want: auth.ErrUnauthorized},
		{name: "placeholder with empty user", id: "anonymous", user: true, want: auth.ErrUnauthorized},
		{name: "placeholder with different user", id: "anonymous", user: true, userID: "member", want: auth.ErrUnauthorized},
		{name: "canceled anonymous result", canceled: true, want: context.Canceled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			inherited, err := ContextWithAuthentication(context.Background(), mockAuthedResp("previous", "ACTIVE", nil))
			require.NoError(t, err)
			inherited = helpers.TransitSessionWith(inherited, &auth.TokenAccessDetails{UserID: "previous", AccessUUID: "session"})
			inherited = helpers.TransitAPITokenWith(inherited, &apitoken.CredentialDetails{UserID: "previous", TokenID: "api"})
			type traceKey struct{}
			inherited = context.WithValue(inherited, traceKey{}, "trace")
			inherited = context.WithValue(inherited, explicitBearerKey{}, explicitBearerOrigin{userID: "previous", sessionID: "session"})
			if tc.canceled {
				var cancel context.CancelFunc
				inherited, cancel = context.WithCancel(inherited)
				cancel()
			}
			result := &accessmanager.MiddlewareAuthedUserResponse{Authenticated: tc.authenticated, UserID: tc.id}
			if tc.user {
				result.User = &user.UniversalUser{ID: tc.userID}
			}
			if tc.session {
				result.Token = &auth.TokenAccessDetails{UserID: "member", AccessUUID: "session"}
			}
			if tc.api {
				result.APIToken = &apitoken.CredentialDetails{UserID: "member", TokenID: "api"}
			}
			if tc.absent {
				result = nil
			}
			got, err := ContextWithAuthentication(inherited, result)
			require.ErrorIs(t, err, tc.want)
			require.False(t, helpers.AcquireAuthenticatedFrom(got))
			require.Empty(t, helpers.AcquireAuthenticatedUserIDFrom(got))
			require.Nil(t, helpers.AcquireSessionFrom(got))
			require.Nil(t, helpers.AcquireAPITokenFrom(got))
			require.False(t, IsExplicitBearerSession(got))
			require.Equal(t, "trace", got.Value(traceKey{}))
			require.Equal(t, "previous", helpers.AcquireAuthenticatedUserIDFrom(inherited))
			if tc.want == nil && tc.id != "" {
				require.Equal(t, tc.id, helpers.AcquireFrom(got))
				require.Equal(t, tc.userID, helpers.AcquireUserFrom(got).ID)
			} else {
				require.Empty(t, helpers.AcquireFrom(got))
				require.Nil(t, helpers.AcquireUserFrom(got))
			}
		})
	}
}
