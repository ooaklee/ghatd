package accessmanager

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/ooaklee/ghatd/external/emailmanager"
	"github.com/ooaklee/ghatd/external/oauth"
	user "github.com/ooaklee/ghatd/external/user/v2"
)

// Connection verification reuses the Settings proof store, but has a separate
// purpose. It proves the current account email without changing sign-in methods.
const connectionVerificationStage = "connect_email"

type connectionVerificationSnapshot struct {
	Email    string `json:"email"`
	Revision int64  `json:"revision"`
}

// Native verification is opt-in and requires a purpose-isolated store. Custom
// stores that only support disconnect remain compatible and advertise false.
func (s *Service) connectionVerificationStore() oauth.DisconnectChallengeStore {
	if s.oauthConnections == nil {
		return nil
	}
	provider, ok := s.oauthConnections.Store.(interface {
		ConnectionVerificationStore() oauth.DisconnectChallengeStore
	})
	if !ok {
		return nil
	}
	return provider.ConnectionVerificationStore()
}

func (s *Service) MobileOAuthConnectionVerificationAvailable(redirect string) bool {
	if !s.MobileOAuthDisconnectRedirectAllowed(redirect) || s.EmailManager == nil || !s.supportsConnectionRevisions() {
		return false
	}
	_, ok := s.connectionVerificationStore().(oauth.DisconnectChallengeReader)
	return ok
}

func (s *Service) StartMobileOAuthConnectionVerification(ctx context.Context, provider, redirect, token string) (*OAuthDisconnectStartResponse, error) {
	if !s.MobileOAuthConnectionVerificationAvailable(redirect) {
		return nil, user.ErrOAuthUnsupported
	}
	if provider != "apple" && provider != "google" {
		return nil, ErrBadRequest
	}
	account, details, err := s.connectionAccount(ctx, token, false)
	if err != nil {
		return nil, err
	}
	allowed, err := s.oauthConnections.Store.AcquireCooldown(ctx, account.ID)
	if err != nil {
		return nil, err
	}
	if !allowed {
		return nil, oauth.ErrDisconnectCooldown
	}
	payload, err := json.Marshal(connectionVerificationSnapshot{Email: account.Email, Revision: account.EmailRevision})
	if err != nil {
		return nil, err
	}
	challenge, code, proof, err := oauth.NewDisconnectChallenge(account.ID, details.AccessUUID, provider, payload)
	if err != nil {
		return nil, err
	}
	challenge.Stage, challenge.RedirectURI = connectionVerificationStage, redirect
	link := redirect + "#oauth_connect=" + provider + "&challenge_id=" + url.QueryEscape(challenge.ID) + "&token=" + url.QueryEscape(proof)
	label := "Google"
	if provider == "apple" {
		label = "Apple"
	}
	body := fmt.Sprintf(`<p>Verify your current sign-in email before connecting %s to your account.</p><p><a href="%s">Review email verification</a></p><p>Or enter this 8-character code in the app where you started:</p><p><strong>%s</strong></p><p>This request expires in 10 minutes. Opening the link does not change your account. Confirm in the same app session, then continue to %s to connect it. You remain signed in. If you did not request this, ignore this email.</p>`, label, html.EscapeString(link), code, label)
	if err = s.EmailManager.SendCustomEmail(ctx, &emailmanager.SendCustomEmailRequest{EmailSubject: "Verify email before connecting " + label, EmailPreview: "Confirm it is you without signing out", EmailBody: body, EmailTo: account.Email, WithFooter: true, UserId: account.ID, RecipientType: "USER"}); err != nil {
		return nil, ErrOAuthDisconnectDelivery
	}
	if err = s.connectionVerificationStore().Save(ctx, challenge); err != nil {
		return nil, err
	}
	return connectionVerificationResponse(challenge, account.Email), nil
}

func connectionVerificationResponse(c *oauth.DisconnectChallenge, email string) *OAuthDisconnectStartResponse {
	return &OAuthDisconnectStartResponse{ChallengeID: c.ID, ExpiresIn: int((time.Until(time.UnixMilli(c.ExpiresAt)) + time.Second - 1) / time.Second), ResendCooldownSeconds: 60, VerificationStage: connectionVerificationStage, Email: email, SignInEmail: email}
}

func (s *Service) readConnectionVerification(ctx context.Context, provider, id, redirect, token string) (*oauth.DisconnectChallenge, *user.UniversalUser, error) {
	if !s.MobileOAuthConnectionVerificationAvailable(redirect) {
		return nil, nil, user.ErrOAuthUnsupported
	}
	account, details, err := s.connectionAccount(ctx, token, false)
	if err != nil {
		return nil, nil, err
	}
	reader := s.connectionVerificationStore().(oauth.DisconnectChallengeReader) // required by native opt-in
	c, err := reader.Read(ctx, id, account.ID, details.AccessUUID, provider)
	if err != nil {
		return nil, nil, err
	}
	if c.Stage != connectionVerificationStage || c.RedirectURI != redirect {
		return nil, nil, oauth.ErrDisconnectProofInvalid
	}
	var snapshot connectionVerificationSnapshot
	if json.Unmarshal(c.Payload, &snapshot) != nil || snapshot.Email != account.Email || snapshot.Revision != account.EmailRevision {
		return nil, nil, oauth.ErrDisconnectProofInvalid
	}
	return c, account, nil
}

