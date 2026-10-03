package middleware

import (
	"context"
	"net/http"

	"github.com/ooaklee/ghatd/external/accessmanager"
	"github.com/ooaklee/ghatd/external/common"
	"github.com/ooaklee/ghatd/external/errormanifest"
	"github.com/ooaklee/ghatd/external/logger"
	"github.com/ooaklee/ghatd/external/toolbox"
	"github.com/ooaklee/reply/v2"
	"go.uber.org/zap"
)

// jwtValidationType defines the type of JWT validation to perform
type jwtValidationType int

const (
	// jwtValidationStandard is the JWT validation for a user in any state
	jwtValidationStandard jwtValidationType = iota

	// jwtValidationActive is the JWT validation for a user in the active state
	jwtValidationActive

	// jwtValidationAdmin is the JWT validation for a user with the admin role
	jwtValidationAdmin
)

// accessManagerService holds method of valid access manaer service
type accessManagerService interface {
	MiddlewareAdminJWTRequired(r *http.Request) (*accessmanager.MiddlewareAuthedUserResponse, error)
	MiddlewareAdminAPITokenRequired(r *http.Request) (*accessmanager.MiddlewareAuthedUserResponse, error)
	MiddlewareActiveJWTRequired(r *http.Request) (*accessmanager.MiddlewareAuthedUserResponse, error)
	MiddlewareJWTRequired(r *http.Request) (*accessmanager.MiddlewareAuthedUserResponse, error)
	MiddlewareValidAPITokenRequired(r *http.Request) (*accessmanager.MiddlewareAuthedUserResponse, error)
	MiddlewareRateLimitOrActiveJWTRequired(r *http.Request) (*accessmanager.MiddlewareAuthedUserResponse, error)
	RefreshToken(ctx context.Context, r *accessmanager.RefreshTokenRequest) (*accessmanager.RefreshTokenResponse, error)
}

// Middleware manages accessmanager middleware logic
type Middleware struct {
	service                  accessManagerService
	errorMaps                []reply.ErrorManifest
	cookiePrefixAuthToken    string
	cookiePrefixRefreshToken string
	environment              string
	cookieDomain             string
	// emptyMeSessionResponse is set only on a route-local /me copy. Other
	// middleware keep manifest responses, including the legacy 202 envelope.
	emptyMeSessionResponse bool
}

// NewMiddlewareRequest holds expected dependencies for an accessmanager middleware
type NewMiddlewareRequest struct {
	Service                  accessManagerService
	ErrorMaps                []reply.ErrorManifest
	Environment              string
	CookiePrefixAuthToken    string
	CookiePrefixRefreshToken string
	CookieDomain             string
}

// NewMiddleware creates new accessmanager middleware
func NewMiddleware(r *NewMiddlewareRequest) *Middleware {

	return &Middleware{
		service:                  r.Service,
		errorMaps:                r.ErrorMaps,
		cookiePrefixAuthToken:    r.CookiePrefixAuthToken,
		cookiePrefixRefreshToken: r.CookiePrefixRefreshToken,
		environment:              r.Environment,
		cookieDomain:             r.CookieDomain,
	}
}

// ActiveValidApiTokenOrAuthenticated creates a middleware ensure that the request is passed with a
// valid token or an authenticated user, API tokens will take precedence
func (m *Middleware) ActiveValidApiTokenOrAuthenticated(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		// check for API header
		if req.Header.Get(common.SystemWideXApiToken) != "" {
			m.handleValidAPITokenRequiredRequest(w, req, handler)
			return
		}

		// Otherwise, run JWT validation
		m.handleJWTRequest(w, req, handler, jwtValidationStandard)
	})
}

// ActiveValidApiTokenOrJWTRequired creates a middleware ensure that the request is passed with a
// valid token or an active JWT token, API tokens will take precedence
func (m *Middleware) ActiveValidApiTokenOrJWTRequired(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		// check for API header
		if req.Header.Get(common.SystemWideXApiToken) != "" {
			m.handleValidAPITokenRequiredRequest(w, req, handler)
			return
		}

		// Otherwise, run active JWT validation
		m.handleJWTRequest(w, req, handler, jwtValidationActive)
	})
}

