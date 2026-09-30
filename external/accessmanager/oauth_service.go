package accessmanager

import (
	"context"
	"errors"
	"github.com/ooaklee/ghatd/external/audit"
	"github.com/ooaklee/ghatd/external/auth"
	"github.com/ooaklee/ghatd/external/oauth"
	user "github.com/ooaklee/ghatd/external/user/v2"
	"net/http"
	"net/url"
	"time"
)

// ErrOAuthReauthenticationRequired requires recent proof from this particular session.
var ErrOAuthReauthenticationRequired = errors.New("OAuthReauthenticationRequired")

// oauthUserService is an optional persistence capability, preserving existing host interfaces.
type oauthUserService interface {
	GetUserByOAuthIdentity(context.Context, *user.OAuthIdentity) (*user.UniversalUser, error)
	CreateOAuthUser(context.Context, *user.CreateOAuthUserRequest) (*user.CreateOAuthUserResponse, error)
	LinkOAuthIdentity(context.Context, string, *user.OAuthIdentity) (*user.UniversalUser, error)
	RecordOAuthLogin(context.Context, string, time.Time) (*user.UniversalUser, error)
}

// authenticationTimeCreator preserves session freshness without changing AuthService.
type authenticationTimeCreator interface {
	CreateTokenWithAuthenticationTime(context.Context, auth.UserModel, time.Time) (*auth.TokenDetails, error)
}

// createSessionToken carries trusted login time; legacy implementations grant no linking freshness.
func (s *Service) createSessionToken(ctx context.Context, user auth.UserModel, at time.Time) (*auth.TokenDetails, error) {
	if creator, ok := s.AuthService.(authenticationTimeCreator); ok {
		return creator.CreateTokenWithAuthenticationTime(ctx, user, at)
	}
	return s.AuthService.CreateToken(ctx, user)
}

// secureOAuthProvider fails closed when a legacy provider lacks token/state guarantees.
func (s *Service) secureOAuthProvider(name string) (oauth.ContextualFlowProvider, error) {
	for _, provider := range s.OauthServices {
		if provider.ProviderGetName() == name {
			if secure, ok := provider.(oauth.ContextualFlowProvider); ok {
				return secure, nil
			}
			return nil, oauth.ErrSecureProviderIncompleteConfig
		}
	}
	return nil, ErrProvidersPassedNotFound
}

// OAuthProviders lists only supported providers with the required host capabilities.
func (s *Service) OAuthProviders() []string {
	result := []string{}
	if _, ok := s.UserService.(oauthUserService); !ok {
		return result
	}
	if _, ok := s.AuthService.(authenticationTimeCreator); !ok {
		return result
	}
	for _, name := range []string{"google", "apple"} {
		if _, err := s.secureOAuthProvider(name); err == nil {
			result = append(result, name)
		}
	}
	return result
}

// OauthLogin stores browser completion and fresh linking context before redirecting.
func (s *Service) OauthLogin(ctx context.Context, r *OauthLoginRequest) (*OauthLoginResponse, error) {
	if r == nil {
		return nil, ErrBadRequest
	}
	provider, err := s.secureOAuthProvider(r.Provider)
	if err != nil {
		return nil, err
	}
	if _, ok := s.UserService.(oauthUserService); !ok {
		return nil, user.ErrOAuthUnsupported
	}
	if _, ok := s.AuthService.(authenticationTimeCreator); !ok {
		return nil, user.ErrOAuthUnsupported
	}
	if r.Link != nil {
		if err := s.validateOAuthLinkProof(ctx, r.Link); err != nil {
			return nil, err
		}
	}
	txn, err := provider.BeginSecureTransactionWithOptions(ctx, r.RequestUrl, oauth.SecureFlowOptions{Browser: r.Browser, Link: r.Link})
	if err != nil {
		return nil, err
	}
	return &OauthLoginResponse{CookieCore: &http.Cookie{Name: provider.ProviderGetCookieKey(), Value: txn.TransactionID, Expires: time.Now().Add(oauth.TransactionTTL), MaxAge: int(oauth.TransactionTTL.Seconds())}, ProviderAuthCodeUrl: txn.AuthorisationURL}, nil
}

// validateOAuthLinkProof rechecks the initiating session, freshness and current account state.
func (s *Service) validateOAuthLinkProof(ctx context.Context, proof *oauth.LinkProof) error {
	if proof == nil || proof.UserID == "" || proof.AccessUUID == "" || proof.AuthenticationTime.IsZero() || proof.AuthenticationTime.After(time.Now()) || time.Since(proof.AuthenticationTime) > 5*time.Minute {
		return ErrOAuthReauthenticationRequired
	}
	owner, err := s.EphemeralStore.FetchAuth(ctx, &auth.TokenAccessDetails{UserID: proof.UserID, AccessUUID: proof.AccessUUID})
	if err != nil || owner != proof.UserID {
		return ErrOAuthReauthenticationRequired
	}
	response, err := s.UserService.GetUserByID(ctx, &user.GetUserByIDRequest{ID: proof.UserID})
	if err != nil {
		return ErrOAuthReauthenticationRequired
	}
	if !oauthAccountActive(response.User) {
		return user.ErrOAuthRestricted
	}
	return nil
}

// OAuthLink starts linking using a signed access cookie rather than a client-supplied user ID.
func (s *Service) OAuthLink(ctx context.Context, r *OauthLoginRequest, accessToken string) (*OauthLoginResponse, error) {
	details, err := s.AuthService.ExtractAccessTokenMetadataByString(ctx, accessToken)
	if err != nil || !details.IsAuthorized {
		return nil, ErrOAuthReauthenticationRequired
	}
	request := *r
	request.Link = &oauth.LinkProof{UserID: details.UserID, AccessUUID: details.AccessUUID, AuthenticationTime: details.AuthenticationTime}
	return s.OauthLogin(ctx, &request)
}

