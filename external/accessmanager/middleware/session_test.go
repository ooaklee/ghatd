package middleware

import (
	"context"
	"errors"
	"testing"

	"github.com/ooaklee/ghatd/external/accessmanager"
	helpers "github.com/ooaklee/ghatd/external/accessmanager/helpers"
	"github.com/ooaklee/ghatd/external/apitoken"
	"github.com/ooaklee/ghatd/external/auth"
	"github.com/stretchr/testify/require"
)

// sessionAuthenticatorFunc supplies a case-owned verifier without HTTP effects.
type sessionAuthenticatorFunc func(context.Context, string) (*accessmanager.MiddlewareAuthedUserResponse, error)

// AuthenticateSession delegates exactly the selected credential and context.
func (f sessionAuthenticatorFunc) AuthenticateSession(ctx context.Context, credential string) (*accessmanager.MiddlewareAuthedUserResponse, error) {
	return f(ctx, credential)
}

// TestAuthenticateSessionContext covers strict publication, live verification,
// operational failures and inheritance isolation with independent case fixtures.
func TestAuthenticateSessionContext(t *testing.T) {
	outage := errors.New("private-store-unavailable")
	for _, tc := range []struct {
		// name identifies the verification or publication boundary.
		name string
		// change corrupts only this case's otherwise valid verifier result.
		change func(*accessmanager.MiddlewareAuthedUserResponse)
		// failure is returned unchanged by the verifier.
		failure error
		// nilContext, nilVerifier and emptyCredential are pre-verification failures.
		nilContext, nilVerifier, emptyCredential bool
		// nilResult and cancellation control independent lifecycle failures.
		nilResult, cancelBefore, cancelAfter bool
		// want is the native cause; nil means a complete published session.
		want error
	}{
		{name: "verified session"},
		{name: "legacy purpose remains compatible", change: func(r *accessmanager.MiddlewareAuthedUserResponse) { r.Token.TokenUse = "" }},
		{name: "nil context", nilContext: true, want: accessmanager.ErrSessionVerificationUnavailable},
		{name: "unwired verifier", nilVerifier: true, want: accessmanager.ErrSessionVerificationUnavailable},
		{name: "missing credential", emptyCredential: true, want: auth.ErrNoBearerHeaderFound},
		{name: "nil successful result", nilResult: true, want: accessmanager.ErrSessionVerificationUnavailable},
		{name: "cancelled before", cancelBefore: true, want: context.Canceled},
		{name: "cancelled during successful verifier", cancelAfter: true, want: context.Canceled},
		{name: "native outage", failure: outage, want: outage},
		{name: "known revocation", failure: accessmanager.ErrUnauthorizedTokenNotFoundInStore, want: accessmanager.ErrUnauthorizedTokenNotFoundInStore},
		{name: "anonymous result", change: func(r *accessmanager.MiddlewareAuthedUserResponse) { r.Authenticated = false }, want: auth.ErrUnauthorized},
		{name: "missing user", change: func(r *accessmanager.MiddlewareAuthedUserResponse) { r.User = nil }, want: auth.ErrUnauthorized},
		{name: "missing identity", change: func(r *accessmanager.MiddlewareAuthedUserResponse) { r.UserID = "" }, want: auth.ErrUnauthorized},
		{name: "wrong user", change: func(r *accessmanager.MiddlewareAuthedUserResponse) { r.User.ID = "other" }, want: auth.ErrUnauthorized},
		{name: "wrong token user", change: func(r *accessmanager.MiddlewareAuthedUserResponse) { r.Token.UserID = "other" }, want: auth.ErrUnauthorized},
		{name: "wrong token type", change: func(r *accessmanager.MiddlewareAuthedUserResponse) { r.Token.UserType = "other" }, want: auth.ErrUnauthorized},
		{name: "missing token", change: func(r *accessmanager.MiddlewareAuthedUserResponse) { r.Token = nil }, want: auth.ErrUnauthorized},
		{name: "missing session ID", change: func(r *accessmanager.MiddlewareAuthedUserResponse) { r.Token.AccessUUID = "" }, want: auth.ErrUnauthorized},
		{name: "wrong purpose", change: func(r *accessmanager.MiddlewareAuthedUserResponse) { r.Token.TokenUse = "email_verification" }, want: auth.ErrUnauthorized},
		{name: "API and session mixing", change: func(r *accessmanager.MiddlewareAuthedUserResponse) {
			r.APIToken = &apitoken.CredentialDetails{UserID: "member", TokenID: "api"}
		}, want: auth.ErrUnauthorized},
	} {
		t.Run(tc.name, func(t *testing.T) {
			type traceKey struct{}
			base, cancel := context.WithCancel(context.WithValue(context.Background(), traceKey{}, "trace"))
			defer cancel()
			prior := mockAuthedResp("previous", "ACTIVE", nil)
			prior.Token = &auth.TokenAccessDetails{UserID: "previous", AccessUUID: "prior-session"}
			input, err := ContextWithAuthentication(base, prior)
			require.NoError(t, err)
			result := mockAuthedResp("member", "ACTIVE", nil)
			result.Token = &auth.TokenAccessDetails{UserID: "member", AccessUUID: "session", TokenUse: auth.TokenUseAccess, Audience: []string{"mobile"}}
			if tc.change != nil {
				tc.change(result)
			}
			calls := 0
			var verifier SessionAuthenticator = sessionAuthenticatorFunc(func(ctx context.Context, credential string) (*accessmanager.MiddlewareAuthedUserResponse, error) {
				calls++
				require.Equal(t, "selected-credential", credential)
				require.Equal(t, "trace", ctx.Value(traceKey{}))
				require.False(t, helpers.AcquireAuthenticatedFrom(ctx))
				require.Empty(t, helpers.AcquireFrom(ctx))
				require.Nil(t, helpers.AcquireUserFrom(ctx))
				require.Nil(t, helpers.AcquireSessionFrom(ctx))
				require.Nil(t, helpers.AcquireAPITokenFrom(ctx))
				if tc.cancelAfter {
					cancel()
				}
				if tc.nilResult {
					return nil, nil
				}
				return result, tc.failure
			})
			if tc.nilVerifier {
				verifier = nil
			}
			ctx, credential := input, "selected-credential"
			if tc.nilContext {
				ctx = nil
			}
			if tc.emptyCredential {
				credential = ""
			}
			if tc.cancelBefore {
				cancel()
			}
			got, err := AuthenticateSessionContext(ctx, verifier, credential)
			if tc.nilContext || tc.nilVerifier || tc.emptyCredential || tc.cancelBefore {
				require.Zero(t, calls)
			} else {
				require.Equal(t, 1, calls)
			}
			require.NotNil(t, got)
			require.Equal(t, "previous", helpers.AcquireAuthenticatedUserIDFrom(input), "input context must not be mutated")
			if !tc.nilContext {
				require.Equal(t, "trace", got.Value(traceKey{}))
			}
			if tc.want != nil {
				require.ErrorIs(t, err, tc.want)
				require.False(t, helpers.AcquireAuthenticatedFrom(got))
				require.Empty(t, helpers.AcquireFrom(got))
				require.Nil(t, helpers.AcquireUserFrom(got))
				require.Nil(t, helpers.AcquireSessionFrom(got))
				require.Nil(t, helpers.AcquireAPITokenFrom(got))
				return
			}
			require.NoError(t, err)
			require.Equal(t, "member", helpers.AcquireAuthenticatedUserIDFrom(got))
			require.Equal(t, "session", helpers.AcquireSessionFrom(got).AccessUUID)
			result.Token.Audience[0] = "changed"
			result.Token.AccessUUID = "changed-session"
			result.Token.UserID = "changed-user"
			require.Equal(t, "session", helpers.AcquireSessionFrom(got).AccessUUID)
			require.Equal(t, "member", helpers.AcquireSessionFrom(got).UserID)
			require.Equal(t, []string{"mobile"}, helpers.AcquireSessionFrom(got).Audience)
		})
	}
}