// ValidAPITokenRequired creates a middleware ensure that the request is passed with a
// valid api user token, must exist and be in `ACTIVE` state
//
// `NOTE` - Status of user account should always trump token status
func (m *Middleware) ValidAPITokenRequired(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		m.handleValidAPITokenRequiredRequest(w, req, handler)
	})
}

// AdminJWTRequired creates a middleware to ensure that the request is passed with a
// valid token, non-expired token. User must be a platform Admin and `MUST` be
// in an `ACTIVE` user state.
func (m *Middleware) AdminJWTRequired(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		m.handleJWTRequest(w, req, handler, jwtValidationAdmin)
	})
}

// AdminApiTokenOrJWTRequired creates a middleware ensure that the request is passed with a
// valid token or an active JWT token, for an admin account API tokens will take precedence
func (m *Middleware) AdminApiTokenOrJWTRequired(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		// check for API header
		if req.Header.Get(common.SystemWideXApiToken) != "" {
			m.handleAdminAPITokenRequiredRequest(w, req, handler)
			return
		}

		// Otherwise, run admin JWT validation
		m.handleJWTRequest(w, req, handler, jwtValidationAdmin)
	})
}

// ActiveJWTRequired creates a middleware ensure that the request is passed with a
// valid token, and the user is in an `ACTIVE` state (status)
func (m *Middleware) ActiveJWTRequired(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		m.handleJWTRequest(w, req, handler, jwtValidationActive)
	})
}

// JWTRequired creates a middleware ensure that the request is passed with a
// valid token, non expired token
func (m *Middleware) JWTRequired(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		m.handleJWTRequest(w, req, handler, jwtValidationStandard)
	})
}

// validationFunc returns the appropriate service validation function based on validation type
func (m *Middleware) validationFunc(validationType jwtValidationType) func(*http.Request) (*accessmanager.MiddlewareAuthedUserResponse, error) {
	switch validationType {
	case jwtValidationAdmin:
		return m.service.MiddlewareAdminJWTRequired
	case jwtValidationActive:
		return m.service.MiddlewareActiveJWTRequired
	default:
		return m.service.MiddlewareJWTRequired
	}
}

// getCookies retrieves and validates auth and refresh token cookies from the request
func (m *Middleware) getCookies(req *http.Request) (authCookie, refreshCookie *http.Cookie, err error) {
	authCookie, _ = req.Cookie(m.cookiePrefixAuthToken)
	refreshCookie, refreshErr := req.Cookie(m.cookiePrefixRefreshToken)

	if refreshErr != nil && refreshErr != http.ErrNoCookie {
		return nil, nil, refreshErr
	}

	if refreshCookie == nil {
		return nil, nil, accessmanager.ErrUnauthorizedUnableToAttainRequestorID
	}

	return authCookie, refreshCookie, nil
}

// refreshedSession holds a validated request and replacement cookies until the
// final publication boundary. Preparing it never mutates the caller's request
// or writes a response; cancellation can still prevent cookie publication.
type refreshedSession struct {
	request *http.Request
	tokens  accessmanager.RefreshTokenResponse
}

