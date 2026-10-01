package accessmanager_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/accessmanager"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// These exercise the multi-step UI contract through the real HTTP routes and
// isolated Mongo/Redis fixture, including changes between verification stages.
func TestOAuthSettingsFlowImmediateContinuation(t *testing.T) {
	f := newConnectionFixture(t)
	staleAt := time.Now().Add(-6 * time.Minute)
	stale, err := f.auth.CreateTokenWithAuthenticationTime(f.ctx, f.account, staleAt)
	require.NoError(t, err)
	require.NoError(t, f.ephemeral.CreateAuth(f.ctx, f.account.ID, stale))
	f.tokens = stale
	id, code, _ := f.start(t, "new@example.test")
	require.Equal(t, f.account.Email, f.mail.custom.EmailTo)
	result := f.request("POST", "/google/disconnect/confirm", confirmBody(id, code, ""))
	require.Equal(t, 202, result.Code, result.Body.String())
	var response struct {
		Data accessmanager.OAuthDisconnectResponse `json:"data"`
	}
	require.NoError(t, json.Unmarshal(result.Body.Bytes(), &response))
	require.False(t, response.Data.Disconnected)
	require.NotNil(t, response.Data.NextChallenge)
	require.NotEqual(t, id, response.Data.NextChallenge.ChallengeID)
	require.Equal(t, "new@example.test", f.mail.custom.EmailTo)
	require.Equal(t, f.account.Email, f.current(t).Email)
	require.NotEmpty(t, f.current(t).OAuthIdentities)
	review := f.request("GET", "/google/disconnect/challenges/"+response.Data.NextChallenge.ChallengeID, "")
	require.Equal(t, 200, review.Code, review.Body.String())
	require.Contains(t, review.Body.String(), `"email":"new@example.test"`)
	require.Contains(t, review.Body.String(), `"verification_stage":"sign_in_email"`)
	require.NotContains(t, review.Body.String(), "code_hash")
	require.NotContains(t, review.Body.String(), "token_hash")
}

func TestOAuthSettingsFlowRejectsIdentityChangeBeforeApproval(t *testing.T) {
	f := newConnectionFixture(t)
	stale, err := f.auth.CreateTokenWithAuthenticationTime(f.ctx, f.account, time.Now().Add(-6*time.Minute))
	require.NoError(t, err)
	require.NoError(t, f.ephemeral.CreateAuth(f.ctx, f.account.ID, stale))
	f.tokens = stale
	id, code, _ := f.start(t, "new@example.test")
	_, err = f.db.Collection("users").UpdateOne(f.ctx, bson.M{"_id": f.account.ID}, bson.M{"$set": bson.M{"oauth_identities.0.linked_at": time.Now().Add(time.Second)}})
	require.NoError(t, err)
	review := f.request("GET", "/google/disconnect/challenges/"+id, "")
	require.Equal(t, 409, review.Code, review.Body.String())
	result := f.request("POST", "/google/disconnect/confirm", confirmBody(id, code, ""))
	require.Equal(t, 409, result.Code, result.Body.String())
	require.Equal(t, f.account.Email, f.mail.custom.EmailTo, "candidate email must not be sent for a stale snapshot")
	require.Equal(t, f.account.Email, f.current(t).Email)
	require.NotEmpty(t, f.current(t).OAuthIdentities)
}

func TestOAuthSettingsFlowPreservesDirectCandidateAuthenticationTime(t *testing.T) {
	f := newConnectionFixture(t)
	authenticatedAt := time.Now().Add(-4 * time.Minute).Truncate(time.Second)
	token, err := f.auth.CreateTokenWithAuthenticationTime(f.ctx, f.account, authenticatedAt)
	require.NoError(t, err)
	require.NoError(t, f.ephemeral.CreateAuth(f.ctx, f.account.ID, token))
	f.tokens = token
	id, code, _ := f.start(t, "new@example.test")
	result := f.request("POST", "/google/disconnect/confirm", confirmBody(id, code, ""))
	require.Equal(t, 200, result.Code, result.Body.String())
	var refreshed string
	for _, c := range result.Result().Cookies() {
		if c.Name == "access" {
			refreshed = c.Value
		}
	}
	require.NotEmpty(t, refreshed)
	details, err := f.auth.ExtractAccessTokenMetadataByString(f.ctx, refreshed)
	require.NoError(t, err)
	require.WithinDuration(t, authenticatedAt, details.AuthenticationTime, time.Second,
		"new inbox proof must preserve the recorded original-account authentication timestamp")
	require.False(t, strings.Contains(result.Body.String(), "next_challenge"))
}

func TestOAuthSettingsFlowUsesTimeOfMailboxProof(t *testing.T) {
	for _, candidate := range []string{"original@example.test", "new@example.test"} {
		t.Run(candidate, func(t *testing.T) {
			f := newConnectionFixture(t)
			f.tokens = f.staleSessionFor(t, 6*time.Minute)
			id, code, _ := f.start(t, candidate)
			// Model time spent waiting for mail without sleeping in the test.
			key := "oauth:disconnect:" + f.namespace + ":" + id
			raw, err := f.redis.Get(key).Result()
			require.NoError(t, err)
			var record map[string]any
			require.NoError(t, json.Unmarshal([]byte(raw), &record))
			record["auth_time_ms"] = time.Now().Add(-8 * time.Minute).UnixMilli()
			rawRecord, err := json.Marshal(record)
			require.NoError(t, err)
			require.NoError(t, f.redis.Set(key, rawRecord, time.Minute).Err())
			proofAt := time.Now()
			result := f.request("POST", "/google/disconnect/confirm", confirmBody(id, code, ""))
			if candidate != f.account.Email {
				require.Equal(t, 202, result.Code, result.Body.String())
				var response struct {
					Data accessmanager.OAuthDisconnectResponse `json:"data"`
				}
				require.NoError(t, json.Unmarshal(result.Body.Bytes(), &response))
				require.NotNil(t, response.Data.NextChallenge)
				result = f.request("POST", "/google/disconnect/confirm", confirmBody(response.Data.NextChallenge.ChallengeID, "", extractLinkToken(t, f.mail.custom.EmailBody)))
			}
			require.Equal(t, 200, result.Code, result.Body.String())
			var accessToken string
			for _, c := range result.Result().Cookies() {
				if c.Name == "access" {
					accessToken = c.Value
				}
			}
			details, err := f.auth.ExtractAccessTokenMetadataByString(f.ctx, accessToken)
			require.NoError(t, err)
			require.WithinDuration(t, proofAt, details.AuthenticationTime, 2*time.Second)
		})
	}
}
