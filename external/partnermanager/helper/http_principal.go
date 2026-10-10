package partnermanagerhelper

import (
	"context"
	"errors"

	"github.com/golang-jwt/jwt/v5"
	"github.com/ooaklee/ghatd/external/auth"
	"github.com/ooaklee/ghatd/external/http/browsersecurity"
	"github.com/ooaklee/ghatd/external/partneraccess"
	"github.com/ooaklee/ghatd/external/partnermanager"
	partnerhttp "github.com/ooaklee/ghatd/external/partnermanager/http"
	userv2 "github.com/ooaklee/ghatd/external/user/v2"
	"net/http"
	"strings"
)

// NativeTokenVerifier borrows the owning live token verifier. Metadata and the
// verified JWT must come from the same request and owning verifier. The helper
// checks the metadata actor and signature/algorithm/audience separately; it trusts
// neither a decoded body nor a session result's native audience alone.
type NativeTokenVerifier interface {
	// ExtractTokenMetadata extracts verified native token access details from the
	// request via the owning live token verifier.
	ExtractTokenMetadata(context.Context, *http.Request) (*auth.TokenAccessDetails, error)
	// VerifyToken verifies the request's native JWT and returns the verified token
	// from the owning live verifier.
	VerifyToken(context.Context, *http.Request) (*jwt.Token, error)
}

// HTTPPrincipalConfig supplies current owning ports and an explicit host binding.
// Configure once before serving. CookieName is the authentication cookie, not
// the CSRF cookie: pass the existing shared guard to partnerhttp.Config.Security
// to reuse the host-chosen CSRF cookie and binding. No grants are provisioned.
type HTTPPrincipalConfig struct {
	CookieName             string
	TrustedNativeClientIDs []string
	Members                partneraccess.SessionAuthenticator
	Sessions               partneraccess.SessionVerifier
	Tokens                 NativeTokenVerifier
	// TransportIdentity must bind the verified actor, credential and optional
	// verified native audience to the same identity expected by the shared guard.
	// Host-specific accompanying cookies remain inside this callback.
	TransportIdentity func(*http.Request, string, string, string) (browsersecurity.Identity, error)
}

// NewHTTPPrincipalResolver validates and copies configuration without invoking
// its borrowed ports. Every Resolve reauthenticates and checks current admission.
// An empty native allowlist disables native requests without disabling members.
func NewHTTPPrincipalResolver(cfg HTTPPrincipalConfig) (partnerhttp.PrincipalResolver, error) {
	if nilHelperPort(cfg.Members) || nilHelperPort(cfg.Sessions) || nilHelperPort(cfg.Tokens) || cfg.TransportIdentity == nil {
		return nil, partnermanager.ErrUnavailable
	}
	if !validReferralCookieName(cfg.CookieName) {
		return nil, partnermanager.ErrInvalid
	}
	ids := make(map[string]bool, len(cfg.TrustedNativeClientIDs))
	for _, id := range cfg.TrustedNativeClientIDs {
		if id == "" || len(id) > 128 {
			return nil, partnermanager.ErrInvalid
		}
		ids[id] = true
	}
	return &httpPrincipalResolver{members: cfg.Members, sessions: cfg.Sessions, tokens: cfg.Tokens,
		authCookie: cfg.CookieName, nativeIDs: ids, transportIdentity: cfg.TransportIdentity}, nil
}

// httpPrincipalResolver holds the borrowed member/session/token ports, cookie
// name, native audience allowlist and the host transport-identity binding used
// to authenticate each request.
type httpPrincipalResolver struct {
	members           partneraccess.SessionAuthenticator
	sessions          partneraccess.SessionVerifier
	tokens            NativeTokenVerifier
	authCookie        string
	nativeIDs         map[string]bool
	transportIdentity func(*http.Request, string, string, string) (browsersecurity.Identity, error)
}

// partnersAuthError builds the transport Error carrying an HTTP status and
// public code.
func partnersAuthError(status int, code string) error {
	return &partnerhttp.Error{Status: status, Code: code}
}

