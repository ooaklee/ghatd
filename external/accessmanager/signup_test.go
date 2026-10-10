package accessmanager

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSignupEvidenceCookieAdmission(t *testing.T) {
	cases := []struct {
		name       string
		configured bool
		cookies    []*http.Cookie
		want       string
	}{
		{name: "disabled", cookies: []*http.Cookie{{Name: "referral", Value: "signed"}}},
		{name: "absent", configured: true},
		{name: "bounded evidence", configured: true, cookies: []*http.Cookie{nil, {Name: "other", Value: "ignored"}, {Name: "referral", Value: "signed"}}, want: "signed"},
		{name: "maximum size", configured: true, cookies: []*http.Cookie{{Name: "referral", Value: strings.Repeat("a", 2048)}}, want: strings.Repeat("a", 2048)},
		{name: "oversize", configured: true, cookies: []*http.Cookie{{Name: "referral", Value: strings.Repeat("a", 2049)}}},
		{name: "duplicate identical", configured: true, cookies: []*http.Cookie{{Name: "referral", Value: "signed"}, {Name: "referral", Value: "signed"}}},
		{name: "duplicate changed", configured: true, cookies: []*http.Cookie{{Name: "referral", Value: "first"}, {Name: "referral", Value: "second"}}},
		{name: "empty", configured: true, cookies: []*http.Cookie{{Name: "referral"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := &Service{}
			if tc.configured {
				_, err := s.WithSignupEvidenceCookie("referral")
				require.NoError(t, err)
			}
			require.Equal(t, tc.want, s.readSignupEvidence(tc.cookies))
		})
	}
}

func TestSignupCookieConfiguration(t *testing.T) {
	cases := []struct {
		name, cookie string
		invalid      bool
	}{
		{"explicit", "__Host-referral", false}, {"empty", "", true}, {"space", "referral cookie", true}, {"separator", "referral;cookie", true}, {"oversize", strings.Repeat("a", 129), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := (&Service{}).WithSignupEvidenceCookie(tc.cookie)
			if tc.invalid {
				require.ErrorIs(t, err, ErrBadRequest)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestSignupTransportCannotSupplyPrivateEvidence(t *testing.T) {
	cases := []struct{ name, payload string }{
		{"snake case", `{"attribution_evidence":"forged","email":"member@example.test"}`},
		{"field name", `{"AttributionEvidence":"forged","email":"member@example.test"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var r CreateUserRequest
			require.NoError(t, json.Unmarshal([]byte(tc.payload), &r))
			require.Empty(t, r.AttributionEvidence)
			payload, err := json.Marshal(OauthCallbackResponse{AttributionEvidence: "private-signed-evidence"})
			require.NoError(t, err)
			require.NotContains(t, string(payload), "private-signed-evidence")
		})
	}
}