// TestSessionPublicationClearsBearerTransportOrigin prevents re-publication of
// the same identity through a non-bearer adapter from inheriting bearer-only access.
func TestSessionPublicationClearsBearerTransportOrigin(t *testing.T) {
	for _, publisher := range []string{"selected session", "compatibility publisher"} {
		t.Run(publisher, func(t *testing.T) {
			result := mockAuthedResp("member", "ACTIVE", nil)
			result.Token = &auth.TokenAccessDetails{UserID: "member", AccessUUID: "session"}
			original, err := ContextWithAuthentication(context.Background(), result)
			require.NoError(t, err)
			original = context.WithValue(original, explicitBearerKey{}, explicitBearerOrigin{userID: "member", sessionID: "session"})
			require.True(t, IsExplicitBearerSession(original))
			var published context.Context
			if publisher == "selected session" {
				published, err = AuthenticateSessionContext(original, sessionAuthenticatorFunc(func(context.Context, string) (*accessmanager.MiddlewareAuthedUserResponse, error) { return result, nil }), "selected")
			} else {
				published, err = ContextWithAuthentication(original, result)
			}
			require.NoError(t, err)
			require.Equal(t, "member", helpers.AcquireAuthenticatedUserIDFrom(published))
			require.False(t, IsExplicitBearerSession(published))
			require.True(t, IsExplicitBearerSession(original), "publication must not mutate the input")
		})
	}
}
