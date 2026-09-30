package accessmanager

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net/mail"
	"net/url"
	"strings"
	"time"

	"github.com/ooaklee/ghatd/external/auth"
	"github.com/ooaklee/ghatd/external/emailmanager"
	"github.com/ooaklee/ghatd/external/oauth"
	user "github.com/ooaklee/ghatd/external/user/v2"
)

// ConfigureOAuthConnections is an opt-in for verified disconnects. Status reads
// need no new configuration. Call during host composition, before serving.
type OAuthConnectionsConfig struct {
	Origin string
	Store  oauth.DisconnectChallengeStore
}

func (s *Service) ConfigureOAuthConnections(config OAuthConnectionsConfig) error {
	u, err := url.Parse(config.Origin)
	if err != nil || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || u.ForceQuery || u.Opaque != "" || u.String() != config.Origin || config.Store == nil {
		return ErrBadRequest
	}
	localhost := u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1" || u.Hostname() == "::1"
	if u.Scheme != "https" && (u.Scheme != "http" || !localhost) {
		return ErrBadRequest
	}
	s.oauthConnections = &config
	return nil
}
func (s *Service) OAuthConnectionsOrigin() string {
	if s.oauthConnections == nil {
		return ""
	}
	return s.oauthConnections.Origin
}

type oauthConnectionsUsers interface {
	SupportsOAuthConnections() bool
	DisconnectOAuthProvider(context.Context, *user.DisconnectOAuthProviderRequest) (*user.UniversalUser, error)
	LinkOAuthIdentityAtRevision(context.Context, string, *user.OAuthIdentity, int64) (*user.UniversalUser, error)
}
type OAuthConnectionsResponse struct {
	Connected           []string `json:"connected"`
	Available           []string `json:"available"`
	Email               string   `json:"email"`
	DisconnectAvailable bool     `json:"disconnect_available"`
}
type OAuthDisconnectStartResponse struct {
	ChallengeID           string `json:"challenge_id"`
	ExpiresIn             int    `json:"expires_in"`
	ResendCooldownSeconds int    `json:"resend_cooldown_seconds"`
}
type OAuthDisconnectConfirmRequest struct {
	ChallengeID string `json:"challenge_id"`
	Code        string `json:"code,omitempty"`
	Token       string `json:"token,omitempty"`
}
type OAuthDisconnectResponse struct {
	Session      *auth.TokenDetails `json:"-"`
	Disconnected bool               `json:"disconnected"`
	Connected    []string           `json:"connected"`
	Email        string             `json:"email"`
}

func connectedProviderNames(account *user.UniversalUser) []string {
	names := []string{}
	for _, name := range []string{"google", "apple"} {
		for _, identity := range account.OAuthIdentities {
			if identity.Provider == name {
				names = append(names, name)
				break
			}
		}
	}
	return names
}
func (s *Service) connectionAccount(ctx context.Context, token string, fresh bool) (*user.UniversalUser, *auth.TokenAccessDetails, error) {
	details, err := s.AuthService.ExtractAccessTokenMetadataByString(ctx, token)
	if err != nil || details == nil || !details.IsAuthorized {
		return nil, nil, ErrOAuthReauthenticationRequired
	}
	owner, err := s.EphemeralStore.FetchAuth(ctx, details)
	if err != nil || owner != details.UserID {
		return nil, nil, ErrOAuthReauthenticationRequired
	}
	if fresh && (details.AuthenticationTime.IsZero() || details.AuthenticationTime.After(time.Now()) || time.Since(details.AuthenticationTime) > 5*time.Minute) {
		return nil, nil, ErrOAuthReauthenticationRequired
	}
	result, err := s.UserService.GetUserByID(ctx, &user.GetUserByIDRequest{ID: details.UserID})
	if err != nil {
		return nil, nil, err
	}
	if result == nil || !oauthAccountActive(result.User) {
		return nil, nil, user.ErrOAuthRestricted
	}
	if result.User.EmailRevision != details.EmailRevision {
		return nil, nil, ErrOAuthReauthenticationRequired
	}
	return result.User, details, nil
}
func (s *Service) supportsConnectionRevisions() bool {
	signer, ok := s.AuthService.(interface{ SupportsEmailRevision() bool })
	return ok && signer.SupportsEmailRevision()
}
func (s *Service) OAuthConnections(ctx context.Context, token string) (*OAuthConnectionsResponse, error) {
	account, _, err := s.connectionAccount(ctx, token, false)
	if err != nil {
		return nil, err
	}
	repo, ok := s.UserService.(oauthConnectionsUsers)
	return &OAuthConnectionsResponse{Connected: connectedProviderNames(account), Available: s.OAuthProviders(), Email: account.Email, DisconnectAvailable: ok && repo.SupportsOAuthConnections() && s.supportsConnectionRevisions() && s.oauthConnections != nil && s.EmailManager != nil}, nil
}
func (s *Service) StartOAuthDisconnect(ctx context.Context, provider, email, token string) (*OAuthDisconnectStartResponse, error) {
	repo, ok := s.UserService.(oauthConnectionsUsers)
	if !ok || !repo.SupportsOAuthConnections() || !s.supportsConnectionRevisions() || s.oauthConnections == nil || s.EmailManager == nil {
		return nil, user.ErrOAuthUnsupported
	}
	if provider != "google" && provider != "apple" {
		return nil, ErrBadRequest
	}
	account, details, err := s.connectionAccount(ctx, token, true)
	if err != nil {
		return nil, err
	}
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		email = account.Email
	}
	parsed, err := mail.ParseAddress(email)
	if err != nil || parsed.Address != email || len(email) > 254 {
		return nil, ErrBadRequest
	}
	request := &user.DisconnectOAuthProviderRequest{UserID: account.ID, Provider: provider, ExpectedEmail: account.Email, VerifiedEmail: email, EmailRevision: account.EmailRevision}
	for _, identity := range account.OAuthIdentities {
		if identity.Provider == provider {
			request.Identities = append(request.Identities, user.OAuthIdentitySnapshot{Key: identity.Key, LinkedAt: identity.LinkedAt})
		}
	}
	if len(request.Identities) == 0 {
		return nil, user.ErrOAuthConnectionConflict
	}
	allowed, err := s.oauthConnections.Store.AcquireCooldown(ctx, account.ID)
	if err != nil {
		return nil, err
	}
	if !allowed {
		return nil, oauth.ErrDisconnectCooldown
	}
	payload, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}
	challenge, code, proof, err := oauth.NewDisconnectChallenge(account.ID, details.AccessUUID, provider, payload)
	if err != nil {
		return nil, err
	}
	// Keep the provider marker first for the host's early URL scrubber.
	link := s.oauthConnections.Origin + "/settings#oauth_disconnect=" + provider + "&challenge_id=" + url.QueryEscape(challenge.ID) + "&token=" + url.QueryEscape(proof)
	label := "Google"
	if provider == "apple" {
		label = "Apple"
	}
	body := fmt.Sprintf(`<p>Verify this email to disconnect %s from your account. Once confirmed, %s will be your sign-in email. Your account and its data stay the same.</p><p><a href="%s">Verify email and review disconnect</a></p><p>Or enter this 8-character code in the browser where you started:</p><p><strong>%s</strong></p><p>This request expires in 10 minutes. Opening the link does not change your account; confirm in the same browser session. If you did not request this, ignore this email.</p>`, label, html.EscapeString(email), html.EscapeString(link), code)
	err = s.EmailManager.SendCustomEmail(ctx, &emailmanager.SendCustomEmailRequest{EmailSubject: "Verify email before disconnecting " + label, EmailPreview: "Keep access to your account with email sign-in", EmailBody: body, EmailTo: email, WithFooter: true, UserId: account.ID, RecipientType: "USER"})
	if err != nil {
		return nil, ErrOAuthDisconnectDelivery
	}
	// Publish only after successful delivery: failed sends leave no usable proof.
	if err = s.oauthConnections.Store.Save(ctx, challenge); err != nil {
		return nil, err
	}
	return &OAuthDisconnectStartResponse{ChallengeID: challenge.ID, ExpiresIn: int(oauth.DisconnectChallengeTTL.Seconds()), ResendCooldownSeconds: 60}, nil
}

