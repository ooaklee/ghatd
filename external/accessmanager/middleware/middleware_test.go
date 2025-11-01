package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ooaklee/ghatd/external/accessmanager"
	accessmanagerhelpers "github.com/ooaklee/ghatd/external/accessmanager/helpers"
	"github.com/ooaklee/ghatd/external/common"
	"github.com/ooaklee/reply"
)

// mockAccessManagerService is a mock implementation of accessManagerService for testing
type mockAccessManagerService struct {
	middlewareAdminJWTRequiredFunc             func(r *http.Request) (string, error)
	middlewareAdminAPITokenRequiredFunc        func(r *http.Request) (string, error)
	middlewareActiveJWTRequiredFunc            func(r *http.Request) (string, error)
	middlewareJWTRequiredFunc                  func(r *http.Request) (string, error)
	middlewareValidAPITokenRequiredFunc        func(r *http.Request) (string, error)
	middlewareRateLimitOrActiveJWTRequiredFunc func(r *http.Request) (string, error)
	refreshTokenFunc                           func(ctx context.Context, r *accessmanager.RefreshTokenRequest) (*accessmanager.RefreshTokenResponse, error)
}

func (m *mockAccessManagerService) MiddlewareAdminJWTRequired(r *http.Request) (string, error) {
	if m.middlewareAdminJWTRequiredFunc != nil {
		return m.middlewareAdminJWTRequiredFunc(r)
	}
	return "", nil
}

func (m *mockAccessManagerService) MiddlewareAdminAPITokenRequired(r *http.Request) (string, error) {
	if m.middlewareAdminAPITokenRequiredFunc != nil {
		return m.middlewareAdminAPITokenRequiredFunc(r)
	}
	return "", nil
}

func (m *mockAccessManagerService) MiddlewareActiveJWTRequired(r *http.Request) (string, error) {
	if m.middlewareActiveJWTRequiredFunc != nil {
		return m.middlewareActiveJWTRequiredFunc(r)
	}
	return "", nil
}

func (m *mockAccessManagerService) MiddlewareJWTRequired(r *http.Request) (string, error) {
	if m.middlewareJWTRequiredFunc != nil {
		return m.middlewareJWTRequiredFunc(r)
	}
	return "", nil
}

func (m *mockAccessManagerService) MiddlewareValidAPITokenRequired(r *http.Request) (string, error) {
	if m.middlewareValidAPITokenRequiredFunc != nil {
		return m.middlewareValidAPITokenRequiredFunc(r)
	}
	return "", nil
}

func (m *mockAccessManagerService) MiddlewareRateLimitOrActiveJWTRequired(r *http.Request) (string, error) {
	if m.middlewareRateLimitOrActiveJWTRequiredFunc != nil {
		return m.middlewareRateLimitOrActiveJWTRequiredFunc(r)
	}
	return "", nil
}

func (m *mockAccessManagerService) RefreshToken(ctx context.Context, r *accessmanager.RefreshTokenRequest) (*accessmanager.RefreshTokenResponse, error) {
	if m.refreshTokenFunc != nil {
		return m.refreshTokenFunc(ctx, r)
	}
	return nil, nil
}

// createTestMiddleware creates a middleware instance for testing
func createTestMiddleware(service accessManagerService) *Middleware {
	return NewMiddleware(&NewMiddlewareRequest{
		Service:                  service,
		ErrorMaps:                []reply.ErrorManifest{},
		Environment:              "test",
		CookiePrefixAuthToken:    "test_auth",
		CookiePrefixRefreshToken: "test_refresh",
		CookieDomain:             "test.com",
	})
}

// createTestHandler creates a simple test handler that writes success response
func createTestHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Extract user ID from context if it exists
		userID := accessmanagerhelpers.AcquireFrom(r.Context())
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("success:" + userID))
	})
}

