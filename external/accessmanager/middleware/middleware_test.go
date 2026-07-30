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
	"github.com/ooaklee/ghatd/external/ephemeral"
	userv2 "github.com/ooaklee/ghatd/external/user/v2"
	"github.com/ooaklee/reply/v2"
)

// mockAccessManagerService is a mock implementation of accessManagerService for testing
type mockAccessManagerService struct {
	middlewareAdminJWTRequiredFunc             func(r *http.Request) (*accessmanager.MiddlewareAuthedUserResponse, error)
	middlewareAdminAPITokenRequiredFunc        func(r *http.Request) (*accessmanager.MiddlewareAuthedUserResponse, error)
	middlewareActiveJWTRequiredFunc            func(r *http.Request) (*accessmanager.MiddlewareAuthedUserResponse, error)
	middlewareJWTRequiredFunc                  func(r *http.Request) (*accessmanager.MiddlewareAuthedUserResponse, error)
	middlewareValidAPITokenRequiredFunc        func(r *http.Request) (*accessmanager.MiddlewareAuthedUserResponse, error)
	middlewareRateLimitOrActiveJWTRequiredFunc func(r *http.Request) (*accessmanager.MiddlewareAuthedUserResponse, error)
	refreshTokenFunc                           func(ctx context.Context, r *accessmanager.RefreshTokenRequest) (*accessmanager.RefreshTokenResponse, error)
}

func (m *mockAccessManagerService) MiddlewareAdminJWTRequired(r *http.Request) (*accessmanager.MiddlewareAuthedUserResponse, error) {
	if m.middlewareAdminJWTRequiredFunc != nil {
		return m.middlewareAdminJWTRequiredFunc(r)
	}
	return nil, nil
}

func (m *mockAccessManagerService) MiddlewareAdminAPITokenRequired(r *http.Request) (*accessmanager.MiddlewareAuthedUserResponse, error) {
	if m.middlewareAdminAPITokenRequiredFunc != nil {
		return m.middlewareAdminAPITokenRequiredFunc(r)
	}
	return nil, nil
}

func (m *mockAccessManagerService) MiddlewareActiveJWTRequired(r *http.Request) (*accessmanager.MiddlewareAuthedUserResponse, error) {
	if m.middlewareActiveJWTRequiredFunc != nil {
		return m.middlewareActiveJWTRequiredFunc(r)
	}
	return nil, nil
}

func (m *mockAccessManagerService) MiddlewareJWTRequired(r *http.Request) (*accessmanager.MiddlewareAuthedUserResponse, error) {
	if m.middlewareJWTRequiredFunc != nil {
		return m.middlewareJWTRequiredFunc(r)
	}
	return nil, nil
}

func (m *mockAccessManagerService) MiddlewareValidAPITokenRequired(r *http.Request) (*accessmanager.MiddlewareAuthedUserResponse, error) {
	if m.middlewareValidAPITokenRequiredFunc != nil {
		return m.middlewareValidAPITokenRequiredFunc(r)
	}
	return nil, nil
}

func (m *mockAccessManagerService) MiddlewareRateLimitOrActiveJWTRequired(r *http.Request) (*accessmanager.MiddlewareAuthedUserResponse, error) {
	if m.middlewareRateLimitOrActiveJWTRequiredFunc != nil {
		return m.middlewareRateLimitOrActiveJWTRequiredFunc(r)
	}
	return nil, nil
}

func (m *mockAccessManagerService) RefreshToken(ctx context.Context, r *accessmanager.RefreshTokenRequest) (*accessmanager.RefreshTokenResponse, error) {
	if m.refreshTokenFunc != nil {
		return m.refreshTokenFunc(ctx, r)
	}
	return nil, nil
}

// helper to build a mock authenticated user response with a minimal user object
func mockAuthedResp(userID, status string, roles []string) *accessmanager.MiddlewareAuthedUserResponse {
	return &accessmanager.MiddlewareAuthedUserResponse{
		Authenticated: true,
		UserID:        userID,
		User: &userv2.UniversalUser{
			ID:     userID,
			Email:  userID + "@example.com",
			Status: status,
			Roles:  roles,
		},
	}
}

