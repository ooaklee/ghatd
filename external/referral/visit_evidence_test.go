package referral

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Audit disposition: named scope/time/tamper/rotation/config cases exercise
// signed visit-cookie identity independently of signup attribution tokens.
func TestSignedVisitCookieBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name, mode string
		want       error
	}{
		{name: "valid_identity_roundtrip"},
		{name: "exact_expiry_is_excluded", mode: "expiry", want: ErrDenied},
		{name: "before_issue_is_excluded", mode: "before", want: ErrDenied},
		{name: "foreign_link_is_excluded", mode: "link", want: ErrDenied},
		{name: "signature_tampering_is_excluded", mode: "tamper", want: ErrDenied},
		{name: "signup_token_is_not_a_visit_nonce", mode: "purpose", want: ErrDenied},
		{name: "retired_link_cannot_reuse_measurement", mode: "retired", want: ErrDenied},
		{name: "old_key_rotation_preserves_original_digest", mode: "rotation"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _, _, link, _ := fixture(t)
			signer := visitSigner(t, s.clock)
			token, identity, err := signer.IssueVisit(link)
			require.NoError(t, err)
			switch tc.mode {
			case "expiry":
				signer.clock = fakeClock{identity.ExpiresAt}
			case "before":
				signer.clock = fakeClock{s.clock.Now().Add(-time.Second)}
			case "link":
				link.ID = "foreign"
			case "tamper":
				token += "X"
			case "purpose":
				token, _, err = signer.Issue(link)
				require.NoError(t, err)
			case "retired":
				now := s.clock.Now()
				link.RetiredAt = &now
			case "rotation":
				cfg := signer.config
				cfg.ActiveKeyID = "next"
				cfg.Keys["next"] = []byte(strings.Repeat("N", 32))
				signer, err = NewEvidenceSigner(cfg, fakeClock{s.clock.Now().Add(time.Minute)})
				require.NoError(t, err)
			}
			out, err := signer.VerifyVisit(token, link)
			require.ErrorIs(t, err, tc.want)
			if tc.want != nil {
				require.Empty(t, out)
			} else {
				require.Equal(t, identity, out)
			}
		})
	}
}

func TestVisitCookieOptInAndFrozenClockEntropy(t *testing.T) {
	for _, tc := range []struct {
		name    string
		window  time.Duration
		missing bool
		want    error
	}{
		{name: "measurement_disabled_without_affecting_signup", window: 0},
		{name: "minimum_window", window: time.Second},
		{name: "maximum_window", window: 24 * time.Hour},
		{name: "missing_injected_nonce_generator", window: time.Hour, missing: true, want: ErrInvalid},
		{name: "window_over_cap", window: 24*time.Hour + time.Second, want: ErrInvalid},
		{name: "window_under_floor", window: time.Nanosecond, want: ErrInvalid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _, _, link, _ := fixture(t)
			cfg := visitSigner(t, s.clock).config
			cfg.VisitWindow = tc.window
			if tc.missing {
				cfg.VisitIDs = nil
			}
			signer, err := NewEvidenceSigner(cfg, s.clock)
			require.ErrorIs(t, err, tc.want)
			if tc.want != nil {
				require.Nil(t, signer)
				return
			}
			signup, _, err := signer.Issue(link)
			require.NoError(t, err)
			require.NotEmpty(t, signup)
			first, a, err := signer.IssueVisit(link)
			if tc.window == 0 {
				require.ErrorIs(t, err, ErrUnavailable)
				require.Empty(t, first)
				require.Empty(t, a)
				return
			}
			require.NoError(t, err)
			second, b, err := signer.IssueVisit(link)
			require.NoError(t, err)
			require.NotEqual(t, first, second)
			require.NotEqual(t, a.Digest, b.Digest)
			require.Equal(t, a.ExpiresAt, b.ExpiresAt)
			_, err = signer.Verify(first, s.clock.Now())
			require.ErrorIs(t, err, ErrDenied)
			encoded, err := json.Marshal(a)
			require.NoError(t, err)
			require.Equal(t, "{}", string(encoded), "authenticated visit identity is never browser-decoded JSON")
		})
	}
}