// attemptTokenRefresh rotates once, verifies the replacement and prepares its
// identity context. The caller must publish only through serveRefreshed.
func (m *Middleware) attemptTokenRefresh(
	req *http.Request,
	refreshCookie *http.Cookie,
	validateFunc func(*http.Request) (*accessmanager.MiddlewareAuthedUserResponse, error),
) (*refreshedSession, error) {
	if err := req.Context().Err(); err != nil {
		return nil, err
	}
	if refreshCookie == nil || refreshCookie.Value == "" {
		return nil, accessmanager.ErrEmptyRefreshToken
	}

	// Refresh the tokens
	tokenResp, err := m.service.RefreshToken(req.Context(), &accessmanager.RefreshTokenRequest{
		RefreshToken: refreshCookie.Value,
	})
	if contextErr := req.Context().Err(); contextErr != nil {
		return nil, contextErr
	}
	if err != nil {
		return nil, err
	}
	if tokenResp == nil || tokenResp.AccessToken == "" || tokenResp.RefreshToken == "" {
		return nil, accessmanager.ErrSessionVerificationUnavailable
	}

	// Validate a detached header set so rejected refreshes cannot replace the
	// caller's presented credential. Publish it only after validation succeeds.
	retry := req.Clone(req.Context())
	retry.Header.Set("Authorization", "Bearer "+tokenResp.AccessToken)

	// Retry validation with new token
	authedUserResp, err := validateFunc(retry)
	if contextErr := req.Context().Err(); contextErr != nil {
		return nil, contextErr
	}
	if err != nil {
		return nil, err
	}
	if authedUserResp == nil || !authedUserResp.Authenticated {
		return nil, accessmanager.ErrSessionVerificationUnavailable
	}
	ctx, err := ContextWithAuthentication(retry.Context(), authedUserResp)
	if err != nil {
		if contextErr := retry.Context().Err(); contextErr != nil {
			return nil, contextErr
		}
		return nil, accessmanager.ErrSessionVerificationUnavailable
	}
	// Downstream credential-management handlers must see the same pair that
	// authenticated this request, not the consumed predecessor cookies. This is
	// still a detached request; rejection never mutates the incoming headers.
	cookies := retry.Cookies()
	retry.Header.Del("Cookie")
	for _, cookie := range cookies {
		if cookie.Name != m.cookiePrefixAuthToken && cookie.Name != m.cookiePrefixRefreshToken {
			retry.AddCookie(cookie)
		}
	}
	retry.AddCookie(&http.Cookie{Name: m.cookiePrefixAuthToken, Value: tokenResp.AccessToken})
	retry.AddCookie(&http.Cookie{Name: m.cookiePrefixRefreshToken, Value: tokenResp.RefreshToken})
	return &refreshedSession{request: retry.WithContext(ctx), tokens: *tokenResp}, nil
}

// serveRefreshed commits replacement cookies only after identity publication and
// the final cancellation check. Cancellation after this boundary cannot undo
// headers already written, nor does it roll back server-side token rotation.
func (m *Middleware) serveRefreshed(w http.ResponseWriter, handler http.Handler, session *refreshedSession) {
	if err := session.request.Context().Err(); err != nil {
		m.fail(w, err)
		return
	}
	tokenResp := session.tokens
	toolbox.AddAuthCookies(
		w,
		m.environment,
		m.cookieDomain,
		m.cookiePrefixAuthToken,
		tokenResp.AccessToken,
		tokenResp.AccessTokenExpiresAt,
		m.cookiePrefixRefreshToken,
		tokenResp.RefreshToken,
		tokenResp.RefreshTokenExpiresAt,
	)

	handler.ServeHTTP(w, session.request)
}

// handleJWTRequest is a unified handler for all JWT validation types
func (m *Middleware) handleJWTRequest(
	w http.ResponseWriter,
	req *http.Request,
	handler http.Handler,
	validationType jwtValidationType,
) {
	req = req.Clone(req.Context())
	if err := req.Context().Err(); err != nil {
		m.fail(w, err)
		return
	}
	// Get cookies
	authCookie, refreshCookie, err := m.getCookies(req)
	if err != nil {
		toolbox.RemoveAuthCookies(w, m.environment, m.cookieDomain, m.cookiePrefixAuthToken, m.cookiePrefixRefreshToken)
		m.fail(w, err)
		return
	}

	// Set authorization header if auth cookie exists
	if authCookie != nil {
		req.Header["Authorization"] = []string{"Bearer " + authCookie.Value}
	}

	// Get validation function
	validateFunc := m.validationFunc(validationType)

	// Attempt validation
	authedUserResp, err := validateFunc(req)
	if err != nil {
		if contextErr := req.Context().Err(); contextErr != nil {
			m.fail(w, contextErr)
			return
		}
		kind := accessmanager.ClassifySessionError(err)
		if kind != accessmanager.SessionErrorRefreshable {
			if kind == accessmanager.SessionErrorInvalidCredential {
				toolbox.RemoveAuthCookies(w, m.environment, m.cookieDomain, m.cookiePrefixAuthToken, m.cookiePrefixRefreshToken)
			}
			m.fail(w, err)
			return
		}
		// Only an absent/expired access credential can trigger refresh. Account
		// denials and dependency failures cannot be repaired by rotating tokens.
		refreshed, refreshErr := m.attemptTokenRefresh(req, refreshCookie, validateFunc)
		if refreshErr != nil {
			kind := accessmanager.ClassifySessionError(refreshErr)
			if kind == accessmanager.SessionErrorInvalidCredential || kind == accessmanager.SessionErrorRefreshable {
				toolbox.RemoveAuthCookies(w, m.environment, m.cookieDomain, m.cookiePrefixAuthToken, m.cookiePrefixRefreshToken)
			}
			m.fail(w, refreshErr)
			return
		}

		// Refresh succeeded, use the new authedUser
		m.serveRefreshed(w, handler, refreshed)
		return
	}

	// Validation succeeded
	if authedUserResp == nil || !authedUserResp.Authenticated {
		m.fail(w, accessmanager.ErrSessionVerificationUnavailable)
		return
	}
	m.serveAuthenticated(w, req, handler, authedUserResp)
}

