package router_test

import (
	"net/http"
	"net/http/httptest"
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
			expectLocation: "/v1/ams/login?t=login-token-value&next_step=" + testFrontendAppUrl,
		},
		{
			name:           "Redirect to verify API endpoint when type is 2 (emailVerification) - no requested url",
			rawQuery:       "__t=verify-token-value&type=2",
			expectStatus:   http.StatusTemporaryRedirect,
			expectLocation: "/v1/ams/verify?t=verify-token-value&next_step=" + testFrontendAppUrl,
		},
		{
			name:           "Redirect to login API endpoint with requested url as next_step",
			rawQuery:       "__t=login-token-value&type=1&request_url=/dashboard",
			expectStatus:   http.StatusTemporaryRedirect,
			expectLocation: "/v1/ams/login?t=login-token-value&next_step=/dashboard",
		},
		{
			name:           "Redirect to verify API endpoint with requested url as next_step",
			rawQuery:       "__t=verify-token-value&type=2&request_url=/onboarding",
			expectStatus:   http.StatusTemporaryRedirect,
			expectLocation: "/v1/ams/verify?t=verify-token-value&next_step=/onboarding",
		},
		{
			name:           "Supports HTML-escaped &amp; query separator",
			rawQuery:       "__t=verify-token-value&amp;type=2&amp;request_url=/onboarding",
			expectStatus:   http.StatusTemporaryRedirect,
			expectLocation: "/v1/ams/verify?t=verify-token-value&next_step=/onboarding",
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
			expectLocation: "/v1/ams/verify?t=verify-token-value&next_step=/dashboard",
		},
		{
			name:           "request_url containing '=' is preserved in full",
			rawQuery:       "__t=verify-token-value&type=2&request_url=/path?foo=bar",
			expectStatus:   http.StatusTemporaryRedirect,
			expectLocation: "/v1/ams/verify?t=verify-token-value&next_step=/path?foo=bar",
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
	assert.Equal(t, "/v1/ams/verify?t=token-abc&next_step=/welcome", rec.Header().Get("Location"))
}
