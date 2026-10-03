package accessmanager_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/accessmanager"
	"github.com/ooaklee/ghatd/external/auth"
	"github.com/ooaklee/ghatd/external/ephemeral"
	userv2 "github.com/ooaklee/ghatd/external/user/v2"
	"github.com/stretchr/testify/require"
)

// sessionClaimsStub isolates credential selection from cryptographic tests.
type sessionClaimsStub struct {
	accessmanager.AuthService
	details *auth.TokenAccessDetails
	err     error
	seen    string
}

func (s *sessionClaimsStub) ExtractAccessTokenMetadataByString(_ context.Context, credential string) (*auth.TokenAccessDetails, error) {
	s.seen = credential
	return s.details, s.err
}

func (s *sessionClaimsStub) ExtractTokenMetadata(context.Context, *http.Request) (*auth.TokenAccessDetails, error) {
	return s.details, s.err
}

// This single stateful compatibility lifecycle intentionally retains successive
// calls across two transports. The related boundary matrix lives in
// service_session_guards_test.go; splitting this sequence would lose the shared
// session's revocation/reactivation and claim-change assertions.
func TestAuthenticateSessionMatchesHTTPAndRechecksLiveAuthority(t *testing.T) {
	ctx := context.Background()
	claims := &sessionClaimsStub{details: &auth.TokenAccessDetails{UserID: "member", AccessUUID: "session", EmailRevision: 3, AuthenticationTime: time.Now().Add(-time.Minute), Audience: []string{"native"}, SigningAlgorithm: "HS256"}}
	user := &userv2.UniversalUser{ID: "member", EmailRevision: 3, Status: userv2.AccountStatusKeyActive}
	revoked, calls := false, 0
	store := &refreshEphemeralStoreMock{fetchAuthFunc: func(_ context.Context, details ephemeral.TokenDetailsAccess) (string, error) {
		calls++
		require.Equal(t, "session", details.GetTokenAccessUuid())
		if revoked {
			return "", ephemeral.ErrAuthNotFound
		}
		return "member", nil
	}}
	service := &accessmanager.Service{AuthService: claims, EphemeralStore: store, UserService: &refreshUserServiceMock{user: user}}
	first, err := service.AuthenticateSession(ctx, "selected-credential")
	require.NoError(t, err)
	require.Equal(t, "selected-credential", claims.seen)
	require.True(t, first.Authenticated)
	require.Equal(t, claims.details, first.Token)
	request := httptest.NewRequest("GET", "/", nil)
	second, err := service.MiddlewareJWTRequired(request)
	require.NoError(t, err)
	require.Equal(t, first, second)
	revoked = true
	_, err = service.AuthenticateSession(ctx, "selected-credential")
	require.ErrorIs(t, err, accessmanager.ErrUnauthorizedTokenNotFoundInStore)
	require.Equal(t, 3, calls)
	revoked = false
	user.EmailRevision++
	_, err = service.AuthenticateSession(ctx, "selected-credential")
	require.ErrorIs(t, err, accessmanager.ErrOAuthReauthenticationRequired)
	user.EmailRevision--
	claims.details.UserType = "web_app"
	user.Type = "api_service"
	_, err = service.AuthenticateSession(ctx, "selected-credential")
	require.ErrorIs(t, err, accessmanager.ErrOAuthReauthenticationRequired)
	user.Type = "web_app"
	_, err = service.AuthenticateSession(ctx, "selected-credential")
	require.NoError(t, err)
	claims.details.TokenUse = auth.TokenUseLogin
	_, err = service.AuthenticateSession(ctx, "selected-credential")
	require.ErrorIs(t, err, auth.ErrUnauthorized)
	claims.details.TokenUse = auth.TokenUseAccess
	user.ID = "different"
	_, err = service.AuthenticateSession(ctx, "selected-credential")
	require.ErrorIs(t, err, auth.ErrUnauthorized)
	claims.details = nil
	_, err = service.AuthenticateSession(ctx, "selected-credential")
	require.ErrorIs(t, err, auth.ErrUnauthorized)
	_, err = service.AuthenticateSession(ctx, "")
	require.ErrorIs(t, err, auth.ErrNoBearerHeaderFound)
}
