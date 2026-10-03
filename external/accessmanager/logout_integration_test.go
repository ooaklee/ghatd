package accessmanager_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/ooaklee/ghatd/external/auth"
	"github.com/ooaklee/ghatd/external/ephemeral"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// TestLogoutLiveHTTP exercises real router, signed tokens, Mongo accounts and
// Redis records. Frozen-store assertions prove selected-record effects, not an
// atomic revocation guarantee against concurrent login/refresh.
func TestLogoutLiveHTTP(t *testing.T) {
	for _, other := range []bool{false, true} {
		for _, identity := range []string{"owner", "inactive", "stale revision", "foreign refresh", "bearer only", "no credentials", "repeated", "expired access"} {
			t.Run(map[bool]string{false: "normal", true: "others"}[other]+"/"+identity, func(t *testing.T) {
				f := newConnectionFixture(t)
				foreign, foreignTokens := f.newAccount(t, "foreign@example.test", "foreign")
				extra, err := f.auth.CreateToken(f.ctx, f.account)
				require.NoError(t, err)
				require.NoError(t, f.ephemeral.CreateAuth(f.ctx, f.account.ID, extra))
				path := "/api/v1/ams/logout"
				want := 200
				if other {
					path += "/other-sessions"
					want = 202
				}
				if identity == "inactive" {
					_, err = f.db.Collection("users").UpdateOne(f.ctx, bson.M{"_id": f.account.ID}, bson.M{"$set": bson.M{"status": "SUSPENDED"}})
					require.NoError(t, err)
					if other {
						want = 401
					}
				}
				if identity == "stale revision" {
					_, err = f.db.Collection("users").UpdateOne(f.ctx, bson.M{"_id": f.account.ID}, bson.M{"$inc": bson.M{"email_revision": 1}})
					require.NoError(t, err)
					if other {
						want = 401
					}
				}
				refresh := f.tokens.RefreshToken
				access := f.tokens.AccessToken
				if identity == "expired access" {
					parsed, err := jwt.Parse(access, func(*jwt.Token) (any, error) { return []byte("fixture-access"), nil })
					require.NoError(t, err)
					claims := parsed.Claims.(jwt.MapClaims)
					claims["exp"] = time.Now().Add(-time.Minute).Unix()
					access, err = jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte("fixture-access"))
					require.NoError(t, err)
				}
				if identity == "foreign refresh" {
					refresh = foreignTokens.RefreshToken
					want = 403
					if other {
						want = 401
					}
				}
				if identity == "no credentials" {
					want = 202
					if other {
						want = 401
					}
				}
				if identity == "bearer only" && other {
					want = 401
				}
				if identity == "repeated" && !other {
					_, err = f.ephemeral.DeleteAuth(f.ctx, f.account.ID+":"+f.tokens.AccessUUID)
					require.NoError(t, err)
					_, err = f.ephemeral.DeleteAuth(f.ctx, f.account.ID+":"+f.tokens.RefreshUUID)
					require.NoError(t, err)
				}
				r := httptest.NewRequest("GET", path+"?ActorID=forged&UserID="+foreign.ID, nil)
				if identity == "bearer only" {
					r.Header.Set("Authorization", "Bearer "+access)
				} else if identity != "no credentials" {
					r.AddCookie(&http.Cookie{Name: "access", Value: access})
					r.AddCookie(&http.Cookie{Name: "refresh", Value: refresh})
				}
				w := httptest.NewRecorder()
				f.router.ServeHTTP(w, r)
				require.Equal(t, want, w.Code, w.Body.String())
				check := func(owner, id string, present bool) {
					t.Helper()
					got, err := f.ephemeral.FetchAuth(f.ctx, &auth.TokenAccessDetails{UserID: owner, AccessUUID: id})
					if present {
						require.NoError(t, err)
						require.Equal(t, owner, got)
					} else {
						require.True(t, ephemeral.IsAuthNotFound(err), "%v", err)
					}
				}
				check(foreign.ID, foreignTokens.AccessUUID, true)
				check(foreign.ID, foreignTokens.RefreshUUID, true)
				success := want >= 200 && want < 300
				check(f.account.ID, extra.AccessUUID, !(other && success))
				check(f.account.ID, extra.RefreshUUID, !(other && success))
				removed := (!other && success && identity != "no credentials") || (other && identity == "expired access")
				check(f.account.ID, f.tokens.AccessUUID, !removed)
				check(f.account.ID, f.tokens.RefreshUUID, !removed || identity == "bearer only")
				if other && identity == "expired access" {
					for _, cookie := range w.Result().Cookies() {
						switch cookie.Name {
						case "access":
							details, err := f.auth.ExtractAccessTokenMetadataByString(f.ctx, cookie.Value)
							require.NoError(t, err)
							check(f.account.ID, details.AccessUUID, true)
						case "refresh":
							details, err := f.auth.ExtractRefreshTokenMetadataByString(f.ctx, cookie.Value)
							require.NoError(t, err)
							check(f.account.ID, details.RefreshUUID, true)
						}
					}
				}
				if success {
					require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
				}
			})
		}
	}
}
