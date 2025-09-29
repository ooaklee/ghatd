package middleware

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/newrelic/go-agent/v3/newrelic"
	"github.com/ooaklee/ghatd/external/accessmanager"
	accessmanagerhelpers "github.com/ooaklee/ghatd/external/accessmanager/helpers"
	"github.com/ooaklee/ghatd/external/common"
	"github.com/ooaklee/ghatd/external/toolbox"
	"github.com/ooaklee/reply"
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
	newRelicApplication      *newrelic.Application
	service                  accessManagerService
	errorMaps                []reply.ErrorManifest
	cookiePrefixAuthToken    string
	cookiePrefixRefreshToken string
	environment              string
	cookieDomain             string
}

// NewMiddlewareRequest holds expected dependencies for an accessmanager middleware
type NewMiddlewareRequest struct {
	NewRelicConf             *newrelic.Application
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
		newRelicApplication:      r.NewRelicConf,
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

		// Add newrelic transaction
		if m.newRelicApplication != nil {
			newRelicTransaction := m.newRelicApplication.StartTransaction(fmt.Sprintf("%s %s", req.Method, req.URL.Path))
			// req is a *http.Request, this marks the transaction as a web transaction
			newRelicTransaction.SetWebRequestHTTP(req)

			// Add to context
			req = req.WithContext(accessmanagerhelpers.TransitTransactionWith(req.Context(), newRelicTransaction))
		}

		// check for API header
		userFullToken := req.Header.Get(common.SystemWideXApiToken)

		// if present, run API middleware logic
		if userFullToken != "" {
			m.handleValidAPITokenRequiredRequest(w, req, handler)

			m.endNewrelicTransaction(req)

			return
		}

		// Otherwise, Run authenticated token check
		m.handleJWTRequiredRequest(w, req, handler)

		m.endNewrelicTransaction(req)

	})
}

// ActiveValidApiTokenOrJWTRequired creates a middleware ensure that the request is passed with a
// valid token or an active JWT token, API tokens will take precedence
func (m *Middleware) ActiveValidApiTokenOrJWTRequired(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {

		// Add newrelic transaction
		if m.newRelicApplication != nil {
			newRelicTransaction := m.newRelicApplication.StartTransaction(fmt.Sprintf("%s %s", req.Method, req.URL.Path))
			// req is a *http.Request, this marks the transaction as a web transaction
			newRelicTransaction.SetWebRequestHTTP(req)

			// Add to context
			req = req.WithContext(accessmanagerhelpers.TransitTransactionWith(req.Context(), newRelicTransaction))
		}

		// check for API header
		userFullToken := req.Header.Get(common.SystemWideXApiToken)

		// if present, run API middleware logic
		if userFullToken != "" {
			m.handleValidAPITokenRequiredRequest(w, req, handler)

			m.endNewrelicTransaction(req)
			return
		}

		// Otherwise, Run active token check
		m.handleActiveJWTRequiredRequest(w, req, handler)

		m.endNewrelicTransaction(req)
	})
}

// ValidAPITokenRequired creates a middleware ensure that the request is passed with a
// valid api user token, must exist and be in `ACTIVE` state
//
// `NOTE` - Status of user account should always trump token status
func (m *Middleware) ValidAPITokenRequired(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {

		// Add newrelic transaction
		if m.newRelicApplication != nil {
			newRelicTransaction := m.newRelicApplication.StartTransaction(fmt.Sprintf("%s %s", req.Method, req.URL.Path))
			// req is a *http.Request, this marks the transaction as a web transaction
			newRelicTransaction.SetWebRequestHTTP(req)

			// Add to context
			req = req.WithContext(accessmanagerhelpers.TransitTransactionWith(req.Context(), newRelicTransaction))
		}

		m.handleValidAPITokenRequiredRequest(w, req, handler)

		m.endNewrelicTransaction(req)
	})
}

// AdminJWTRequired creates a middleware to ensure that the request is passed with a
// valid token, non-expired token. User must be a platform Admin and `MUST` be
// in an `ACTIVE` user state.
func (m *Middleware) AdminJWTRequired(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {

		// Add newrelic transaction
		if m.newRelicApplication != nil {
			newRelicTransaction := m.newRelicApplication.StartTransaction(fmt.Sprintf("%s %s", req.Method, req.URL.Path))
			// req is a *http.Request, this marks the transaction as a web transaction
			newRelicTransaction.SetWebRequestHTTP(req)

			// Add to context
			req = req.WithContext(accessmanagerhelpers.TransitTransactionWith(req.Context(), newRelicTransaction))
		}

		// Otherwise, Run active token check
		m.handleAdminJWTRequiredRequest(w, req, handler)

		m.endNewrelicTransaction(req)

	})
}