func TestNewMiddleware(t *testing.T) {
	service := &mockAccessManagerService{}

	middleware := NewMiddleware(&NewMiddlewareRequest{
		Service:                  service,
		ErrorMaps:                []reply.ErrorManifest{},
		Environment:              "production",
		CookiePrefixAuthToken:    "auth_token",
		CookiePrefixRefreshToken: "refresh_token",
		CookieDomain:             "example.com",
	})

	if middleware == nil {
		t.Fatal("Expected middleware to be created, got nil")
	}

	if middleware.service != service {
		t.Error("Service not properly initialized")
	}

	if middleware.environment != "production" {
		t.Errorf("Expected environment to be 'production', got '%s'", middleware.environment)
	}

	if middleware.cookiePrefixAuthToken != "auth_token" {
		t.Errorf("Expected cookiePrefixAuthToken to be 'auth_token', got '%s'", middleware.cookiePrefixAuthToken)
	}

	if middleware.cookiePrefixRefreshToken != "refresh_token" {
		t.Errorf("Expected cookiePrefixRefreshToken to be 'refresh_token', got '%s'", middleware.cookiePrefixRefreshToken)
	}

	if middleware.cookieDomain != "example.com" {
		t.Errorf("Expected cookieDomain to be 'example.com', got '%s'", middleware.cookieDomain)
	}
}

func TestValidAPITokenRequired_Success(t *testing.T) {
	userID := "test-user-123"
	mockService := &mockAccessManagerService{
		middlewareValidAPITokenRequiredFunc: func(r *http.Request) (string, error) {
			return userID, nil
		},
	}

	middleware := createTestMiddleware(mockService)
	handler := createTestHandler()
	wrappedHandler := middleware.ValidAPITokenRequired(handler)

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set(common.SystemWideXApiToken, "test-token")
	w := httptest.NewRecorder()

	wrappedHandler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status code %d, got %d", http.StatusOK, w.Code)
	}

	expected := "success:" + userID
	if w.Body.String() != expected {
		t.Errorf("Expected body '%s', got '%s'", expected, w.Body.String())
	}
}

func TestValidAPITokenRequired_Failure(t *testing.T) {
	mockService := &mockAccessManagerService{
		middlewareValidAPITokenRequiredFunc: func(r *http.Request) (string, error) {
			return "", errors.New("invalid token")
		},
	}

	middleware := createTestMiddleware(mockService)
	handler := createTestHandler()
	wrappedHandler := middleware.ValidAPITokenRequired(handler)

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set(common.SystemWideXApiToken, "invalid-token")
	w := httptest.NewRecorder()

	wrappedHandler.ServeHTTP(w, req)

	if w.Code == http.StatusOK {
		t.Error("Expected non-200 status code for invalid token")
	}
}

func TestAdminJWTRequired_Success(t *testing.T) {
	userID := "admin-user-123"
	mockService := &mockAccessManagerService{
		middlewareAdminJWTRequiredFunc: func(r *http.Request) (string, error) {
			return userID, nil
		},
	}

	middleware := createTestMiddleware(mockService)
	handler := createTestHandler()
	wrappedHandler := middleware.AdminJWTRequired(handler)

	req := httptest.NewRequest("GET", "/admin", nil)
	req.AddCookie(&http.Cookie{Name: "test_auth", Value: "valid-jwt-token"})
	req.AddCookie(&http.Cookie{Name: "test_refresh", Value: "valid-refresh-token"})
	w := httptest.NewRecorder()

	wrappedHandler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status code %d, got %d", http.StatusOK, w.Code)
	}
}

func TestAdminJWTRequired_MissingRefreshToken(t *testing.T) {
	mockService := &mockAccessManagerService{
		middlewareAdminJWTRequiredFunc: func(r *http.Request) (string, error) {
			return "", errors.New("unauthorized")
		},
	}

	middleware := createTestMiddleware(mockService)
	handler := createTestHandler()
	wrappedHandler := middleware.AdminJWTRequired(handler)

	req := httptest.NewRequest("GET", "/admin", nil)
	req.AddCookie(&http.Cookie{Name: "test_auth", Value: "valid-jwt-token"})
	// Missing refresh token
	w := httptest.NewRecorder()

	wrappedHandler.ServeHTTP(w, req)

	if w.Code == http.StatusOK {
		t.Error("Expected non-200 status code when refresh token is missing")
	}
}