// Resolve authenticates one request from the configured cookie or a single
// Bearer Authorization header (never both, never X-Api-Token). It enforces
// active email-verified membership, durable session admission, and for native
// requests verifies the token's signature, algorithm and exact allowed audience
// with a matching user ID. It returns a verified principal binding ActorID
// server-side or a typed denial; dependency failures surface as 503.
func (p *httpPrincipalResolver) Resolve(ctx context.Context, r *http.Request) (partnerhttp.Principal, error) {
	denied := partnersAuthError(401, "PARTNERS_AUTH_REQUIRED")
	if ctx == nil {
		return partnerhttp.Principal{}, denied
	}
	if err := ctx.Err(); err != nil {
		return partnerhttp.Principal{}, err
	}
	if p == nil || nilHelperPort(p.members) || nilHelperPort(p.sessions) || nilHelperPort(p.tokens) || p.transportIdentity == nil {
		return partnerhttp.Principal{}, partnersAuthError(503, "PARTNERS_DEPENDENCY_UNAVAILABLE")
	}
	if r == nil || len(r.Header.Values("X-Api-Token")) != 0 {
		return partnerhttp.Principal{}, denied
	}
	cookie, err := browsersecurity.UniqueCookie(r, p.authCookie)
	if err != nil && !errors.Is(err, http.ErrNoCookie) {
		return partnerhttp.Principal{}, denied
	}
	credential := ""
	if cookie != nil {
		credential = cookie.Value
	}
	native := len(r.Header.Values("Authorization")) > 0
	if native {
		if len(r.Header.Values("Cookie")) != 0 || len(r.Header.Values("Authorization")) != 1 || len(p.nativeIDs) == 0 {
			return partnerhttp.Principal{}, denied
		}
		header := r.Header.Get("Authorization")
		if !strings.HasPrefix(header, "Bearer ") {
			return partnerhttp.Principal{}, denied
		}
		credential = strings.TrimPrefix(header, "Bearer ")
	}
	if !validPartnersCredential(credential) {
		return partnerhttp.Principal{}, denied
	}
	result, err := p.members.AuthenticateSession(ctx, credential)
	if err := ctx.Err(); err != nil {
		return partnerhttp.Principal{}, err
	}
	if err != nil || result == nil || !result.Authenticated || result.User == nil || result.UserID == "" || result.User.ID != result.UserID || result.User.Status != userv2.AccountStatusKeyActive {
		return partnerhttp.Principal{}, denied
	}
	if result.User.Verification == nil || !result.User.Verification.EmailVerified {
		return partnerhttp.Principal{}, partnersAuthError(403, "PARTNERS_VERIFICATION_REQUIRED")
	}
	// Check the durable deletion admission before exposing even programme
	// disclosure. Owning commands recheck this and exact capabilities on replay.
	err = p.sessions.CheckPartnerSession(ctx, result.UserID, credential)
	if err := ctx.Err(); err != nil {
		return partnerhttp.Principal{}, err
	}
	if err != nil {
		if errors.Is(err, partnermanager.ErrDenied) {
			return partnerhttp.Principal{}, partnersAuthError(403, "PARTNERS_FORBIDDEN")
		}
		var failure *partnerhttp.Error
		if errors.As(err, &failure) {
			return partnerhttp.Principal{}, failure
		}
		return partnerhttp.Principal{}, partnersAuthError(503, "PARTNERS_DEPENDENCY_UNAVAILABLE")
	}
	client := ""
	if native {
		copyRequest := r.Clone(ctx)
		copyRequest.Header = r.Header.Clone()
		details, err := p.tokens.ExtractTokenMetadata(ctx, copyRequest)
		if err := ctx.Err(); err != nil {
			return partnerhttp.Principal{}, err
		}
		if err != nil || details == nil || details.UserID != result.UserID {
			return partnerhttp.Principal{}, denied
		}
		verified, err := p.tokens.VerifyToken(ctx, copyRequest)
		if err := ctx.Err(); err != nil {
			return partnerhttp.Principal{}, err
		}
		if err != nil || verified == nil || !verified.Valid || verified.Method == nil || verified.Claims == nil || verified.Method.Alg() != "HS256" {
			return partnerhttp.Principal{}, denied
		}
		audiences, err := verified.Claims.GetAudience()
		if err != nil || len(audiences) != 1 || !p.nativeIDs[audiences[0]] {
			return partnerhttp.Principal{}, denied
		}
		client = audiences[0]
	}
	if err := ctx.Err(); err != nil {
		return partnerhttp.Principal{}, err
	}
	identity, err := p.transportIdentity(r, result.UserID, credential, client)
	if err := ctx.Err(); err != nil {
		return partnerhttp.Principal{}, err
	}
	if err != nil {
		return partnerhttp.Principal{}, denied
	}
	return partnerhttp.Principal{ActorID: result.UserID, Credential: credential, Verified: true, Transport: identity}, nil
}

// validPartnersCredential accepts only non-empty credentials up to 8192
// printable non-comma ASCII characters, rejecting anything that could not be a
// single session token.
func validPartnersCredential(value string) bool {
	if len(value) == 0 || len(value) > 8192 {
		return false
	}
	for _, char := range value {
		if char < 33 || char > 126 || char == ',' {
			return false
		}
	}
	return true
}