// oauthAccountActive denies every restricted status before provider tokens are issued.
func oauthAccountActive(account *user.UniversalUser) bool {
	return account != nil && account.Status == user.AccountStatusKeyActive && account.Verification != nil && account.Verification.EmailVerified
}

// OauthCallback resolves signed issuer/subject identities and never links matching email implicitly.
func (s *Service) OauthCallback(ctx context.Context, r *OauthCallbackRequest) (*OauthCallbackResponse, error) {
	if r == nil {
		return nil, ErrBadRequest
	}
	provider, err := s.secureOAuthProvider(r.Provider)
	if err != nil {
		return nil, err
	}
	response := &OauthCallbackResponse{ProviderStateCookieKey: provider.ProviderGetCookieKey()}
	var cookie *http.Cookie
	for _, candidate := range r.RequestCookies {
		if candidate.Name == provider.ProviderGetCookieKey() {
			if cookie != nil {
				return response, oauth.ErrSecureTransactionInvalidState
			}
			cookie = candidate
		}
	}
	if cookie == nil {
		return response, ErrProviderCookieNotFound
	}
	result, err := provider.CompleteSecureTransaction(ctx, &oauth.SecureCallbackRequest{TransactionID: cookie.Value, Query: r.UrlUri, Method: r.Method})
	if result != nil && result.Transaction != nil {
		response.Browser = result.Transaction.Options.Browser
		response.RequestUrl = result.ReturnPath
	}
	if err != nil {
		return response, err
	}
	if result == nil || result.Transaction == nil || result.Provider != r.Provider {
		return response, oauth.ErrSecureIDTokenInvalid
	}
	info, ok := result.UserInfo.(oauth.IdentityUserInfo)
	if !ok {
		return response, oauth.ErrSecureIDTokenInvalid
	}
	identity, err := user.CanonicalOAuthIdentity(&user.OAuthIdentity{Provider: r.Provider, Issuer: info.GetProviderIssuer(), Subject: info.GetProviderSubject()})
	if err != nil {
		return response, oauth.ErrSecureIDTokenInvalid
	}
	repository, ok := s.UserService.(oauthUserService)
	if !ok {
		return response, user.ErrOAuthUnsupported
	}
	// A supplied email must be verified; repeat Apple sign-in may omit it entirely.
	if (r.Provider == "google" || info.GetUserEmail() != "") && !info.IsUserEmailVerifiedByProvider() {
		return response, oauth.ErrSecureIDTokenUnverified
	}
	if proof := result.Transaction.Options.Link; proof != nil {
		if err = s.validateOAuthLinkProof(ctx, proof); err != nil {
			return response, err
		}
		if _, err = repository.LinkOAuthIdentity(ctx, proof.UserID, identity); err != nil {
			return response, err
		}
		response.Linked = true
		path, _ := url.Parse(response.RequestUrl)
		query := path.Query()
		query.Set("oauth_linked", r.Provider)
		path.RawQuery = query.Encode()
		response.RequestUrl = path.String()
		s.auditOAuth(ctx, proof.UserID, r.Provider, "user.oauth_linked")
		return response, nil
	}
	account, err := repository.GetUserByOAuthIdentity(ctx, identity)
	if errors.Is(err, user.ErrUserNotFound) {
		if info.GetUserEmail() == "" || !info.IsUserEmailVerifiedByProvider() {
			return response, oauth.ErrSecureIDTokenUnverified
		}
		created, createErr := repository.CreateOAuthUser(ctx, &user.CreateOAuthUserRequest{Identity: *identity, Email: info.GetUserEmail(), FirstName: info.GetUserFirstName(), LastName: info.GetUserLastName()})
		if createErr != nil {
			return response, createErr
		}
		account = created.User
		if created.Created {
			s.associateNewUser(ctx, account)
			s.auditOAuth(ctx, account.ID, r.Provider, string(audit.UserAccountNewSso))
		}
	} else if err != nil {
		return response, err
	}
	if !oauthAccountActive(account) {
		return response, user.ErrOAuthRestricted
	}
	account, err = repository.RecordOAuthLogin(ctx, account.ID, time.Now())
	if err != nil {
		return response, err
	}
	if !oauthAccountActive(account) {
		return response, user.ErrOAuthRestricted
	}
	tokens, err := s.createSessionToken(ctx, account, time.Now())
	if err != nil {
		return response, err
	}
	if err = s.EphemeralStore.CreateAuth(ctx, account.ID, tokens); err != nil {
		return response, err
	}
	response.AccessToken = tokens.AccessToken
	response.RefreshToken = tokens.RefreshToken
	response.AccessTokenExpiresAt = tokens.AtExpires
	response.RefreshTokenExpiresAt = tokens.RtExpires
	s.auditOAuth(ctx, account.ID, r.Provider, string(audit.UserLoginSso))
	return response, nil
}

// auditOAuth logs only the application user ID and provider name.
func (s *Service) auditOAuth(ctx context.Context, id, provider, action string) {
	if s.AuditService != nil {
		_ = s.AuditService.LogAuditEvent(ctx, &audit.LogAuditEventRequest{ActorId: audit.AuditActorIdSystem, Action: audit.AuditAction(action), TargetId: id, TargetType: audit.User, Domain: "accessmanager", Details: audit.UserSsoEventDetails{SsoProvider: provider}})
	}
}
