package accessmanager_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/accessmanager"
	"github.com/ooaklee/ghatd/external/auth"
	"github.com/ooaklee/ghatd/external/ephemeral"
	user "github.com/ooaklee/ghatd/external/user/v2"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// interleavedLoginRepository introduces a real concurrent update after the domain
// read, immediately before its conditional write. It never synthesizes receipts.
type interleavedLoginRepository struct {
	*user.Repository
	before func()
}

func (r *interleavedLoginRepository) SetFreshLogin(ctx context.Context, c *user.SetLoginStateRequest) (*user.UniversalUser, error) {
	r.before()
	return r.Repository.SetFreshLogin(ctx, c)
}
func (r *interleavedLoginRepository) SetVerifiedEmailActivation(ctx context.Context, c *user.SetLoginStateRequest) (*user.UniversalUser, error) {
	r.before()
	return r.Repository.SetVerifiedEmailActivation(ctx, c)
}

func TestLoginStateProofHTTPIntegration(t *testing.T) {
	for _, flow := range []string{"active", "activation", "verification"} {
		cases := []string{"success", "legacy type revision", "profile update", "status change", "revision change", "email change", "type change"}
		if flow != "active" {
			cases = append(cases, "competing activation")
		}
		for _, name := range cases {
			t.Run(flow+"/"+name, func(t *testing.T) {
				f := newConnectionFixture(t)
				activate := flow != "active"
				if name == "legacy type revision" {
					_, err := f.db.Collection("users").UpdateOne(f.ctx, bson.M{"_id": f.account.ID}, bson.M{"$unset": bson.M{"type": "", "email_revision": ""}})
					require.NoError(t, err)
					f.account = f.current(t)
				}
				if name == "profile update" {
					_, err := f.db.Collection("users").UpdateOne(f.ctx, bson.M{"_id": f.account.ID}, bson.M{"$set": bson.M{"roles": bson.A{"ADMIN"}}})
					require.NoError(t, err)
					f.account = f.current(t)
					require.True(t, f.account.IsAdmin())
				}
				var proof *auth.TokenDetails
				var err error
				path := accessmanager.APIAccessManagerUserLogin
				if activate {
					_, err = f.db.Collection("users").UpdateOne(f.ctx, bson.M{"_id": f.account.ID}, bson.M{"$set": bson.M{"status": user.AccountStatusKeyProvisioned, "verification.email_verified": false}})
					require.NoError(t, err)
					f.account = f.current(t)
					proof, err = f.auth.CreateEmailVerificationToken(f.ctx, f.account)
				} else {
					proof, err = f.auth.CreateInitalToken(f.ctx, f.account)
				}
				require.NoError(t, err)
				token, key, ttl := proof.EphemeralToken, proof.EphemeralUUID, proof.EtTTL
				if activate {
					token, key, ttl = proof.EmailVerificationToken, proof.EmailVerificationUUID, proof.EvTTL
				}
				if flow == "verification" {
					path = accessmanager.APIAccessManagerUserEmail
				}
				require.NoError(t, f.ephemeral.StoreToken(f.ctx, key, f.account.ID, ttl))
				f.users.UserRepository = &interleavedLoginRepository{Repository: f.users.UserRepository.(*user.Repository), before: func() {
					fields := bson.M{}
					switch name {
					case "profile update":
						fields["personal_info.first_name"] = "Concurrent"
						fields["metadata.updated_at"] = "profile-change"
						fields["roles"] = bson.A{}
					case "status change":
						fields["status"] = user.AccountStatusKeySuspended
					case "revision change":
						fields["email_revision"] = f.account.EmailRevision + 1
					case "email change":
						fields["email"] = "changed@example.test"
					case "type change":
						fields["type"] = "other"
					case "competing activation":
						fields["status"] = user.AccountStatusKeyActive
					}
					if len(fields) > 0 {
						_, err := f.db.Collection("users").UpdateOne(f.ctx, bson.M{"_id": f.account.ID}, bson.M{"$set": fields})
						require.NoError(t, err)
					}
				}}
				request := httptest.NewRequest(http.MethodGet, "https://app.example/api/v1/ams"+path+"?t="+url.QueryEscape(token), nil)
				response := httptest.NewRecorder()
				f.router.ServeHTTP(response, request)
				ok := name == "success" || name == "profile update" || name == "legacy type revision"
				want := 409
				if ok {
					want = 200
				}
				require.Equal(t, want, response.Code, response.Body.String())
				require.Equal(t, "no-store", response.Header().Get("Cache-Control"))
				details, err := f.auth.ExtractAccessTokenMetadataByString(f.ctx, token)
				require.NoError(t, err)
				_, err = f.ephemeral.FetchAuth(f.ctx, details)
				require.True(t, ephemeral.IsAuthNotFound(err), "proof not consumed")
				if !ok {
					require.Empty(t, response.Result().Cookies())
					require.Contains(t, response.Body.String(), "USV2-040")
					return
				}
				var access string
				for _, cookie := range response.Result().Cookies() {
					if cookie.Name == "access" {
						access = cookie.Value
					}
				}
				require.NotEmpty(t, access)
				claims, err := f.auth.ExtractAccessTokenMetadataByString(f.ctx, access)
				require.NoError(t, err)
				stored := f.current(t)
				at, err := time.Parse(time.RFC3339Nano, stored.Metadata.LastFreshLoginAt)
				require.NoError(t, err)
				require.Equal(t, at.Unix(), claims.AuthenticationTime.Unix())
				_, err = f.service.AuthenticateSession(f.ctx, access)
				require.NoError(t, err)
				if name == "profile update" {
					require.False(t, claims.IsAdmin, "session used the stale pre-write administrator role")
					require.Equal(t, "Concurrent", stored.PersonalInfo.FirstName)
					require.Empty(t, stored.Roles)
					if !activate {
						require.Equal(t, "profile-change", stored.Metadata.UpdatedAt)
					}
				}
				if activate {
					require.True(t, stored.Verification.EmailVerified)
					require.Equal(t, user.AccountStatusKeyActive, stored.Status)
				}
			})
		}
	}
}
