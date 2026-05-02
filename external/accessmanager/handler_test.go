package accessmanager_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ooaklee/reply"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ooaklee/ghatd/external/accessmanager"
	"github.com/ooaklee/ghatd/external/apitoken"
	"github.com/ooaklee/ghatd/external/auth"
	userv2 "github.com/ooaklee/ghatd/external/user/v2"
	"github.com/ooaklee/ghatd/external/validator"
)

// mockAccessmanagerService is a configurable mock of accessmanager.AccessmanagerService.
// Tests assign the relevant *Func field to provide the desired behaviour for the
// method under test; unused methods return zero-values.
type mockAccessmanagerService struct {
	deleteAuthFunc                                func(ctx context.Context, tokenID string) (int64, error)
	tokenAsStringValidatorFunc                    func(ctx context.Context, r *accessmanager.TokenAsStringValidatorRequest) (*accessmanager.TokenAsStringValidatorResponse, error)
	createUserFunc                                func(ctx context.Context, r *accessmanager.CreateUserRequest) (*accessmanager.CreateUserResponse, error)
	validateEmailVerificationCodeFunc             func(ctx context.Context, r *accessmanager.ValidateEmailVerificationCodeRequest) (*accessmanager.ValidateEmailVerificationCodeResponse, error)
	createInitalLoginOrVerificationTokenEmailFunc func(ctx context.Context, r *accessmanager.CreateInitalLoginOrVerificationTokenEmailRequest) error
	loginUserFunc                                 func(ctx context.Context, r *accessmanager.LoginUserRequest) (*accessmanager.LoginUserResponse, error)
	refreshTokenFunc                              func(ctx context.Context, r *accessmanager.RefreshTokenRequest) (*accessmanager.RefreshTokenResponse, error)
	logoutUserFunc                                func(ctx context.Context, r *http.Request) error
	createUserAPITokenFunc                        func(ctx context.Context, r *accessmanager.CreateUserAPITokenRequest) (*accessmanager.CreateUserAPITokenResponse, error)
	deleteUserAPITokenFunc                        func(ctx context.Context, r *accessmanager.DeleteUserAPITokenRequest) error
	updateUserAPITokenStatusFunc                  func(ctx context.Context, r *accessmanager.UserAPITokenStatusRequest) error
	getSpecificUserAPITokensFunc                  func(ctx context.Context, r *accessmanager.GetSpecificUserAPITokensRequest) (*accessmanager.GetSpecificUserAPITokensResponse, error)
	getUserAPITokenThresholdFunc                  func(ctx context.Context, r *accessmanager.GetUserAPITokenThresholdRequest) (*accessmanager.GetUserAPITokenThresholdResponse, error)
	oauthLoginFunc                                func(ctx context.Context, r *accessmanager.OauthLoginRequest) (*accessmanager.OauthLoginResponse, error)
	oauthCallbackFunc                             func(ctx context.Context, r *accessmanager.OauthCallbackRequest) (*accessmanager.OauthCallbackResponse, error)
	removeRefreshTokenWithCookieValueFunc         func(ctx context.Context, refreshTokenCookieValue string) (auth.UserModel, string, error)
	logoutUserOthersFunc                          func(ctx context.Context, r *accessmanager.LogoutUserOthersRequest) error
	updateUserEmailFunc                           func(ctx context.Context, r *accessmanager.UpdateUserEmailRequest) (bool, error)
}

func (m *mockAccessmanagerService) DeleteAuth(ctx context.Context, tokenID string) (int64, error) {
	if m.deleteAuthFunc != nil {
		return m.deleteAuthFunc(ctx, tokenID)
	}
	return 0, nil
}

func (m *mockAccessmanagerService) TokenAsStringValidator(ctx context.Context, r *accessmanager.TokenAsStringValidatorRequest) (*accessmanager.TokenAsStringValidatorResponse, error) {
	if m.tokenAsStringValidatorFunc != nil {
		return m.tokenAsStringValidatorFunc(ctx, r)
	}
	return nil, nil
}

func (m *mockAccessmanagerService) CreateUser(ctx context.Context, r *accessmanager.CreateUserRequest) (*accessmanager.CreateUserResponse, error) {
	if m.createUserFunc != nil {
		return m.createUserFunc(ctx, r)
	}
	return nil, nil
}

