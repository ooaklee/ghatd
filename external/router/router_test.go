package router_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/gorilla/mux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ooaklee/ghatd/external/router"
)

const (
	testApiVerifyEndpoint = "/v1/ams/verify?t=%s"
	testApiLoginEndpoint  = "/v1/ams/login?t=%s"
	testFrontendLoginUrl  = "https://app.example.com/login"
	testFrontendAppUrl    = "https://app.example.com/home"
	testHealthEndpoint    = "/v0/health/check"
	testAuthVerifyPath    = "/v0/auth/verify"
)

func TestNewRouter(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name              string
		set404            bool
		setHealth         bool
		withMiddleware    bool
		requestPath       string
		expectStatus      int
		expectBodyContain string
	}{
		{
			name:              "Success - default 404 handler is invoked for unknown path",
			set404:            true,
			requestPath:       "/does-not-exist",
			expectStatus:      http.StatusNotFound,
			expectBodyContain: "not-found",
		},
		{
			name:              "Success - healthcheck handler is registered at expected endpoint",
			setHealth:         true,
			requestPath:       testHealthEndpoint,
			expectStatus:      http.StatusOK,
			expectBodyContain: "healthy",
		},
		{
			name:              "Success - middleware is wired into router pipeline",
			setHealth:         true,
			withMiddleware:    true,
			requestPath:       testHealthEndpoint,
			expectStatus:      http.StatusOK,
			expectBodyContain: "healthy",
		},
		{
			name:         "Success - nil 404 handler still allows healthcheck to respond",
			setHealth:    true,
			requestPath:  testHealthEndpoint,
			expectStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var (
				notFoundHandler  func(w http.ResponseWriter, r *http.Request)
				healthHandler    func(w http.ResponseWriter, r *http.Request)
				middlewareCalled bool
			)

			if tt.set404 {
				notFoundHandler = func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusNotFound)
					_, _ = w.Write([]byte("not-found"))
				}
			}

			if tt.setHealth {
				healthHandler = func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusOK)
					_, _ = w.Write([]byte("healthy"))
				}
			}

			var middlewares []mux.MiddlewareFunc
			if tt.withMiddleware {
				middlewares = append(middlewares, func(next http.Handler) http.Handler {
					return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						middlewareCalled = true
						next.ServeHTTP(w, r)
					})
				})
			}

			r := router.NewRouter(notFoundHandler, healthHandler, middlewares...)
			require.NotNil(t, r)
			require.NotNil(t, r.GetRouter())

			req := httptest.NewRequest(http.MethodGet, tt.requestPath, nil)
			rec := httptest.NewRecorder()

			r.GetRouter().ServeHTTP(rec, req)

			assert.Equal(t, tt.expectStatus, rec.Code)
			if tt.expectBodyContain != "" {
				assert.Contains(t, rec.Body.String(), tt.expectBodyContain)
			}
			if tt.withMiddleware {
				assert.True(t, middlewareCalled, "expected middleware to be invoked")
			}
		})
	}
}