// RateLimitOrActiveJWTRequired creates a middleware ensuring that the request is rate limited if
// number of request exceeds X from the same IP (and unauth request are given "unknown user ID")
//
//	or passed with a valid token, and the user is in an `ACTIVE` state (status)
func (m *Middleware) RateLimitOrActiveJWTRequired(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		req = req.Clone(req.Context())
		if err := req.Context().Err(); err != nil {
			m.fail(w, err)
			return
		}
		authCookie, _ := req.Cookie(m.cookiePrefixAuthToken)
		refreshCookie, _ := req.Cookie(m.cookiePrefixRefreshToken)
		hasAuthCookie := authCookie != nil && authCookie.Value != ""
		hasRefreshCookie := refreshCookie != nil && refreshCookie.Value != ""

		// If both cookies are absent or empty, use the public rate-limited flow.
		if !hasAuthCookie && !hasRefreshCookie {
			m.handleRateLimitOrActiveUnauthenticated(w, req, handler, authCookie != nil || refreshCookie != nil, "empty-or-missing-auth-cookies", nil, authCookie, refreshCookie)
			return
		}

		// Otherwise handle JWT authentication with refresh capability
		if !hasAuthCookie {
			m.handleRateLimitOrActiveUnauthenticated(w, req, handler, true, "missing-auth-cookie", accessmanager.ErrUnauthorizedUnableToAttainRequestorID, authCookie, refreshCookie)
			return
		}

		if !hasRefreshCookie {
			m.handleRateLimitOrActiveUnauthenticated(w, req, handler, true, "missing-refresh-cookie", accessmanager.ErrUnauthorizedUnableToAttainRequestorID, authCookie, refreshCookie)
			return
		}

		req.Header["Authorization"] = []string{"Bearer " + authCookie.Value}

		authedUserResp, err := m.service.MiddlewareRateLimitOrActiveJWTRequired(req)
		if err != nil {
			if contextErr := req.Context().Err(); contextErr != nil {
				m.fail(w, contextErr)
				return
			}
			kind := accessmanager.ClassifySessionError(err)
			if kind == accessmanager.SessionErrorUnknown || kind == accessmanager.SessionErrorDenied {
				m.fail(w, err)
				return
			}
			if kind == accessmanager.SessionErrorInvalidCredential {
				m.handleRateLimitOrActiveUnauthenticated(w, req, handler, true, "invalid-auth-credential", err, authCookie, refreshCookie)
				return
			}
			// Refresh never runs for infrastructure errors or account denials.
			refreshed, refreshErr := m.attemptTokenRefresh(req, refreshCookie, m.service.MiddlewareRateLimitOrActiveJWTRequired)
			if refreshErr != nil {
				kind := accessmanager.ClassifySessionError(refreshErr)
				if kind == accessmanager.SessionErrorUnknown || kind == accessmanager.SessionErrorDenied {
					m.fail(w, refreshErr)
					return
				}
				m.handleRateLimitOrActiveUnauthenticated(w, req, handler, true, "auth-validation-or-refresh-failed", refreshErr, authCookie, refreshCookie)
				return
			}

			m.serveRefreshed(w, handler, refreshed)
			return
		}

		if authedUserResp == nil || !authedUserResp.Authenticated {
			m.fail(w, accessmanager.ErrSessionVerificationUnavailable)
			return
		}
		m.serveAuthenticated(w, req, handler, authedUserResp)
	})
}

