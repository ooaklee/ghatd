package accessmanager_test

import (
	"encoding/json"
	"regexp"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/accessmanager"
	"github.com/ooaklee/ghatd/external/oauth"
	user "github.com/ooaklee/ghatd/external/user/v2"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// relayAccount creates an isolated Apple account and session; it never changes
// the live developer account or makes requests to an external provider.
func (f *connectionFixture) relayAccount(t *testing.T, email string) {
	t.Helper()
	created, err := f.users.CreateOAuthUser(f.ctx, &user.CreateOAuthUserRequest{Identity: user.OAuthIdentity{Provider: "apple", Issuer: "https://appleid.apple.com", Subject: "relay-owner"}, Email: email})
	require.NoError(t, err)
	f.account = created.User
	f.tokens, err = f.auth.CreateTokenWithAuthenticationTime(f.ctx, f.account, time.Now())
	require.NoError(t, err)
	require.NoError(t, f.ephemeral.CreateAuth(f.ctx, f.account.ID, f.tokens))
}

func (f *connectionFixture) enableGoogle(t *testing.T) {
	t.Helper()
	provider, err := oauth.NewGoogleSecureProvider(&oauth.NewGoogleSecureProviderRequest{ClientID: "fixture", ClientSecret: "fixture", RedirectURL: "https://app.example/api/v1/ams/oauth/google/callback", Store: oauth.NewRedisTransactionStore(f.redis, f.namespace)})
	require.NoError(t, err)
	f.service.OauthServices = []accessmanager.OauthService{provider}
}

func (f *connectionFixture) linkGoogle(t *testing.T) {
	t.Helper()
	_, err := f.users.LinkOAuthIdentity(f.ctx, f.account.ID, &user.OAuthIdentity{Provider: "google", Issuer: oauth.GoogleSecureIssuer, Subject: "relay-fallback"})
	require.NoError(t, err)
}

func (f *connectionFixture) appleStart(t *testing.T, email string) (string, string) {
	t.Helper()
	raw, err := json.Marshal(map[string]string{"email": email})
	require.NoError(t, err)
	w := f.request("POST", "/apple/disconnect", string(raw))
	require.Equal(t, 202, w.Code, w.Body.String())
	var response struct {
		Data accessmanager.OAuthDisconnectStartResponse
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	code := regexp.MustCompile(`<strong>([A-Z0-9]{8})</strong>`).FindStringSubmatch(f.mail.custom.EmailBody)
	require.Len(t, code, 2)
	return response.Data.ChallengeID, code[1]
}

func TestRelayDisconnectRequiresUsableFallback(t *testing.T) {
	for _, setup := range []string{"sole-apple", "google-disabled", "google-unlinked"} {
		t.Run(setup, func(t *testing.T) {
			f := newConnectionFixture(t)
			f.relayAccount(t, "alias@privaterelay.appleid.com")
			f.enableNativeDisconnect(t)
			if setup == "google-disabled" {
				f.linkGoogle(t)
			}
			if setup == "google-unlinked" {
				f.enableGoogle(t)
			}
			before := f.current(t)
			status := f.request("GET", "", "")
			require.Equal(t, 200, status.Code)
			require.Contains(t, status.Body.String(), `"replacement_email_required_for":`)
			var response struct {
				Data accessmanager.OAuthConnectionsResponse
			}
			require.NoError(t, json.Unmarshal(status.Body.Bytes(), &response))
			require.Contains(t, response.Data.ReplacementEmailRequiredFor, "apple")
			for _, email := range []string{before.Email, "other@private.icloud.com"} {
				raw, _ := json.Marshal(map[string]string{"email": email})
				web := f.request("POST", "/apple/disconnect", string(raw))
				require.Equal(t, 409, web.Code, web.Body.String())
				require.Contains(t, web.Body.String(), "OAuthReplacementEmailRequired")
				native := f.nativeRequest("POST", "/apple/disconnect", map[string]string{"email": email, "redirect_uri": nativeSettingsURI})
				require.Equal(t, 409, native.Code, native.Body.String())
			}
			require.Nil(t, f.mail.custom)
			require.Equal(t, before.EmailRevision, f.current(t).EmailRevision)
			require.Equal(t, before.OAuthIdentities, f.current(t).OAuthIdentities)
		})
	}
}

func TestRelayDisconnectWithIndependentEmailOrEnabledProvider(t *testing.T) {
	for _, email := range []string{"independent@icloud.com", "private@alias.example", "alias@privaterelay.appleid.com"} {
		t.Run(email, func(t *testing.T) {
			f := newConnectionFixture(t)
			f.relayAccount(t, "alias@privaterelay.appleid.com")
			if user.IsApplePrivateRelayEmail(email) {
				f.enableGoogle(t)
				f.linkGoogle(t)
			}
			id, code := f.appleStart(t, email)
			require.Equal(t, "alias@privaterelay.appleid.com", f.current(t).Email)
			require.Contains(t, f.mail.custom.EmailBody, "Opening the link does not change your account")
			review := f.request("GET", "/apple/disconnect/challenges/"+id, "")
			require.Equal(t, 200, review.Code, review.Body.String())
			w := f.request("POST", "/apple/disconnect/confirm", confirmBody(id, code, ""))
			require.Equal(t, 200, w.Code, w.Body.String())
			require.NotEmpty(t, w.Result().Cookies())
			after := f.current(t)
			require.Equal(t, f.account.ID, after.ID)
			require.Equal(t, email, after.Email)
			require.Equal(t, int64(1), after.EmailRevision)
			for _, identity := range after.OAuthIdentities {
				require.NotEqual(t, "apple", identity.Provider)
			}
		})
	}
}

func TestRelayFallbackRecheckedAfterEmailSent(t *testing.T) {
	for _, change := range []string{"disabled", "unlinked"} {
		t.Run(change, func(t *testing.T) {
			f := newConnectionFixture(t)
			f.relayAccount(t, "alias@private.icloud.com")
			f.enableGoogle(t)
			f.linkGoogle(t)
			id, code := f.appleStart(t, f.account.Email)
			if change == "disabled" {
				f.service.OauthServices = nil
			} else {
				_, err := f.db.Collection("users").UpdateOne(f.ctx, bson.M{"_id": f.account.ID}, bson.M{"$pull": bson.M{"oauth_identities": bson.M{"provider": "google"}}})
				require.NoError(t, err)
			}
			for _, w := range []struct{ method, path, body string }{{"GET", "/apple/disconnect/challenges/" + id, ""}, {"POST", "/apple/disconnect/confirm", confirmBody(id, code, "")}} {
				response := f.request(w.method, w.path, w.body)
				require.Equal(t, 409, response.Code, response.Body.String())
				require.Contains(t, response.Body.String(), "OAuthReplacementEmailRequired")
			}
			require.Equal(t, int64(0), f.current(t).EmailRevision)
			require.Equal(t, f.account.Email, f.current(t).Email)
		})
	}
}

func TestRelayReplacementUsesBothEmailStages(t *testing.T) {
	f := newConnectionFixture(t)
	f.relayAccount(t, "alias@privaterelay.appleid.com")
	f.tokens = f.staleSessionFor(t, 6*time.Minute)
	id, code := f.appleStart(t, "independent@example.test")
	require.Equal(t, f.account.Email, f.mail.custom.EmailTo)
	w := f.request("POST", "/apple/disconnect/confirm", confirmBody(id, code, ""))
	require.Equal(t, 202, w.Code, w.Body.String())
	require.Equal(t, f.account.Email, f.current(t).Email)
	require.Len(t, f.current(t).OAuthIdentities, 1)
	require.Equal(t, "independent@example.test", f.mail.custom.EmailTo)
	var response struct {
		Data accessmanager.OAuthDisconnectResponse
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	code = regexp.MustCompile(`<strong>([A-Z0-9]{8})</strong>`).FindStringSubmatch(f.mail.custom.EmailBody)[1]
	w = f.request("POST", "/apple/disconnect/confirm", confirmBody(response.Data.NextChallenge.ChallengeID, code, ""))
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Equal(t, f.account.ID, f.current(t).ID)
	require.Equal(t, "independent@example.test", f.current(t).Email)
}

func TestRelayReplacementNeverMergesExistingAccount(t *testing.T) {
	f := newConnectionFixture(t)
	f.relayAccount(t, "alias@private.icloud.com")
	other, _ := f.newAccount(t, "taken@example.test", "other")
	storedOther, err := f.users.GetUserByID(f.ctx, &user.GetUserByIDRequest{ID: other.ID})
	require.NoError(t, err)
	id, code := f.appleStart(t, other.Email)
	w := f.request("POST", "/apple/disconnect/confirm", confirmBody(id, code, ""))
	require.Equal(t, 409, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "OAuthEmailConflict")
	require.NotContains(t, w.Body.String(), other.ID)
	require.Equal(t, f.account.Email, f.current(t).Email)
	require.Len(t, f.current(t).OAuthIdentities, 1)
	unchanged, err := f.users.GetUserByID(f.ctx, &user.GetUserByIDRequest{ID: other.ID})
	require.NoError(t, err)
	require.Equal(t, other.Email, unchanged.User.Email)
	require.Equal(t, storedOther.User.OAuthIdentities, unchanged.User.OAuthIdentities)
}

func TestRemovingLastGoogleAlsoProtectsAppleRelay(t *testing.T) {
	f := newConnectionFixture(t)
	f.account, f.tokens = f.newAccount(t, "alias@private.icloud.com", "relay-google")
	w := f.request("POST", "/google/disconnect", `{"email":"alias@private.icloud.com"}`)
	require.Equal(t, 409, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "OAuthReplacementEmailRequired")
	require.Nil(t, f.mail.custom)
}

// TestRelayRepositoryGuard models a fallback disappearing after the service
// read. It bypasses the service deliberately to exercise the atomic Mongo filter.
func TestRelayRepositoryGuard(t *testing.T) {
	f := newConnectionFixture(t)
	f.relayAccount(t, "alias@privaterelay.appleid.com")
	f.linkGoogle(t)
	account := f.current(t)
	change := &user.DisconnectOAuthProviderRequest{UserID: account.ID, Provider: "apple", ExpectedEmail: account.Email, VerifiedEmail: account.Email, EmailRevision: account.EmailRevision}
	for _, identity := range account.OAuthIdentities {
		if identity.Provider == "apple" {
			change.Identities = append(change.Identities, user.OAuthIdentitySnapshot{Key: identity.Key, LinkedAt: identity.LinkedAt})
		}
	}
	_, err := f.users.DisconnectOAuthProvider(f.ctx, change)
	require.ErrorIs(t, err, user.ErrOAuthReplacementEmailRequired)
	change.AllowedRelayFallbackProviders = []string{"apple", "unknown"}
	_, err = f.users.DisconnectOAuthProvider(f.ctx, change)
	require.ErrorIs(t, err, user.ErrOAuthReplacementEmailRequired)
	change.AllowedRelayFallbackProviders = []string{"google"}
	_, err = f.db.Collection("users").UpdateOne(f.ctx, bson.M{"_id": account.ID}, bson.M{"$pull": bson.M{"oauth_identities": bson.M{"provider": "google"}}})
	require.NoError(t, err)
	_, err = f.users.DisconnectOAuthProvider(f.ctx, change)
	require.ErrorIs(t, err, user.ErrOAuthConnectionConflict)
	require.Equal(t, int64(0), f.current(t).EmailRevision)
	require.Len(t, f.current(t).OAuthIdentities, 1)
}

func TestIndependentAccountCannotChooseRelayAsSoleFallback(t *testing.T) {
	f := newConnectionFixture(t)
	w := f.request("POST", "/google/disconnect", `{"email":"alias@private.icloud.com"}`)
	require.Equal(t, 409, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "OAuthReplacementEmailRequired")
	require.Nil(t, f.mail.custom)
	require.Equal(t, f.account.Email, f.current(t).Email)
}