// AdminApiTokenOrJWTRequired creates a middleware ensure that the request is passed with a
// valid token or an active JWT token, for an admin account API tokens will take precedence
func (m *Middleware) AdminApiTokenOrJWTRequired(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {

		// Add newrelic transaction
		if m.newRelicApplication != nil {
			newRelicTransaction := m.newRelicApplication.StartTransaction(fmt.Sprintf("%s %s", req.Method, req.URL.Path))
			// req is a *http.Request, this marks the transaction as a web transaction
			newRelicTransaction.SetWebRequestHTTP(req)

			// Add to context
			req = req.WithContext(accessmanagerhelpers.TransitTransactionWith(req.Context(), newRelicTransaction))
		}

		// check for API header
		userFullToken := req.Header.Get(common.SystemWideXApiToken)

		// if present, run API middleware logic
		if userFullToken != "" {
			m.handleAdminAPITokenRequiredRequest(w, req, handler)

			m.endNewrelicTransaction(req)

			return
		}

		// Otherwise, Run active token check
		m.handleAdminJWTRequiredRequest(w, req, handler)

		m.endNewrelicTransaction(req)
	})
}

// ActiveJWTRequired creates a middleware ensure that the request is passed with a
// valid token, and the user is in an `ACTIVE` state (status)
func (m *Middleware) ActiveJWTRequired(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {

		// Add newrelic transaction
		if m.newRelicApplication != nil {
			newRelicTransaction := m.newRelicApplication.StartTransaction(fmt.Sprintf("%s %s", req.Method, req.URL.Path))
			// req is a *http.Request, this marks the transaction as a web transaction
			newRelicTransaction.SetWebRequestHTTP(req)

			// Add to context
			req = req.WithContext(accessmanagerhelpers.TransitTransactionWith(req.Context(), newRelicTransaction))
		}

		m.handleActiveJWTRequiredRequest(w, req, handler)

		m.endNewrelicTransaction(req)

	})
}

// JWTRequired creates a middleware ensure that the request is passed with a
// valid token, non expired token
func (m *Middleware) JWTRequired(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {

		// Add newrelic transaction
		if m.newRelicApplication != nil {
			newRelicTransaction := m.newRelicApplication.StartTransaction(fmt.Sprintf("%s %s", req.Method, req.URL.Path))
			// req is a *http.Request, this marks the transaction as a web transaction
			newRelicTransaction.SetWebRequestHTTP(req)

			// Add to context
			req = req.WithContext(accessmanagerhelpers.TransitTransactionWith(req.Context(), newRelicTransaction))
		}

		m.handleJWTRequiredRequest(w, req, handler)

		m.endNewrelicTransaction(req)

	})
}

// handleJWTRequiredRequest is checking to make sure the request
// coming in has a valid JWT
func (m *Middleware) handleJWTRequiredRequest(w http.ResponseWriter, req *http.Request, handler http.Handler) {

	var (
		userId string
		err    error
	)

	// check to see if request is coming with cookies
	cookie, aTokenErr := req.Cookie(m.cookiePrefixAuthToken)
	refreshTokenCookie, _ := req.Cookie(m.cookiePrefixRefreshToken)
	if aTokenErr != nil && aTokenErr != http.ErrNoCookie && refreshTokenCookie == nil {
		m.endNewrelicTransaction(req)

		//nolint will set up default fallback later
		m.getBaseResponseHandler().NewHTTPErrorResponse(w, aTokenErr)
		return
	}

	if refreshTokenCookie == nil {
		toolbox.RemoveAuthCookies(w, m.environment, m.cookieDomain, m.cookiePrefixAuthToken, m.cookiePrefixRefreshToken)
		m.endNewrelicTransaction(req)

		//nolint will set up default fallback later
		m.getBaseResponseHandler().NewHTTPErrorResponse(w, errors.New(accessmanager.ErrKeyUnauthorizedUnableToAttainRequestorID))
		return
	}

	if cookie != nil {
		req.Header["Authorization"] = []string{"Bearer " + cookie.Value}
	}

	userId, err = m.service.MiddlewareJWTRequired(req)
	if err != nil {
		// handle the case where the access token is expired
		if refreshTokenCookie.Value != "" {

			m.refreshTokenAndUpdateRequest(w, req, refreshTokenCookie.Value)

			// retry the request with the new access token
			userId, err = m.service.MiddlewareJWTRequired(req)
			if err != nil {
				toolbox.RemoveAuthCookies(w, m.environment, m.cookieDomain, m.cookiePrefixAuthToken, m.cookiePrefixRefreshToken)
				m.endNewrelicTransaction(req)

				//nolint will set up default fallback later
				m.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
				return
			}
		} else {
			toolbox.RemoveAuthCookies(w, m.environment, m.cookieDomain, m.cookiePrefixAuthToken, m.cookiePrefixRefreshToken)
			m.endNewrelicTransaction(req)

			//nolint will set up default fallback later
			m.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
			return
		}
	}

	request := req.WithContext(accessmanagerhelpers.TransitWith(req.Context(), userId))

	responseWriter := middlewareResponseWriter(w, accessmanagerhelpers.AcquireTransactionFrom(req.Context()))
	handler.ServeHTTP(responseWriter, request)
}