func (s *Service) ReviewMobileOAuthConnectionVerification(ctx context.Context, provider, id, redirect, token string) (*OAuthDisconnectStartResponse, error) {
	c, account, err := s.readConnectionVerification(ctx, provider, id, redirect, token)
	if err != nil {
		return nil, err
	}
	return connectionVerificationResponse(c, account.Email), nil
}

func (s *Service) ConfirmMobileOAuthConnectionVerification(ctx context.Context, provider string, request *OAuthDisconnectConfirmRequest, redirect, token string) (*OAuthDisconnectResponse, error) {
	if request == nil || len(request.Code) > 8 || len(request.Token) > 43 || (request.Code == "") == (request.Token == "") {
		return nil, ErrBadRequest
	}
	c, account, err := s.readConnectionVerification(ctx, provider, request.ChallengeID, redirect, token)
	if err != nil {
		return nil, err
	}
	spent, err := s.connectionVerificationStore().Consume(ctx, c.ID, account.ID, c.AccessUUID, provider, strings.ToUpper(strings.TrimSpace(request.Code)), request.Token)
	if err != nil {
		return nil, err
	}
	if spent.Stage != connectionVerificationStage || spent.RedirectURI != redirect || string(spent.Payload) != string(c.Payload) {
		return nil, oauth.ErrDisconnectProofInvalid
	}
	// Recheck account/session after spending proof. Minting tokens at this
	// revision cannot restore access after a concurrent account-security change.
	account, _, err = s.connectionAccount(ctx, token, false)
	if err != nil {
		return nil, err
	}
	tokens, err := s.createSessionToken(ctx, account, time.Now())
	if err != nil {
		return nil, err
	}
	if err = s.EphemeralStore.CreateAuth(ctx, account.ID, tokens); err != nil {
		return nil, err
	}
	s.auditOAuth(ctx, account.ID, provider, "user.oauth_connection_verified")
	// The existing session and other devices stay valid. Only this returned
	// session receives fresh proof; no provider identity has been changed yet.
	return &OAuthDisconnectResponse{Reauthenticated: true, Connected: connectedProviderNames(account), Email: account.Email, Session: tokens}, nil
}

type mobileConnectionVerificationService interface {
	StartMobileOAuthConnectionVerification(context.Context, string, string, string) (*OAuthDisconnectStartResponse, error)
	ReviewMobileOAuthConnectionVerification(context.Context, string, string, string, string) (*OAuthDisconnectStartResponse, error)
	ConfirmMobileOAuthConnectionVerification(context.Context, string, *OAuthDisconnectConfirmRequest, string, string) (*OAuthDisconnectResponse, error)
}

// Native-only, JSON POST/no Origin, with the same strict Settings return URI
// allowlist and single session cookie as the disconnection transport.
func (h *Handler) MobileOAuthConnectionVerification(w http.ResponseWriter, r *http.Request) {
	oauthHeaders(w)
	service, ok := h.Service.(mobileConnectionVerificationService)
	if !ok {
		_ = h.GetBaseResponseHandler().NewHTTPErrorResponse(w, user.ErrOAuthUnsupported)
		return
	}
	var body struct {
		OAuthDisconnectConfirmRequest
		RedirectURI string `json:"redirect_uri"`
	}
	var err error
	if r.Method == http.MethodGet {
		body.RedirectURI, err = nativeConnectionRedirect(r)
	} else if !decodeMobileJSON(w, r, &body) {
		err = ErrBadRequest
	}
	if err != nil {
		_ = h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}
	token, err := uniqueConnectionCookie(r, h.CookiePrefixAuthToken)
	if err != nil {
		_ = h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}
	provider := mux.Vars(r)["provider"]
	var response *OAuthDisconnectStartResponse
	switch {
	case r.Method == http.MethodGet:
		response, err = service.ReviewMobileOAuthConnectionVerification(r.Context(), provider, mux.Vars(r)["challengeID"], body.RedirectURI, token)
	case strings.HasSuffix(r.URL.Path, "/confirm"):
		var result *OAuthDisconnectResponse
		result, err = service.ConfirmMobileOAuthConnectionVerification(r.Context(), provider, &body.OAuthDisconnectConfirmRequest, body.RedirectURI, token)
		if err == nil {
			h.writeOAuthDisconnectResponse(w, result)
			return
		}
	default:
		if body.ChallengeID != "" || body.Code != "" || body.Token != "" {
			err = ErrBadRequest
			break
		}
		response, err = service.StartMobileOAuthConnectionVerification(r.Context(), provider, body.RedirectURI, token)
	}
	if err != nil {
		_ = h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}
	status := http.StatusAccepted
	if r.Method == http.MethodGet {
		status = http.StatusOK
	}
	_ = h.GetBaseResponseHandler().NewHTTPDataResponse(w, status, response)
}