func (m *mockAccessmanagerService) ValidateEmailVerificationCode(ctx context.Context, r *accessmanager.ValidateEmailVerificationCodeRequest) (*accessmanager.ValidateEmailVerificationCodeResponse, error) {
	if m.validateEmailVerificationCodeFunc != nil {
		return m.validateEmailVerificationCodeFunc(ctx, r)
	}
	return nil, nil
}

func (m *mockAccessmanagerService) CreateInitalLoginOrVerificationTokenEmail(ctx context.Context, r *accessmanager.CreateInitalLoginOrVerificationTokenEmailRequest) error {
	if m.createInitalLoginOrVerificationTokenEmailFunc != nil {
		return m.createInitalLoginOrVerificationTokenEmailFunc(ctx, r)
	}
	return nil
}

func (m *mockAccessmanagerService) LoginUser(ctx context.Context, r *accessmanager.LoginUserRequest) (*accessmanager.LoginUserResponse, error) {
	if m.loginUserFunc != nil {
		return m.loginUserFunc(ctx, r)
	}
	return nil, nil
}

func (m *mockAccessmanagerService) RefreshToken(ctx context.Context, r *accessmanager.RefreshTokenRequest) (*accessmanager.RefreshTokenResponse, error) {
	if m.refreshTokenFunc != nil {
		return m.refreshTokenFunc(ctx, r)
	}
	return nil, nil
}

func (m *mockAccessmanagerService) LogoutUser(ctx context.Context, r *http.Request) error {
	if m.logoutUserFunc != nil {
		return m.logoutUserFunc(ctx, r)
	}
	return nil
}

func (m *mockAccessmanagerService) CreateUserAPIToken(ctx context.Context, r *accessmanager.CreateUserAPITokenRequest) (*accessmanager.CreateUserAPITokenResponse, error) {
	if m.createUserAPITokenFunc != nil {
		return m.createUserAPITokenFunc(ctx, r)
	}
	return nil, nil
}

func (m *mockAccessmanagerService) DeleteUserAPIToken(ctx context.Context, r *accessmanager.DeleteUserAPITokenRequest) error {
	if m.deleteUserAPITokenFunc != nil {
		return m.deleteUserAPITokenFunc(ctx, r)
	}
	return nil
}

func (m *mockAccessmanagerService) UpdateUserAPITokenStatus(ctx context.Context, r *accessmanager.UserAPITokenStatusRequest) error {
	if m.updateUserAPITokenStatusFunc != nil {
		return m.updateUserAPITokenStatusFunc(ctx, r)
	}
	return nil
}

func (m *mockAccessmanagerService) GetSpecificUserAPITokens(ctx context.Context, r *accessmanager.GetSpecificUserAPITokensRequest) (*accessmanager.GetSpecificUserAPITokensResponse, error) {
	if m.getSpecificUserAPITokensFunc != nil {
		return m.getSpecificUserAPITokensFunc(ctx, r)
	}
	return nil, nil
}

func (m *mockAccessmanagerService) GetUserAPITokenThreshold(ctx context.Context, r *accessmanager.GetUserAPITokenThresholdRequest) (*accessmanager.GetUserAPITokenThresholdResponse, error) {
	if m.getUserAPITokenThresholdFunc != nil {
		return m.getUserAPITokenThresholdFunc(ctx, r)
	}
	return nil, nil
}

func (m *mockAccessmanagerService) OauthLogin(ctx context.Context, r *accessmanager.OauthLoginRequest) (*accessmanager.OauthLoginResponse, error) {
	if m.oauthLoginFunc != nil {
		return m.oauthLoginFunc(ctx, r)
	}
	return nil, nil
}

func (m *mockAccessmanagerService) OauthCallback(ctx context.Context, r *accessmanager.OauthCallbackRequest) (*accessmanager.OauthCallbackResponse, error) {
	if m.oauthCallbackFunc != nil {
		return m.oauthCallbackFunc(ctx, r)
	}
	return nil, nil
}

