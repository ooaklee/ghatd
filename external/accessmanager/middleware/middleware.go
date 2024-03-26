package middleware

import (
	"fmt"
	"net/http"

	"github.com/newrelic/go-agent/v3/newrelic"
	"github.com/ooaklee/ghatd/external/accessmanager"
	accessmanagerhelpers "github.com/ooaklee/ghatd/external/accessmanager/helpers"
	"github.com/ooaklee/ghatd/external/common"
	"github.com/ooaklee/reply"
)

// accessManagerService holds method of valid access manaer service
type accessManagerService interface {
	MiddlewareAdminJWTRequired(r *http.Request) (string, error)
	MiddlewareActiveJWTRequired(r *http.Request) (string, error)
	MiddlewareJWTRequired(r *http.Request) (string, error)
	MiddlewareValidAPITokenRequired(r *http.Request) (string, error)
	MiddlewareRateLimitOrActiveJWTRequired(r *http.Request) (string, error)
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

	errorMaps := append(r.ErrorMaps, accessmanager.AccessmanagerErrorMap)

	return &Middleware{
		newRelicApplication:      r.NewRelicConf,
		service:                  r.Service,
		errorMaps:                errorMaps,
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
		// TODO: Will need to move to common package or something
		userFullToken := req.Header.Get(common.SystemWideXApiToken)

		// if present, run API middleware logic
		if userFullToken != "" {
			m.handleValidAPITokenRequiredRequest(w, req, handler)
			return
		}

		// Otherwise, Run authenticated token check
		m.handleJWTRequiredRequest(w, req, handler)

		if m.newRelicApplication != nil {
			newRelicTransaction := accessmanagerhelpers.AcquireTransactionFrom(req.Context())
			newRelicTransaction.End()
		}

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
		// TODO: Will need to move to common package or something
		userFullToken := req.Header.Get(common.SystemWideXApiToken)

		// if present, run API middleware logic
		if userFullToken != "" {
			m.handleValidAPITokenRequiredRequest(w, req, handler)
			return
		}

		// Otherwise, Run active token check
		m.handleActiveJWTRequiredRequest(w, req, handler)

		if m.newRelicApplication != nil {
			newRelicTransaction := accessmanagerhelpers.AcquireTransactionFrom(req.Context())
			newRelicTransaction.End()
		}
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

		if m.newRelicApplication != nil {
			newRelicTransaction := accessmanagerhelpers.AcquireTransactionFrom(req.Context())
			newRelicTransaction.End()
		}

		return

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

		// check to see if request is coming with cookies
		cookie, err := req.Cookie(m.cookiePrefixAuthToken)

		if err != nil && err != http.ErrNoCookie {
			//nolint will set up default fallback later
			m.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
			return
		}

		if cookie != nil {
			req.Header["Authorization"] = []string{"Bearer " + cookie.Value}
		}

		userID, err := m.service.MiddlewareAdminJWTRequired(req)
		if err != nil {
			//nolint will set up default fallback later
			m.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
			return
		}

		request := req.WithContext(accessmanagerhelpers.TransitWith(req.Context(), userID))

		responseWriter := middlewareResponseWriter(w, accessmanagerhelpers.AcquireTransactionFrom(req.Context()))
		handler.ServeHTTP(responseWriter, request)

		if m.newRelicApplication != nil {
			newRelicTransaction := accessmanagerhelpers.AcquireTransactionFrom(req.Context())
			newRelicTransaction.End()
		}

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

		if m.newRelicApplication != nil {
			newRelicTransaction := accessmanagerhelpers.AcquireTransactionFrom(req.Context())
			newRelicTransaction.End()
		}

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

		if m.newRelicApplication != nil {
			newRelicTransaction := accessmanagerhelpers.AcquireTransactionFrom(req.Context())
			newRelicTransaction.End()
		}

	})
}

// handleJWTRequiredRequest is checking to make sure the request
// coming in has a valid JWT
func (m *Middleware) handleJWTRequiredRequest(w http.ResponseWriter, req *http.Request, handler http.Handler) {

	// check to see if request is coming with cookies
	cookie, err := req.Cookie(m.cookiePrefixAuthToken)

	if err != nil && err != http.ErrNoCookie {
		//nolint will set up default fallback later
		m.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	if cookie != nil {
		req.Header["Authorization"] = []string{"Bearer " + cookie.Value}
	}

	userID, err := m.service.MiddlewareJWTRequired(req)
	if err != nil {
		//nolint will set up default fallback later
		m.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	request := req.WithContext(accessmanagerhelpers.TransitWith(req.Context(), userID))

	responseWriter := middlewareResponseWriter(w, accessmanagerhelpers.AcquireTransactionFrom(req.Context()))
	handler.ServeHTTP(responseWriter, request)
}

// RateLimitOrActiveJWTRequired creates a middleware ensuring that the request is rate limited if
// number of request exceeds X from the same IP (and unauth request are given "unknown user ID")
//
//	or passed with a valid token, and the user is in an `ACTIVE` state (status)
func (m *Middleware) RateLimitOrActiveJWTRequired(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {

		// Add newrelic transaction
		if m.newRelicApplication != nil {
			newRelicTransaction := m.newRelicApplication.StartTransaction(fmt.Sprintf("%s %s", req.Method, req.URL.Path))
			// req is a *http.Request, this marks the transaction as a web transaction
			newRelicTransaction.SetWebRequestHTTP(req)

			// Add to context
			req = req.WithContext(accessmanagerhelpers.TransitTransactionWith(req.Context(), newRelicTransaction))
		}

		// check to see if request is coming with cookies
		cookie, err := req.Cookie(m.cookiePrefixAuthToken)

		if err != nil && err != http.ErrNoCookie {
			//nolint will set up default fallback later
			m.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
			return
		}

		if cookie != nil {
			req.Header["Authorization"] = []string{"Bearer " + cookie.Value}
		}

		userID, err := m.service.MiddlewareRateLimitOrActiveJWTRequired(req)
		if err != nil {
			//nolint will set up default fallback later
			m.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
			return
		}

		request := req.WithContext(accessmanagerhelpers.TransitWith(req.Context(), userID))

		responseWriter := middlewareResponseWriter(w, accessmanagerhelpers.AcquireTransactionFrom(req.Context()))
		handler.ServeHTTP(responseWriter, request)

		if m.newRelicApplication != nil {
			newRelicTransaction := accessmanagerhelpers.AcquireTransactionFrom(req.Context())
			newRelicTransaction.End()
		}

	})
}

// handleActiveJWTRequiredRequest is checking to make sure the request
// coming in has a valid JWT which is in active state associated to it
func (m *Middleware) handleActiveJWTRequiredRequest(w http.ResponseWriter, req *http.Request, handler http.Handler) {

	// check to see if request is coming with cookies
	cookie, err := req.Cookie(m.cookiePrefixAuthToken)

	if err != nil && err != http.ErrNoCookie {
		//nolint will set up default fallback later
		m.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	if cookie != nil {
		req.Header["Authorization"] = []string{"Bearer " + cookie.Value}
	}

	userID, err := m.service.MiddlewareActiveJWTRequired(req)
	if err != nil {
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
		//nolint will set up default fallback later
		m.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	request := req.WithContext(accessmanagerhelpers.TransitWith(req.Context(), userID))

	responseWriter := middlewareResponseWriter(w, accessmanagerhelpers.AcquireTransactionFrom(req.Context()))
	handler.ServeHTTP(responseWriter, request)
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