func TestActiveJWTRequired_Success(t *testing.T) {
	userID := "active-user-123"
	mockService := &mockAccessManagerService{
		middlewareActiveJWTRequiredFunc: func(r *http.Request) (string, error) {
			return userID, nil
		},
	}

	middleware := createTestMiddleware(mockService)
	handler := createTestHandler()
	wrappedHandler := middleware.ActiveJWTRequired(handler)

	req := httptest.NewRequest("GET", "/active", nil)
	req.AddCookie(&http.Cookie{Name: "test_auth", Value: "valid-jwt-token"})
	req.AddCookie(&http.Cookie{Name: "test_refresh", Value: "valid-refresh-token"})
	w := httptest.NewRecorder()

	wrappedHandler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status code %d, got %d", http.StatusOK, w.Code)
	}
}

func TestJWTRequired_Success(t *testing.T) {
	userID := "user-123"
	mockService := &mockAccessManagerService{
		middlewareJWTRequiredFunc: func(r *http.Request) (string, error) {
			return userID, nil
		},
	}

	middleware := createTestMiddleware(mockService)
	handler := createTestHandler()
	wrappedHandler := middleware.JWTRequired(handler)

	req := httptest.NewRequest("GET", "/protected", nil)
	req.AddCookie(&http.Cookie{Name: "test_auth", Value: "valid-jwt-token"})
	req.AddCookie(&http.Cookie{Name: "test_refresh", Value: "valid-refresh-token"})
	w := httptest.NewRecorder()

	wrappedHandler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status code %d, got %d", http.StatusOK, w.Code)
	}
}

func TestJWTRequired_TokenRefresh(t *testing.T) {
	userID := "user-123"
	callCount := 0

	mockService := &mockAccessManagerService{
		middlewareJWTRequiredFunc: func(r *http.Request) (string, error) {
			callCount++
			// First call fails (expired token), second call succeeds
			if callCount == 1 {
				return "", errors.New("token expired")
			}
			return userID, nil
		},
		refreshTokenFunc: func(ctx context.Context, r *accessmanager.RefreshTokenRequest) (*accessmanager.RefreshTokenResponse, error) {
			return &accessmanager.RefreshTokenResponse{
				AccessToken:           "new-access-token",
				RefreshToken:          "new-refresh-token",
				AccessTokenExpiresAt:  1700000000,
				RefreshTokenExpiresAt: 1700000000,
			}, nil
		},
	}

	middleware := createTestMiddleware(mockService)
	handler := createTestHandler()
	wrappedHandler := middleware.JWTRequired(handler)

	req := httptest.NewRequest("GET", "/protected", nil)
	req.AddCookie(&http.Cookie{Name: "test_auth", Value: "expired-jwt-token"})
	req.AddCookie(&http.Cookie{Name: "test_refresh", Value: "valid-refresh-token"})
	w := httptest.NewRecorder()

	wrappedHandler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status code %d after refresh, got %d", http.StatusOK, w.Code)
	}

	if callCount != 2 {
		t.Errorf("Expected middleware function to be called twice (initial + retry), got %d", callCount)
	}
}

func TestJWTRequired_RefreshTokenFailure(t *testing.T) {
	mockService := &mockAccessManagerService{
		middlewareJWTRequiredFunc: func(r *http.Request) (string, error) {
			return "", errors.New("token expired")
		},
		refreshTokenFunc: func(ctx context.Context, r *accessmanager.RefreshTokenRequest) (*accessmanager.RefreshTokenResponse, error) {
			return nil, errors.New("refresh token invalid")
		},
	}

	middleware := createTestMiddleware(mockService)
	handler := createTestHandler()
	wrappedHandler := middleware.JWTRequired(handler)

	req := httptest.NewRequest("GET", "/protected", nil)
	req.AddCookie(&http.Cookie{Name: "test_auth", Value: "expired-jwt-token"})
	req.AddCookie(&http.Cookie{Name: "test_refresh", Value: "invalid-refresh-token"})
	w := httptest.NewRecorder()

	wrappedHandler.ServeHTTP(w, req)

	if w.Code == http.StatusOK {
		t.Error("Expected non-200 status code when refresh token is invalid")
	}
}