// handleRateLimitOrActiveUnauthenticated downgrades a failed auth request to
// rate-limited access, optionally clearing stale cookies.
func (m *Middleware) handleRateLimitOrActiveUnauthenticated(
	w http.ResponseWriter,
	req *http.Request,
	handler http.Handler,
	clearCookies bool,
	reason string,
	authErr error,
	authCookie *http.Cookie,
	refreshCookie *http.Cookie,
) {
	if err := req.Context().Err(); err != nil {
		m.fail(w, err)
		return
	}
	if clearCookies {
		toolbox.RemoveAuthCookies(w, m.environment, m.cookieDomain, m.cookiePrefixAuthToken, m.cookiePrefixRefreshToken)
		logger.Info(req.Context(), "rate-limit-or-active-auth-downgraded",
			zap.String("reason", reason),
			zap.Uint8("cause-kind", uint8(accessmanager.ClassifySessionError(authErr))),
			zap.Bool("has-auth-cookie", authCookie != nil),
			zap.Bool("auth-cookie-empty", authCookie != nil && authCookie.Value == ""),
			zap.Bool("has-refresh-cookie", refreshCookie != nil),
			zap.Bool("refresh-cookie-empty", refreshCookie != nil && refreshCookie.Value == ""),
		)
	}

	// The anonymous adapter must not inherit any selected credential or actor.
	// Preserve unrelated cookies/metadata, but reject authenticated results even
	// if a custom adapter selects a credential outside the supported transports.
	req = req.Clone(clearAuthentication(req.Context()))
	req.Header.Del("Authorization")
	req.Header.Del(common.SystemWideXApiToken)
	cookies := req.Cookies()
	req.Header.Del("Cookie")
	for _, cookie := range cookies {
		if cookie.Name != m.cookiePrefixAuthToken && cookie.Name != m.cookiePrefixRefreshToken {
			req.AddCookie(cookie)
		}
	}
	authedUserResp, err := m.service.MiddlewareRateLimitOrActiveJWTRequired(req)
	if err != nil {
		m.fail(w, err)
		return
	}
	if authedUserResp == nil || authedUserResp.Authenticated || authedUserResp.Token != nil || authedUserResp.APIToken != nil {
		m.fail(w, accessmanager.ErrSessionVerificationUnavailable)
		return
	}

	m.serveAuthenticated(w, req, handler, authedUserResp)
}

// handleAdminAPITokenRequiredRequest is checking to make sure the request
// coming in has a valid admin Api token associated to it
func (m *Middleware) handleAdminAPITokenRequiredRequest(w http.ResponseWriter, req *http.Request, handler http.Handler) {
	authedUserResp, err := m.service.MiddlewareAdminAPITokenRequired(req)
	if err != nil {
		m.fail(w, err)
		return
	}

	m.serveAuthenticated(w, req, handler, authedUserResp)
}

// handleValidAPITokenRequiredRequest is checking to make sure the request
// coming in has a valid token associated to it
func (m *Middleware) handleValidAPITokenRequiredRequest(w http.ResponseWriter, req *http.Request, handler http.Handler) {
	authedUserResp, err := m.service.MiddlewareValidAPITokenRequired(req)
	if err != nil {
		m.fail(w, err)
		return
	}

	m.serveAuthenticated(w, req, handler, authedUserResp)
}

// serveAuthenticated publishes a validated result through the shared context
// adapter. Inconsistent identities fail before a protected handler can run.
func (m *Middleware) serveAuthenticated(w http.ResponseWriter, req *http.Request, handler http.Handler, result *accessmanager.MiddlewareAuthedUserResponse) {
	if err := req.Context().Err(); err != nil {
		m.fail(w, err)
		return
	}
	ctx, err := ContextWithAuthentication(req.Context(), result)
	if err != nil {
		// A successful verifier returned an inconsistent identity, not evidence
		// of an absent visitor session. Never convert this to a probe's 202.
		m.fail(w, accessmanager.ErrSessionVerificationUnavailable)
		return
	}
	handler.ServeHTTP(w, req.WithContext(ctx))
}

// getBaseResponseHandler returns response handler configured with auth error map
func (m *Middleware) getBaseResponseHandler() *reply.Replier {
	return reply.NewReplier(m.errorMaps)
}

// fail resolves ordinary wrapped manifest errors before delegating to reply.
// Unknown or ambiguous failures use opaque responses, never raw diagnostics.
func (m *Middleware) fail(w http.ResponseWriter, err error) {
	public := errormanifest.CanonicalError(err, m.errorMaps)
	if m.emptyMeSessionResponse && public == accessmanager.ErrUnauthorizedUnableToAttainRequestorID {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	_ = m.getBaseResponseHandler().NewHTTPErrorResponse(w, public)
}