// RateLimitOrActiveJWTRequired creates a middleware ensuring that the request is rate limited if
// number of request exceeds X from the same IP (and unauth request are given "unknown user ID")
//
//	or passed with a valid token, and the user is in an `ACTIVE` state (status)
func (m *Middleware) RateLimitOrActiveJWTRequired(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {

		var (
			userId string
			err    error
		)

		// Add newrelic transaction
		if m.newRelicApplication != nil {
			newRelicTransaction := m.newRelicApplication.StartTransaction(fmt.Sprintf("%s %s", req.Method, req.URL.Path))
			// req is a *http.Request, this marks the transaction as a web transaction
			newRelicTransaction.SetWebRequestHTTP(req)

			// Add to context
			req = req.WithContext(accessmanagerhelpers.TransitTransactionWith(req.Context(), newRelicTransaction))
		}

		// check to see if request is coming with cookies
		cookie, aTokenErr := req.Cookie(m.cookiePrefixAuthToken)
		refreshTokenCookie, rAuthErr := req.Cookie(m.cookiePrefixRefreshToken)
		if (aTokenErr != nil && aTokenErr != http.ErrNoCookie) && (rAuthErr != nil && rAuthErr != http.ErrNoCookie) {
			m.endNewrelicTransaction(req)

			//nolint will set up default fallback later
			m.getBaseResponseHandler().NewHTTPErrorResponse(w, aTokenErr)
			return
		}

		// if both cookies are empty, then we need to
		// carry on with rate limiting flow
		if cookie == nil && refreshTokenCookie == nil {
			userId, err = m.service.MiddlewareRateLimitOrActiveJWTRequired(req)
			if err != nil {
				m.endNewrelicTransaction(req)

				//nolint will set up default fallback later
				m.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
				return
			}
		}

		// if there is a cookie, the we need refresh logic
		if cookie != nil || refreshTokenCookie != nil {

			if refreshTokenCookie == nil {
				toolbox.RemoveAuthCookies(w, m.environment, m.cookieDomain, m.cookiePrefixAuthToken, m.cookiePrefixRefreshToken)
				m.endNewrelicTransaction(req)

				//nolint will set up default fallback later
				m.getBaseResponseHandler().NewHTTPErrorResponse(w, errors.New(accessmanager.ErrKeyUnauthorizedUnableToAttainRequestorID))
				return
			}

			if cookie != nil {
				req.Header["Authorization"] = []string{"Bearer " + cookie.Value}
			}

			userId, err = m.service.MiddlewareRateLimitOrActiveJWTRequired(req)
			if err != nil {
				// handle the case where the access token is expired
				if refreshTokenCookie.Value != "" {

					m.refreshTokenAndUpdateRequest(w, req, refreshTokenCookie.Value)

					// retry the request with the new access token
					userId, err = m.service.MiddlewareRateLimitOrActiveJWTRequired(req)
					if err != nil {
						toolbox.RemoveAuthCookies(w, m.environment, m.cookieDomain, m.cookiePrefixAuthToken, m.cookiePrefixRefreshToken)
						m.endNewrelicTransaction(req)

						//nolint will set up default fallback later
						m.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
						return
					}
				} else {
					toolbox.RemoveAuthCookies(w, m.environment, m.cookieDomain, m.cookiePrefixAuthToken, m.cookiePrefixRefreshToken)
					m.endNewrelicTransaction(req)

					//nolint will set up default fallback later
					m.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
					return
				}

			}
		}

		request := req.WithContext(accessmanagerhelpers.TransitWith(req.Context(), userId))

		responseWriter := middlewareResponseWriter(w, accessmanagerhelpers.AcquireTransactionFrom(req.Context()))
		handler.ServeHTTP(responseWriter, request)

		m.endNewrelicTransaction(req)
	})
}

