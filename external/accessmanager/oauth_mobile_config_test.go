package accessmanager

import (
	"context"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/oauth"
	"github.com/stretchr/testify/require"
)

// Every operation panics: configuration must be passive, including failed setup.
type configurationOnlyMobileStore struct{}

func (configurationOnlyMobileStore) SaveStart(context.Context, string, []byte, time.Duration) error {
	panic("configuration must not write starts")
}
func (configurationOnlyMobileStore) ConsumeStart(context.Context, string) ([]byte, error) {
	panic("configuration must not consume starts")
}
func (configurationOnlyMobileStore) SaveGrant(context.Context, string, string, string, string, []byte, time.Duration) error {
	panic("configuration must not write grants")
}
func (configurationOnlyMobileStore) ConsumeGrant(context.Context, string, string, string, string) ([]byte, error) {
	panic("configuration must not consume grants")
}

func TestMobileOAuthConfigurationValidation(t *testing.T) {
	type configurationCase struct {
		name, origin, redirect                               string
		callbacks                                            MobileOAuthProviderCallbacks
		noStore, disabled, originSet, redirectSet, wantError bool
	}
	const google = "https://app.example/api/v1/ams/oauth/google/callback"
	const apple = "https://app.example/api/v1/ams/oauth/apple/callback"
	cases := []configurationCase{
		{name: "zero callbacks preserves existing consumers"},
		{name: "both callbacks", callbacks: MobileOAuthProviderCallbacks{Google: google, Apple: apple}},
		{name: "Google only", callbacks: MobileOAuthProviderCallbacks{Google: google}},
		{name: "Apple only", callbacks: MobileOAuthProviderCallbacks{Apple: apple}},
		{name: "disabled ignores unusable configuration", disabled: true, noStore: true, origin: "not a URL", callbacks: MobileOAuthProviderCallbacks{Google: "not a URL", Apple: "not a URL"}},
		{name: "missing store", noStore: true, wantError: true},
	}
	for _, item := range []struct{ name, origin string }{
		{"empty", ""}, {"HTTP", "http://app.example"}, {"userinfo", "https://user@app.example"},
		{"path", "https://app.example/path"}, {"query", "https://app.example?query"}, {"empty query", "https://app.example?"},
		{"fragment", "https://app.example#fragment"}, {"localhost", "https://localhost"}, {"IP", "https://127.0.0.1"},
	} {
		cases = append(cases, configurationCase{name: "origin " + item.name, origin: item.origin, originSet: true, wantError: true})
	}
	for _, item := range []struct{ name, redirect string }{
		{"empty", ""}, {"HTTPS", "https://evil.example/callback"}, {"javascript", "javascript:alert(1)"},
		{"simple scheme", "client:/oauth/callback"}, {"authority", "com.example.client://oauth/callback"},
		{"extra slash", "com.example.client:///oauth/callback"}, {"wrong path", "com.example.client:/other"},
		{"empty query", "com.example.client:/oauth/callback?"}, {"fragment", "com.example.client:/oauth/callback#x"},
		{"encoded path", "com.example.client:/oauth/%63allback"}, {"leading whitespace", " com.example.client:/oauth/callback"},
	} {
		cases = append(cases, configurationCase{name: "native return " + item.name, redirect: item.redirect, redirectSet: true, wantError: true})
	}
	for _, item := range []struct{ name, callback string }{
		{"HTTP", "http://app.example/api/v1/ams/oauth/google/callback"},
		{"wrong host", "https://other.example/api/v1/ams/oauth/google/callback"},
		{"different port", "https://app.example:443/api/v1/ams/oauth/google/callback"},
		{"wrong provider path", apple}, {"wrong prefix", "https://app.example/wrong/callback"},
		{"trailing slash", google + "/"}, {"query", google + "?x=1"}, {"empty query", google + "?"}, {"fragment", google + "#x"},
		{"userinfo", "https://user@app.example/api/v1/ams/oauth/google/callback"},
		{"opaque", "https:app.example/api/v1/ams/oauth/google/callback"},
		{"encoded path", "https://app.example/api/v1/ams/oauth/google/%63allback"},
		{"malformed", "https://%gh/api/v1/ams/oauth/google/callback"},
	} {
		cases = append(cases, configurationCase{name: "Google callback " + item.name, callbacks: MobileOAuthProviderCallbacks{Google: item.callback}, wantError: true})
	}
	cases = append(cases, configurationCase{name: "Apple callback validated independently", callbacks: MobileOAuthProviderCallbacks{Google: google, Apple: google}, wantError: true})
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			cfg := MobileOAuthConfig{Origin: "https://app.example", RedirectURIs: []string{"com.example.client:/oauth/callback"}, Store: configurationOnlyMobileStore{}, ProviderCallbacks: test.callbacks}
			if test.originSet || test.origin != "" {
				cfg.Origin = test.origin
			}
			if test.redirectSet {
				cfg.RedirectURIs[0] = test.redirect
			}
			if test.disabled {
				cfg.RedirectURIs = nil
			}
			if test.noStore {
				cfg.Store = nil
			}
			h := &Handler{}
			// A failed reconfiguration must also clear an earlier enabled configuration.
			require.NoError(t, h.ConfigureMobileOAuth(MobileOAuthConfig{Origin: "https://app.example", RedirectURIs: []string{"com.example.previous:/oauth/callback"}, Store: configurationOnlyMobileStore{}}))
			err := h.ConfigureMobileOAuth(cfg)
			if test.wantError {
				require.ErrorIs(t, err, oauth.ErrSecureProviderIncompleteConfig)
				require.Nil(t, h.mobileOAuth)
				return
			}
			require.NoError(t, err)
			if test.disabled {
				require.Nil(t, h.mobileOAuth)
				return
			}
			require.Equal(t, cfg.ProviderCallbacks, h.mobileOAuth.ProviderCallbacks)
			require.True(t, h.mobileRedirectAllowed(cfg.RedirectURIs[0]))
			require.False(t, h.mobileRedirectAllowed("com.example.previous:/oauth/callback"))
			cfg.RedirectURIs[0] = "com.example.changed:/oauth/callback"
			cfg.ProviderCallbacks.Google = "https://changed.example/"
			require.True(t, h.mobileRedirectAllowed("com.example.client:/oauth/callback"))
			require.False(t, h.mobileRedirectAllowed(cfg.RedirectURIs[0]))
			require.Equal(t, test.callbacks, h.mobileOAuth.ProviderCallbacks)
		})
	}
}

func TestMobileOAuthDuplicateAllowlistRefusal(t *testing.T) {
	type allowlistCase struct {
		name      string
		uris      []string
		wantError bool
	}
	for _, test := range []allowlistCase{
		{"distinct applications", []string{"com.example.first:/oauth/callback", "com.example.second:/oauth/callback"}, false},
		{"duplicate application", []string{"com.example.first:/oauth/callback", "com.example.first:/oauth/callback"}, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			h := &Handler{}
			err := h.ConfigureMobileOAuth(MobileOAuthConfig{Origin: "https://app.example", RedirectURIs: test.uris, Store: configurationOnlyMobileStore{}})
			if test.wantError {
				require.ErrorIs(t, err, oauth.ErrSecureProviderIncompleteConfig)
				require.Nil(t, h.mobileOAuth)
			} else {
				require.NoError(t, err)
				for _, uri := range test.uris {
					require.True(t, h.mobileRedirectAllowed(uri))
				}
			}
		})
	}
}
