package providers

import (
	"github.com/ooaklee/ghatd/external/accessmanager"
	"github.com/ooaklee/ghatd/external/oauth"
	oauthhelper "github.com/ooaklee/ghatd/external/oauth/helper"
)

// OAuthConfig supplies optional provider credentials, explicit transports and
// one borrowed secure transaction store. Each configured provider retains its
// owner's callback and credential validation, including Apple key precedence.
type OAuthConfig struct {
	Google oauthhelper.GoogleProviderConfig
	Apple  oauthhelper.AppleProviderConfig
	Store  oauth.SecureTransactionStore
}

// BuildOAuth composes configured Google then Apple providers. Completely absent
// settings omit a provider; partial settings abort the composition with no
// partial provider list. It sends no provider requests. Apple's explicitly
// selected file key source can perform a local read through its owning helper.
func BuildOAuth(cfg OAuthConfig) ([]accessmanager.OauthService, error) {
	providers := []accessmanager.OauthService{}
	google, err := oauthhelper.BuildGoogleProvider(cfg.Google, cfg.Store)
	if err != nil {
		return nil, err
	}
	if google != nil {
		providers = append(providers, google)
	}
	apple, err := oauthhelper.BuildAppleProvider(cfg.Apple, cfg.Store)
	if err != nil {
		return nil, err
	}
	if apple != nil {
		providers = append(providers, apple)
	}
	return providers, nil
}
