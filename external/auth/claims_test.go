package auth_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/ooaklee/ghatd/external/auth"
	userv2 "github.com/ooaklee/ghatd/external/user/v2"
	"github.com/stretchr/testify/require"
)

// claimFixture returns fresh legacy-compatible wire fields for either family.
func claimFixture(refresh bool) jwt.MapClaims {
	claims := jwt.MapClaims{"sub": "member", "exp": time.Now().Add(time.Hour).Unix(), "iss": "issuer", "aud": "api"}
	if refresh {
		claims["refresh_uuid"] = "record"
	} else {
		claims["access_uuid"], claims["admin"], claims["authorized"] = "record", false, true
	}
	return claims
}

// claimService creates case-owned configuration with synthetic signing keys.
func claimService() *auth.Service {
	return auth.NewService(&auth.NewServiceRequest{AccessTokenSecret: "access", RefreshTokenSecret: "refresh", Issuer: "issuer", Audience: []string{"api"}})
}

func TestTypedClaimsAcrossCredentialFamilies(t *testing.T) {
	for _, tc := range []struct {
		name, purpose    string
		refresh, session bool
	}{
		{"session access", auth.TokenUseAccess, false, true}, {"session refresh", auth.TokenUseRefresh, true, false},
		{"login proof", auth.TokenUseLogin, false, false}, {"email proof", auth.TokenUseEmailVerification, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			audiences := []string{"api"}
			svc := auth.NewService(&auth.NewServiceRequest{AccessTokenSecret: "access", RefreshTokenSecret: "refresh", Issuer: "issuer", Audience: audiences})
			audiences[0] = "mutated configuration"
			user := &userv2.UniversalUser{ID: "stable-id", Type: userv2.UserConfigTypeWebApp, Status: userv2.AccountStatusKeyActive}
			require.Equal(t, "USER", user.GetType())
			require.Equal(t, "web_app", user.GetUserType())
			at := time.Now().Add(-time.Minute).Truncate(time.Second)
			var tokens *auth.TokenDetails
			var err error
			switch tc.purpose {
			case auth.TokenUseLogin:
				tokens, err = svc.CreateInitalToken(ctx, user)
			case auth.TokenUseEmailVerification:
				tokens, err = svc.CreateEmailVerificationToken(ctx, user)
			default:
				tokens, err = svc.CreateTokenWithAuthenticationTime(ctx, user, at)
			}
			require.NoError(t, err)
			raw, id := tokens.AccessToken, tokens.AccessUUID
			switch tc.purpose {
			case auth.TokenUseRefresh:
				raw, id = tokens.RefreshToken, tokens.RefreshUUID
			case auth.TokenUseLogin:
				raw, id = tokens.EphemeralToken, tokens.EphemeralUUID
			case auth.TokenUseEmailVerification:
				raw, id = tokens.EmailVerificationToken, tokens.EmailVerificationUUID
			}
			var parsed *jwt.Token
			if tc.refresh {
				parsed, err = svc.ParseRefreshTokenFromString(ctx, raw)
				require.NoError(t, err)
				details, err := svc.ExtractRefreshTokenMetadataByString(ctx, raw)
				require.NoError(t, err)
				require.Equal(t, user.ID, details.UserID)
				require.Equal(t, user.Type, details.UserType)
				require.Equal(t, tc.purpose, details.TokenUse)
				require.Equal(t, id, details.RefreshUUID)
				rotated, err := svc.CreateTokenWithAuthenticationTime(ctx, user, details.AuthenticationTime)
				require.NoError(t, err)
				next, err := svc.ExtractAccessTokenMetadataByString(ctx, rotated.AccessToken)
				require.NoError(t, err)
				require.True(t, at.Equal(next.AuthenticationTime))
				require.Equal(t, user.Type, next.UserType)
			} else {
				parsed, err = svc.ParseAccessTokenFromString(ctx, raw)
				require.NoError(t, err)
				details, err := svc.ExtractAccessTokenMetadataByString(ctx, raw)
				require.NoError(t, err)
				require.Equal(t, user.ID, details.UserID)
				require.Equal(t, user.Type, details.UserType)
				require.Equal(t, tc.purpose, details.TokenUse)
				require.Equal(t, tc.session, details.IsSessionCredential())
				require.Equal(t, []string{"api"}, details.Audience)
				require.Equal(t, "issuer", details.Issuer)
				if tc.session {
					require.True(t, at.Equal(details.AuthenticationTime))
				} else {
					require.True(t, details.AuthenticationTime.IsZero())
				}
			}
			wire := parsed.Claims.(jwt.MapClaims)
			require.Equal(t, id, wire["jti"])
			require.Equal(t, user.ID, wire["sub"])
			require.Equal(t, "issuer", wire["iss"])
			require.Equal(t, []interface{}{"api"}, wire["aud"])
			require.NotNil(t, wire["iat"])
		})
	}
}