func (m *mockAccessmanagerService) RemoveRefreshTokenWithCookieValue(ctx context.Context, refreshTokenCookieValue string) (auth.UserModel, string, error) {
	if m.removeRefreshTokenWithCookieValueFunc != nil {
		return m.removeRefreshTokenWithCookieValueFunc(ctx, refreshTokenCookieValue)
	}
	return nil, "", nil
}

func (m *mockAccessmanagerService) LogoutUserOthers(ctx context.Context, r *accessmanager.LogoutUserOthersRequest) error {
	if m.logoutUserOthersFunc != nil {
		return m.logoutUserOthersFunc(ctx, r)
	}
	return nil
}

func (m *mockAccessmanagerService) UpdateUserEmail(ctx context.Context, r *accessmanager.UpdateUserEmailRequest) (bool, error) {
	if m.updateUserEmailFunc != nil {
		return m.updateUserEmailFunc(ctx, r)
	}
	return false, nil
}

// Compile-time guard: mock satisfies the production service interface.
var _ accessmanager.AccessmanagerService = (*mockAccessmanagerService)(nil)

// newTestHandler builds a handler wired to the supplied mock service.
func newTestHandler(svc accessmanager.AccessmanagerService) *accessmanager.Handler {
	return accessmanager.NewHandler(&accessmanager.NewHandlerRequest{
		Service:                  svc,
		Validator:                validator.NewValidator(),
		ErrorMaps:                []reply.ErrorManifest{accessmanager.AccessmanagerErrorMap},
		Environment:              "local",
		CookiePrefixAuthToken:    testCookieAuth,
		CookiePrefixRefreshToken: testCookieRefresh,
		CookieDomain:             "test.local",
	})
}

func TestHandler_ValidateEmailVerificationCode(t *testing.T) {
	t.Parallel()

	successResponse := &accessmanager.ValidateEmailVerificationCodeResponse{
		AccessToken:           "access-token-value",
		RefreshToken:          "refresh-token-value",
		AccessTokenExpiresAt:  1700000000,
		RefreshTokenExpiresAt: 1700003600,
	}

	tests := []struct {
		name             string
		query            string
		mockResponse     *accessmanager.ValidateEmailVerificationCodeResponse
		mockErr          error
		expectStatus     int
		expectLocation   string
		expectAuthCookie bool
	}{
		{
			name:             "Success - returns token response when next_step query param is absent (regression)",
			query:            "?t=" + testValidToken128,
			mockResponse:     successResponse,
			expectStatus:     http.StatusOK,
			expectAuthCookie: true,
		},
		{
			name:             "Success - redirects when next_step query param is provided",
			query:            "?t=" + testValidToken128 + "&next_step=/dashboard",
			mockResponse:     successResponse,
			expectStatus:     http.StatusTemporaryRedirect,
			expectLocation:   "/dashboard",
			expectAuthCookie: true,
		},
		{
			name:             "Success - empty next_step query param does not trigger redirect",
			query:            "?t=" + testValidToken128 + "&next_step=",
			mockResponse:     successResponse,
			expectStatus:     http.StatusOK,
			expectAuthCookie: true,
		},
		{
			name:         "Failure - invalid (short) token returns mapping error",
			query:        "?t=" + testShortToken,
			expectStatus: http.StatusBadRequest,
		},
		{
			name:         "Failure - service returns error",
			query:        "?t=" + testValidToken128,
			mockErr:      errors.New(accessmanager.ErrKeyInvalidVerificationToken),
			expectStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			svc := &mockAccessmanagerService{
				validateEmailVerificationCodeFunc: func(ctx context.Context, r *accessmanager.ValidateEmailVerificationCodeRequest) (*accessmanager.ValidateEmailVerificationCodeResponse, error) {
					return tt.mockResponse, tt.mockErr
				},
			}

			h := newTestHandler(svc)

			req := httptest.NewRequest(http.MethodGet, "/v1/ams/verify"+tt.query, nil)
			rec := httptest.NewRecorder()

			// Should never panic - even when next_step query param is absent.
			require.NotPanics(t, func() {
				h.ValidateEmailVerificationCode(rec, req)
			})

			assert.Equal(t, tt.expectStatus, rec.Code)

			if tt.expectLocation != "" {
				assert.Equal(t, tt.expectLocation, rec.Header().Get("Location"))
			}

			if tt.expectAuthCookie {
				cookies := rec.Result().Cookies()
				assert.True(t, hasCookie(cookies, testCookieAuth), "expected auth cookie to be set")
				assert.True(t, hasCookie(cookies, testCookieRefresh), "expected refresh cookie to be set")
			}
		})
	}
}

