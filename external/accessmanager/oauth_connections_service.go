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
	// Empty disables native disconnects. Each address must be registered by
	// its app independently of the OAuth browser callback scheme.
	MobileRedirectURIs []string
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
	seen := make(map[string]bool)
	for _, redirect := range config.MobileRedirectURIs {
		if !oauth.ValidDisconnectRedirectURI(redirect) || seen[redirect] {
			return ErrBadRequest
		}
		seen[redirect] = true
	}
	config.MobileRedirectURIs = append([]string(nil), config.MobileRedirectURIs...)
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
	// VerificationStage reports which mailbox this challenge verifies:
	// "current_email" approves changing the sign-in email, "sign_in_email"
	// confirms the final (possibly new) sign-in email. Legacy challenges
	// are reported as "sign_in_email" final confirmations.
	VerificationStage string `json:"verification_stage"`
	// Email is the mailbox receiving this challenge.
	Email string `json:"email"`
	// SignInEmail is the final sign-in email the disconnect will apply.
	SignInEmail string `json:"sign_in_email"`
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
	// NextChallenge is set when a current-email approval stage succeeded and a
	// distinct sign-in-email challenge is now pending. Disconnected stays false.
	NextChallenge *OAuthDisconnectStartResponse `json:"next_challenge,omitempty"`
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
	account, details, freshNow, err := s.connectionAccountWithFreshness(ctx, token)
	if err != nil {
		return nil, nil, err
	}
	if fresh && !freshNow {
		return nil, nil, ErrOAuthReauthenticationRequired
	}
	return account, details, nil
}

