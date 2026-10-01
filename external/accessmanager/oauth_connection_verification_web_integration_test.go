package accessmanager_test

import (
	"encoding/json"
	"html"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/accessmanager"
	"github.com/stretchr/testify/require"
)

// webReauthenticateStart starts web Settings reauthentication for the fixture
// account with a stale session and returns the public challenge metadata plus
// the emailed code and link proof.
func webReauthenticateStart(t *testing.T, f *connectionFixture) (accessmanager.OAuthDisconnectStartResponse, string, string) {
	t.Helper()
	w := f.request("POST", "/apple/reauthenticate", `{}`)
	require.Equal(t, 202, w.Code, w.Body.String())
	var result struct {
		Data accessmanager.OAuthDisconnectStartResponse `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	require.Equal(t, "connect_email", result.Data.VerificationStage)
	require.Equal(t, f.account.Email, f.mail.custom.EmailTo)
	require.Equal(t, f.account.Email, result.Data.Email)
	require.Equal(t, f.account.Email, result.Data.SignInEmail)
	code := regexp.MustCompile(`<strong>([A-Z0-9]{8})</strong>`).FindStringSubmatch(f.mail.custom.EmailBody)
	require.Len(t, code, 2)
	link := regexp.MustCompile(`href="([^"]+)"`).FindStringSubmatch(f.mail.custom.EmailBody)
	require.Len(t, link, 2)
	parsed, err := url.Parse(html.UnescapeString(link[1]))
	require.NoError(t, err)
	require.Equal(t, "https://app.example/settings", parsed.Scheme+"://"+parsed.Host+parsed.Path)
	require.Empty(t, parsed.RawQuery)
	fragment, err := url.ParseQuery(parsed.Fragment)
	require.NoError(t, err)
	require.Equal(t, "apple", fragment.Get("oauth_connect"))
	require.Empty(t, fragment.Get("oauth_disconnect"))
	require.Equal(t, result.Data.ChallengeID, fragment.Get("challenge_id"))
	require.NotEmpty(t, fragment.Get("token"))
	return result.Data, code[1], fragment.Get("token")
}

// webFreshAccessCookie extracts the freshly issued access token from the
// confirmation response cookies.
func webFreshAccessCookie(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var fresh string
	for _, cookie := range w.Result().Cookies() {
		if cookie.Name == "access" {
			fresh = cookie.Value
		}
	}
	require.NotEmpty(t, fresh)
	return fresh
}

func TestWebConnectionVerificationRefreshesSessionWithoutChangingAccount(t *testing.T) {
	for _, useLink := range []bool{false, true} {
		t.Run(map[bool]string{false: "code", true: "link"}[useLink], func(t *testing.T) {
			f := newConnectionFixture(t)
			// A stale session can start verification: that is the whole point of
			// the flow, mirroring OAuthReauthenticationRequired on connect.
			var err error
			f.tokens, err = f.auth.CreateTokenWithAuthenticationTime(f.ctx, f.account, time.Now().Add(-6*time.Minute))
			require.NoError(t, err)
			require.NoError(t, f.ephemeral.CreateAuth(f.ctx, f.account.ID, f.tokens))
			original := f.tokens.AccessToken
			challenge, code, proof := webReauthenticateStart(t, f)

			// Read-only review exposes only public metadata and consumes nothing.
			review := f.request("GET", "/apple/reauthenticate/challenges/"+challenge.ChallengeID, "")
			require.Equal(t, 200, review.Code, review.Body.String())
			require.Contains(t, review.Body.String(), `"verification_stage":"connect_email"`)
			require.NotContains(t, review.Body.String(), "code_hash")
			require.NotContains(t, review.Body.String(), "token_hash")
			require.NotContains(t, review.Body.String(), "payload")
			require.Empty(t, review.Result().Cookies())

			// Advertised capability on the web status response.
			status := f.request("GET", "", "")
			require.Equal(t, 200, status.Code)
			require.Contains(t, status.Body.String(), `"connect_verification_available":true`)

			body := map[string]string{"challenge_id": challenge.ChallengeID, "code": code}
			if useLink {
				body = map[string]string{"challenge_id": challenge.ChallengeID, "token": proof}
			}
			raw, _ := json.Marshal(body)
			confirmed := f.request("POST", "/apple/reauthenticate/confirm", string(raw))
			require.Equal(t, 200, confirmed.Code, confirmed.Body.String())
			require.Contains(t, confirmed.Body.String(), `"reauthenticated":true`)
			require.Contains(t, confirmed.Body.String(), `"disconnected":false`)
			require.Contains(t, confirmed.Body.String(), f.account.Email)

			// Only the new session is fresh; the stale one keeps its old
			// authentication time and remains usable for status reads.
			fresh := webFreshAccessCookie(t, confirmed)
			freshDetails, err := f.auth.ExtractAccessTokenMetadataByString(f.ctx, fresh)
			require.NoError(t, err)
			require.Equal(t, f.account.ID, freshDetails.UserID)
			require.WithinDuration(t, time.Now(), freshDetails.AuthenticationTime, 2*time.Second)
			oldDetails, err := f.auth.ExtractAccessTokenMetadataByString(f.ctx, original)
			require.NoError(t, err)
			require.True(t, oldDetails.AuthenticationTime.Before(freshDetails.AuthenticationTime))
			require.Equal(t, 200, f.request("GET", "", "").Code)

			// Proof is single-use: replay fails after a successful confirmation.
			require.NotEqual(t, 200, f.request("POST", "/apple/reauthenticate/confirm", string(raw)).Code)

			// The account is unchanged: same email and no new provider links.
			after, err := f.service.OAuthConnections(f.ctx, original)
			require.NoError(t, err)
			require.Equal(t, f.account.Email, after.Email)
			require.Equal(t, []string{"google"}, after.Connected)
		})
	}
}

