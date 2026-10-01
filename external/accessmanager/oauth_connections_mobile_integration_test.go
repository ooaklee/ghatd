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
)

const nativeSettingsURI = "com.example.companion.settings:/oauth/disconnect"
const otherNativeSettingsURI = "com.example.other.settings:/oauth/disconnect"

func (f *connectionFixture) enableNativeDisconnect(t *testing.T) {
	t.Helper()
	require.NoError(t, f.service.ConfigureOAuthConnections(accessmanager.OAuthConnectionsConfig{
		Origin: "https://app.example", Store: oauth.NewRedisDisconnectChallengeStore(f.redis, f.namespace),
		MobileRedirectURIs: []string{nativeSettingsURI, otherNativeSettingsURI},
	}))
}
func (f *connectionFixture) nativeRequest(method, path string, body any) *httptest.ResponseRecorder {
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(method, "https://app.example/api/v1/ams/oauth/mobile/connections"+path, strings.NewReader(string(raw)))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "access", Value: f.tokens.AccessToken})
	w := httptest.NewRecorder()
	f.router.ServeHTTP(w, req)
	return w
}
func nativeBody(id, code, token string) map[string]string {
	body := map[string]string{"redirect_uri": nativeSettingsURI, "challenge_id": id}
	if code != "" {
		body["code"] = code
	}
	if token != "" {
		body["token"] = token
	}
	return body
}
func (f *connectionFixture) nativeStart(t *testing.T, email string) accessmanager.OAuthDisconnectStartResponse {
	t.Helper()
	w := f.nativeRequest("POST", "/google/disconnect", map[string]string{"email": email, "redirect_uri": nativeSettingsURI})
	require.Equal(t, 202, w.Code, w.Body.String())
	var result struct {
		Data accessmanager.OAuthDisconnectStartResponse `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	require.NotEmpty(t, result.Data.ChallengeID)
	return result.Data
}
func (f *connectionFixture) nativeMailProof(t *testing.T) (string, string) {
	t.Helper()
	code := regexp.MustCompile(`<strong>([A-Z0-9]{8})</strong>`).FindStringSubmatch(f.mail.custom.EmailBody)
	require.Len(t, code, 2)
	match := regexp.MustCompile(`href="([^"]+)"`).FindStringSubmatch(f.mail.custom.EmailBody)
	require.Len(t, match, 2)
	link, err := url.Parse(html.UnescapeString(match[1]))
	require.NoError(t, err)
	require.Equal(t, nativeSettingsURI, strings.Split(link.String(), "#")[0])
	require.Empty(t, link.RawQuery)
	values, err := url.ParseQuery(link.Fragment)
	require.NoError(t, err)
	require.Len(t, values, 3)
	require.Equal(t, "google", values.Get("oauth_disconnect"))
	require.NotContains(t, f.mail.custom.EmailBody, "browser where you started")
	return code[1], values.Get("token")
}
func nativeReviewPath(id, redirect string) string {
	return "/google/disconnect/challenges/" + id + "?redirect_uri=" + url.QueryEscape(redirect)
}

func TestNativeSettingsSameEmailAndCookieRotation(t *testing.T) {
	f := newConnectionFixture(t)
	f.enableNativeDisconnect(t)
	f.tokens = f.staleSessionFor(t, 6*time.Minute)
	start := f.nativeStart(t, f.account.Email)
	require.Equal(t, "sign_in_email", start.VerificationStage)
	_, proof := f.nativeMailProof(t)
	review := f.nativeRequest("GET", nativeReviewPath(start.ChallengeID, nativeSettingsURI), nil)
	require.Equal(t, 200, review.Code, review.Body.String())
	require.NotContains(t, review.Body.String(), proof)
	require.Equal(t, 1, len(f.current(t).OAuthIdentities))
	done := f.nativeRequest("POST", "/google/disconnect/confirm", nativeBody(start.ChallengeID, "", proof))
	require.Equal(t, 200, done.Code, done.Body.String())
	require.Contains(t, done.Body.String(), `"disconnected":true`)
	require.NotEmpty(t, done.Result().Cookies())
	require.Equal(t, f.account.ID, f.current(t).ID)
	require.Equal(t, f.account.Email, f.current(t).Email)
	require.Empty(t, f.current(t).OAuthIdentities)
	again := f.nativeRequest("POST", "/google/disconnect/confirm", nativeBody(start.ChallengeID, "", proof))
	require.NotEqual(t, 200, again.Code)
}

func TestNativeSettingsStagedEmailChange(t *testing.T) {
	f := newConnectionFixture(t)
	f.enableNativeDisconnect(t)
	f.tokens = f.staleSessionFor(t, 6*time.Minute)
	start := f.nativeStart(t, "replacement@example.test")
	require.Equal(t, "current_email", start.VerificationStage)
	require.Equal(t, f.account.Email, start.Email)
	code, _ := f.nativeMailProof(t)
	stage := f.nativeRequest("POST", "/google/disconnect/confirm", nativeBody(start.ChallengeID, code, ""))
	require.Equal(t, 202, stage.Code, stage.Body.String())
	require.Empty(t, stage.Result().Cookies())
	require.Equal(t, f.account.Email, f.current(t).Email)
	require.NotEmpty(t, f.current(t).OAuthIdentities)
	var response struct {
		Data accessmanager.OAuthDisconnectResponse `json:"data"`
	}
	require.NoError(t, json.Unmarshal(stage.Body.Bytes(), &response))
	require.False(t, response.Data.Disconnected)
	next := response.Data.NextChallenge
	require.NotNil(t, next)
	require.Equal(t, "replacement@example.test", next.Email)
	require.Equal(t, "sign_in_email", next.VerificationStage)
	_, proof := f.nativeMailProof(t)
	require.Equal(t, 200, f.nativeRequest("GET", nativeReviewPath(next.ChallengeID, nativeSettingsURI), nil).Code)
	done := f.nativeRequest("POST", "/google/disconnect/confirm", nativeBody(next.ChallengeID, "", proof))
	require.Equal(t, 200, done.Code, done.Body.String())
	require.Equal(t, f.account.ID, f.current(t).ID)
	require.Equal(t, "replacement@example.test", f.current(t).Email)
	require.Empty(t, f.current(t).OAuthIdentities)
}

