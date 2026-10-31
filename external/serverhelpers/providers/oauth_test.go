package providers

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"net/http"
	"testing"

	"github.com/ooaklee/ghatd/external/oauth"
	oauthhelper "github.com/ooaklee/ghatd/external/oauth/helper"
	"github.com/stretchr/testify/require"
)

type idleStore struct{ calls int }

func (s *idleStore) Save(context.Context, *oauth.StoredTransaction) error { s.calls++; return nil }
func (s *idleStore) Consume(context.Context, string) (*oauth.StoredTransaction, error) {
	s.calls++
	return nil, oauth.ErrSecureTransactionNotFound
}

type idleTransport struct{ calls int }

func (t *idleTransport) RoundTrip(*http.Request) (*http.Response, error) {
	t.calls++
	return nil, oauth.ErrSecureProviderIncompleteConfig
}

func TestOAuthCompositionUsesOptionalOwnersWithoutRequests(t *testing.T) {
	for _, tc := range []struct {
		name          string
		google, apple bool
		broken        string
		want          []string
	}{
		{"disabled", false, false, "", []string{}}, {"google", true, false, "", []string{"google"}}, {"apple", false, true, "", []string{"apple"}}, {"both", true, true, "", []string{"google", "apple"}},
		{"partial google", true, true, "google", nil}, {"partial apple discards google", true, true, "apple", nil}, {"missing store", true, false, "store", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store, transport := &idleStore{}, &idleTransport{}
			cfg := OAuthConfig{Store: store}
			if tc.google {
				cfg.Google = oauthhelper.GoogleProviderConfig{ClientID: "fixture-client", ClientSecret: "fixture-secret-canary", RedirectURL: "https://app.example.test/oauth/google", HTTPClient: &http.Client{Transport: transport}}
			}
			if tc.apple {
				key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
				require.NoError(t, err)
				der, err := x509.MarshalPKCS8PrivateKey(key)
				require.NoError(t, err)
				cfg.Apple = oauthhelper.AppleProviderConfig{ClientID: "fixture-service", TeamID: "fixture-team", KeyID: "fixture-key", PrivateKeyBase64: base64.StdEncoding.EncodeToString(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})), RedirectURL: "https://app.example.test/oauth/apple", HTTPClient: &http.Client{Transport: transport}}
			}
			switch tc.broken {
			case "google":
				cfg.Google.RedirectURL = ""
			case "apple":
				cfg.Apple.KeyID = ""
			case "store":
				cfg.Store = nil
			}
			got, err := BuildOAuth(cfg)
			require.Zero(t, store.calls)
			require.Zero(t, transport.calls)
			if tc.broken != "" {
				require.Error(t, err)
				require.Nil(t, got)
				require.NotContains(t, err.Error(), "fixture-secret-canary")
				return
			}
			require.NoError(t, err)
			names := []string{}
			for _, provider := range got {
				names = append(names, provider.ProviderGetName())
			}
			require.Equal(t, tc.want, names)
		})
	}
}
