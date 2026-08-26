package oauth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/url"
	"time"

	"github.com/ooaklee/ghatd/external/logger"
	"github.com/ooaklee/ghatd/external/observability"
	"go.uber.org/zap"
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

// GetUserEmail returns the email address supplied by Google.
func (g *GoogleProviderOauthUserInfo) GetUserEmail() string {
	return g.Email
}

// GetUserFirstName returns the given name supplied by Google.
func (g *GoogleProviderOauthUserInfo) GetUserFirstName() string {
	return g.FirstName
}

// GetUserLastName returns the family name supplied by Google.
func (g *GoogleProviderOauthUserInfo) GetUserLastName() string {
	return g.FamilyName
}

// IsUserEmailVerifiedByProvider reports whether Google verified the email address.
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
	httpClient               *http.Client
}

// NewGoogleProvider creates a Google OAuth provider.
func NewGoogleProvider(r *NewGoogleProviderRequest) *GoogleProvider {
	httpClient := r.HTTPClient
	if httpClient == nil {
		httpClient = observability.NewHTTPClient(http.DefaultTransport, 30*time.Second)
	}

	return &GoogleProvider{
		config: &oauth2.Config{
			RedirectURL:  r.RedirectURL,
			ClientID:     r.ClientID,
			ClientSecret: r.ClientSecret,
			Scopes:       []string{"https://www.googleapis.com/auth/userinfo.email", "https://www.googleapis.com/auth/userinfo.profile"},
			Endpoint:     google.Endpoint,
		},
		providerUserInfoEndpoint: "https://www.googleapis.com/oauth2/v2/userinfo",
		providerCookieKey:        "oauthstate",
		providerName:             "google",
		httpClient:               httpClient,
	}
}

// ProviderGetUserData exchanges the callback code and retrieves the Google user profile.
func (p *GoogleProvider) ProviderGetUserData(ctx context.Context, requestUriEntries url.Values) (OauthUserInfo, error) {

	logger := logger.AcquirePackageFrom(ctx, "external/oauth")
	var userInfo GoogleProviderOauthUserInfo
	var providerOauthCode string = requestUriEntries["code"][0]

	if providerOauthCode == "" {
		logger.Error("provider-oauth-code-not-detected")
		return nil, ErrProviderCodeNotDetected
	}

	// oauth2 uses the client stored in the context for its token exchange. This
	// keeps that request in the same distributed trace as the callback.
	ctx = context.WithValue(ctx, oauth2.HTTPClient, p.httpClient)
	providerToken, err := p.config.Exchange(ctx, providerOauthCode)
	if err != nil {
		logger.Error("provider-oauth-code-exchange-incorrect", zap.Error(err))
		return nil, ErrProviderCodeExchangeIncorrect
	}

	userInfoRequest, err := http.NewRequestWithContext(ctx, http.MethodGet, p.providerUserInfoEndpoint, nil)
	if err != nil {
		logger.Error("provider-failed-creating-user-info-request", zap.Error(err))
		return nil, ErrProviderFailedGettingUserInfo
	}
	userInfoRequest.Header.Set("Authorization", "Bearer "+providerToken.AccessToken)
	userInfoResponse, err := p.httpClient.Do(userInfoRequest)
	if err != nil {
		logger.Error("provider-failed-getting-user-info", zap.Error(err))
		return nil, ErrProviderFailedGettingUserInfo
	}

	defer userInfoResponse.Body.Close()
	if userInfoResponse.StatusCode < http.StatusOK || userInfoResponse.StatusCode >= http.StatusMultipleChoices {
		logger.Error("provider-user-info-returned-invalid-status", zap.Int("status-code", userInfoResponse.StatusCode))
		return nil, ErrProviderFailedGettingUserInfo
	}

	err = json.NewDecoder(userInfoResponse.Body).Decode(&userInfo)
	if err != nil {
		logger.Error("provider-failed-to-marshall-user-info", zap.Error(err))
		return nil, ErrProviderFailedToMarshallUserInfo
	}

	return &userInfo, nil
}

// ProviderGetCookieKey returns the cookie key used for OAuth state.
func (p *GoogleProvider) ProviderGetCookieKey() string {
	return p.providerCookieKey
}

// ProviderGenerateProtectionToken creates random state used to protect the
// authorisation flow from CSRF attacks.
func (p *GoogleProvider) ProviderGenerateProtectionToken() string {
	b := make([]byte, 16)
	rand.Read(b)
	return base64.URLEncoding.EncodeToString(b)
}

// ProviderGetName returns the provider identifier.
func (p *GoogleProvider) ProviderGetName() string {
	return p.providerName
}

// ProviderGenerateAuthCodeUrl returns the Google authorisation URL.
func (p *GoogleProvider) ProviderGenerateAuthCodeUrl(protectionToken string) string {
	return p.config.AuthCodeURL(protectionToken)
}

// ProviderVerifyRequestIsAuthentic validates callback state against the protection cookie.
func (p *GoogleProvider) ProviderVerifyRequestIsAuthentic(requestUriEntries url.Values, protectionCookien *http.Cookie) (string, bool) {

	return p.providerCookieKey, requestUriEntries["state"][0] == protectionCookien.Value
}