func TestHandler_LoginUser(t *testing.T) {
	t.Parallel()

	successResponse := &accessmanager.LoginUserResponse{
		AccessToken:           "access-token-value",
		RefreshToken:          "refresh-token-value",
		AccessTokenExpiresAt:  1700000000,
		RefreshTokenExpiresAt: 1700003600,
	}

	tests := []struct {
		name             string
		query            string
		mockResponse     *accessmanager.LoginUserResponse
		mockErr          error
		expectStatus     int
		expectLocation   string
		expectAuthCookie bool
	}{
		{
			name:             "Success - token response when next_step absent",
			query:            "?t=" + testValidToken128,
			mockResponse:     successResponse,
			expectStatus:     http.StatusOK,
			expectAuthCookie: true,
		},
		{
			name:             "Success - redirects when next_step provided",
			query:            "?t=" + testValidToken128 + "&next_step=/home",
			mockResponse:     successResponse,
			expectStatus:     http.StatusTemporaryRedirect,
			expectLocation:   "/home",
			expectAuthCookie: true,
		},
		{
			name:         "Failure - invalid token mapping error",
			query:        "?t=" + testShortToken,
			expectStatus: http.StatusBadRequest,
		},
		{
			name:         "Failure - service returns error",
			query:        "?t=" + testValidToken128,
			mockErr:      errors.New(accessmanager.ErrKeyInvalidVerificationToken),
			expectStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			svc := &mockAccessmanagerService{
				loginUserFunc: func(ctx context.Context, r *accessmanager.LoginUserRequest) (*accessmanager.LoginUserResponse, error) {
					return tt.mockResponse, tt.mockErr
				},
			}

			h := newTestHandler(svc)

			req := httptest.NewRequest(http.MethodGet, "/v1/ams/login"+tt.query, nil)
			rec := httptest.NewRecorder()

			require.NotPanics(t, func() {
				h.LoginUser(rec, req)
			})

			assert.Equal(t, tt.expectStatus, rec.Code)

			if tt.expectLocation != "" {
				assert.Equal(t, tt.expectLocation, rec.Header().Get("Location"))
			}

			if tt.expectAuthCookie {
				cookies := rec.Result().Cookies()
				assert.True(t, hasCookie(cookies, testCookieAuth), "expected auth cookie to be set")
				assert.True(t, hasCookie(cookies, testCookieRefresh), "expected refresh cookie to be set")
			}
		})
	}
}

func TestHandler_CreateUser(t *testing.T) {
	t.Parallel()

	createdUser := &userv2.UniversalUser{
		ID:    "user-id-1",
		Email: "ada@example.com",
	}

	tests := []struct {
		name         string
		body         string
		mockResponse *accessmanager.CreateUserResponse
		mockErr      error
		expectStatus int
	}{
		{
			name:         "Success - returns 201 with created user",
			body:         `{"first_name":"Ada","last_name":"Lovelace","email":"ada@example.com"}`,
			mockResponse: &accessmanager.CreateUserResponse{User: createdUser},
			expectStatus: http.StatusCreated,
		},
		{
			name:         "Failure - bad body returns 400",
			body:         `{"first_name":"A"}`,
			expectStatus: http.StatusBadRequest,
		},
		{
			name:         "Failure - service returns conflicting state",
			body:         `{"first_name":"Ada","last_name":"Lovelace","email":"ada@example.com"}`,
			mockErr:      errors.New(accessmanager.ErrKeyConflictingUserState),
			expectStatus: http.StatusConflict,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			svc := &mockAccessmanagerService{
				createUserFunc: func(ctx context.Context, r *accessmanager.CreateUserRequest) (*accessmanager.CreateUserResponse, error) {
					return tt.mockResponse, tt.mockErr
				},
			}

			h := newTestHandler(svc)

			req := httptest.NewRequest(http.MethodPost, "/v1/ams/users", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()

			require.NotPanics(t, func() {
				h.CreateUser(rec, req)
			})

			assert.Equal(t, tt.expectStatus, rec.Code)
		})
	}
}