// connectionAccountWithFreshness validates the session once and reports
// whether the signed authentication time is recent (<=5 minutes). Legacy
// zero-time sessions are never fresh.
func (s *Service) connectionAccountWithFreshness(ctx context.Context, token string) (*user.UniversalUser, *auth.TokenAccessDetails, bool, error) {
	details, err := s.AuthService.ExtractAccessTokenMetadataByString(ctx, token)
	if err != nil || details == nil || !details.IsAuthorized {
		return nil, nil, false, ErrOAuthReauthenticationRequired
	}
	owner, err := s.EphemeralStore.FetchAuth(ctx, details)
	if err != nil || owner != details.UserID {
		return nil, nil, false, ErrOAuthReauthenticationRequired
	}
	fresh := !details.AuthenticationTime.IsZero() && !details.AuthenticationTime.After(time.Now()) && time.Since(details.AuthenticationTime) <= 5*time.Minute
	result, err := s.UserService.GetUserByID(ctx, &user.GetUserByIDRequest{ID: details.UserID})
	if err != nil {
		return nil, nil, false, err
	}
	if result == nil || !oauthAccountActive(result.User) {
		return nil, nil, false, user.ErrOAuthRestricted
	}
	if result.User.EmailRevision != details.EmailRevision {
		return nil, nil, false, ErrOAuthReauthenticationRequired
	}
	return result.User, details, fresh, nil
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

// disconnectSnapshot captures the immutable account state a challenge must be
// revalidated against on every transition until final confirmation.
func disconnectSnapshot(account *user.UniversalUser, provider string) (*user.DisconnectOAuthProviderRequest, error) {
	request := &user.DisconnectOAuthProviderRequest{UserID: account.ID, Provider: provider, ExpectedEmail: account.Email, EmailRevision: account.EmailRevision}
	for _, identity := range account.OAuthIdentities {
		if identity.Provider == provider {
			request.Identities = append(request.Identities, user.OAuthIdentitySnapshot{Key: identity.Key, LinkedAt: identity.LinkedAt})
		}
	}
	if len(request.Identities) == 0 {
		return nil, user.ErrOAuthConnectionConflict
	}
	return request, nil
}

// validateDisconnectSnapshot rejects any account or selected-provider change
// since initiation, including relinks that do not increment the email revision.
func validateDisconnectSnapshot(account *user.UniversalUser, provider string, payload []byte) (*user.DisconnectOAuthProviderRequest, error) {
	var change user.DisconnectOAuthProviderRequest
	if json.Unmarshal(payload, &change) != nil || change.UserID != account.ID || change.Provider != provider || change.ExpectedEmail != account.Email || change.EmailRevision != account.EmailRevision {
		return nil, user.ErrOAuthConnectionConflict
	}
	current, err := disconnectSnapshot(account, provider)
	if err != nil || len(current.Identities) != len(change.Identities) {
		return nil, user.ErrOAuthConnectionConflict
	}
	for _, expected := range change.Identities {
		matched := false
		for _, actual := range current.Identities {
			if expected.Key == actual.Key && expected.LinkedAt.Equal(actual.LinkedAt) {
				matched = true
				break
			}
		}
		if !matched {
			return nil, user.ErrOAuthConnectionConflict
		}
	}
	return &change, nil
}

// MobileOAuthDisconnectRedirectAllowed reports the optional native Settings
// capability. Read support is required to check transport before consuming proof.
func (s *Service) MobileOAuthDisconnectRedirectAllowed(redirect string) bool {
	if s.oauthConnections == nil || redirect == "" {
		return false
	}
	if _, ok := s.oauthConnections.Store.(oauth.DisconnectChallengeReader); !ok {
		return false
	}
	for _, allowed := range s.oauthConnections.MobileRedirectURIs {
		if redirect == allowed {
			return true
		}
	}
	return false
}

func (s *Service) StartMobileOAuthDisconnect(ctx context.Context, provider, email, redirect, token string) (*OAuthDisconnectStartResponse, error) {
	if !s.MobileOAuthDisconnectRedirectAllowed(redirect) {
		return nil, user.ErrOAuthUnsupported
	}
	return s.startOAuthDisconnect(ctx, provider, email, token, redirect)
}

func (s *Service) StartOAuthDisconnect(ctx context.Context, provider, email, token string) (*OAuthDisconnectStartResponse, error) {
	return s.startOAuthDisconnect(ctx, provider, email, token, "")
}

func (s *Service) startOAuthDisconnect(ctx context.Context, provider, email, token, redirect string) (*OAuthDisconnectStartResponse, error) {
	repo, ok := s.UserService.(oauthConnectionsUsers)
	if !ok || !repo.SupportsOAuthConnections() || !s.supportsConnectionRevisions() || s.oauthConnections == nil || s.EmailManager == nil {
		return nil, user.ErrOAuthUnsupported
	}
	if provider != "google" && provider != "apple" {
		return nil, ErrBadRequest
	}
	// Same-email disconnects may start from any valid session: verifying the
	// current account email is itself account proof in this passwordless model.
	// A different sign-in email needs either recent authentication now or the
	// current-email approval stage below.
	account, details, fresh, err := s.connectionAccountWithFreshness(ctx, token)
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
	if email != account.Email && !fresh {
		// Stale session plus a new inbox must never proceed directly: ask the
		// user to first prove the CURRENT email. No account change happens here.
		return s.startDisconnectChallenge(ctx, account, details, provider, account.Email, email, "current_email", redirect)
	}
	return s.startDisconnectChallenge(ctx, account, details, provider, email, email, "sign_in_email", redirect, details.AuthenticationTime)
}

func (s *Service) startDisconnectChallenge(ctx context.Context, account *user.UniversalUser, details *auth.TokenAccessDetails, provider, recipient, signInEmail, stage, redirect string, authTime ...time.Time) (*OAuthDisconnectStartResponse, error) {
	return s.dispatchDisconnectChallenge(ctx, account, details, provider, recipient, signInEmail, stage, redirect, true, authTime...)
}

// dispatchDisconnectChallenge creates, emails and publishes one challenge. The
// user-facing resend cooldown is only applied to explicit start requests: the
// automatic follow-up after a consumed current-email approval replaces, not
// retries, the previous stage's email.
func (s *Service) dispatchDisconnectChallenge(ctx context.Context, account *user.UniversalUser, details *auth.TokenAccessDetails, provider, recipient, signInEmail, stage, redirect string, cooldown bool, authTime ...time.Time) (*OAuthDisconnectStartResponse, error) {
	snapshot, err := disconnectSnapshot(account, provider)
	if err != nil {
		return nil, err
	}
	snapshot.VerifiedEmail = signInEmail
	if cooldown {
		allowed, err := s.oauthConnections.Store.AcquireCooldown(ctx, account.ID)
		if err != nil {
			return nil, err
		}
		if !allowed {
			return nil, oauth.ErrDisconnectCooldown
		}
	}
	payload, err := json.Marshal(snapshot)
	if err != nil {
		return nil, err
	}
	var at time.Time
	if len(authTime) > 0 {
		at = authTime[0]
	}
	challenge, code, proof, err := oauth.NewDisconnectChallenge(account.ID, details.AccessUUID, provider, payload)
	if err != nil {
		return nil, err
	}
	challenge.Stage = stage
	challenge.RedirectURI = redirect
	if !at.IsZero() {
		challenge.AuthTimeMillis = at.UnixMilli()
	}
	// Keep the provider marker first for the host's early URL scrubber.
	linkBase := s.oauthConnections.Origin + "/settings"
	if redirect != "" {
		linkBase = redirect
	}
	link := linkBase + "#oauth_disconnect=" + provider + "&challenge_id=" + url.QueryEscape(challenge.ID) + "&token=" + url.QueryEscape(proof)
	label := "Google"
	if provider == "apple" {
		label = "Apple"
	}
	subject, body := disconnectEmailText(label, recipient, signInEmail, stage, link, code)
	if redirect != "" {
		body = strings.ReplaceAll(body, "browser where you started", "app where you started")
		body = strings.ReplaceAll(body, "same browser session", "same app session")
	}
	err = s.EmailManager.SendCustomEmail(ctx, &emailmanager.SendCustomEmailRequest{EmailSubject: subject, EmailPreview: "Keep access to your account with email sign-in", EmailBody: body, EmailTo: recipient, WithFooter: true, UserId: account.ID, RecipientType: "USER"})
	if err != nil {
		return nil, ErrOAuthDisconnectDelivery
	}
	// Publish only after successful delivery: failed sends leave no usable proof.
	if err = s.oauthConnections.Store.Save(ctx, challenge); err != nil {
		return nil, err
	}
	return &OAuthDisconnectStartResponse{ChallengeID: challenge.ID, ExpiresIn: int(oauth.DisconnectChallengeTTL.Seconds()), ResendCooldownSeconds: 60, VerificationStage: stage, Email: recipient, SignInEmail: signInEmail}, nil
}

// disconnectEmailText keeps stage-specific copy: current-email approval asks
// the user to authorise changing the sign-in email; the final stage confirms
// the exact mailbox that will become the sign-in email. Both carry a link and
// an 8-character code; opening the link alone never changes the account.
func disconnectEmailText(label, recipient, signInEmail, stage, link, code string) (string, string) {
	if stage == "current_email" {
		subject := "Confirm your current email before changing your sign-in email"
		body := fmt.Sprintf(`<p>You requested a change from %s to %s and removal of %s sign-in. Confirm your current address first; the new address will be verified separately before your account changes.</p><p><a href="%s">Confirm your current email and review the request</a></p><p>Or enter this 8-character code in the browser where you started:</p><p><strong>%s</strong></p><p>This request expires in 10 minutes. Opening the link does not change your account. If you did not request this, ignore this email. Your email and connected provider will stay unchanged.</p>`, html.EscapeString(recipient), html.EscapeString(signInEmail), label, html.EscapeString(link), code)
		return subject, body
	}
	subject := "Verify email before disconnecting " + label
	body := fmt.Sprintf(`<p>Verify this email to disconnect %s from your account. Once confirmed, %s will be your sign-in email. Your account and its data stay the same.</p><p><a href="%s">Verify email and review disconnect</a></p><p>Or enter this 8-character code in the browser where you started:</p><p><strong>%s</strong></p><p>This request expires in 10 minutes. Opening the link does not change your account; confirm in the same browser session. If you did not request this, ignore this email.</p>`, label, html.EscapeString(signInEmail), html.EscapeString(link), code)
	return subject, body
}

var ErrOAuthDisconnectDelivery = errors.New("OAuthDisconnectDeliveryFailed")
var ErrOAuthDisconnectSessionRequired = errors.New("OAuthDisconnectSessionRequired")

// ErrOAuthDisconnectChallengeNotFound reports an absent, expired or unowned
// review challenge. It deliberately matches "does not exist" semantics so a
// well-formed identifier reveals nothing about whether it is live for anyone
// else; owner and session checks are still enforced before any lookup reply.
var ErrOAuthDisconnectChallengeNotFound = errors.New("OAuthDisconnectChallengeNotFound")

func (s *Service) ConfirmMobileOAuthDisconnect(ctx context.Context, provider string, request *OAuthDisconnectConfirmRequest, redirect, token string) (*OAuthDisconnectResponse, error) {
	if !s.MobileOAuthDisconnectRedirectAllowed(redirect) {
		return nil, user.ErrOAuthUnsupported
	}
	return s.confirmOAuthDisconnect(ctx, provider, request, token, redirect)
}

func (s *Service) ConfirmOAuthDisconnect(ctx context.Context, provider string, request *OAuthDisconnectConfirmRequest, token string) (*OAuthDisconnectResponse, error) {
	return s.confirmOAuthDisconnect(ctx, provider, request, token, "")
}

func (s *Service) confirmOAuthDisconnect(ctx context.Context, provider string, request *OAuthDisconnectConfirmRequest, token, redirect string) (*OAuthDisconnectResponse, error) {
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
	// Native challenges must not be spent on a web endpoint or by another app.
	// Read is optional for legacy web-only stores; native opt-in requires it.
	if reader, ok := s.oauthConnections.Store.(oauth.DisconnectChallengeReader); ok {
		pending, readErr := reader.Read(ctx, request.ChallengeID, account.ID, details.AccessUUID, provider)
		if readErr != nil {
			return nil, readErr
		}
		if pending.RedirectURI != redirect {
			return nil, oauth.ErrDisconnectProofInvalid
		}
	}
	challenge, err := s.oauthConnections.Store.Consume(ctx, request.ChallengeID, account.ID, details.AccessUUID, provider, strings.ToUpper(strings.TrimSpace(request.Code)), request.Token)
	if err != nil {
		return nil, err
	}
	if challenge.RedirectURI != redirect {
		return nil, oauth.ErrDisconnectProofInvalid
	}
	change, err := validateDisconnectSnapshot(account, provider, challenge.Payload)
	if err != nil {
		return nil, err
	}
	if challenge.Stage == "current_email" {
		if change.VerifiedEmail == change.ExpectedEmail {
			return nil, oauth.ErrDisconnectProofInvalid
		}
		// The proof just consumed confirms the existing account. Carry that
		// timestamp, not the time the email was requested, into the next stage.
		next, nextErr := s.dispatchDisconnectChallenge(ctx, account, details, provider, change.VerifiedEmail, change.VerifiedEmail, "sign_in_email", redirect, false, time.Now())
		if nextErr != nil {
			// The old proof is spent. A failure to prepare the next challenge
			// must offer restart, never retry a proof that cannot work again.
			return nil, ErrOAuthDisconnectDelivery
		}
		return &OAuthDisconnectResponse{Disconnected: false, Connected: connectedProviderNames(account), Email: account.Email, NextChallenge: next}, nil
	}
	if challenge.Stage != "" && challenge.Stage != "sign_in_email" {
		return nil, oauth.ErrDisconnectProofInvalid
	}
	authTime := details.AuthenticationTime // pre-upgrade queued challenges
	if change.VerifiedEmail == change.ExpectedEmail {
		authTime = time.Now()
	} else if challenge.Stage != "" {
		if challenge.AuthTimeMillis <= 0 {
			return nil, oauth.ErrDisconnectProofInvalid
		}
		authTime = time.UnixMilli(challenge.AuthTimeMillis)
		if authTime.After(time.Now()) {
			return nil, oauth.ErrDisconnectProofInvalid
		}
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
	updated, err := repo.DisconnectOAuthProvider(ctx, change)
	if err != nil {
		return nil, err
	}
	s.auditOAuth(ctx, account.ID, provider, "user.oauth_disconnected")
	// The persisted revision invalidates old credentials immediately, including
	// tokens minted by an overlapping login/refresh. Redis cleanup is secondary.
	if err = s.EphemeralStore.DeleteAllTokenExceptedSpecified(ctx, account.ID, nil); err != nil {
		return nil, ErrOAuthDisconnectSessionRequired
	}
	tokens, err := s.createSessionToken(ctx, updated, authTime)
	if err != nil {
		return nil, ErrOAuthDisconnectSessionRequired
	}
	if err = s.EphemeralStore.CreateAuth(ctx, updated.ID, tokens); err != nil {
		return nil, ErrOAuthDisconnectSessionRequired
	}
	return &OAuthDisconnectResponse{Disconnected: true, Connected: connectedProviderNames(updated), Email: updated.Email, Session: tokens}, nil
}

// ReviewOAuthDisconnectChallenge renders the pending challenge state for the
// magic-link review screen. It takes no emailed proof, cannot consume or
// mutate anything, and exposes only the public start-response shape after
// revalidating session, owner, provider and the account snapshot.
func (s *Service) ReviewMobileOAuthDisconnectChallenge(ctx context.Context, provider, challengeID, redirect, token string) (*OAuthDisconnectStartResponse, error) {
	if !s.MobileOAuthDisconnectRedirectAllowed(redirect) {
		return nil, user.ErrOAuthUnsupported
	}
	return s.reviewOAuthDisconnectChallenge(ctx, provider, challengeID, token, redirect)
}

func (s *Service) ReviewOAuthDisconnectChallenge(ctx context.Context, provider, challengeID, token string) (*OAuthDisconnectStartResponse, error) {
	return s.reviewOAuthDisconnectChallenge(ctx, provider, challengeID, token, "")
}

func (s *Service) reviewOAuthDisconnectChallenge(ctx context.Context, provider, challengeID, token, redirect string) (*OAuthDisconnectStartResponse, error) {
	if !s.supportsConnectionRevisions() || s.oauthConnections == nil || (provider != "google" && provider != "apple") || !oauth.DisconnectIDPattern().MatchString(challengeID) {
		return nil, ErrBadRequest
	}
	reader, ok := s.oauthConnections.Store.(oauth.DisconnectChallengeReader)
	if !ok {
		return nil, user.ErrOAuthUnsupported
	}
	account, details, err := s.connectionAccount(ctx, token, false)
	if err != nil {
		return nil, err
	}
	challenge, err := reader.Read(ctx, challengeID, account.ID, details.AccessUUID, provider)
	if errors.Is(err, oauth.ErrDisconnectProofInvalid) {
		return nil, ErrOAuthDisconnectChallengeNotFound
	}
	if err != nil {
		return nil, err
	}
	if challenge.RedirectURI != redirect {
		return nil, ErrOAuthDisconnectChallengeNotFound
	}
	snapshot, err := validateDisconnectSnapshot(account, provider, challenge.Payload)
	if err != nil {
		return nil, err
	}
	stage := challenge.Stage
	if stage == "" {
		stage = "sign_in_email"
	}
	if stage != "sign_in_email" && stage != "current_email" {
		return nil, oauth.ErrDisconnectProofInvalid
	}
	recipient := snapshot.VerifiedEmail
	if stage == "current_email" {
		recipient = snapshot.ExpectedEmail
	}
	remaining := time.Until(time.UnixMilli(challenge.ExpiresAt))
	if remaining <= 0 {
		return nil, ErrOAuthDisconnectChallengeNotFound
	}
	return &OAuthDisconnectStartResponse{ChallengeID: challenge.ID, ExpiresIn: int((remaining + time.Second - 1) / time.Second), ResendCooldownSeconds: 60, VerificationStage: stage, Email: recipient, SignInEmail: snapshot.VerifiedEmail}, nil
}