// handleAdminJWTRequiredRequest is checking to make sure the request
// coming in has a valid admin JWT which is in active state associated to it
func (m *Middleware) handleAdminJWTRequiredRequest(w http.ResponseWriter, req *http.Request, handler http.Handler) {

	var (
		userId string
		err    error
	)

	// check to see if request is coming with cookies
	cookie, aTokenErr := req.Cookie(m.cookiePrefixAuthToken)
	refreshTokenCookie, _ := req.Cookie(m.cookiePrefixRefreshToken)
	if aTokenErr != nil && aTokenErr != http.ErrNoCookie && refreshTokenCookie == nil {
		m.endNewrelicTransaction(req)

		//nolint will set up default fallback later
		m.getBaseResponseHandler().NewHTTPErrorResponse(w, aTokenErr)
		return
	}

	if refreshTokenCookie == nil {
		toolbox.RemoveAuthCookies(w, m.environment, m.cookieDomain, m.cookiePrefixAuthToken, m.cookiePrefixRefreshToken)
		m.endNewrelicTransaction(req)

		//nolint will set up default fallback later
		m.getBaseResponseHandler().NewHTTPErrorResponse(w, errors.New(accessmanager.ErrKeyUnauthorizedUnableToAttainRequestorID))
		return
	}

	if cookie != nil {
		req.Header["Authorization"] = []string{"Bearer " + cookie.Value}
	}

	userId, err = m.service.MiddlewareAdminJWTRequired(req)
	if err != nil {
		// handle the case where the access token is expired
		if refreshTokenCookie.Value != "" {

			m.refreshTokenAndUpdateRequest(w, req, refreshTokenCookie.Value)

			// retry the request with the new access token
			userId, err = m.service.MiddlewareAdminJWTRequired(req)
			if err != nil {
				toolbox.RemoveAuthCookies(w, m.environment, m.cookieDomain, m.cookiePrefixAuthToken, m.cookiePrefixRefreshToken)
				m.endNewrelicTransaction(req)

				//nolint will set up default fallback later
				m.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
				return
			}
		} else {
			toolbox.RemoveAuthCookies(w, m.environment, m.cookieDomain, m.cookiePrefixAuthToken, m.cookiePrefixRefreshToken)
			m.endNewrelicTransaction(req)

			//nolint will set up default fallback later
			m.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
			return
		}
	}

	request := req.WithContext(accessmanagerhelpers.TransitWith(req.Context(), userId))

	responseWriter := middlewareResponseWriter(w, accessmanagerhelpers.AcquireTransactionFrom(req.Context()))
	handler.ServeHTTP(responseWriter, request)

}

// handleActiveJWTRequiredRequest is checking to make sure the request
// coming in has a valid JWT which is in active state associated to it
func (m *Middleware) handleActiveJWTRequiredRequest(w http.ResponseWriter, req *http.Request, handler http.Handler) {

	var (
		userId string
		err    error
	)

	// check to see if request is coming with cookies
	cookie, aTokenErr := req.Cookie(m.cookiePrefixAuthToken)
	refreshTokenCookie, _ := req.Cookie(m.cookiePrefixRefreshToken)
	if aTokenErr != nil && aTokenErr != http.ErrNoCookie && refreshTokenCookie == nil {
		m.endNewrelicTransaction(req)

		//nolint will set up default fallback later
		m.getBaseResponseHandler().NewHTTPErrorResponse(w, aTokenErr)
		return
	}

	if refreshTokenCookie == nil {
		toolbox.RemoveAuthCookies(w, m.environment, m.cookieDomain, m.cookiePrefixAuthToken, m.cookiePrefixRefreshToken)
		m.endNewrelicTransaction(req)

		//nolint will set up default fallback later
		m.getBaseResponseHandler().NewHTTPErrorResponse(w, errors.New(accessmanager.ErrKeyUnauthorizedUnableToAttainRequestorID))
		return
	}

	if cookie != nil {
		req.Header["Authorization"] = []string{"Bearer " + cookie.Value}
	}

	userId, err = m.service.MiddlewareActiveJWTRequired(req)
	if err != nil {
		// handle the case where the access token is expired
		if refreshTokenCookie.Value != "" {

			m.refreshTokenAndUpdateRequest(w, req, refreshTokenCookie.Value)

			// retry the request with the new access token
			userId, err = m.service.MiddlewareActiveJWTRequired(req)
			if err != nil {
				toolbox.RemoveAuthCookies(w, m.environment, m.cookieDomain, m.cookiePrefixAuthToken, m.cookiePrefixRefreshToken)
				m.endNewrelicTransaction(req)

				//nolint will set up default fallback later
				m.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
				return
			}
		} else {
			toolbox.RemoveAuthCookies(w, m.environment, m.cookieDomain, m.cookiePrefixAuthToken, m.cookiePrefixRefreshToken)
			m.endNewrelicTransaction(req)

			//nolint will set up default fallback later
			m.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
			return
		}
	}

	request := req.WithContext(accessmanagerhelpers.TransitWith(req.Context(), userId))

	responseWriter := middlewareResponseWriter(w, accessmanagerhelpers.AcquireTransactionFrom(req.Context()))
	handler.ServeHTTP(responseWriter, request)
}

