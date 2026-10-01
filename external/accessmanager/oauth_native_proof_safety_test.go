package accessmanager_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/accessmanager"
	"github.com/stretchr/testify/require"
)

// TestNativeSettingsWrongProofKeepsBothEmailStages protects the native transport
// as well as the shared service: an incorrect code must not spend the valid proof
// or apply either half of a staged email change.
func TestNativeSettingsWrongProofKeepsBothEmailStages(t *testing.T) {
	f := newConnectionFixture(t)
	f.enableNativeDisconnect(t)
	f.tokens = f.staleSessionFor(t, 6*time.Minute)
	before := f.current(t)
	start := f.nativeStart(t, "native-replacement@example.test")
	firstCode, _ := f.nativeMailProof(t)
	wrong := firstCode[:7] + "A"
	if wrong == firstCode {
		wrong = firstCode[:7] + "B"
	}
	rejected := f.nativeRequest("POST", "/google/disconnect/confirm", nativeBody(start.ChallengeID, wrong, ""))
	require.Equal(t, 400, rejected.Code)
	require.Equal(t, before, f.current(t))

	approved := f.nativeRequest("POST", "/google/disconnect/confirm", nativeBody(start.ChallengeID, firstCode, ""))
	require.Equal(t, 202, approved.Code, approved.Body.String())
	require.Equal(t, before, f.current(t))
	var response struct {
		Data accessmanager.OAuthDisconnectResponse `json:"data"`
	}
	require.NoError(t, json.Unmarshal(approved.Body.Bytes(), &response))
	next := response.Data.NextChallenge
	require.NotNil(t, next)
	require.Equal(t, "sign_in_email", next.VerificationStage)

	// Reusing the first proof cannot skip the second inbox or consume its proof.
	again := f.nativeRequest("POST", "/google/disconnect/confirm", nativeBody(start.ChallengeID, firstCode, ""))
	require.Equal(t, 400, again.Code)
	require.Equal(t, before, f.current(t))
	secondCode, _ := f.nativeMailProof(t)
	wrong = secondCode[:7] + "A"
	if wrong == secondCode {
		wrong = secondCode[:7] + "B"
	}
	rejected = f.nativeRequest("POST", "/google/disconnect/confirm", nativeBody(next.ChallengeID, wrong, ""))
	require.Equal(t, 400, rejected.Code)
	require.Equal(t, before, f.current(t))
	completed := f.nativeRequest("POST", "/google/disconnect/confirm", nativeBody(next.ChallengeID, secondCode, ""))
	require.Equal(t, 200, completed.Code, completed.Body.String())
	after := f.current(t)
	require.Equal(t, before.ID, after.ID)
	require.Equal(t, "native-replacement@example.test", after.Email)
	require.Empty(t, after.OAuthIdentities)
	for i := 0; i < 2; i++ {
		replayed := f.nativeRequest("POST", "/google/disconnect/confirm", nativeBody(next.ChallengeID, secondCode, ""))
		require.Equal(t, 401, replayed.Code)
		require.Equal(t, after, f.current(t))
	}
}

// TestNativeSettingsExpiredOrLockedProofCannotChangeAccount exercises the real
// Redis expiry and attempt limit through the native HTTP handlers. The fixture
// owns its database and key namespace; no application account is modified.
func TestNativeSettingsExpiredOrLockedProofCannotChangeAccount(t *testing.T) {
	for _, stage := range []string{"current_email", "sign_in_email"} {
		for _, failure := range []string{"expired", "locked"} {
			t.Run(stage+"/"+failure, func(t *testing.T) {
				f := newConnectionFixture(t)
				f.enableNativeDisconnect(t)
				f.tokens = f.staleSessionFor(t, 6*time.Minute)
				before := f.current(t)
				email := f.account.Email
				if stage == "current_email" {
					email = "native-replacement@example.test"
				}
				start := f.nativeStart(t, email)
				require.Equal(t, stage, start.VerificationStage)
				code, proof := f.nativeMailProof(t)
				if failure == "expired" {
					key := "oauth:disconnect:" + f.namespace + ":" + start.ChallengeID
					require.NoError(t, f.redis.PExpire(key, time.Millisecond).Err())
					require.Eventually(t, func() bool {
						n, err := f.redis.Exists(key).Result()
						return err == nil && n == 0
					}, time.Second, time.Millisecond)
				} else {
					wrong := code[:7] + "A"
					if wrong == code {
						wrong = code[:7] + "B"
					}
					for attempt := 0; attempt < 5; attempt++ {
						r := f.nativeRequest("POST", "/google/disconnect/confirm", nativeBody(start.ChallengeID, wrong, ""))
						expected := 400
						if attempt == 4 {
							expected = 423
						}
						require.Equal(t, expected, r.Code)
						require.Equal(t, before, f.current(t))
					}
				}
				expected := 400
				if failure == "locked" {
					expected = 423
				}
				for _, body := range []map[string]string{
					nativeBody(start.ChallengeID, code, ""),
					nativeBody(start.ChallengeID, "", proof),
				} {
					r := f.nativeRequest("POST", "/google/disconnect/confirm", body)
					require.Equal(t, expected, r.Code)
					require.Equal(t, before, f.current(t))
				}
				review := f.nativeRequest("GET", nativeReviewPath(start.ChallengeID, nativeSettingsURI), nil)
				if failure == "expired" {
					expected = 404 // Metadata review distinguishes a missing challenge.
				}
				require.Equal(t, expected, review.Code)
				require.Equal(t, before, f.current(t))
			})
		}
	}
}