var ErrOAuthDisconnectDelivery = errors.New("OAuthDisconnectDeliveryFailed")
var ErrOAuthDisconnectSessionRequired = errors.New("OAuthDisconnectSessionRequired")

func (s *Service) ConfirmOAuthDisconnect(ctx context.Context, provider string, request *OAuthDisconnectConfirmRequest, token string) (*OAuthDisconnectResponse, error) {
	repo, ok := s.UserService.(oauthConnectionsUsers)
	if !ok || !repo.SupportsOAuthConnections() || !s.supportsConnectionRevisions() || s.oauthConnections == nil {
		return nil, user.ErrOAuthUnsupported
	}
	if request == nil || (provider != "google" && provider != "apple") || len(request.Code) > 8 || len(request.Token) > 43 || (request.Code == "") == (request.Token == "") {
		return nil, ErrBadRequest
	}
	account, details, err := s.connectionAccount(ctx, token, false)
	if err != nil {
		return nil, err
	}
	challenge, err := s.oauthConnections.Store.Consume(ctx, request.ChallengeID, account.ID, details.AccessUUID, provider, strings.ToUpper(strings.TrimSpace(request.Code)), request.Token)
	if err != nil {
		return nil, err
	}
	var change user.DisconnectOAuthProviderRequest
	if json.Unmarshal(challenge.Payload, &change) != nil || change.UserID != account.ID || change.Provider != provider || change.ExpectedEmail != account.Email || change.EmailRevision != account.EmailRevision {
		return nil, user.ErrOAuthConnectionConflict
	}
	if change.VerifiedEmail != account.Email {
		owner, lookupErr := findUserByEmail(ctx, s.UserService, &user.GetUserByEmailRequest{Email: change.VerifiedEmail})
		if lookupErr != nil && !errors.Is(lookupErr, user.ErrUserNotFound) {
			return nil, lookupErr
		}
		if owner != nil && owner.User != nil && owner.User.ID != account.ID {
			return nil, user.ErrEmailAlreadyExists
		}
	}
	updated, err := repo.DisconnectOAuthProvider(ctx, &change)
	if err != nil {
		return nil, err
	}
	s.auditOAuth(ctx, account.ID, provider, "user.oauth_disconnected")
	// The persisted revision invalidates old credentials immediately, including
	// tokens minted by an overlapping login/refresh. Redis cleanup is secondary.
	if err = s.EphemeralStore.DeleteAllTokenExceptedSpecified(ctx, account.ID, nil); err != nil {
		return nil, ErrOAuthDisconnectSessionRequired
	}
	tokens, err := s.createSessionToken(ctx, updated, details.AuthenticationTime)
	if err != nil {
		return nil, ErrOAuthDisconnectSessionRequired
	}
	if err = s.EphemeralStore.CreateAuth(ctx, updated.ID, tokens); err != nil {
		return nil, ErrOAuthDisconnectSessionRequired
	}
	return &OAuthDisconnectResponse{Disconnected: true, Connected: connectedProviderNames(updated), Email: updated.Email, Session: tokens}, nil
}