func TestActiveValidApiTokenOrJWTRequired_APITokenPresent(t *testing.T) {
	userID := "api-user-123"
	mockService := &mockAccessManagerService{
		middlewareValidAPITokenRequiredFunc: func(r *http.Request) (string, error) {
			return userID, nil
		},
	}

	middleware := createTestMiddleware(mockService)
	handler := createTestHandler()
	wrappedHandler := middleware.ActiveValidApiTokenOrJWTRequired(handler)

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set(common.SystemWideXApiToken, "valid-api-token")
	w := httptest.NewRecorder()

	wrappedHandler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status code %d, got %d", http.StatusOK, w.Code)
	}
}

func TestActiveValidApiTokenOrJWTRequired_JWTPresent(t *testing.T) {
	userID := "jwt-user-123"
	mockService := &mockAccessManagerService{
		middlewareActiveJWTRequiredFunc: func(r *http.Request) (string, error) {
			return userID, nil
		},
	}

	middleware := createTestMiddleware(mockService)
	handler := createTestHandler()
	wrappedHandler := middleware.ActiveValidApiTokenOrJWTRequired(handler)

	req := httptest.NewRequest("GET", "/test", nil)
	req.AddCookie(&http.Cookie{Name: "test_auth", Value: "valid-jwt-token"})
	req.AddCookie(&http.Cookie{Name: "test_refresh", Value: "valid-refresh-token"})
	w := httptest.NewRecorder()

	wrappedHandler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status code %d, got %d", http.StatusOK, w.Code)
	}
}

func TestActiveValidApiTokenOrAuthenticated_APITokenPrecedence(t *testing.T) {
	apiTokenUserID := "api-user-123"
	jwtUserID := "jwt-user-123"

	mockService := &mockAccessManagerService{
		middlewareValidAPITokenRequiredFunc: func(r *http.Request) (string, error) {
			return apiTokenUserID, nil
		},
		middlewareJWTRequiredFunc: func(r *http.Request) (string, error) {
			return jwtUserID, nil
		},
	}

	middleware := createTestMiddleware(mockService)
	handler := createTestHandler()
	wrappedHandler := middleware.ActiveValidApiTokenOrAuthenticated(handler)

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set(common.SystemWideXApiToken, "valid-api-token")
	req.AddCookie(&http.Cookie{Name: "test_auth", Value: "valid-jwt-token"})
	req.AddCookie(&http.Cookie{Name: "test_refresh", Value: "valid-refresh-token"})
	w := httptest.NewRecorder()

	wrappedHandler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status code %d, got %d", http.StatusOK, w.Code)
	}

	// Verify API token was used (not JWT)
	expected := "success:" + apiTokenUserID
	if w.Body.String() != expected {
		t.Errorf("Expected API token to take precedence. Got body '%s', expected '%s'", w.Body.String(), expected)
	}
}

func TestAdminApiTokenOrJWTRequired_APITokenPresent(t *testing.T) {
	userID := "admin-api-user-123"
	mockService := &mockAccessManagerService{
		middlewareAdminAPITokenRequiredFunc: func(r *http.Request) (string, error) {
			return userID, nil
		},
	}

	middleware := createTestMiddleware(mockService)
	handler := createTestHandler()
	wrappedHandler := middleware.AdminApiTokenOrJWTRequired(handler)

	req := httptest.NewRequest("GET", "/admin", nil)
	req.Header.Set(common.SystemWideXApiToken, "valid-admin-api-token")
	w := httptest.NewRecorder()

	wrappedHandler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status code %d, got %d", http.StatusOK, w.Code)
	}
}

