package accessmanager

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	user "github.com/ooaklee/ghatd/external/user/v2"
	"github.com/stretchr/testify/require"
)

func TestOAuthConflictAndLinkRequiredRemainDistinctAcrossTransports(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		code string
	}{
		{"email collision during login", user.ErrOAuthLinkRequired, "link_required"},
		{"identity already linked elsewhere", user.ErrOAuthIdentityConflict, "identity_conflict"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Wrapped diagnostics must never escape into native JSON or browser URLs.
			err := fmt.Errorf("private-account@example.test: %w", tc.err)
			h := &Handler{}
			native := httptest.NewRecorder()
			h.mobileError(native, err)
			require.Equal(t, http.StatusBadRequest, native.Code)
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
