package accessmanager_test

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/accessmanager"
	"github.com/ooaklee/ghatd/external/auth"
	"github.com/ooaklee/ghatd/external/ephemeral"
	"github.com/ooaklee/ghatd/external/toolbox"
	user "github.com/ooaklee/ghatd/external/user/v2"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// TestSessionGuardsLiveAuthority uses real signed JWTs, namespaced Redis
// sessions and case-owned Mongo accounts. Changes happen after token issuance;
// no test mutates a deployed account or clears a shared Redis database.
func TestSessionGuardsLiveAuthority(t *testing.T) {
	for _, tc := range []struct {
		name string
		want error
	}{
		{"unchanged", nil},
		{"promoted", nil},
		{"demoted", accessmanager.ErrUnauthorizedAdminAccessAttempted},
		{"suspended", accessmanager.ErrUnauthorizedNonActiveStatus},
		{"revoked", accessmanager.ErrUnauthorizedTokenNotFoundInStore},
		{"wrong session owner", accessmanager.ErrSessionVerificationUnavailable},
		{"email revision changed", accessmanager.ErrOAuthReauthenticationRequired},
		{"user type changed", accessmanager.ErrOAuthReauthenticationRequired},
		{"account deleted", user.ErrUserNotFound},
		{"Redis unavailable", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newConnectionFixture(t)
			collection := f.db.Collection(user.UserCollection)
			if tc.name != "promoted" {
				_, err := collection.UpdateOne(f.ctx, bson.M{"_id": f.account.ID}, bson.M{"$set": bson.M{"roles": []string{user.UserRoleAdmin}}})
				require.NoError(t, err)
			}
			current := f.current(t)
			tokens, err := f.auth.CreateTokenWithAuthenticationTime(f.ctx, current, time.Now())
			require.NoError(t, err)
			require.NoError(t, f.ephemeral.CreateAuth(f.ctx, current.ID, tokens))
			r := httptest.NewRequest("GET", "/protected", nil)
			r.Header.Set("Authorization", "Bearer "+tokens.AccessToken)
			before, err := f.service.MiddlewareActiveJWTRequired(r)
			require.NoError(t, err)
			require.Equal(t, tc.name != "promoted", before.Token.IsAdmin)
			var change bson.M
			switch tc.name {
			case "promoted":
				change = bson.M{"roles": []string{user.UserRoleAdmin}}
			case "demoted":
				change = bson.M{"roles": []string{user.UserRoleUser}}
			case "suspended":
				change = bson.M{"status": user.AccountStatusKeySuspended}
			case "email revision changed":
				change = bson.M{"email_revision": current.EmailRevision + 1}
			case "user type changed":
				change = bson.M{"type": "different-account-type"}
			case "revoked":
				deleted, err := f.ephemeral.DeleteAuth(f.ctx, toolbox.CombinedUuidFormat(current.ID, tokens.AccessUUID))
				require.NoError(t, err)
				require.EqualValues(t, 1, deleted)
			case "wrong session owner":
				key := f.namespace + "-test_" + toolbox.CombinedUuidFormat(current.ID, tokens.AccessUUID)
				require.NoError(t, f.redis.Set(key, "another-owner", time.Minute).Err())
			case "account deleted":
				_, err := collection.DeleteOne(f.ctx, bson.M{"_id": current.ID})
				require.NoError(t, err)
			case "Redis unavailable":
				require.NoError(t, f.redis.Close())
			}
			if change != nil {
				_, err := collection.UpdateOne(f.ctx, bson.M{"_id": current.ID}, bson.M{"$set": change})
				require.NoError(t, err)
			}
			got, err := f.service.MiddlewareAdminJWTRequired(r)
			if tc.name == "Redis unavailable" {
				require.Error(t, err)
				require.NotErrorIs(t, err, accessmanager.ErrUnauthorizedTokenNotFoundInStore)
				require.False(t, ephemeral.IsAuthNotFound(err))
				require.Nil(t, got)
				return
			}
			require.ErrorIs(t, err, tc.want)
			if tc.want == nil {
				require.True(t, got.User.IsAdmin())
				require.Equal(t, current.ID, got.UserID)
			} else {
				require.Nil(t, got)
			}
			if tc.name == "revoked" || tc.name == "wrong session owner" {
				for _, mode := range []string{"standard", "active", "optional"} {
					var result *accessmanager.MiddlewareAuthedUserResponse
					var guardErr error
					switch mode {
					case "standard":
						result, guardErr = f.service.MiddlewareJWTRequired(r)
					case "active":
						result, guardErr = f.service.MiddlewareActiveJWTRequired(r)
					case "optional":
						result, guardErr = f.service.MiddlewareRateLimitOrActiveJWTRequired(r)
					}
					require.ErrorIs(t, guardErr, tc.want, mode)
					require.Nil(t, result, mode)
				}
				_, err = f.service.AuthenticateSession(f.ctx, tokens.AccessToken)
				require.ErrorIs(t, err, tc.want)
			}
			// The verifier still parses the original token: no refresh or token
			// replacement was used to make promotion/demotion tests pass.
			parsed, err := f.auth.ExtractAccessTokenMetadataByString(f.ctx, tokens.AccessToken)
			require.NoError(t, err)
			require.Equal(t, auth.TokenUseAccess, parsed.TokenUse)
			require.Equal(t, before.Token.IsAdmin, parsed.IsAdmin)
		})
	}
}