// handleAdminAPITokenRequiredRequest is checking to make sure the request
// coming in has a valid admin Api token associated to it
func (m *Middleware) handleAdminAPITokenRequiredRequest(w http.ResponseWriter, req *http.Request, handler http.Handler) {
	userID, err := m.service.MiddlewareAdminAPITokenRequired(req)
	if err != nil {
		m.endNewrelicTransaction(req)

		//nolint will set up default fallback later
		m.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	request := req.WithContext(accessmanagerhelpers.TransitWith(req.Context(), userID))

	responseWriter := middlewareResponseWriter(w, accessmanagerhelpers.AcquireTransactionFrom(req.Context()))
	handler.ServeHTTP(responseWriter, request)
}

// handleValidAPITokenRequiredRequest is checking to make sure the request
// coming in has a valid token associated to it
func (m *Middleware) handleValidAPITokenRequiredRequest(w http.ResponseWriter, req *http.Request, handler http.Handler) {
	userID, err := m.service.MiddlewareValidAPITokenRequired(req)
	if err != nil {
		m.endNewrelicTransaction(req)

		//nolint will set up default fallback later
		m.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	request := req.WithContext(accessmanagerhelpers.TransitWith(req.Context(), userID))

	responseWriter := middlewareResponseWriter(w, accessmanagerhelpers.AcquireTransactionFrom(req.Context()))
	handler.ServeHTTP(responseWriter, request)
}

// endNewrelicTransaction is a helper function to end the newrelic transaction
// if the newrelic application is not nil
func (m *Middleware) endNewrelicTransaction(req *http.Request) {
	if m.newRelicApplication != nil {
		newRelicTransaction := accessmanagerhelpers.AcquireTransactionFrom(req.Context())
		newRelicTransaction.End()
	}
}

// refreshTokenAndUpdateRequest is a helper function to refresh the token and update the request
// with the new tokens and headers
func (m *Middleware) refreshTokenAndUpdateRequest(w http.ResponseWriter, req *http.Request, refreshToken string) {

	// refresh the tokens
	tokenResp, refreshErr := m.service.RefreshToken(req.Context(), &accessmanager.RefreshTokenRequest{
		RefreshToken: refreshToken,
	})
	if refreshErr != nil {
		toolbox.RemoveAuthCookies(w, m.environment, m.cookieDomain, m.cookiePrefixAuthToken, m.cookiePrefixRefreshToken)
		m.endNewrelicTransaction(req)

		//nolint will set up default fallback later
		m.getBaseResponseHandler().NewHTTPErrorResponse(w, refreshErr)
		return
	}

	// set the new tokens in the cookies
	toolbox.AddAuthCookies(w, m.environment, m.cookieDomain, m.cookiePrefixAuthToken, tokenResp.AccessToken, tokenResp.AccessTokenExpiresAt, m.cookiePrefixRefreshToken, tokenResp.RefreshToken, tokenResp.RefreshTokenExpiresAt)

	// set the new access token in the header
	req.Header["Authorization"] = []string{"Bearer " + tokenResp.AccessToken}
}

type httpResponseWriter struct {
	http.ResponseWriter
	statusCode int
}

// middlewareResponseWriter handles events when responses that implicitly returns 200 OK do
// no call WriteHeader(int).
func middlewareResponseWriter(w http.ResponseWriter, txn *newrelic.Transaction) *httpResponseWriter {

	// writer is a http.ResponseWriter, use the returned writer in place of the original
	w = txn.SetWebResponse(w)

	return &httpResponseWriter{w, http.StatusOK}
}

func (lrw *httpResponseWriter) WriteHeader(code int) {
	lrw.statusCode = code
	lrw.ResponseWriter.WriteHeader(code)
}

// getBaseResponseHandler returns response handler configured with auth error map
func (m *Middleware) getBaseResponseHandler() *reply.Replier {
	return reply.NewReplier(m.errorMaps)
}