func TestNewRouter_NoHandlersConfigured(t *testing.T) {
	t.Parallel()

	// When no 404 handler is provided, mux falls back to the default 404 response.
	r := router.NewRouter(nil, nil)
	require.NotNil(t, r)
	require.NotNil(t, r.GetRouter())

	req := httptest.NewRequest(http.MethodGet, "/anything", nil)
	rec := httptest.NewRecorder()
	r.GetRouter().ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestNewAuthVerifyHandler(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		rawQuery       string
		expectStatus   int
		expectLocation string
	}{
		{
			name:           "Redirect to frontend login when verification token missing",
			rawQuery:       "type=2",
			expectStatus:   http.StatusTemporaryRedirect,
			expectLocation: testFrontendLoginUrl,
		},
		{
			name:           "Redirect to frontend login when verification email type missing",
			rawQuery:       "__t=abc123",
			expectStatus:   http.StatusTemporaryRedirect,
			expectLocation: testFrontendLoginUrl,
		},
		{
			name:           "Redirect to frontend login when query string is empty",
			rawQuery:       "",
			expectStatus:   http.StatusTemporaryRedirect,
			expectLocation: testFrontendLoginUrl,
		},
		{
			name:           "Redirect to login API endpoint when type is 1 (loginVerification) - no requested url",
			rawQuery:       "__t=login-token-value&type=1",
			expectStatus:   http.StatusTemporaryRedirect,
			expectLocation: "/v1/ams/login?t=login-token-value&next_step=" + url.QueryEscape(testFrontendAppUrl),
		},
		{
			name:           "Redirect to verify API endpoint when type is 2 (emailVerification) - no requested url",
			rawQuery:       "__t=verify-token-value&type=2",
			expectStatus:   http.StatusTemporaryRedirect,
			expectLocation: "/v1/ams/verify?t=verify-token-value&next_step=" + url.QueryEscape(testFrontendAppUrl),
		},
		{
			name:           "Redirect to login API endpoint with requested url as next_step",
			rawQuery:       "__t=login-token-value&type=1&request_url=/dashboard",
			expectStatus:   http.StatusTemporaryRedirect,
			expectLocation: "/v1/ams/login?t=login-token-value&next_step=" + url.QueryEscape("https://app.example.com/dashboard"),
		},
		{
			name:           "Redirect to verify API endpoint with requested url as next_step",
			rawQuery:       "__t=verify-token-value&type=2&request_url=/onboarding",
			expectStatus:   http.StatusTemporaryRedirect,
			expectLocation: "/v1/ams/verify?t=verify-token-value&next_step=" + url.QueryEscape("https://app.example.com/onboarding"),
		},
		{
			name:           "Supports HTML-escaped &amp; query separator",
			rawQuery:       "__t=verify-token-value&amp;type=2&amp;request_url=/onboarding",
			expectStatus:   http.StatusTemporaryRedirect,
			expectLocation: "/v1/ams/verify?t=verify-token-value&next_step=" + url.QueryEscape("https://app.example.com/onboarding"),
		},
		{
			name:           "Single-param query with only token still parses (no &) and redirects to login due to missing type",
			rawQuery:       "__t=lonely-token",
			expectStatus:   http.StatusTemporaryRedirect,
			expectLocation: testFrontendLoginUrl,
		},
		{
			name:           "URL-encoded request_url is decoded before being used as next_step",
			rawQuery:       "__t=verify-token-value&type=2&request_url=%2Fdashboard",
			expectStatus:   http.StatusTemporaryRedirect,
			expectLocation: "/v1/ams/verify?t=verify-token-value&next_step=" + url.QueryEscape("https://app.example.com/dashboard"),
		},
		{
			name:           "request_url containing '=' is preserved in full",
			rawQuery:       "__t=verify-token-value&type=2&request_url=/path?foo=bar",
			expectStatus:   http.StatusTemporaryRedirect,
			expectLocation: "/v1/ams/verify?t=verify-token-value&next_step=" + url.QueryEscape("https://app.example.com/path?foo=bar"),
		},
		{
			name:           "URL-encoded request_url with nested query is resolved against frontend app",
			rawQuery:       "__t=login-token-value&type=1&request_url=%2Fapp%2Fplan%3Fslug%3Dpro",
			expectStatus:   http.StatusTemporaryRedirect,
			expectLocation: "/v1/ams/login?t=login-token-value&next_step=" + url.QueryEscape("https://app.example.com/app/plan?slug=pro"),
		},
		{
			name:           "External request_url falls back to frontend app url",
			rawQuery:       "__t=login-token-value&type=1&request_url=https%3A%2F%2Fexample-tunnel.trycloudflare.com%2Fapp%2Fplan",
			expectStatus:   http.StatusTemporaryRedirect,
			expectLocation: "/v1/ams/login?t=login-token-value&next_step=" + url.QueryEscape(testFrontendAppUrl),
		},
		{
			name:           "Unsupported type falls through to default branch and sets Location header",
			rawQuery:       "__t=some-token&type=99",
			expectStatus:   http.StatusTemporaryRedirect,
			expectLocation: testFrontendLoginUrl,
		},
	}

	handler := router.NewAuthVerifyHandler(testApiVerifyEndpoint, testApiLoginEndpoint, testFrontendLoginUrl, testFrontendAppUrl)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest(http.MethodGet, testAuthVerifyPath+"?"+tt.rawQuery, nil)
			// httptest.NewRequest URL-encodes by parsing, but we want to control raw query verbatim
			req.URL.RawQuery = tt.rawQuery
			rec := httptest.NewRecorder()

			require.NotPanics(t, func() {
				handler(rec, req)
			})

			assert.Equal(t, tt.expectStatus, rec.Code)
			assert.Equal(t, tt.expectLocation, rec.Header().Get("Location"))
		})
	}
}

// TestNewAuthVerifyHandler_RegisteredOnRouter wires the handler onto a Router via
// the public NewRouter constructor to verify end-to-end integration through mux.
func TestNewAuthVerifyHandler_RegisteredOnRouter(t *testing.T) {
	t.Parallel()

	r := router.NewRouter(nil, nil)
	r.GetRouter().HandleFunc(
		router.AuthVerifyEndpoint,
		router.NewAuthVerifyHandler(testApiVerifyEndpoint, testApiLoginEndpoint, testFrontendLoginUrl, testFrontendAppUrl),
	)

	req := httptest.NewRequest(http.MethodGet, router.AuthVerifyEndpoint+"?__t=token-abc&type=2&request_url=/welcome", nil)
	req.URL.RawQuery = "__t=token-abc&type=2&request_url=/welcome"
	rec := httptest.NewRecorder()

	r.GetRouter().ServeHTTP(rec, req)

	assert.Equal(t, http.StatusTemporaryRedirect, rec.Code)
	assert.Equal(t, "/v1/ams/verify?t=token-abc&next_step="+url.QueryEscape("https://app.example.com/welcome"), rec.Header().Get("Location"))
}

