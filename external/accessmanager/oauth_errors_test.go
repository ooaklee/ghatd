package accessmanager

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/ooaklee/ghatd/external/oauth"
	user "github.com/ooaklee/ghatd/external/user/v2"
	"github.com/stretchr/testify/require"
)

func TestOAuthConflictAndLinkRequiredRemainDistinctAcrossTransports(t *testing.T) {
	for _, tc := range []struct {
		name   string
		err    error
		code   string
		status int
	}{
		{"email collision during login", user.ErrOAuthLinkRequired, "link_required", 400},
		{"identity already linked elsewhere", user.ErrOAuthIdentityConflict, "identity_conflict", 400},
		{"provider cancelled", oauth.ErrProviderCancelled, "cancelled", 400},
		{"invalid proof", oauth.ErrSecureTransactionInvalidState, "invalid", 400},
		{"reauthentication", ErrOAuthReauthenticationRequired, "reauth_required", 403},
		{"verifier unavailable", ErrSessionVerificationUnavailable, "unavailable", 503},
		{"adapter outage", errors.New("private outage"), "failed", 500},
		{"joined denial and outage", errors.Join(ErrOAuthReauthenticationRequired, errors.New("private outage")), "failed", 500},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Wrapped diagnostics must never escape into native JSON or browser URLs.
			err := fmt.Errorf("private-account@example.test: %w", tc.err)
			h := &Handler{}
			native := httptest.NewRecorder()
			h.mobileError(native, err)
			require.Equal(t, tc.status, native.Code)
			require.Contains(t, native.Body.String(), `"error":"`+tc.code+`"`)
			require.NotContains(t, native.Body.String(), "private-account")
			require.Empty(t, native.Result().Cookies())
			browser := httptest.NewRecorder()
			h.oauthErrorRedirect(browser, httptest.NewRequest(http.MethodGet, "/callback", nil), err, "/settings#account")
			require.Equal(t, http.StatusSeeOther, browser.Code)
			location, parseErr := url.Parse(browser.Header().Get("Location"))
			require.NoError(t, parseErr)
			require.Equal(t, tc.code, location.Query().Get("oauth_error"))
			require.Equal(t, "/settings#account", location.Query().Get("request_url"))
			require.NotContains(t, location.String(), "private-account")
			require.Empty(t, browser.Result().Cookies())
		})
	}
}