func TestMalformedIdentityClaimsAndConfiguredTrustFailClosed(t *testing.T) {
	for _, family := range []string{"access", "refresh"} {
		t.Run(family, func(t *testing.T) {
			for _, tc := range []struct {
				name, key string
				value     any
				omit      bool
			}{
				{"boolean type", "user_type", true, false}, {"null type", "user_type", nil, false}, {"empty type", "user_type", "", false},
				{"whitespace type", "user_type", "web app", false}, {"control type", "user_type", "web\x00app", false},
				{"oversize type", "user_type", strings.Repeat("x", 129), false},
				{"unknown purpose", "token_use", "unknown", false}, {"null purpose", "token_use", nil, false}, {"empty purpose", "token_use", "", false},
				{"different token id", "jti", "wrong", false}, {"null token id", "jti", nil, false}, {"object token id", "jti", map[string]string{"id": "record"}, false},
				{"wrong issuer", "iss", "other", false}, {"missing issuer", "iss", nil, true},
				{"wrong audience", "aud", "other", false}, {"missing audience", "aud", nil, true}, {"null audience", "aud", nil, false},
				{"null expiry", "exp", nil, false}, {"missing expiry", "exp", nil, true},
				{"future issuance", "iat", time.Now().Add(time.Hour).Unix(), false}, {"empty subject", "sub", "", false},
				{"empty record", "record_id", "", false},
			} {
				t.Run(tc.name, func(t *testing.T) {
					wire := claimFixture(family == "refresh")
					key := tc.key
					if key == "record_id" {
						key = "access_uuid"
						if family == "refresh" {
							key = "refresh_uuid"
						}
					}
					if tc.omit {
						delete(wire, key)
					} else {
						wire[key] = tc.value
					}
					raw, err := jwt.NewWithClaims(jwt.SigningMethodHS256, wire).SignedString([]byte(family))
					require.NoError(t, err)
					svc := claimService()
					if family == "refresh" {
						_, err = svc.ExtractRefreshTokenMetadataByString(context.Background(), raw)
					} else {
						_, err = svc.ExtractAccessTokenMetadataByString(context.Background(), raw)
					}
					require.Error(t, err)
				})
			}
		})
	}
}

func TestCredentialPurposeBoundaries(t *testing.T) {
	for _, family := range []string{"access", "refresh"} {
		for _, purpose := range []string{"", auth.TokenUseAccess, auth.TokenUseRefresh, auth.TokenUseLogin, auth.TokenUseEmailVerification} {
			name := purpose
			if name == "" {
				name = "legacy absent"
			}
			t.Run(family+"/"+name, func(t *testing.T) {
				wire := claimFixture(family == "refresh")
				if purpose != "" {
					wire["token_use"] = purpose
				}
				raw, err := jwt.NewWithClaims(jwt.SigningMethodHS256, wire).SignedString([]byte(family))
				require.NoError(t, err)
				svc := claimService()
				allowed := purpose == "" || (family == "access" && purpose != auth.TokenUseRefresh) || (family == "refresh" && purpose == auth.TokenUseRefresh)
				if family == "refresh" {
					details, err := svc.ExtractRefreshTokenMetadataByString(context.Background(), raw)
					if !allowed {
						require.Error(t, err)
						require.Nil(t, details)
						return
					}
					require.NoError(t, err)
					require.Empty(t, details.UserType)
					require.True(t, details.AuthenticationTime.IsZero())
				} else {
					details, err := svc.ExtractAccessTokenMetadataByString(context.Background(), raw)
					if !allowed {
						require.Error(t, err)
						require.Nil(t, details)
						return
					}
					require.NoError(t, err)
					require.Empty(t, details.UserType)
					require.True(t, details.AuthenticationTime.IsZero())
					require.Equal(t, purpose == "" || purpose == auth.TokenUseAccess, details.IsSessionCredential())
				}
			})
		}
	}
}

