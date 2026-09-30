package auth

import (
	"context"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
)

type revisionUser struct{ freshnessUser }

func (revisionUser) GetEmailRevision() int64 { return 3 }

func TestEmailRevisionSurvivesEveryCredentialType(t *testing.T) {
	ctx := context.Background()
	s := NewService(&NewServiceRequest{AccessTokenSecret: "access", RefreshTokenSecret: "refresh"})
	initial, err := s.CreateInitalToken(ctx, revisionUser{})
	require.NoError(t, err)
	verification, err := s.CreateEmailVerificationToken(ctx, revisionUser{})
	require.NoError(t, err)
	session, err := s.CreateTokenWithAuthenticationTime(ctx, revisionUser{}, time.Now())
	require.NoError(t, err)
	for _, raw := range []string{initial.EphemeralToken, verification.EmailVerificationToken, session.AccessToken} {
		details, err := s.ExtractAccessTokenMetadataByString(ctx, raw)
		require.NoError(t, err)
		require.EqualValues(t, 3, details.EmailRevision)
	}
	refresh, err := s.ExtractRefreshTokenMetadataByString(ctx, session.RefreshToken)
	require.NoError(t, err)
	require.EqualValues(t, 3, refresh.EmailRevision)
}

func TestEmailRevisionClaimValidationAndLegacyCompatibility(t *testing.T) {
	ctx := context.Background()
	s := NewService(&NewServiceRequest{AccessTokenSecret: "access", RefreshTokenSecret: "refresh"})
	for _, test := range []struct {
		name           string
		value          interface{}
		present, valid bool
		want           int64
	}{
		{"legacy", nil, false, true, 0}, {"zero", 0, true, true, 0}, {"current", 3, true, true, 3},
		{"negative", -1, true, false, 0}, {"fractional", 1.5, true, false, 0},
		{"overflow", 9007199254740992, true, false, 0}, {"string", "3", true, false, 0},
		{"null", nil, true, false, 0}, {"boolean", true, true, false, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			claims := jwt.MapClaims{"exp": time.Now().Add(time.Minute).Unix(), "sub": "user", "access_uuid": "a", "refresh_uuid": "r", "admin": false, "authorized": true}
			if test.present {
				claims["email_revision"] = test.value
			}
			for _, kind := range []string{"access", "refresh"} {
				raw, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(kind))
				require.NoError(t, err)
				var revision int64
				if kind == "access" {
					details, parseErr := s.ExtractAccessTokenMetadataByString(ctx, raw)
					err = parseErr
					if details != nil {
						revision = details.EmailRevision
					}
				} else {
					details, parseErr := s.ExtractRefreshTokenMetadataByString(ctx, raw)
					err = parseErr
					if details != nil {
						revision = details.EmailRevision
					}
				}
				if test.valid {
					require.NoError(t, err)
					require.Equal(t, test.want, revision)
				} else {
					require.Error(t, err)
				}
			}
		})
	}
}
