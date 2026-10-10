package referral

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestEvidenceVerification(t *testing.T) {
	now := time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		name   string
		edit   func(string) string
		at     time.Time
		denied bool
	}{
		{name: "eligible signup", at: now.Add(time.Hour)},
		{name: "exact expiry excluded", at: now.Add(24 * time.Hour), denied: true},
		{name: "before consent excluded", at: now.Add(-time.Second), denied: true},
		{name: "tampered signature", at: now, edit: func(s string) string { return s + "X" }, denied: true},
		{name: "unknown key", at: now, edit: func(s string) string { return strings.Replace(s, "old.", "unknown.", 1) }, denied: true},
		{name: "malformed token", at: now, edit: func(string) string { return "malformed" }, denied: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			config := EvidenceConfig{ProgramID: "partners-v1", ActiveKeyID: "old", Window: 24 * time.Hour, Keys: map[string][]byte{"old": bytes.Repeat([]byte{1}, 32)}}
			signer, err := NewEvidenceSigner(config, fakeClock{t: now})
			require.NoError(t, err)
			token, _, err := signer.Issue(Link{ID: "link", Code: "public-code", ProgramID: "partners-v1"})
			require.NoError(t, err)
			if test.edit != nil {
				token = test.edit(token)
			}
			_, err = signer.Verify(token, test.at)
			if test.denied {
				require.ErrorIs(t, err, ErrDenied)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// Rotation and delayed processing are one lifecycle: old issued evidence must
// verify at signup time until the host deliberately removes its old key.
func TestEvidenceRotationAndDelayedProcessing(t *testing.T) {
	now := time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC)
	old := bytes.Repeat([]byte{1}, 32)
	next := bytes.Repeat([]byte{2}, 32)
	config := EvidenceConfig{ProgramID: "partners-v1", ActiveKeyID: "old", Window: 24 * time.Hour, Keys: map[string][]byte{"old": old}}
	issuer, err := NewEvidenceSigner(config, fakeClock{t: now})
	require.NoError(t, err)
	token, _, err := issuer.Issue(Link{ID: "link", Code: "code", ProgramID: "partners-v1"})
	require.NoError(t, err)
	config.ActiveKeyID = "next"
	config.Keys["next"] = next
	verifier, err := NewEvidenceSigner(config, fakeClock{t: now.Add(30 * 24 * time.Hour)})
	require.NoError(t, err)
	_, err = verifier.Verify(token, now.Add(time.Hour))
	require.NoError(t, err)
	delete(config.Keys, "old")
	verifier, err = NewEvidenceSigner(config, fakeClock{t: now})
	require.NoError(t, err)
	_, err = verifier.Verify(token, now.Add(time.Hour))
	require.ErrorIs(t, err, ErrDenied)
}
