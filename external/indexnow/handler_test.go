package indexnow

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newVerificationService builds a minimal service for handler tests. The
// service constructor is exercised in the root-owned service tests; here only
// the handler contract matters.
func newVerificationService(t *testing.T, keyLocation string) *Service {
	t.Helper()
	s, err := NewService("acme-indexnow-key1", Config{
		SiteURL:     "https://example.com",
		KeyLocation: keyLocation,
	})
	require.NoError(t, err)
	return s
}

func TestHandlerVerificationResponses(t *testing.T) {
	tests := []struct {
		name           string
		keyLocation    string
		method         string
		path           string
		wantStatus     int
		wantBody       string
		wantContentLen string
	}{
		{
			name:           "get default path returns key as utf-8 body",
			keyLocation:    "",
			method:         http.MethodGet,
			path:           "/acme-indexnow-key1.txt",
			wantStatus:     http.StatusOK,
			wantBody:       "acme-indexnow-key1",
			wantContentLen: "18",
		},
		{
			name:           "head custom path returns headers without body",
			keyLocation:    "/my-key-file.txt",
			method:         http.MethodHead,
			path:           "/my-key-file.txt",
			wantStatus:     http.StatusOK,
			wantBody:       "",
			wantContentLen: "18",
		},
		{
			name:           "get custom path returns key body",
			keyLocation:    "/my-key-file.txt",
			method:         http.MethodGet,
			path:           "/my-key-file.txt",
			wantStatus:     http.StatusOK,
			wantBody:       "acme-indexnow-key1",
			wantContentLen: "18",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := mux.NewRouter()
			NewHandler(newVerificationService(t, tt.keyLocation)).RegisterRoutes(router)

			req := httptest.NewRequest(tt.method, tt.path, nil)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			assert.Equal(t, tt.wantStatus, rec.Code)
			assert.Equal(t, "text/plain; charset=utf-8", rec.Header().Get("Content-Type"))
			assert.Equal(t, tt.wantContentLen, rec.Header().Get("Content-Length"))
			if tt.method == http.MethodHead {
				assert.Empty(t, rec.Body.String(), "HEAD handler must write no body")
			} else {
				assert.Equal(t, tt.wantBody, rec.Body.String())
			}
		})
	}
}

func TestHandlerNegativeMethodsAndPaths(t *testing.T) {
	router := mux.NewRouter()
	NewHandler(newVerificationService(t, "/my-key-file.txt")).RegisterRoutes(router)

	tests := []struct {
		name   string
		method string
		path   string
	}{
		{name: "post to key path is not routed", method: http.MethodPost, path: "/my-key-file.txt"},
		{name: "put to key path is not routed", method: http.MethodPut, path: "/my-key-file.txt"},
		{name: "delete to key path is not routed", method: http.MethodDelete, path: "/my-key-file.txt"},
		{name: "patch to key path is not routed", method: http.MethodPatch, path: "/my-key-file.txt"},
		{name: "options to key path is not routed", method: http.MethodOptions, path: "/my-key-file.txt"},
		{name: "other path does not reflect key", method: http.MethodGet, path: "/elsewhere.txt"},
		{name: "root path does not reflect key", method: http.MethodGet, path: "/"},
		{name: "prefix path does not reflect key", method: http.MethodGet, path: "/my-key-file.txt/extra"},
		{name: "parent directory path does not reflect key", method: http.MethodGet, path: "/nested/../../my-key-file.txt"},
		{name: "head elsewhere does not reflect key", method: http.MethodHead, path: "/elsewhere.txt"},
		{name: "encoded letter does not reflect key", method: http.MethodGet, path: "/%6dy-key-file.txt"},
		{name: "encoded HEAD does not reflect key", method: http.MethodHead, path: "/%6dy-key-file.txt"},
		{name: "encoded slash does not reflect key", method: http.MethodGet, path: "/%2fmy-key-file.txt"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, nil)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			assert.NotEqual(t, http.StatusOK, rec.Code, "unmatched route must not answer with the key")
			assert.NotContains(t, rec.Body.String(), "acme-indexnow-key1")
		})
	}
}

func TestHandlerNeverPermitsSubmissionOrReflection(t *testing.T) {
	router := mux.NewRouter()
	NewHandler(newVerificationService(t, "/my-key-file.txt")).RegisterRoutes(router)
	router.NotFoundHandler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})

	tests := []struct {
		name   string
		method string
		path   string
	}{
		{name: "post to key path is not submission", method: http.MethodPost, path: "/my-key-file.txt"},
		{name: "put to key path rejected", method: http.MethodPut, path: "/my-key-file.txt"},
		{name: "other path does not reflect key", method: http.MethodGet, path: "/elsewhere.txt"},
		{name: "root path does not reflect key", method: http.MethodGet, path: "/"},
		{name: "traversal style path does not reflect key", method: http.MethodGet, path: "/acme-indexnow-key1.txt/other"},
		{name: "submission style path is not routed", method: http.MethodPost, path: "/indexnow"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, nil)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			assert.NotEqual(t, http.StatusOK, rec.Code, "unmatched route must not answer with the key")
			assert.NotContains(t, rec.Body.String(), "acme-indexnow-key1")
		})
	}
}

func TestHandlerRegisterRoutesDefensive(t *testing.T) {
	type testCase struct {
		name                              string
		nilRouter, nilService, nilHandler bool
	}
	cases := []testCase{
		{name: "nil router", nilRouter: true},
		{name: "nil service", nilService: true},
		{name: "nil handler", nilHandler: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			router := mux.NewRouter()
			service := newVerificationService(t, "")
			if tc.nilRouter {
				router = nil
			}
			if tc.nilService {
				service = nil
			}
			handler := NewHandler(service)
			if tc.nilHandler {
				handler = nil
			}
			assert.NotPanics(t, func() { handler.RegisterRoutes(router) })
		})
	}
}
