package oauth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"

	"github.com/ooaklee/ghatd/external/logger"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

// GoogleProviderOauthUserInfo holds the information held by provider that represents
// a typical user
type GoogleProviderOauthUserInfo struct {
	OauthProviderUserId string `json:"id" bson:"_id"`
	Email               string `json:"email" bson:"email"`
	VerifiedEmail       bool   `json:"verified_email" bson:"verified_email"`
	FullName            string `json:"name" bson:"name"`
	FirstName           string `json:"given_name" bson:"given_name"`
	FamilyName          string `json:"family_name" bson:"family_name"`
	PictureUrl          string `json:"picture" bson:"picture"`
	Locale              string `json:"locale" bson:"locale"`
}

func (g *GoogleProviderOauthUserInfo) GetUserEmail() string {
	return g.Email
}

func (g *GoogleProviderOauthUserInfo) GetUserFirstName() string {
	return g.FirstName
}

func (g *GoogleProviderOauthUserInfo) GetUserLastName() string {
	return g.FamilyName
}

func (g *GoogleProviderOauthUserInfo) IsUserEmailVerifiedByProvider() bool {
	return g.VerifiedEmail
}

////////////////////////
////               ////
////////////////////////

// GoogleProvider holds and manages google oauth business logic
type GoogleProvider struct {
	config                   *oauth2.Config
	providerUserInfoEndpoint string
	providerCookieKey        string
	providerName             string
}

// NewGoogleProvider created oauth provider for google
func NewGoogleProvider(r *NewGoogleProviderRequest) *GoogleProvider {
	return &GoogleProvider{
		config: &oauth2.Config{
			RedirectURL:  r.RedirectURL,
			ClientID:     r.ClientID,
			ClientSecret: r.ClientSecret,
			Scopes:       []string{"https://www.googleapis.com/auth/userinfo.email", "https://www.googleapis.com/auth/userinfo.profile"},
			Endpoint:     google.Endpoint,
		},
		providerUserInfoEndpoint: "https://www.googleapis.com/oauth2/v2/userinfo?access_token=",
		providerCookieKey:        "oauthstate",
		providerName:             "google",
	}
}

func (p *GoogleProvider) ProviderGetUserData(ctx context.Context, requestUriEntries url.Values) (OauthUserInfo, error) {

	loggr := logger.AcquireFrom(ctx)
	var userInfo GoogleProviderOauthUserInfo
	var providerOauthCode string = requestUriEntries["code"][0]

	if providerOauthCode == "" {
		loggr.Error("provider-oauth-code-not-detected")
		return nil, errors.New("ErrKeyProviderCodeNotDetected")
	}

	providerToken, err := p.config.Exchange(ctx, providerOauthCode)
	if err != nil {
		loggr.Error(fmt.Sprintf("provider-oauth-code-exchange-incorrect: %s", err.Error()))
		return nil, errors.New("ErrKeyProviderCodeExchangeIncorrect")
	}

	userInfoResponse, err := http.Get(p.providerUserInfoEndpoint + providerToken.AccessToken)
	if err != nil {
		loggr.Error(fmt.Sprintf("provider-failed-getting-user-info: %s", err.Error()))
		return nil, errors.New("ErrKeyProviderFailedGettingUserInfo")
	}

	defer userInfoResponse.Body.Close()

	err = json.NewDecoder(userInfoResponse.Body).Decode(&userInfo)
	if err != nil {
		loggr.Error(fmt.Sprintf("provider-failed-to-marshall-user-info: %s", err.Error()))
		return nil, errors.New("ErrKeyProviderFailedToMarshallUserInfo")
	}

	return &userInfo, nil
}

func (p *GoogleProvider) ProviderGetCookieKey() string {
	return p.providerCookieKey
}

// ProviderGenerateProtectionToken handles creating a small string of random text
// that can be used to when generating the auth url to protect user from CSRF attacks
func (p *GoogleProvider) ProviderGenerateProtectionToken() string {
	b := make([]byte, 16)
	rand.Read(b)
	return base64.URLEncoding.EncodeToString(b)
}

func (p *GoogleProvider) ProviderGetName() string {
	return p.providerName
}

func (p *GoogleProvider) ProviderGenerateAuthCodeUrl(protectionToken string) string {
	return p.config.AuthCodeURL(protectionToken)
}

func (p *GoogleProvider) ProviderVerifyRequestIsAuthentic(requestUriEntries url.Values, protectionCookien *http.Cookie) (string, bool) {

	return p.providerCookieKey, requestUriEntries["state"][0] == protectionCookien.Value
}
