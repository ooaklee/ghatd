package accessmanager_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ooaklee/ghatd/external/accessmanager"
	user "github.com/ooaklee/ghatd/external/user/v2"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// Native service methods must enforce their own return-address allowlist, even
// when called directly by a host rather than through the standard HTTP handler.
func TestNativeConnectionVerificationDoesNotReviewWebChallenge(t *testing.T) {
	f := newConnectionFixture(t)
	f.enableNativeDisconnect(t)
	challenge, err := f.service.StartOAuthConnectionVerification(f.ctx, "apple", f.tokens.AccessToken)
	require.NoError(t, err)
	webReturn := "https://app.example/settings"
	_, err = f.service.ReviewMobileOAuthConnectionVerification(f.ctx, "apple", challenge.ChallengeID, webReturn, f.tokens.AccessToken)
	require.ErrorIs(t, err, user.ErrOAuthUnsupported)
	_, err = f.service.ConfirmMobileOAuthConnectionVerification(f.ctx, "apple", &accessmanager.OAuthDisconnectConfirmRequest{
		ChallengeID: challenge.ChallengeID, Code: "ABCD1234",
	}, webReturn, f.tokens.AccessToken)
	require.ErrorIs(t, err, user.ErrOAuthUnsupported)
	// Rejected native calls must leave the browser challenge available to review.
	_, err = f.service.ReviewOAuthConnectionVerification(f.ctx, "apple", challenge.ChallengeID, f.tokens.AccessToken)
	require.NoError(t, err)
}

func TestWebConnectionVerificationRejectsNonObjectStartAndDuplicateCookies(t *testing.T) {
	f := newConnectionFixture(t)
	for _, body := range []string{"null", "[]", "true", `""`, "{} {}", `{"email":"other@example.test"}`} {
		require.Equal(t, 400, f.request("POST", "/apple/reauthenticate", body).Code, body)
	}
	request := httptest.NewRequest("POST", "https://app.example/api/v1/ams/oauth/connections/apple/reauthenticate", strings.NewReader(`{}`))
	request.Header.Set("Origin", "https://app.example")
	request.Header.Set("Content-Type", "application/json")
	request.AddCookie(&http.Cookie{Name: "access", Value: f.tokens.AccessToken})
	request.AddCookie(&http.Cookie{Name: "access", Value: f.tokens.AccessToken})
	response := httptest.NewRecorder()
	f.router.ServeHTTP(response, request)
	require.Equal(t, 401, response.Code)
	require.Nil(t, f.mail.custom)
}

func TestWebConnectionReviewRejectsProofInQueryAndMismatchedOrigin(t *testing.T) {
	f := newConnectionFixture(t)
	c, _, _ := webReauthenticateStart(t, f)
	path := "/apple/reauthenticate/challenges/" + c.ChallengeID
	require.Equal(t, 400, f.request("GET", path+"?token=not-a-proof", "").Code)
	require.Equal(t, 403, f.serve("GET", path, "", "https://other.example", "", f.tokens).Code)
	f.handler.OAuthOrigin = "https://other.example"
	require.Equal(t, 403, f.request("GET", path, "").Code)
	status := f.request("GET", "", "")
	require.Contains(t, status.Body.String(), `"connect_verification_available":false`)
}

// Changing the account while proof is pending must invalidate both read-only
// review and confirmation, even when the caller retains the original cookie.
func TestWebConnectionVerificationRejectsChangedAccount(t *testing.T) {
	for _, change := range []bson.M{{"email": "changed@example.test"}, {"email_revision": int64(1)}, {"status": "SUSPENDED"}} {
		f := newConnectionFixture(t)
		c, code, _ := webReauthenticateStart(t, f)
		_, err := f.db.Collection("users").UpdateOne(f.ctx, bson.M{"_id": f.account.ID}, bson.M{"$set": change})
		require.NoError(t, err)
		require.NotEqual(t, 200, f.request("GET", "/apple/reauthenticate/challenges/"+c.ChallengeID, "").Code)
		body, err := json.Marshal(map[string]string{"challenge_id": c.ChallengeID, "code": code})
		require.NoError(t, err)
		confirmed := f.request("POST", "/apple/reauthenticate/confirm", string(body))
		require.NotEqual(t, 200, confirmed.Code)
		require.Empty(t, confirmed.Result().Cookies())
	}
}