func TestSigningMethodPolicy(t *testing.T) {
	for _, family := range []string{"access", "refresh"} {
		for _, method := range []jwt.SigningMethod{jwt.SigningMethodHS256, jwt.SigningMethodHS384, jwt.SigningMethodHS512} {
			t.Run(family+"/"+method.Alg(), func(t *testing.T) {
				raw, err := jwt.NewWithClaims(method, claimFixture(family == "refresh")).SignedString([]byte(family))
				require.NoError(t, err)
				svc := claimService()
				if family == "refresh" {
					_, err = svc.ParseRefreshTokenFromString(context.Background(), raw)
				} else {
					_, err = svc.ParseAccessTokenFromString(context.Background(), raw)
				}
				if method == jwt.SigningMethodHS256 {
					require.NoError(t, err)
				} else {
					require.ErrorIs(t, err, auth.ErrUnauthorizedTokenUnexpectedSigningMethod)
				}
			})
		}
	}
}

// legacyClaimUser deliberately does not implement the optional type capability.
type legacyClaimUser struct{}

func (legacyClaimUser) GetUserId() string     { return "member" }
func (legacyClaimUser) IsAdmin() bool         { return false }
func (legacyClaimUser) GetUserStatus() string { return "ACTIVE" }

func TestOptionalUserTypeAndSessionClassification(t *testing.T) {
	for _, tc := range []struct {
		name string
		user auth.UserModel
		want string
	}{
		{"legacy implementation", legacyClaimUser{}, ""},
		{"legacy stored account", &userv2.UniversalUser{ID: "member"}, ""},
		{"typed stored account", &userv2.UniversalUser{ID: "member", Type: "partner"}, "partner"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := claimService()
			tokens, err := svc.CreateToken(context.Background(), tc.user)
			require.NoError(t, err)
			details, err := svc.ExtractAccessTokenMetadataByString(context.Background(), tokens.AccessToken)
			require.NoError(t, err)
			require.Equal(t, tc.want, details.UserType)
		})
	}
}

func TestMatchesUserType(t *testing.T) {
	for _, tc := range []struct {
		name, signed string
		user         auth.UserTypeProvider
		want         bool
	}{
		{"legacy claim", "", &userv2.UniversalUser{Type: "partner"}, true},
		{"no type asserted", "", nil, true},
		{"same type", "partner", &userv2.UniversalUser{Type: "partner"}, true},
		{"changed type", "partner", &userv2.UniversalUser{Type: "web_app"}, false},
		{"missing current account", "partner", nil, false},
		{"typed nil stored account", "partner", (*userv2.UniversalUser)(nil), false},
	} {
		t.Run(tc.name, func(t *testing.T) { require.Equal(t, tc.want, auth.MatchesUserType(tc.signed, tc.user)) })
	}
}

func TestInvalidMetadataObjects(t *testing.T) {
	for _, tc := range []struct {
		name  string
		token *jwt.Token
	}{
		{"nil", nil}, {"unverified", &jwt.Token{Claims: claimFixture(false)}},
		{"wrong claims implementation", &jwt.Token{Valid: true, Method: jwt.SigningMethodHS256, Claims: jwt.RegisteredClaims{}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := claimService()
			_, err := svc.CheckAccessTokenValidityGetDetails(context.Background(), tc.token)
			require.Error(t, err)
			_, err = svc.GetRefreshTokenUUID(context.Background(), tc.token)
			require.Error(t, err)
		})
	}
}

func TestStoredUserTypeValidation(t *testing.T) {
	for _, tc := range []struct {
		name, value string
		valid       bool
	}{
		{"legacy absent", "", true}, {"built in", "web_app", true}, {"custom", "partner:custom-v2", true},
		{"byte boundary", strings.Repeat("x", 128), true}, {"oversize", strings.Repeat("x", 129), false},
		{"whitespace", "bad type", false}, {"invalid UTF8", string([]byte{0xff}), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := claimService()
			tokens, err := svc.CreateToken(context.Background(), &userv2.UniversalUser{ID: "member", Type: tc.value})
			if !tc.valid {
				require.Error(t, err)
				require.Nil(t, tokens)
				return
			}
			require.NoError(t, err)
			details, err := svc.ExtractAccessTokenMetadataByString(context.Background(), tokens.AccessToken)
			require.NoError(t, err)
			require.Equal(t, tc.value, details.UserType)
		})
	}
}
