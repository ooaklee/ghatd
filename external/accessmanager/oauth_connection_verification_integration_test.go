package accessmanager_test

import (
	"encoding/json"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/accessmanager"
	"github.com/ooaklee/ghatd/external/oauth"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
)

func startConnectionVerification(t *testing.T, f *connectionFixture) (accessmanager.OAuthDisconnectStartResponse, string, string) {
	t.Helper()
	w := f.nativeRequest("POST", "/apple/reauthenticate", map[string]string{"redirect_uri": nativeSettingsURI})
	require.Equal(t, 202, w.Code, w.Body.String())
	var result struct {
		Data accessmanager.OAuthDisconnectStartResponse `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	require.Equal(t, "connect_email", result.Data.VerificationStage)
	require.Equal(t, f.account.Email, f.mail.custom.EmailTo)
	require.Equal(t, f.account.Email, result.Data.Email)
	code := regexp.MustCompile(`<strong>([A-Z0-9]{8})</strong>`).FindStringSubmatch(f.mail.custom.EmailBody)
	require.Len(t, code, 2)
	link := regexp.MustCompile(`href="([^"]+)"`).FindStringSubmatch(f.mail.custom.EmailBody)
	require.Len(t, link, 2)
	u, err := url.Parse(html.UnescapeString(link[1]))
	require.NoError(t, err)
	q, err := url.ParseQuery(u.Fragment)
	require.NoError(t, err)
	require.Equal(t, "apple", q.Get("oauth_connect"))
	require.Empty(t, q.Get("oauth_disconnect"))
	return result.Data, code[1], q.Get("token")
}

func TestNativeConnectionVerificationKeepsAccountAndRequiresExplicitProof(t *testing.T) {
	for _, useLink := range []bool{false, true} {
		t.Run(map[bool]string{false: "code", true: "link"}[useLink], func(t *testing.T) {
			f := newConnectionFixture(t)
			f.enableNativeDisconnect(t)
			var err error
			f.tokens, err = f.auth.CreateTokenWithAuthenticationTime(f.ctx, f.account, time.Now().Add(-6*time.Minute))
			require.NoError(t, err)
			require.NoError(t, f.ephemeral.CreateAuth(f.ctx, f.account.ID, f.tokens))
			original := f.tokens.AccessToken
			challenge, code, proof := startConnectionVerification(t, f)
			review := f.nativeRequest("GET", "/apple/reauthenticate/challenges/"+challenge.ChallengeID+"?redirect_uri="+url.QueryEscape(nativeSettingsURI), nil)
			require.Equal(t, 200, review.Code, review.Body.String())
			require.Empty(t, review.Result().Cookies())
			before, err := f.service.OAuthConnections(f.ctx, original)
			require.NoError(t, err)
			require.Equal(t, []string{"google"}, before.Connected)
			body := nativeBody(challenge.ChallengeID, code, "")
			if useLink {
				body = nativeBody(challenge.ChallengeID, "", proof)
			}
			confirmed := f.nativeRequest("POST", "/apple/reauthenticate/confirm", body)
			require.Equal(t, 200, confirmed.Code, confirmed.Body.String())
			require.Contains(t, confirmed.Body.String(), `"reauthenticated":true`)
			require.Contains(t, confirmed.Body.String(), `"disconnected":false`)
			var fresh string
			for _, c := range confirmed.Result().Cookies() {
				if c.Name == "access" {
					fresh = c.Value
				}
			}
			require.NotEmpty(t, fresh)
			freshDetails, err := f.auth.ExtractAccessTokenMetadataByString(f.ctx, fresh)
			require.NoError(t, err)
			require.Equal(t, f.account.ID, freshDetails.UserID)
			require.WithinDuration(t, time.Now(), freshDetails.AuthenticationTime, 2*time.Second)
			oldDetails, err := f.auth.ExtractAccessTokenMetadataByString(f.ctx, original)
			require.NoError(t, err)
			require.Greater(t, time.Since(oldDetails.AuthenticationTime), 5*time.Minute)
			require.NotEqual(t, oldDetails.AccessUUID, freshDetails.AccessUUID)
			after, err := f.service.OAuthConnections(f.ctx, fresh)
			require.NoError(t, err)
			require.Equal(t, before.Connected, after.Connected)
			require.Equal(t, before.Email, after.Email)
			require.NotEqual(t, 200, f.nativeRequest("POST", "/apple/reauthenticate/confirm", body).Code)
		})
	}
}