func TestNewAuthVerifyHandler_LocalDevLocationHeaderCarriesFrontendNextStep(t *testing.T) {
	t.Parallel()

	handler := router.NewAuthVerifyHandler(
		"http://localhost:4000/api/v1/ams/verify/email?t=%s",
		"http://localhost:4000/api/v1/ams/login?t=%s",
		"http://localhost:5173/auth/login",
		"http://localhost:5173/",
	)

	req := httptest.NewRequest(http.MethodGet, router.AuthVerifyEndpoint, nil)
	req.URL.RawQuery = "__t=token-abc&type=1&request_url=%2Fapp%2Fplan%3Fslug%3Dpro"
	rec := httptest.NewRecorder()

	handler(rec, req)

	assert.Equal(t, http.StatusTemporaryRedirect, rec.Code)
	assert.Equal(
		t,
		"http://localhost:4000/api/v1/ams/login?t=token-abc&next_step="+url.QueryEscape("http://localhost:5173/app/plan?slug=pro"),
		rec.Header().Get("Location"),
	)
}

func TestAttachDefaultAuthVerifyRoute(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		backendBase    string
		frontendBase   string
		rawQuery       string
		expectStatus   int
		expectLocation string
	}{
		{
			name:           "Redirect to verify API when type is 2 with valid base URLs",
			backendBase:    "https://api.example.com",
			frontendBase:   "https://app.example.com",
			rawQuery:       "__t=token-abc&type=2&request_url=/welcome",
			expectStatus:   http.StatusTemporaryRedirect,
			expectLocation: "https://api.example.com/api/v1/ams/verify/email?t=token-abc&next_step=" + url.QueryEscape("https://app.example.com/welcome"),
		},
		{
			name:           "Redirect to login API when type is 1 with valid base URLs",
			backendBase:    "https://api.example.com",
			frontendBase:   "https://app.example.com",
			rawQuery:       "__t=login-token&type=1&request_url=/dashboard",
			expectStatus:   http.StatusTemporaryRedirect,
			expectLocation: "https://api.example.com/api/v1/ams/login?t=login-token&next_step=" + url.QueryEscape("https://app.example.com/dashboard"),
		},
		{
			name:           "Missing type redirects to frontend login with valid base URLs",
			backendBase:    "https://api.example.com",
			frontendBase:   "https://app.example.com",
			rawQuery:       "__t=token-without-type",
			expectStatus:   http.StatusTemporaryRedirect,
			expectLocation: "https://app.example.com/auth/login",
		},
		{
			name:           "Localhost base URLs compose correctly",
			backendBase:    "http://localhost:4000/",
			frontendBase:   "http://localhost:5173/",
			rawQuery:       "__t=local-token&type=2",
			expectStatus:   http.StatusTemporaryRedirect,
			expectLocation: "http://localhost:4000/api/v1/ams/verify/email?t=local-token&next_step=" + url.QueryEscape("http://localhost:5173/"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := router.NewRouter(nil, nil)
			require.NotNil(t, r)

			req := router.AttachDefaultAuthVerifyRouteRequest{
				Router:          r,
				BackendBaseURL:  tt.backendBase,
				FrontendBaseURL: tt.frontendBase,
			}
			require.NoError(t, router.AttachDefaultAuthVerifyRoute(&req))

			httpReq := httptest.NewRequest(http.MethodGet, router.AuthVerifyEndpoint+"?"+tt.rawQuery, nil)
			httpReq.URL.RawQuery = tt.rawQuery
			rec := httptest.NewRecorder()

			r.GetRouter().ServeHTTP(rec, httpReq)

			assert.Equal(t, tt.expectStatus, rec.Code)
			assert.Equal(t, tt.expectLocation, rec.Header().Get("Location"))
		})
	}
}

func TestAttachDefaultAuthVerifyRouteErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		request *router.AttachDefaultAuthVerifyRouteRequest
		wantErr string
	}{
		{
			name:    "Bad - nil request",
			wantErr: "router/auth-verify-route-nil-request",
		},
		{
			name: "Bad - missing router",
			request: &router.AttachDefaultAuthVerifyRouteRequest{
				BackendBaseURL:  "https://api.example.com",
				FrontendBaseURL: "https://app.example.com",
			},
			wantErr: "router/auth-verify-route-missing-router",
		},
		{
			name: "Bad - missing backend base URL",
			request: &router.AttachDefaultAuthVerifyRouteRequest{
				Router:          router.NewRouter(nil, nil),
				FrontendBaseURL: "https://app.example.com",
			},
			wantErr: "router/auth-verify-route-invalid-backend-base-url",
		},
		{
			name: "Bad - relative frontend base URL",
			request: &router.AttachDefaultAuthVerifyRouteRequest{
				Router:          router.NewRouter(nil, nil),
				BackendBaseURL:  "https://api.example.com",
				FrontendBaseURL: "/app",
			},
			wantErr: "router/auth-verify-route-invalid-frontend-base-url",
		},
		{
			name: "Bad - frontend base URL with query",
			request: &router.AttachDefaultAuthVerifyRouteRequest{
				Router:          router.NewRouter(nil, nil),
				BackendBaseURL:  "https://api.example.com",
				FrontendBaseURL: "https://app.example.com?next=/dashboard",
			},
			wantErr: "router/auth-verify-route-invalid-frontend-base-url",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := router.AttachDefaultAuthVerifyRoute(tt.request)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}
