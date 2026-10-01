package auth

import (
	"context"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

type freshnessUser struct{}

func (freshnessUser) GetUserId() string     { return "user" }
func (freshnessUser) IsAdmin() bool         { return false }
func (freshnessUser) GetUserStatus() string { return "ACTIVE" }

func TestSessionAuthenticationTime(t *testing.T) {
	service := NewService(&NewServiceRequest{AccessTokenSecret: "test-access-secret", RefreshTokenSecret: "test-refresh-secret"})
	for _, at := range []time.Time{time.Time{}, time.Now().Add(-time.Hour).Truncate(time.Second), time.Now().Add(-time.Second).Truncate(time.Second)} {
		tokens, err := service.CreateTokenWithAuthenticationTime(context.Background(), freshnessUser{}, at)
		require.NoError(t, err)
		access, err := service.ExtractAccessTokenMetadataByString(context.Background(), tokens.AccessToken)
		require.NoError(t, err)
		require.True(t, access.AuthenticationTime.Equal(at))
		refresh, err := service.ExtractRefreshTokenMetadataByString(context.Background(), tokens.RefreshToken)
		require.NoError(t, err)
		require.True(t, refresh.AuthenticationTime.Equal(at))
		rotated, err := service.CreateTokenWithAuthenticationTime(context.Background(), freshnessUser{}, refresh.AuthenticationTime)
		require.NoError(t, err)
		details, err := service.ExtractAccessTokenMetadataByString(context.Background(), rotated.AccessToken)
		require.NoError(t, err)
		require.True(t, details.AuthenticationTime.Equal(at))
	}
	legacy, err := service.CreateToken(context.Background(), freshnessUser{})
	require.NoError(t, err)
	details, err := service.ExtractAccessTokenMetadataByString(context.Background(), legacy.AccessToken)
	require.NoError(t, err)
	require.True(t, details.AuthenticationTime.IsZero())
	_, err = service.CreateTokenWithAuthenticationTime(context.Background(), freshnessUser{}, time.Now().Add(time.Hour))
	require.Error(t, err)
}

func TestMalformedAuthenticationTimeIsRejected(t *testing.T) {
	service := NewService(&NewServiceRequest{AccessTokenSecret: "access", RefreshTokenSecret: "refresh"})
	for _, value := range []interface{}{"123", true, nil, 0, -1, 1.5, time.Now().Add(time.Hour).Unix()} {
		access := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"exp": time.Now().Add(time.Minute).Unix(), "sub": "user", "access_uuid": "access", "admin": false, "authorized": true, "auth_time": value})
		raw, err := access.SignedString([]byte("access"))
		require.NoError(t, err)
		_, err = service.ExtractAccessTokenMetadataByString(context.Background(), raw)
		require.Error(t, err)
		refresh := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"exp": time.Now().Add(time.Minute).Unix(), "sub": "user", "refresh_uuid": "refresh", "auth_time": value})
		raw, err = refresh.SignedString([]byte("refresh"))
		require.NoError(t, err)
		_, err = service.ExtractRefreshTokenMetadataByString(context.Background(), raw)
		require.Error(t, err)
	}
	forged := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"exp": time.Now().Add(time.Minute).Unix(), "sub": "user", "access_uuid": "access", "admin": false, "authorized": true, "auth_time": time.Now().Unix()})
	raw, err := forged.SignedString([]byte("attacker"))
	require.NoError(t, err)
	_, err = service.ExtractAccessTokenMetadataByString(context.Background(), raw)
	require.Error(t, err)
}