func mockPublicResp(userID string) *accessmanager.MiddlewareAuthedUserResponse {
	response := mockAuthedResp(userID, userv2.AccountStatusKeyActive, []string{userv2.UserRoleUser})
	response.Authenticated = false
	return response
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

// responseHasCookieValue reports whether a response included the expected cookie value.
func responseHasCookieValue(cookies []*http.Cookie, name, value string) bool {
	for _, cookie := range cookies {
		if cookie.Name == name && cookie.Value == value {
			return true
		}
	}

	return false
}

// responseHasCookieRemoval reports whether a response included an expired cookie
// marker for the supplied name.
func responseHasCookieRemoval(cookies []*http.Cookie, name string) bool {
	for _, cookie := range cookies {
		if cookie.Name == name && cookie.Value == "" && cookie.MaxAge < 0 {
			return true
		}
	}

	return false
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
		middlewareValidAPITokenRequiredFunc: func(r *http.Request) (*accessmanager.MiddlewareAuthedUserResponse, error) {
			return mockAuthedResp(userID, userv2.AccountStatusKeyActive, []string{userv2.UserRoleUser}), nil
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
		middlewareValidAPITokenRequiredFunc: func(r *http.Request) (*accessmanager.MiddlewareAuthedUserResponse, error) {
			return nil, errors.New("invalid token")
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
		middlewareAdminJWTRequiredFunc: func(r *http.Request) (*accessmanager.MiddlewareAuthedUserResponse, error) {
			return mockAuthedResp(userID, userv2.AccountStatusKeyActive, []string{userv2.UserRoleAdmin}), nil
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
		middlewareAdminJWTRequiredFunc: func(r *http.Request) (*accessmanager.MiddlewareAuthedUserResponse, error) {
			return nil, errors.New("unauthorized")
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
		middlewareActiveJWTRequiredFunc: func(r *http.Request) (*accessmanager.MiddlewareAuthedUserResponse, error) {
			return mockAuthedResp(userID, userv2.AccountStatusKeyActive, []string{userv2.UserRoleUser}), nil
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
		middlewareJWTRequiredFunc: func(r *http.Request) (*accessmanager.MiddlewareAuthedUserResponse, error) {
			return mockAuthedResp(userID, userv2.AccountStatusKeyActive, []string{userv2.UserRoleUser}), nil
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
		middlewareJWTRequiredFunc: func(r *http.Request) (*accessmanager.MiddlewareAuthedUserResponse, error) {
			callCount++
			// First call fails (expired token), second call succeeds
			if callCount == 1 {
				return nil, errors.New("token expired")
			}
			return mockAuthedResp(userID, userv2.AccountStatusKeyActive, []string{userv2.UserRoleUser}), nil
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

	if !responseHasCookieValue(w.Result().Cookies(), "test_auth", "new-access-token") {
		t.Error("Expected refreshed access cookie to be set after successful retry validation")
	}
	if !responseHasCookieValue(w.Result().Cookies(), "test_refresh", "new-refresh-token") {
		t.Error("Expected refreshed refresh cookie to be set after successful retry validation")
	}
}

func TestJWTRequired_RefreshTokenFailure(t *testing.T) {
	mockService := &mockAccessManagerService{
		middlewareJWTRequiredFunc: func(r *http.Request) (*accessmanager.MiddlewareAuthedUserResponse, error) {
			return nil, errors.New("token expired")
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

// TestJWTRequired_RefreshRetryValidationFailureDoesNotSetNewCookies verifies refreshed cookies are only committed after retry validation succeeds.
func TestJWTRequired_RefreshRetryValidationFailureDoesNotSetNewCookies(t *testing.T) {
	callCount := 0
	refreshCallCount := 0
	mockService := &mockAccessManagerService{
		middlewareJWTRequiredFunc: func(r *http.Request) (*accessmanager.MiddlewareAuthedUserResponse, error) {
			callCount++
			return nil, errors.New("token invalid")
		},
		refreshTokenFunc: func(ctx context.Context, r *accessmanager.RefreshTokenRequest) (*accessmanager.RefreshTokenResponse, error) {
			refreshCallCount++
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

	if w.Code == http.StatusOK {
		t.Error("Expected non-200 status code when retry validation rejects refreshed token")
	}
	if callCount != 2 {
		t.Errorf("Expected middleware function to be called twice (initial + retry), got %d", callCount)
	}
	if refreshCallCount != 1 {
		t.Errorf("Expected refresh token function to be called once, got %d", refreshCallCount)
	}
	if responseHasCookieValue(w.Result().Cookies(), "test_auth", "new-access-token") {
		t.Error("Did not expect refreshed access cookie to be set before retry validation succeeds")
	}
	if responseHasCookieValue(w.Result().Cookies(), "test_refresh", "new-refresh-token") {
		t.Error("Did not expect refreshed refresh cookie to be set before retry validation succeeds")
	}
}

func TestActiveValidApiTokenOrJWTRequired_APITokenPresent(t *testing.T) {
	userID := "api-user-123"
	mockService := &mockAccessManagerService{
		middlewareValidAPITokenRequiredFunc: func(r *http.Request) (*accessmanager.MiddlewareAuthedUserResponse, error) {
			return mockAuthedResp(userID, userv2.AccountStatusKeyActive, []string{userv2.UserRoleUser}), nil
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
		middlewareActiveJWTRequiredFunc: func(r *http.Request) (*accessmanager.MiddlewareAuthedUserResponse, error) {
			return mockAuthedResp(userID, userv2.AccountStatusKeyActive, []string{userv2.UserRoleUser}), nil
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
		middlewareValidAPITokenRequiredFunc: func(r *http.Request) (*accessmanager.MiddlewareAuthedUserResponse, error) {
			return mockAuthedResp(apiTokenUserID, userv2.AccountStatusKeyActive, []string{userv2.UserRoleUser}), nil
		},
		middlewareJWTRequiredFunc: func(r *http.Request) (*accessmanager.MiddlewareAuthedUserResponse, error) {
			return mockAuthedResp(jwtUserID, userv2.AccountStatusKeyActive, []string{userv2.UserRoleUser}), nil
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
		middlewareAdminAPITokenRequiredFunc: func(r *http.Request) (*accessmanager.MiddlewareAuthedUserResponse, error) {
			return mockAuthedResp(userID, userv2.AccountStatusKeyActive, []string{userv2.UserRoleAdmin}), nil
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
		middlewareAdminJWTRequiredFunc: func(r *http.Request) (*accessmanager.MiddlewareAuthedUserResponse, error) {
			return mockAuthedResp(userID, userv2.AccountStatusKeyActive, []string{userv2.UserRoleAdmin}), nil
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
		middlewareRateLimitOrActiveJWTRequiredFunc: func(r *http.Request) (*accessmanager.MiddlewareAuthedUserResponse, error) {
			return mockPublicResp(userID), nil
		},
	}

	middleware := createTestMiddleware(mockService)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if accessmanagerhelpers.AcquireAuthenticatedFrom(r.Context()) {
			t.Error("public fallback should transmit unauthenticated state")
		}
		w.WriteHeader(http.StatusOK)
	})
	wrappedHandler := middleware.RateLimitOrActiveJWTRequired(handler)

	req := httptest.NewRequest("GET", "/public", nil)
	w := httptest.NewRecorder()

	wrappedHandler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status code %d, got %d", http.StatusOK, w.Code)
	}
}

func TestRateLimitOrActiveJWTRequired_EmptyCookiesFallsBackToPublicFlowAndClearsCookies(t *testing.T) {
	userID := "rate-limited-user"
	callCount := 0
	mockService := &mockAccessManagerService{
		middlewareRateLimitOrActiveJWTRequiredFunc: func(r *http.Request) (*accessmanager.MiddlewareAuthedUserResponse, error) {
			callCount++
			if got := r.Header.Get("Authorization"); got != "" {
				t.Errorf("Expected empty cookies to be removed from Authorization fallback, got %q", got)
			}
			return mockPublicResp(userID), nil
		},
	}

	middleware := createTestMiddleware(mockService)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if accessmanagerhelpers.AcquireAuthenticatedFrom(r.Context()) {
			t.Error("empty-cookie fallback should transmit unauthenticated state")
		}
		w.WriteHeader(http.StatusOK)
	})
	wrappedHandler := middleware.RateLimitOrActiveJWTRequired(handler)

	req := httptest.NewRequest("GET", "/public", nil)
	req.AddCookie(&http.Cookie{Name: "test_auth", Value: ""})
	req.AddCookie(&http.Cookie{Name: "test_refresh", Value: ""})
	w := httptest.NewRecorder()

	wrappedHandler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status code %d, got %d", http.StatusOK, w.Code)
	}
	if callCount != 1 {
		t.Errorf("Expected public flow to be called once, got %d", callCount)
	}
	if !responseHasCookieRemoval(w.Result().Cookies(), "test_auth") {
		t.Error("Expected empty access cookie to be cleared")
	}
	if !responseHasCookieRemoval(w.Result().Cookies(), "test_refresh") {
		t.Error("Expected empty refresh cookie to be cleared")
	}
}

func TestRateLimitOrActiveJWTRequired_MissingRefreshCookieFallsBackToPublicFlowAndClearsCookies(t *testing.T) {
	userID := "rate-limited-user"
	callCount := 0
	mockService := &mockAccessManagerService{
		middlewareRateLimitOrActiveJWTRequiredFunc: func(r *http.Request) (*accessmanager.MiddlewareAuthedUserResponse, error) {
			callCount++
			if got := r.Header.Get("Authorization"); got != "" {
				t.Errorf("Expected missing refresh fallback to remove Authorization, got %q", got)
			}
			return mockPublicResp(userID), nil
		},
	}

	middleware := createTestMiddleware(mockService)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if accessmanagerhelpers.AcquireAuthenticatedFrom(r.Context()) {
			t.Error("missing-refresh fallback should transmit unauthenticated state")
		}
		w.WriteHeader(http.StatusOK)
	})
	wrappedHandler := middleware.RateLimitOrActiveJWTRequired(handler)

	req := httptest.NewRequest("GET", "/public", nil)
	req.AddCookie(&http.Cookie{Name: "test_auth", Value: "orphaned-access-token"})
	w := httptest.NewRecorder()

	wrappedHandler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status code %d, got %d", http.StatusOK, w.Code)
	}
	if callCount != 1 {
		t.Errorf("Expected public flow to be called once, got %d", callCount)
	}
	if !responseHasCookieRemoval(w.Result().Cookies(), "test_auth") {
		t.Error("Expected orphaned access cookie to be cleared")
	}
	if !responseHasCookieRemoval(w.Result().Cookies(), "test_refresh") {
		t.Error("Expected refresh cookie deletion marker to be set")
	}
}

func TestRateLimitOrActiveJWTRequired_MissingAuthCookieFallsBackToPublicFlowAndClearsCookies(t *testing.T) {
	userID := "rate-limited-user"
	callCount := 0
	mockService := &mockAccessManagerService{
		middlewareRateLimitOrActiveJWTRequiredFunc: func(r *http.Request) (*accessmanager.MiddlewareAuthedUserResponse, error) {
			callCount++
			if got := r.Header.Get("Authorization"); got != "" {
				t.Errorf("Expected missing auth fallback to remove Authorization, got %q", got)
			}
			return mockAuthedResp(userID, userv2.AccountStatusKeyActive, []string{userv2.UserRoleUser}), nil
		},
	}

	middleware := createTestMiddleware(mockService)
	handler := createTestHandler()
	wrappedHandler := middleware.RateLimitOrActiveJWTRequired(handler)

	req := httptest.NewRequest("GET", "/public", nil)
	req.AddCookie(&http.Cookie{Name: "test_refresh", Value: "orphaned-refresh-token"})
	w := httptest.NewRecorder()

	wrappedHandler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status code %d, got %d", http.StatusOK, w.Code)
	}
	if callCount != 1 {
		t.Errorf("Expected public flow to be called once, got %d", callCount)
	}
	if !responseHasCookieRemoval(w.Result().Cookies(), "test_auth") {
		t.Error("Expected access cookie deletion marker to be set")
	}
	if !responseHasCookieRemoval(w.Result().Cookies(), "test_refresh") {
		t.Error("Expected orphaned refresh cookie to be cleared")
	}
}

func TestRateLimitOrActiveJWTRequired_FallbackRateLimitErrorReturnsHTTPError(t *testing.T) {
	handlerCalled := false
	mockService := &mockAccessManagerService{
		middlewareRateLimitOrActiveJWTRequiredFunc: func(r *http.Request) (*accessmanager.MiddlewareAuthedUserResponse, error) {
			return nil, ephemeral.ErrRequestorLimitExceeded
		},
	}

	middleware := NewMiddleware(&NewMiddlewareRequest{
		Service:                  mockService,
		ErrorMaps:                []reply.ErrorManifest{ephemeral.EphemeralStoreErrorMap},
		Environment:              "test",
		CookiePrefixAuthToken:    "test_auth",
		CookiePrefixRefreshToken: "test_refresh",
		CookieDomain:             "test.com",
	})
	wrappedHandler := middleware.RateLimitOrActiveJWTRequired(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/public", nil)
	req.AddCookie(&http.Cookie{Name: "test_auth", Value: ""})
	req.AddCookie(&http.Cookie{Name: "test_refresh", Value: ""})
	w := httptest.NewRecorder()

	wrappedHandler.ServeHTTP(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Errorf("Expected status code %d, got %d", http.StatusTooManyRequests, w.Code)
	}
	if handlerCalled {
		t.Error("Did not expect handler to run when anonymous fallback is rate limited")
	}
	if !responseHasCookieRemoval(w.Result().Cookies(), "test_auth") {
		t.Error("Expected rate-limited fallback to clear access cookie")
	}
	if !responseHasCookieRemoval(w.Result().Cookies(), "test_refresh") {
		t.Error("Expected rate-limited fallback to clear refresh cookie")
	}
}

func TestRateLimitOrActiveJWTRequired_WithValidJWT(t *testing.T) {
	userID := "authenticated-user-123"
	mockService := &mockAccessManagerService{
		middlewareRateLimitOrActiveJWTRequiredFunc: func(r *http.Request) (*accessmanager.MiddlewareAuthedUserResponse, error) {
			return mockAuthedResp(userID, userv2.AccountStatusKeyActive, []string{userv2.UserRoleUser}), nil
		},
	}

	middleware := createTestMiddleware(mockService)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !accessmanagerhelpers.AcquireAuthenticatedFrom(r.Context()) {
			t.Error("valid JWT should transmit authenticated state")
		}
		w.WriteHeader(http.StatusOK)
	})
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
		middlewareRateLimitOrActiveJWTRequiredFunc: func(r *http.Request) (*accessmanager.MiddlewareAuthedUserResponse, error) {
			callCount++
			if callCount == 1 {
				return nil, errors.New("token expired")
			}
			return mockAuthedResp(userID, userv2.AccountStatusKeyProvisioned, []string{userv2.UserRoleUser}), nil
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

	if !responseHasCookieValue(w.Result().Cookies(), "test_auth", "new-access-token") {
		t.Error("Expected refreshed access cookie to be set after successful retry validation")
	}
	if !responseHasCookieValue(w.Result().Cookies(), "test_refresh", "new-refresh-token") {
		t.Error("Expected refreshed refresh cookie to be set after successful retry validation")
	}
}

// TestRateLimitOrActiveJWTRequired_RefreshRetryValidationFailureFallsBackWithoutNewCookies verifies RateLimitOrActiveJWTRequired downgrades to the public flow when retry validation rejects refreshed tokens.
func TestRateLimitOrActiveJWTRequired_RefreshRetryValidationFailureFallsBackWithoutNewCookies(t *testing.T) {
	callCount := 0
	refreshCallCount := 0

	mockService := &mockAccessManagerService{
		middlewareRateLimitOrActiveJWTRequiredFunc: func(r *http.Request) (*accessmanager.MiddlewareAuthedUserResponse, error) {
			callCount++
			if r.Header.Get("Authorization") == "" {
				return mockAuthedResp("rate-limited-user", userv2.AccountStatusKeyActive, []string{userv2.UserRoleUser}), nil
			}
			return nil, errors.New("token invalid")
		},
		refreshTokenFunc: func(ctx context.Context, r *accessmanager.RefreshTokenRequest) (*accessmanager.RefreshTokenResponse, error) {
			refreshCallCount++
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
		t.Errorf("Expected status code %d after fallback, got %d", http.StatusOK, w.Code)
	}
	if callCount != 3 {
		t.Errorf("Expected middleware function to be called three times, got %d", callCount)
	}
	if refreshCallCount != 1 {
		t.Errorf("Expected refresh token function to be called once, got %d", refreshCallCount)
	}
	if responseHasCookieValue(w.Result().Cookies(), "test_auth", "new-access-token") {
		t.Error("Did not expect refreshed access cookie to be set before retry validation succeeds")
	}
	if responseHasCookieValue(w.Result().Cookies(), "test_refresh", "new-refresh-token") {
		t.Error("Did not expect refreshed refresh cookie to be set before retry validation succeeds")
	}
	if !responseHasCookieRemoval(w.Result().Cookies(), "test_auth") {
		t.Error("Expected failed refresh validation to clear access cookie")
	}
	if !responseHasCookieRemoval(w.Result().Cookies(), "test_refresh") {
		t.Error("Expected failed refresh validation to clear refresh cookie")
	}
}

// TestRateLimitOrActiveJWTRequired_RefreshTokenFailureFallsBackWithoutNewCookies verifies failed refreshes do not set replacement cookies and continue as public.
func TestRateLimitOrActiveJWTRequired_RefreshTokenFailureFallsBackWithoutNewCookies(t *testing.T) {
	callCount := 0
	refreshCallCount := 0

	mockService := &mockAccessManagerService{
		middlewareRateLimitOrActiveJWTRequiredFunc: func(r *http.Request) (*accessmanager.MiddlewareAuthedUserResponse, error) {
			callCount++
			if r.Header.Get("Authorization") == "" {
				return mockAuthedResp("rate-limited-user", userv2.AccountStatusKeyActive, []string{userv2.UserRoleUser}), nil
			}
			return nil, errors.New("token expired")
		},
		refreshTokenFunc: func(ctx context.Context, r *accessmanager.RefreshTokenRequest) (*accessmanager.RefreshTokenResponse, error) {
			refreshCallCount++
			return nil, errors.New("refresh token invalid")
		},
	}

	middleware := createTestMiddleware(mockService)
	handler := createTestHandler()
	wrappedHandler := middleware.RateLimitOrActiveJWTRequired(handler)

	req := httptest.NewRequest("GET", "/public", nil)
	req.AddCookie(&http.Cookie{Name: "test_auth", Value: "expired-jwt-token"})
	req.AddCookie(&http.Cookie{Name: "test_refresh", Value: "invalid-refresh-token"})
	w := httptest.NewRecorder()

	wrappedHandler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status code %d after fallback, got %d", http.StatusOK, w.Code)
	}
	if callCount != 2 {
		t.Errorf("Expected middleware function to be called twice, got %d", callCount)
	}
	if refreshCallCount != 1 {
		t.Errorf("Expected refresh token function to be called once, got %d", refreshCallCount)
	}
	if responseHasCookieValue(w.Result().Cookies(), "test_auth", "new-access-token") {
		t.Error("Did not expect refreshed access cookie to be set when refresh service fails")
	}
	if responseHasCookieValue(w.Result().Cookies(), "test_refresh", "new-refresh-token") {
		t.Error("Did not expect refreshed refresh cookie to be set when refresh service fails")
	}
	if !responseHasCookieRemoval(w.Result().Cookies(), "test_auth") {
		t.Error("Expected failed refresh to clear access cookie")
	}
	if !responseHasCookieRemoval(w.Result().Cookies(), "test_refresh") {
		t.Error("Expected failed refresh to clear refresh cookie")
	}
}

func TestActiveValidApiTokenOrJWTRequired_EmptyCookiesRemainProtected(t *testing.T) {
	mockService := &mockAccessManagerService{
		middlewareActiveJWTRequiredFunc: func(r *http.Request) (*accessmanager.MiddlewareAuthedUserResponse, error) {
			return nil, errors.New("token invalid")
		},
	}

	middleware := createTestMiddleware(mockService)
	handler := createTestHandler()
	wrappedHandler := middleware.ActiveValidApiTokenOrJWTRequired(handler)

	req := httptest.NewRequest("GET", "/protected", nil)
	req.AddCookie(&http.Cookie{Name: "test_auth", Value: ""})
	req.AddCookie(&http.Cookie{Name: "test_refresh", Value: ""})
	w := httptest.NewRecorder()

	wrappedHandler.ServeHTTP(w, req)

	if w.Code == http.StatusOK {
		t.Error("Expected protected middleware to reject empty auth cookies")
	}
	if !responseHasCookieRemoval(w.Result().Cookies(), "test_auth") {
		t.Error("Expected protected middleware to clear empty access cookie")
	}
	if !responseHasCookieRemoval(w.Result().Cookies(), "test_refresh") {
		t.Error("Expected protected middleware to clear empty refresh cookie")
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