func TestWebConnectionVerificationRejectsWrongAndCrossContextProof(t *testing.T) {
	f := newConnectionFixture(t)
	f.tokens = f.staleSessionFor(t, time.Hour)
	challenge, code, _ := webReauthenticateStart(t, f)

	// Wrong code is rejected and does not consume the proof.
	wrong, _ := json.Marshal(map[string]string{"challenge_id": challenge.ChallengeID, "code": "00000000"})
	require.NotEqual(t, 200, f.request("POST", "/apple/reauthenticate/confirm", string(wrong)).Code)
	correct, _ := json.Marshal(map[string]string{"challenge_id": challenge.ChallengeID, "code": code})
	require.Equal(t, 200, f.request("POST", "/apple/reauthenticate/confirm", string(correct)).Code)

	// A native-issued challenge cannot be confirmed through the web endpoint.
	f2 := newConnectionFixture(t)
	f2.enableNativeDisconnect(t)
	nativeStart := f2.nativeRequest("POST", "/apple/reauthenticate", map[string]string{"redirect_uri": nativeSettingsURI})
	require.Equal(t, 202, nativeStart.Code, nativeStart.Body.String())
	var nativeResult struct {
		Data accessmanager.OAuthDisconnectStartResponse `json:"data"`
	}
	require.NoError(t, json.Unmarshal(nativeStart.Body.Bytes(), &nativeResult))
	nativeCode := regexp.MustCompile(`<strong>([A-Z0-9]{8})</strong>`).FindStringSubmatch(f2.mail.custom.EmailBody)
	require.Len(t, nativeCode, 2)
	// Keep account, session and provider identical: only the transport differs.
	nativeConfirm, _ := json.Marshal(map[string]string{"challenge_id": nativeResult.Data.ChallengeID, "code": nativeCode[1]})
	require.NotEqual(t, 200, f2.request("POST", "/apple/reauthenticate/confirm", string(nativeConfirm)).Code)
	require.Equal(t, 200, f2.nativeRequest("POST", "/apple/reauthenticate/confirm", nativeBody(nativeResult.Data.ChallengeID, nativeCode[1], "")).Code)

	// A disconnect proof cannot reauthenticate through the web endpoint.
	f3 := newConnectionFixture(t)
	f3.tokens = f3.staleSessionFor(t, time.Hour)
	disconnectID, disconnectCode, _ := f3.start(t, f3.account.Email)
	disconnectConfirm, _ := json.Marshal(map[string]string{"challenge_id": disconnectID, "code": disconnectCode})
	require.NotEqual(t, 200, f3.request("POST", "/google/reauthenticate/confirm", string(disconnectConfirm)).Code)
	// ...and the failed reauthentication attempt did not consume it.
	require.Equal(t, 200, f3.request("POST", "/google/disconnect/confirm", string(disconnectConfirm)).Code)
}

