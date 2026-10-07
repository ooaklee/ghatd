package referral

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Host keys authenticate origin time, but structural checks must still reject
// contradictory signed metadata. No optional analytics read admits a signup.
func TestSignedMeasuredOriginTimeAndImmutableAttribution(t *testing.T) {
	for _, tc := range []struct {
		name, mode string
		want       error
	}{
		{"owning_origin_time_is_frozen", "", nil}, {"ID_without_time_is_denied", "missing", ErrDenied},
		{"time_without_ID_is_denied", "id", ErrDenied}, {"zero_origin_time_is_denied", "zero", ErrDenied},
		{"origin_after_issue_is_denied", "future", ErrDenied}, {"origin_before_link_is_denied_by_owning_attribution", "floor", ErrDenied},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, repo, partner, link, e := fixture(t)
			signer := visitSigner(t, s.clock)
			_, identity, err := signer.IssueVisit(link)
			require.NoError(t, err)
			visit, err := s.ObserveVisit(context.Background(), VisitRequest{Code: link.Code, Identity: identity, Consented: true})
			require.NoError(t, err)
			_, evidence, err := signer.IssueMeasured(link, visit.Eligible)
			require.NoError(t, err)
			switch tc.mode {
			case "missing":
				evidence.MeasuredOccurredAt = nil
			case "id":
				evidence.MeasuredClickID = ""
			case "zero":
				zero := time.Time{}
				evidence.MeasuredOccurredAt = &zero
			case "future":
				future := evidence.IssuedAt.Add(time.Nanosecond)
				evidence.MeasuredOccurredAt = &future
			case "floor":
				before := link.CreatedAt.Add(-time.Nanosecond)
				evidence.MeasuredOccurredAt = &before
			}
			payload, err := json.Marshal(evidence)
			require.NoError(t, err)
			message := signer.config.ActiveKeyID + "." + base64.RawURLEncoding.EncodeToString(payload)
			mac := hmac.New(sha256.New, signer.config.Keys[signer.config.ActiveKeyID])
			_, err = mac.Write([]byte(message))
			require.NoError(t, err)
			token := message + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
			verified, err := signer.Verify(token, e.At)
			if tc.mode != "floor" {
				require.ErrorIs(t, err, tc.want)
				if tc.want != nil {
					require.Empty(t, verified)
					return
				}
			} else {
				require.NoError(t, err, "stateless verifier cannot look up link creation")
			}
			repo.clicks = nil
			repo.visits = map[string]VisitReceipt{} // simulate complete raw cleanup
			e.Evidence = verified
			out, err := s.LockAttribution(context.Background(), partner, link, e)
			require.ErrorIs(t, err, tc.want)
			if tc.want != nil {
				require.Empty(t, out)
				return
			}
			require.Equal(t, visit.Eligible.OccurredAt, *out.SourceMeasuredOccurredAt)
			// Mutating caller-owned evidence must not rewrite the frozen result.
			*e.Evidence.MeasuredOccurredAt = e.At
			require.Equal(t, visit.Eligible.OccurredAt, *out.SourceMeasuredOccurredAt)
		})
	}
}

func TestPlainEvidenceWireAndDigestCompatibility(t *testing.T) {
	for _, tc := range []struct {
		name    string
		instant time.Time
	}{
		{"whole_second", time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)},
		{"nanosecond_precision", time.Date(2026, 10, 7, 12, 0, 0, 123456789, time.UTC)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := Evidence{ProgramID: ProgramID, Audience: "partner-signup", Code: "code", LinkID: "link", IssuedAt: tc.instant, ExpiresAt: tc.instant.Add(time.Hour)}
			old := struct {
				ProgramID string    `json:"program_id"`
				Audience  string    `json:"audience"`
				Code      string    `json:"code"`
				LinkID    string    `json:"link_id"`
				IssuedAt  time.Time `json:"issued_at"`
				ExpiresAt time.Time `json:"expires_at"`
			}{e.ProgramID, e.Audience, e.Code, e.LinkID, e.IssuedAt, e.ExpiresAt}
			before, err := json.Marshal(old)
			require.NoError(t, err)
			after, err := json.Marshal(e)
			require.NoError(t, err)
			require.Equal(t, before, after)
			digest, err := signupDigest(e)
			require.NoError(t, err)
			expected := sha256.Sum256(before)
			require.Equal(t, base64.RawURLEncoding.EncodeToString(expected[:]), digest)
		})
	}
}
