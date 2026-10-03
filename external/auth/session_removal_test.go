package auth_test

import (
	"context"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/ooaklee/ghatd/external/auth"
	"github.com/stretchr/testify/require"
)

func TestSessionRemovalVerification(t *testing.T) {
	for _, use := range []string{auth.TokenUseAccess, auth.TokenUseRefresh} {
		for _, name := range []string{"valid", "expired", "legacy purpose", "inactive snapshot", "future expiry", "wrong signature", "wrong purpose", "wrong issuer", "wrong audience", "missing expiry", "malformed expiry", "future issued at", "future not before", "missing owner", "blank owner", "namespace owner", "namespace ID", "wrong jti", "wrong type", "wrong revision", "wrong algorithm", "nil service", "nil context", "canceled", "empty key", "invalid use"} {
			t.Run(use+"/"+name, func(t *testing.T) {
				s := auth.NewService(&auth.NewServiceRequest{AccessTokenSecret: "fixture-key", RefreshTokenSecret: "fixture-key", Issuer: "fixture", Audience: []string{"client"}})
				ctx, cancel := context.WithCancel(context.Background())
				t.Cleanup(cancel)
				claims := jwt.MapClaims{"sub": "owner", "jti": "record", "token_use": use, "email_revision": float64(0), "iss": "fixture", "aud": "client", "exp": time.Now().Add(time.Hour).Unix(), "iat": time.Now().Add(-time.Minute).Unix(), "admin": false, "authorized": true}
				idKey := "access_uuid"
				if use == auth.TokenUseRefresh {
					idKey = "refresh_uuid"
				}
				claims[idKey] = "record"
				key := "fixture-key"
				method := jwt.SigningMethodHS256
				allowed := false
				selected := use
				switch name {
				case "valid", "future expiry":
					allowed = true
				case "expired":
					claims["exp"] = time.Now().Add(-time.Hour).Unix()
					allowed = true
				case "legacy purpose":
					delete(claims, "token_use")
					allowed = true
				case "inactive snapshot":
					claims["authorized"] = false
					allowed = true
				case "wrong signature":
					key = "not-the-key"
				case "wrong purpose":
					claims["token_use"] = auth.TokenUseLogin
				case "wrong issuer":
					claims["iss"] = "other"
				case "wrong audience":
					claims["aud"] = "other"
				case "missing expiry":
					delete(claims, "exp")
				case "malformed expiry":
					claims["exp"] = "expired"
				case "future issued at":
					claims["iat"] = time.Now().Add(time.Hour).Unix()
				case "future not before":
					claims["nbf"] = time.Now().Add(time.Hour).Unix()
					claims["exp"] = time.Now().Add(-time.Hour).Unix()
				case "missing owner":
					delete(claims, "sub")
				case "blank owner":
					claims["sub"] = " "
				case "namespace owner":
					claims["sub"] = "owner:foreign"
				case "namespace ID":
					claims[idKey] = "nested:record"
					claims["jti"] = "nested:record"
				case "wrong jti":
					claims["jti"] = "different"
				case "wrong type":
					claims["user_type"] = float64(10)
				case "wrong revision":
					claims["email_revision"] = float64(-1)
				case "wrong algorithm":
					method = jwt.SigningMethodHS512
				case "nil service":
					s = nil
				case "nil context":
					ctx = nil
				case "canceled":
					cancel()
				case "empty key":
					s = auth.NewService(&auth.NewServiceRequest{})
				case "invalid use":
					selected = auth.TokenUseLogin
				}
				raw, err := jwt.NewWithClaims(method, claims).SignedString([]byte(key))
				require.NoError(t, err)
				result, err := s.ExtractSessionRemovalMetadata(ctx, raw, selected)
				if !allowed {
					require.Error(t, err)
					require.Nil(t, result)
					return
				}
				require.NoError(t, err)
				require.Equal(t, &auth.SessionRemovalDetails{UserID: "owner", TokenID: "record", TokenUse: use}, result)
				if name == "expired" {
					if use == auth.TokenUseAccess {
						_, err = s.ExtractAccessTokenMetadataByString(ctx, raw)
					} else {
						_, err = s.ExtractRefreshTokenMetadataByString(ctx, raw)
					}
					require.Error(t, err, "deletion authority must not upgrade admission")
				}
			})
		}
	}
}