func TestWebConnectionVerificationBindsSessionProviderAndRevision(t *testing.T) {
	f := newConnectionFixture(t)
	f.tokens = f.staleSessionFor(t, time.Hour)
	challenge, code, _ := webReauthenticateStart(t, f)
	body, _ := json.Marshal(map[string]string{"challenge_id": challenge.ChallengeID, "code": code})

	// Another session of the same account cannot spend this proof.
	other := f.staleSessionFor(t, time.Minute)
	saved := f.tokens
	f.tokens = other
	require.NotEqual(t, 200, f.request("POST", "/apple/reauthenticate/confirm", string(body)).Code)
	f.tokens = saved

	// Another account cannot spend it either.
	_, otherTokens := f.newAccount(t, "other@example.test", "other-subject")
	require.NotEqual(t, 200, f.serve("POST", "/apple/reauthenticate/confirm", string(body), "https://app.example", "application/json", otherTokens).Code)

	// A provider mismatch is rejected by the route.
	require.NotEqual(t, 200, f.request("POST", "/google/reauthenticate/confirm", string(body)).Code)

	// Proof survives these failed attempts and still confirms for its owner.
	require.Equal(t, 200, f.request("POST", "/apple/reauthenticate/confirm", string(body)).Code)
}

func TestWebConnectionVerificationGuards(t *testing.T) {
	f := newConnectionFixture(t)
	f.tokens = f.staleSessionFor(t, time.Hour)

	// Start requires a JSON content type.
	require.Equal(t, 400, f.serve("POST", "/apple/reauthenticate", `{}`, "https://app.example", "text/plain", f.tokens).Code)
	// Strict same-Origin check.
	require.Equal(t, 403, f.serve("POST", "/apple/reauthenticate", `{}`, "https://attacker.example", "application/json", f.tokens).Code)
	// Missing Origin header.
	require.Equal(t, 403, f.serve("POST", "/apple/reauthenticate", `{}`, "", "application/json", f.tokens).Code)
	// The body must be the empty JSON object only.
	require.Equal(t, 400, f.serve("POST", "/apple/reauthenticate", `{"redirect_uri":"https://app.example/settings"}`, "https://app.example", "application/json", f.tokens).Code)
	require.Equal(t, 400, f.serve("POST", "/apple/reauthenticate", `{"email":"attacker@example.test"}`, "https://app.example", "application/json", f.tokens).Code)
	require.Equal(t, 400, f.serve("POST", "/apple/reauthenticate", ``, "https://app.example", "application/json", f.tokens).Code)
	// Exactly one session cookie is required.
	require.Equal(t, 401, f.serve("POST", "/apple/reauthenticate", `{}`, "https://app.example", "application/json", nil).Code)

	// A separate fixture avoids the per-account resend cooldown; the helper
	// asserts the 202 start.
	f2 := newConnectionFixture(t)
	challenge, _, _ := webReauthenticateStart(t, f2)
	// Confirm guards mirror the start guards.
	body, _ := json.Marshal(map[string]string{"challenge_id": challenge.ChallengeID, "code": "00000000"})
	require.Equal(t, 400, f2.serve("POST", "/apple/reauthenticate/confirm", string(body), "https://app.example", "text/plain", f2.tokens).Code)
	require.Equal(t, 403, f2.serve("POST", "/apple/reauthenticate/confirm", string(body), "https://attacker.example", "application/json", f2.tokens).Code)
	require.Equal(t, 400, f2.serve("POST", "/apple/reauthenticate/confirm", `{"challenge_id":"`+challenge.ChallengeID+`","code":"00000000","extra":1}`, "https://app.example", "application/json", f2.tokens).Code)
	// Review requires origin agreement and one cookie; a challenge id from
	// another provider reads as absent.
	require.Equal(t, 200, f2.request("GET", "/apple/reauthenticate/challenges/"+challenge.ChallengeID, "").Code)
	require.Equal(t, 401, f2.serve("GET", "/apple/reauthenticate/challenges/"+challenge.ChallengeID, "", "", "", nil).Code)
	// A challenge issued for another provider is invalid on this route; the
	// response deliberately matches ordinary invalid-proof rejection.
	require.Equal(t, 400, f2.request("GET", "/google/reauthenticate/challenges/"+challenge.ChallengeID, "").Code)
	// Malformed identifiers outside the route pattern do not match at all;
	// charset-valid but pattern-invalid identifiers are rejected as bad requests.
	require.Equal(t, 404, f2.request("GET", "/apple/reauthenticate/challenges/short", "").Code)
	require.Equal(t, 400, f2.request("GET", "/apple/reauthenticate/challenges/"+strings.Repeat("x", 43), "").Code)
}
