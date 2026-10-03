package auth_test

import (
	"context"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/ooaklee/ghatd/external/auth"
	"github.com/stretchr/testify/require"
)

func TestSignedAudienceMetadataDoesNotInventSessionFreshness(t *testing.T) {
	for _, tc := range []struct {
		name        string
		value       any
		omit, valid bool
		want        []string
	}{
		{"absent legacy", nil, true, true, nil},
		{"string", "mobile", false, true, []string{"mobile"}},
		{"list", []string{"mobile", "api"}, false, true, []string{"mobile", "api"}},
		{"null", nil, false, false, nil}, {"number", 12, false, false, nil}, {"mixed list", []any{"mobile", 12}, false, false, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := auth.NewService(&auth.NewServiceRequest{AccessTokenSecret: "access", RefreshTokenSecret: "refresh"})
			wire := claimFixture(false)
			delete(wire, "iss")
			if tc.omit {
				delete(wire, "aud")
			} else {
				wire["aud"] = tc.value
			}
			raw, err := jwt.NewWithClaims(jwt.SigningMethodHS256, wire).SignedString([]byte("access"))
			require.NoError(t, err)
			details, err := svc.ExtractAccessTokenMetadataByString(context.Background(), raw)
			if !tc.valid {
				require.Error(t, err)
				require.Nil(t, details)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, details.Audience)
			require.Equal(t, "HS256", details.SigningAlgorithm)
			require.True(t, details.AuthenticationTime.IsZero(), "issuance time is not proof of a recent login")
		})
	}
}
