package oauthhelper

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/ooaklee/ghatd/external/oauth"
	"github.com/stretchr/testify/require"
)

type configurationStore struct{ calls atomic.Int32 }

func (s *configurationStore) Save(context.Context, *oauth.StoredTransaction) error {
	s.calls.Add(1)
	return nil
}
func (s *configurationStore) Consume(context.Context, string) (*oauth.StoredTransaction, error) {
	s.calls.Add(1)
	return nil, oauth.ErrSecureTransactionNotFound
}

type configurationTransport struct{ calls atomic.Int32 }

func (s *configurationTransport) RoundTrip(*http.Request) (*http.Response, error) {
	s.calls.Add(1)
	return nil, oauth.ErrSecureProviderIncompleteConfig
}

func TestBuildGoogleProviderConfiguration(t *testing.T) {
	for _, tc := range []struct {
		name, missing, callback string
		disabled, nilStore      bool
		wantError               bool
	}{
		{name: "disabled", disabled: true, nilStore: true},
		{name: "secure_callback", callback: "https://app.example.test/callback"},
		{name: "loopback_callback", callback: "http://localhost:4000/callback"},
		{name: "missing_client", missing: "client", wantError: true},
		{name: "missing_secret", missing: "secret", wantError: true},
		{name: "missing_callback", missing: "callback", wantError: true},
		{name: "unsafe_callback", callback: "http://public.example.test/callback", wantError: true},
		{name: "store_required", nilStore: true, wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store, transport := &configurationStore{}, &configurationTransport{}
			cfg := GoogleProviderConfig{ClientID: "client_fixture", ClientSecret: "secret_fixture", RedirectURL: "https://app.example.test/callback", HTTPClient: &http.Client{Transport: transport}}
			if tc.callback != "" {
				cfg.RedirectURL = tc.callback
			}
			switch tc.missing {
			case "client":
				cfg.ClientID = ""
			case "secret":
				cfg.ClientSecret = ""
			case "callback":
				cfg.RedirectURL = ""
			}
			if tc.disabled {
				cfg.ClientID, cfg.ClientSecret, cfg.RedirectURL = " ", "", ""
			}
			var selected oauth.SecureTransactionStore = store
			if tc.nilStore {
				selected = nil
			}
			provider, err := BuildGoogleProvider(cfg, selected)
			require.Zero(t, store.calls.Load())
			require.Zero(t, transport.calls.Load())
			if tc.wantError {
				require.Error(t, err)
				require.Nil(t, provider)
				if cfg.ClientSecret != "" {
					require.NotContains(t, err.Error(), cfg.ClientSecret)
				}
				return
			}
			require.NoError(t, err)
			if tc.disabled {
				require.Nil(t, provider)
				return
			}
			require.Equal(t, "google", provider.ProviderGetName())
			require.Implements(t, (*oauth.ContextualFlowProvider)(nil), provider)
		})
	}
}

func configurationSigningKey(t *testing.T, curve elliptic.Curve) []byte {
	t.Helper()
	key, err := ecdsa.GenerateKey(curve, rand.Reader)
	require.NoError(t, err)
	der, err := x509.MarshalPKCS8PrivateKey(key)
	require.NoError(t, err)
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
}

func TestBuildAppleProviderConfiguration(t *testing.T) {
	for _, tc := range []struct {
		name, missing, encoded, file                         string
		disabled, wrongCurve, invalidPEM, nilStore, insecure bool
		wantError                                            bool
	}{
		{name: "disabled", disabled: true, nilStore: true},
		{name: "base64_only"},
		{name: "base64_over_missing_file", file: "missing"},
		{name: "base64_over_invalid_file", file: "invalid"},
		{name: "explicit_file", file: "valid", encoded: "file_only"},
		{name: "invalid_base64_never_falls_back", file: "valid", encoded: "fixture-invalid!", wantError: true},
		{name: "whitespace_base64_never_falls_back", file: "valid", encoded: " ", wantError: true},
		{name: "invalid_encoded_pem_never_falls_back", file: "valid", invalidPEM: true, wantError: true},
		{name: "wrong_curve_never_falls_back", file: "valid", wrongCurve: true, wantError: true},
		{name: "empty_file", file: "empty", encoded: "file_only", wantError: true},
		{name: "missing_file", file: "missing", encoded: "file_only", wantError: true},
		{name: "missing_client", missing: "client", wantError: true},
		{name: "missing_team", missing: "team", wantError: true},
		{name: "missing_key_id", missing: "key_id", wantError: true},
		{name: "missing_key_source", missing: "key_source", wantError: true},
		{name: "missing_callback", missing: "callback", wantError: true},
		{name: "store_required", nilStore: true, wantError: true},
		{name: "https_required", insecure: true, wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store, transport := &configurationStore{}, &configurationTransport{}
			key := configurationSigningKey(t, elliptic.P256())
			encodedKey := key
			if tc.wrongCurve {
				encodedKey = configurationSigningKey(t, elliptic.P384())
			}
			if tc.invalidPEM {
				encodedKey = []byte("fixture invalid PEM")
			}
			cfg := AppleProviderConfig{ClientID: "services_fixture", TeamID: "team_fixture", KeyID: "key_fixture", PrivateKeyBase64: base64.StdEncoding.EncodeToString(encodedKey), RedirectURL: "https://app.example.test/callback", HTTPClient: &http.Client{Transport: transport}}
			if tc.encoded != "" {
				cfg.PrivateKeyBase64 = tc.encoded
			}
			if tc.encoded == "file_only" {
				cfg.PrivateKeyBase64 = ""
			}
			if tc.file != "" {
				cfg.PrivateKeyPath = filepath.Join(t.TempDir(), "selected.p8")
				if tc.file != "missing" {
					content := key
					if tc.file == "invalid" {
						content = []byte("fixture invalid file")
					}
					if tc.file == "empty" {
						content = nil
					}
					require.NoError(t, os.WriteFile(cfg.PrivateKeyPath, content, 0600))
				}
			}
			switch tc.missing {
			case "client":
				cfg.ClientID = ""
			case "team":
				cfg.TeamID = ""
			case "key_id":
				cfg.KeyID = ""
			case "key_source":
				cfg.PrivateKeyBase64 = ""
			case "callback":
				cfg.RedirectURL = ""
			}
			if tc.insecure {
				cfg.RedirectURL = "http://localhost:4000/callback"
			}
			if tc.disabled {
				cfg = AppleProviderConfig{HTTPClient: &http.Client{Transport: transport}}
			}
			var selected oauth.SecureTransactionStore = store
			if tc.nilStore {
				selected = nil
			}
			provider, err := BuildAppleProvider(cfg, selected)
			require.Zero(t, store.calls.Load())
			require.Zero(t, transport.calls.Load())
			if tc.wantError {
				require.Error(t, err)
				require.Nil(t, provider)
				if cfg.PrivateKeyBase64 != "" && cfg.PrivateKeyBase64 != " " {
					require.NotContains(t, err.Error(), cfg.PrivateKeyBase64)
				}
				if cfg.PrivateKeyPath != "" {
					require.NotContains(t, err.Error(), cfg.PrivateKeyPath)
				}
				if tc.missing != "" {
					require.Contains(t, err.Error(), "requires")
				}
				return
			}
			require.NoError(t, err)
			if tc.disabled {
				require.Nil(t, provider)
				return
			}
			require.Equal(t, "apple", provider.ProviderGetName())
			require.Implements(t, (*oauth.ContextualFlowProvider)(nil), provider)
		})
	}
}
