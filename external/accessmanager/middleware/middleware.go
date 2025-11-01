package middleware

import (
	"context"
	"errors"
	"net/http"

	"github.com/ooaklee/ghatd/external/accessmanager"
	accessmanagerhelpers "github.com/ooaklee/ghatd/external/accessmanager/helpers"
	"github.com/ooaklee/ghatd/external/common"
	"github.com/ooaklee/ghatd/external/toolbox"
	"github.com/ooaklee/reply"
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
	MiddlewareAdminJWTRequired(r *http.Request) (string, error)
	MiddlewareAdminAPITokenRequired(r *http.Request) (string, error)
	MiddlewareActiveJWTRequired(r *http.Request) (string, error)
	MiddlewareJWTRequired(r *http.Request) (string, error)
	MiddlewareValidAPITokenRequired(r *http.Request) (string, error)
	MiddlewareRateLimitOrActiveJWTRequired(r *http.Request) (string, error)
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
func (m *Middleware) validationFunc(validationType jwtValidationType) func(*http.Request) (string, error) {
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
		return nil, nil, errors.New(accessmanager.ErrKeyUnauthorizedUnableToAttainRequestorID)
	}

	return authCookie, refreshCookie, nil
}

// attemptTokenRefresh attempts to refresh tokens and retry validation
func (m *Middleware) attemptTokenRefresh(
	w http.ResponseWriter,
	req *http.Request,
	refreshCookie *http.Cookie,
	validateFunc func(*http.Request) (string, error),
) (string, error) {
	if refreshCookie.Value == "" {
		return "", errors.New("empty refresh token")
	}

	// Refresh the tokens
	tokenResp, err := m.service.RefreshToken(req.Context(), &accessmanager.RefreshTokenRequest{
		RefreshToken: refreshCookie.Value,
	})
	if err != nil {
		return "", err
	}

	// Set new tokens in cookies
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

	// Update request header with new access token
	req.Header["Authorization"] = []string{"Bearer " + tokenResp.AccessToken}

	// Retry validation with new token
	return validateFunc(req)
}

// handleJWTRequest is a unified handler for all JWT validation types
func (m *Middleware) handleJWTRequest(
	w http.ResponseWriter,
	req *http.Request,
	handler http.Handler,
	validationType jwtValidationType,
) {
	// Get cookies
	authCookie, refreshCookie, err := m.getCookies(req)
	if err != nil {
		toolbox.RemoveAuthCookies(w, m.environment, m.cookieDomain, m.cookiePrefixAuthToken, m.cookiePrefixRefreshToken)
		m.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	// Set authorization header if auth cookie exists
	if authCookie != nil {
		req.Header["Authorization"] = []string{"Bearer " + authCookie.Value}
	}

	// Get validation function
	validateFunc := m.validationFunc(validationType)

	// Attempt validation
	userID, err := validateFunc(req)
	if err != nil {
		// Try token refresh
		userID, refreshErr := m.attemptTokenRefresh(w, req, refreshCookie, validateFunc)
		if refreshErr != nil {
			toolbox.RemoveAuthCookies(w, m.environment, m.cookieDomain, m.cookiePrefixAuthToken, m.cookiePrefixRefreshToken)
			m.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
			return
		}
		// Refresh succeeded, use the new userID
		req = req.WithContext(accessmanagerhelpers.TransitWith(req.Context(), userID))
		handler.ServeHTTP(w, req)
		return
	}

	// Validation succeeded
	req = req.WithContext(accessmanagerhelpers.TransitWith(req.Context(), userID))
	handler.ServeHTTP(w, req)
}

// RateLimitOrActiveJWTRequired creates a middleware ensuring that the request is rate limited if
// number of request exceeds X from the same IP (and unauth request are given "unknown user ID")
//
//	or passed with a valid token, and the user is in an `ACTIVE` state (status)
func (m *Middleware) RateLimitOrActiveJWTRequired(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		authCookie, _ := req.Cookie(m.cookiePrefixAuthToken)
		refreshCookie, _ := req.Cookie(m.cookiePrefixRefreshToken)

		// If both cookies are absent, use rate limiting flow
		if authCookie == nil && refreshCookie == nil {
			userID, err := m.service.MiddlewareRateLimitOrActiveJWTRequired(req)
			if err != nil {
				m.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
				return
			}
			req = req.WithContext(accessmanagerhelpers.TransitWith(req.Context(), userID))
			handler.ServeHTTP(w, req)
			return
		}

		// Otherwise handle JWT authentication with refresh capability
		if refreshCookie == nil {
			toolbox.RemoveAuthCookies(w, m.environment, m.cookieDomain, m.cookiePrefixAuthToken, m.cookiePrefixRefreshToken)
			m.getBaseResponseHandler().NewHTTPErrorResponse(w, errors.New(accessmanager.ErrKeyUnauthorizedUnableToAttainRequestorID))
			return
		}

		if authCookie != nil {
			req.Header["Authorization"] = []string{"Bearer " + authCookie.Value}
		}

		userID, err := m.service.MiddlewareRateLimitOrActiveJWTRequired(req)
		if err != nil {
			// Try token refresh
			userID, refreshErr := m.attemptTokenRefresh(w, req, refreshCookie, m.service.MiddlewareRateLimitOrActiveJWTRequired)
			if refreshErr != nil {
				toolbox.RemoveAuthCookies(w, m.environment, m.cookieDomain, m.cookiePrefixAuthToken, m.cookiePrefixRefreshToken)
				m.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
				return
			}
			req = req.WithContext(accessmanagerhelpers.TransitWith(req.Context(), userID))
			handler.ServeHTTP(w, req)
			return
		}

		req = req.WithContext(accessmanagerhelpers.TransitWith(req.Context(), userID))
		handler.ServeHTTP(w, req)
	})
}

// handleAdminAPITokenRequiredRequest is checking to make sure the request
// coming in has a valid admin Api token associated to it
func (m *Middleware) handleAdminAPITokenRequiredRequest(w http.ResponseWriter, req *http.Request, handler http.Handler) {
	userID, err := m.service.MiddlewareAdminAPITokenRequired(req)
	if err != nil {
		m.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	req = req.WithContext(accessmanagerhelpers.TransitWith(req.Context(), userID))
	handler.ServeHTTP(w, req)
}

// handleValidAPITokenRequiredRequest is checking to make sure the request
// coming in has a valid token associated to it
func (m *Middleware) handleValidAPITokenRequiredRequest(w http.ResponseWriter, req *http.Request, handler http.Handler) {
	userID, err := m.service.MiddlewareValidAPITokenRequired(req)
	if err != nil {
		m.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	req = req.WithContext(accessmanagerhelpers.TransitWith(req.Context(), userID))
	handler.ServeHTTP(w, req)
}

// getBaseResponseHandler returns response handler configured with auth error map
func (m *Middleware) getBaseResponseHandler() *reply.Replier {
	return reply.NewReplier(m.errorMaps)
}