func TestHandler_CreateInitalLoginOrVerificationTokenEmail(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		body         string
		mockErr      error
		expectStatus int
	}{
		{
			name:         "Success - returns 202 when service succeeds",
			body:         `{"email":"` + testValidEmail + `"}`,
			expectStatus: http.StatusAccepted,
		},
		{
			name:         "Success - returns 202 even when service fails (privacy preserving)",
			body:         `{"email":"` + testValidEmail + `"}`,
			mockErr:      errors.New("some internal error"),
			expectStatus: http.StatusAccepted,
		},
		{
			name:         "Failure - bad body returns mapped error status",
			body:         `not-json`,
			expectStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			svc := &mockAccessmanagerService{
				createInitalLoginOrVerificationTokenEmailFunc: func(ctx context.Context, r *accessmanager.CreateInitalLoginOrVerificationTokenEmailRequest) error {
					return tt.mockErr
				},
			}

			h := newTestHandler(svc)

			req := httptest.NewRequest(http.MethodPost, "/v1/ams/login-or-verify", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()

			require.NotPanics(t, func() {
				h.CreateInitalLoginOrVerificationTokenEmail(rec, req)
			})

			assert.Equal(t, tt.expectStatus, rec.Code)
		})
	}
}

func TestHandler_RefreshToken(t *testing.T) {
	t.Parallel()

	successResponse := &accessmanager.RefreshTokenResponse{
		AccessToken:           "new-access-token",
		RefreshToken:          "new-refresh-token",
		AccessTokenExpiresAt:  1700000000,
		RefreshTokenExpiresAt: 1700003600,
	}

	tests := []struct {
		name             string
		refreshCookie    string
		body             string
		mockResponse     *accessmanager.RefreshTokenResponse
		mockErr          error
		expectStatus     int
		expectAuthCookie bool
	}{
		{
			name:             "Success - returns refreshed token pair from cookie",
			refreshCookie:    testValidToken128,
			mockResponse:     successResponse,
			expectStatus:     http.StatusOK,
			expectAuthCookie: true,
		},
		{
			name:         "Failure - missing refresh cookie and bad body returns 400",
			body:         `not-json`,
			expectStatus: http.StatusBadRequest,
		},
		{
			name:          "Failure - service error wipes auth cookies and returns mapped status",
			refreshCookie: testValidToken128,
			mockErr:       errors.New(accessmanager.ErrKeyEmptyRefreshToken),
			expectStatus:  http.StatusUnauthorized,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			svc := &mockAccessmanagerService{
				refreshTokenFunc: func(ctx context.Context, r *accessmanager.RefreshTokenRequest) (*accessmanager.RefreshTokenResponse, error) {
					return tt.mockResponse, tt.mockErr
				},
			}

			h := newTestHandler(svc)

			body := strings.NewReader(tt.body)
			req := httptest.NewRequest(http.MethodPost, "/v1/ams/refresh", body)
			req.Header.Set("Content-Type", "application/json")

			if tt.refreshCookie != "" {
				req.AddCookie(&http.Cookie{Name: testCookieRefresh, Value: tt.refreshCookie})
			}

			rec := httptest.NewRecorder()

			require.NotPanics(t, func() {
				h.RefreshToken(rec, req)
			})

			assert.Equal(t, tt.expectStatus, rec.Code)

			if tt.expectAuthCookie {
				cookies := rec.Result().Cookies()
				assert.True(t, hasCookie(cookies, testCookieAuth), "expected auth cookie to be set")
				assert.True(t, hasCookie(cookies, testCookieRefresh), "expected refresh cookie to be set")
			}
		})
	}
}

// hasCookie reports whether the cookie set with the given name exists in the slice.
func hasCookie(cookies []*http.Cookie, name string) bool {
	for _, c := range cookies {
		if c.Name == name {
			return true
		}
	}
	return false
}

// Ensure unused imports are referenced (apitoken is reserved for future tests of
// API-token related handlers). Keeping this here avoids `imported and not used`
// errors while documenting the intent for follow-up tests.
var _ = apitoken.UserAPIToken{}