func TestNativeConnectionVerificationIsBoundAndPurposeIsolated(t *testing.T) {
	f := newConnectionFixture(t)
	f.enableNativeDisconnect(t)
	c, code, _ := startConnectionVerification(t, f)
	body := nativeBody(c.ChallengeID, code, "")
	for _, path := range []string{"/apple/disconnect/confirm", "/google/reauthenticate/confirm"} {
		require.NotEqual(t, 200, f.nativeRequest("POST", path, body).Code)
	}
	// Direct wrong-purpose consumption is rejected by the store namespace,
	// independently of HTTP/service stage checks, without spending the proof.
	details, err := f.auth.ExtractAccessTokenMetadataByString(f.ctx, f.tokens.AccessToken)
	require.NoError(t, err)
	store := oauth.NewRedisDisconnectChallengeStore(f.redis, f.namespace)
	_, err = store.Consume(f.ctx, c.ChallengeID, f.account.ID, details.AccessUUID, "apple", code, "")
	require.Error(t, err)
	wrongURI := nativeBody(c.ChallengeID, code, "")
	wrongURI["redirect_uri"] = otherNativeSettingsURI
	require.NotEqual(t, 200, f.nativeRequest("POST", "/apple/reauthenticate/confirm", wrongURI).Code)
	original := f.tokens
	_, f.tokens = f.newAccount(t, "other@example.test", "other-subject")
	require.NotEqual(t, 200, f.nativeRequest("POST", "/apple/reauthenticate/confirm", body).Code)
	f.tokens, err = f.auth.CreateTokenWithAuthenticationTime(f.ctx, f.account, time.Now())
	require.NoError(t, err)
	require.NoError(t, f.ephemeral.CreateAuth(f.ctx, f.account.ID, f.tokens))
	require.NotEqual(t, 200, f.nativeRequest("POST", "/apple/reauthenticate/confirm", body).Code)
	f.tokens = original
	require.Equal(t, 200, f.nativeRequest("POST", "/apple/reauthenticate/confirm", body).Code)
	// A disconnect proof cannot refresh a session either.
	f2 := newConnectionFixture(t)
	f2.enableNativeDisconnect(t)
	disconnect := f2.nativeStart(t, f2.account.Email)
	disconnectCode, _ := f2.nativeMailProof(t)
	require.NotEqual(t, 200, f2.nativeRequest("POST", "/google/reauthenticate/confirm", nativeBody(disconnect.ChallengeID, disconnectCode, "")).Code)
	require.Equal(t, 200, f2.nativeRequest("POST", "/google/disconnect/confirm", nativeBody(disconnect.ChallengeID, disconnectCode, "")).Code)
}

func TestNativeConnectionVerificationGuardsAndCooldown(t *testing.T) {
	f := newConnectionFixture(t)
	require.NotEqual(t, 202, f.nativeRequest("POST", "/apple/reauthenticate", map[string]string{"redirect_uri": nativeSettingsURI}).Code)
	f.enableNativeDisconnect(t)
	require.Equal(t, 400, f.nativeRequest("POST", "/apple/reauthenticate", map[string]string{"redirect_uri": nativeSettingsURI, "email": "attacker@example.test"}).Code)
	for _, cookies := range []int{0, 2} {
		r := httptest.NewRequest("POST", "https://app.example/api/v1/ams/oauth/mobile/connections/apple/reauthenticate", strings.NewReader(`{"redirect_uri":"`+nativeSettingsURI+`"}`))
		r.Header.Set("Content-Type", "application/json")
		for i := 0; i < cookies; i++ {
			r.AddCookie(&http.Cookie{Name: "access", Value: f.tokens.AccessToken})
		}
		w := httptest.NewRecorder()
		f.router.ServeHTTP(w, r)
		require.NotEqual(t, 202, w.Code)
	}
	r := httptest.NewRequest("POST", "https://app.example/api/v1/ams/oauth/mobile/connections/apple/reauthenticate", strings.NewReader(`{"redirect_uri":"`+nativeSettingsURI+`"}`))
	r.Header.Set("Origin", "https://app.example")
	r.Header.Set("Content-Type", "application/json")
	r.AddCookie(&http.Cookie{Name: "access", Value: f.tokens.AccessToken})
	w := httptest.NewRecorder()
	f.router.ServeHTTP(w, r)
	require.NotEqual(t, 202, w.Code)
	c, _, _ := startConnectionVerification(t, f)
	require.Equal(t, 429, f.nativeRequest("POST", "/apple/reauthenticate", map[string]string{"redirect_uri": nativeSettingsURI}).Code)
	for i := 0; i < 5; i++ {
		require.NotEqual(t, 200, f.nativeRequest("POST", "/apple/reauthenticate/confirm", nativeBody(c.ChallengeID, "WRONG123", "")).Code)
	}
	locked := f.nativeRequest("GET", "/apple/reauthenticate/challenges/"+c.ChallengeID+"?redirect_uri="+url.QueryEscape(nativeSettingsURI), nil)
	require.NotEqual(t, 200, locked.Code)
}

func TestNativeConnectionVerificationRejectsChangedAccountAndUndeliveredProof(t *testing.T) {
	for _, changed := range []bson.M{{"email": "changed@example.test"}, {"email_revision": int64(1)}, {"status": "SUSPENDED"}} {
		f := newConnectionFixture(t)
		f.enableNativeDisconnect(t)
		c, code, _ := startConnectionVerification(t, f)
		_, err := f.db.Collection("users").UpdateOne(f.ctx, bson.M{"_id": f.account.ID}, bson.M{"$set": changed})
		require.NoError(t, err)
		require.NotEqual(t, 200, f.nativeRequest("POST", "/apple/reauthenticate/confirm", nativeBody(c.ChallengeID, code, "")).Code)
	}
	f := newConnectionFixture(t)
	f.enableNativeDisconnect(t)
	f.mail.fail = true
	failed := f.nativeRequest("POST", "/apple/reauthenticate", map[string]string{"redirect_uri": nativeSettingsURI})
	require.NotEqual(t, 202, failed.Code)
	keys, err := f.redis.Keys("oauth:disconnect:" + f.namespace + ":verify-connect:*").Result()
	require.NoError(t, err)
	require.Empty(t, keys)
}