func TestNativeSettingsTransportAndAppIsolation(t *testing.T) {
	f := newConnectionFixture(t)
	f.enableNativeDisconnect(t)
	start := f.nativeStart(t, f.account.Email)
	code, _ := f.nativeMailProof(t)
	// Same account/cookie still cannot spend native proof via web or another app.
	require.Equal(t, 404, f.request("GET", "/google/disconnect/challenges/"+start.ChallengeID, "").Code)
	require.Equal(t, 400, f.request("POST", "/google/disconnect/confirm", confirmBody(start.ChallengeID, code, "")).Code)
	require.Equal(t, 404, f.nativeRequest("GET", nativeReviewPath(start.ChallengeID, otherNativeSettingsURI), nil).Code)
	body := nativeBody(start.ChallengeID, code, "")
	body["redirect_uri"] = otherNativeSettingsURI
	require.Equal(t, 400, f.nativeRequest("POST", "/google/disconnect/confirm", body).Code)
	// Wrong session cannot consume or spend attempts either.
	original := f.tokens
	_, f.tokens = f.newAccount(t, "other@example.test", "other")
	require.Equal(t, 404, f.nativeRequest("GET", nativeReviewPath(start.ChallengeID, nativeSettingsURI), nil).Code)
	require.Equal(t, 400, f.nativeRequest("POST", "/google/disconnect/confirm", nativeBody(start.ChallengeID, code, "")).Code)
	f.tokens = original
	done := f.nativeRequest("POST", "/google/disconnect/confirm", nativeBody(start.ChallengeID, code, ""))
	require.Equal(t, 200, done.Code, done.Body.String())
}

func TestNativeSettingsCannotSpendWebChallenge(t *testing.T) {
	f := newConnectionFixture(t)
	f.enableNativeDisconnect(t)
	id, code, _ := f.start(t, f.account.Email)
	require.Equal(t, 404, f.nativeRequest("GET", nativeReviewPath(id, nativeSettingsURI), nil).Code)
	require.Equal(t, 400, f.nativeRequest("POST", "/google/disconnect/confirm", nativeBody(id, code, "")).Code)
	require.Equal(t, 200, f.request("POST", "/google/disconnect/confirm", confirmBody(id, code, "")).Code)
}

func TestNativeSettingsOptInAndRequestGuards(t *testing.T) {
	f := newConnectionFixture(t)
	status := f.nativeRequest("GET", "?redirect_uri="+url.QueryEscape(nativeSettingsURI), nil)
	require.Equal(t, 200, status.Code, status.Body.String())
	require.Contains(t, status.Body.String(), `"connected":["google"]`)
	require.Contains(t, status.Body.String(), `"disconnect_available":false`)
	disabled := f.nativeRequest("POST", "/google/disconnect", map[string]string{"redirect_uri": nativeSettingsURI})
	require.NotEqual(t, 202, disabled.Code)
	f.enableNativeDisconnect(t)
	for _, tc := range []struct {
		name, origin, contentType, body string
		cookies                         int
	}{
		{"browser", "https://app.example", "application/json", `{"redirect_uri":"` + nativeSettingsURI + `"}`, 1},
		{"null-origin", "null", "application/json", `{}`, 1},
		{"form", "", "application/x-www-form-urlencoded", `{}`, 1},
		{"unknown-field", "", "application/json", `{"redirect_uri":"` + nativeSettingsURI + `","surprise":true}`, 1},
		{"missing-cookie", "", "application/json", `{"redirect_uri":"` + nativeSettingsURI + `"}`, 0},
		{"duplicate-cookie", "", "application/json", `{"redirect_uri":"` + nativeSettingsURI + `"}`, 2},
		{"untrusted-return", "", "application/json", `{"redirect_uri":"evil.example:/oauth/disconnect"}`, 1},
		{"oversized", "", "application/json", `{"email":"` + strings.Repeat("a", 9000) + `"}`, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "https://app.example/api/v1/ams/oauth/mobile/connections/google/disconnect", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", tc.contentType)
			if tc.origin != "" {
				req.Header.Set("Origin", tc.origin)
			}
			for i := 0; i < tc.cookies; i++ {
				req.AddCookie(&http.Cookie{Name: "access", Value: f.tokens.AccessToken})
			}
			w := httptest.NewRecorder()
			f.router.ServeHTTP(w, req)
			require.GreaterOrEqual(t, w.Code, 400, w.Body.String())
			require.Nil(t, f.mail.custom)
		})
	}
	// Explicitly empty Origin headers are also browser requests, not native.
	req := httptest.NewRequest("POST", "https://app.example/api/v1/ams/oauth/mobile/connections/google/disconnect", strings.NewReader(`{}`))
	req.Header["Origin"] = []string{""}
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	f.router.ServeHTTP(w, req)
	require.Equal(t, 400, w.Code)
	require.Equal(t, 403, f.serve("POST", "/google/disconnect", `{}`, "", "application/json", f.tokens).Code)
}
