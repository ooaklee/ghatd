package oauthhelper

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/ooaklee/ghatd/external/oauth"
)

// GoogleProviderConfig supplies optional credentials and callback policy.
// HTTPClient is a caller-owned runtime dependency, not an enablement field.
type GoogleProviderConfig struct {
	// ClientID and ClientSecret are resolved credentials for the web client.
	ClientID     string
	ClientSecret string
	// RedirectURL is the registered callback; native validation owns its policy.
	RedirectURL string
	// HTTPClient is optional; nil retains the secure provider default.
	HTTPClient *http.Client
}

// AppleProviderConfig supplies optional Services ID credentials and an explicit
// signing-key source. Non-empty base64 takes precedence over the path, even when
// invalid. The only construction I/O is reading that path in file mode.
type AppleProviderConfig struct {
	// ClientID is the registered Services ID, paired with its team and signing key.
	ClientID string
	TeamID   string
	KeyID    string
	// PrivateKeyBase64 takes precedence over PrivateKeyPath when non-empty.
	PrivateKeyBase64 string
	// PrivateKeyPath is read only when the encoded source is empty.
	PrivateKeyPath string
	// RedirectURL is the registered HTTPS callback.
	RedirectURL string
	// HTTPClient is optional; nil retains the secure provider default.
	HTTPClient *http.Client
}

// BuildGoogleProvider returns nil for all-blank credentials, refuses partial
// configuration and delegates callback/store validation to the secure provider.
// Errors contain no credential values or underlying provider diagnostics.
func BuildGoogleProvider(cfg GoogleProviderConfig, store oauth.SecureTransactionStore) (*oauth.GoogleSecureProvider, error) {
	set := configuredFields(cfg.ClientID, cfg.ClientSecret, cfg.RedirectURL)
	if set == 0 {
		return nil, nil
	}
	if set != 3 {
		return nil, fmt.Errorf("google OAuth requires client ID, secret and callback URL")
	}
	provider, err := oauth.NewGoogleSecureProvider(&oauth.NewGoogleSecureProviderRequest{ClientID: cfg.ClientID, ClientSecret: cfg.ClientSecret, RedirectURL: cfg.RedirectURL, HTTPClient: cfg.HTTPClient, Store: store})
	if err != nil {
		return nil, fmt.Errorf("google OAuth configuration is invalid")
	}
	return provider, nil
}

// BuildAppleProvider returns nil for all-blank logical fields and refuses partial
// configuration before key loading. It preserves LoadAppleSigningKey precedence;
// the native provider validates the PEM, P-256 curve, callback and store. Errors
// omit key material, paths and underlying filesystem/provider diagnostics.
func BuildAppleProvider(cfg AppleProviderConfig, store oauth.SecureTransactionStore) (*oauth.AppleProvider, error) {
	keySource := cfg.PrivateKeyPath
	if cfg.PrivateKeyBase64 != "" {
		keySource = cfg.PrivateKeyBase64
	}
	set := configuredFields(cfg.ClientID, cfg.TeamID, cfg.KeyID, keySource, cfg.RedirectURL)
	if set == 0 {
		return nil, nil
	}
	if set != 5 {
		return nil, fmt.Errorf("apple OAuth requires Services ID, team ID, key ID, private key file or base64 key, and HTTPS callback URL")
	}
	key, err := LoadAppleSigningKey(cfg.PrivateKeyBase64, cfg.PrivateKeyPath)
	if err != nil {
		return nil, err
	}
	provider, err := oauth.NewAppleProvider(&oauth.NewAppleProviderRequest{ClientID: cfg.ClientID, TeamID: cfg.TeamID, KeyID: cfg.KeyID, PrivateKeyPEM: key, RedirectURL: cfg.RedirectURL, HTTPClient: cfg.HTTPClient, Store: store})
	if err != nil {
		return nil, fmt.Errorf("apple OAuth configuration is invalid")
	}
	return provider, nil
}

func configuredFields(values ...string) int {
	count := 0
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			count++
		}
	}
	return count
}