func TestAdminApiTokenOrJWTRequired_JWTPresent(t *testing.T) {
	userID := "admin-jwt-user-123"
	mockService := &mockAccessManagerService{
		middlewareAdminJWTRequiredFunc: func(r *http.Request) (string, error) {
			return userID, nil
		},
	}

	middleware := createTestMiddleware(mockService)
	handler := createTestHandler()
	wrappedHandler := middleware.AdminApiTokenOrJWTRequired(handler)

	req := httptest.NewRequest("GET", "/admin", nil)
	req.AddCookie(&http.Cookie{Name: "test_auth", Value: "valid-jwt-token"})
	req.AddCookie(&http.Cookie{Name: "test_refresh", Value: "valid-refresh-token"})
	w := httptest.NewRecorder()

	wrappedHandler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status code %d, got %d", http.StatusOK, w.Code)
	}
}

func TestRateLimitOrActiveJWTRequired_NoCookies(t *testing.T) {
	userID := "rate-limited-user"
	mockService := &mockAccessManagerService{
		middlewareRateLimitOrActiveJWTRequiredFunc: func(r *http.Request) (string, error) {
			return userID, nil
		},
	}

	middleware := createTestMiddleware(mockService)
	handler := createTestHandler()
	wrappedHandler := middleware.RateLimitOrActiveJWTRequired(handler)

	req := httptest.NewRequest("GET", "/public", nil)
	w := httptest.NewRecorder()

	wrappedHandler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status code %d, got %d", http.StatusOK, w.Code)
	}
}

func TestRateLimitOrActiveJWTRequired_WithValidJWT(t *testing.T) {
	userID := "authenticated-user-123"
	mockService := &mockAccessManagerService{
		middlewareRateLimitOrActiveJWTRequiredFunc: func(r *http.Request) (string, error) {
			return userID, nil
		},
	}

	middleware := createTestMiddleware(mockService)
	handler := createTestHandler()
	wrappedHandler := middleware.RateLimitOrActiveJWTRequired(handler)

	req := httptest.NewRequest("GET", "/public", nil)
	req.AddCookie(&http.Cookie{Name: "test_auth", Value: "valid-jwt-token"})
	req.AddCookie(&http.Cookie{Name: "test_refresh", Value: "valid-refresh-token"})
	w := httptest.NewRecorder()

	wrappedHandler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status code %d, got %d", http.StatusOK, w.Code)
	}
}

func TestRateLimitOrActiveJWTRequired_TokenRefresh(t *testing.T) {
	userID := "user-123"
	callCount := 0

	mockService := &mockAccessManagerService{
		middlewareRateLimitOrActiveJWTRequiredFunc: func(r *http.Request) (string, error) {
			callCount++
			if callCount == 1 {
				return "", errors.New("token expired")
			}
			return userID, nil
		},
		refreshTokenFunc: func(ctx context.Context, r *accessmanager.RefreshTokenRequest) (*accessmanager.RefreshTokenResponse, error) {
			return &accessmanager.RefreshTokenResponse{
				AccessToken:           "new-access-token",
				RefreshToken:          "new-refresh-token",
				AccessTokenExpiresAt:  1700000000,
				RefreshTokenExpiresAt: 1700000000,
			}, nil
		},
	}

	middleware := createTestMiddleware(mockService)
	handler := createTestHandler()
	wrappedHandler := middleware.RateLimitOrActiveJWTRequired(handler)

	req := httptest.NewRequest("GET", "/public", nil)
	req.AddCookie(&http.Cookie{Name: "test_auth", Value: "expired-jwt-token"})
	req.AddCookie(&http.Cookie{Name: "test_refresh", Value: "valid-refresh-token"})
	w := httptest.NewRecorder()

	wrappedHandler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status code %d after refresh, got %d", http.StatusOK, w.Code)
	}

	if callCount != 2 {
		t.Errorf("Expected middleware function to be called twice, got %d", callCount)
	}
}

func TestGetBaseResponseHandler(t *testing.T) {
	mockService := &mockAccessManagerService{}
	middleware := createTestMiddleware(mockService)

	handler := middleware.getBaseResponseHandler()
	if handler == nil {
		t.Fatal("Expected non-nil response handler")
	}
}
