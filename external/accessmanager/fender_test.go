package accessmanager_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ooaklee/ghatd/external/accessmanager"
	"github.com/ooaklee/ghatd/external/validator"
)

const (
	testShortToken    = "too-short"
	testValidEmail    = "user@example.com"
	testCookieAuth    = "test_auth_token"
	testCookieRefresh = "test_refresh_token"
)

// testValidToken128 is a string exactly 128 characters long, satisfying the
// `validate:"min=128"` rule on token request fields.
var testValidToken128 = strings.Repeat("a", 128)

// newTestValidator returns a real validator used by mappers under test.
func newTestValidator() accessmanager.AccessmanagerValidator {
	return validator.NewValidator()
}

func TestMapRequestToValidateEmailVerificationCodeRequest(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		query       string
		expectError bool
		expectToken string
	}{
		{
			name:        "Success - valid token in query string",
			query:       "?t=" + testValidToken128,
			expectError: false,
			expectToken: testValidToken128,
		},
		{
			name:        "Success - valid token alongside next_step query param",
			query:       "?t=" + testValidToken128 + "&next_step=/dashboard",
			expectError: false,
			expectToken: testValidToken128,
		},
		{
			name:        "Failure - token below min length",
			query:       "?t=" + testShortToken,
			expectError: true,
		},
		{
			name:        "Failure - token query param missing entirely",
			query:       "",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest(http.MethodGet, "/v1/ams/verify"+tt.query, nil)

			parsed, err := accessmanager.MapRequestToValidateEmailVerificationCodeRequest(req, newTestValidator())

			if tt.expectError {
				require.Error(t, err)
				assert.Equal(t, accessmanager.ErrKeyInvalidVerificationToken, err.Error())
				assert.Nil(t, parsed)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, parsed)
			assert.Equal(t, tt.expectToken, parsed.Token)
		})
	}
}

func TestMapRequestToLoginUserRequest(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		query       string
		expectError bool
		expectToken string
	}{
		{
			name:        "Success - valid login token in query string",
			query:       "?t=" + testValidToken128,
			expectError: false,
			expectToken: testValidToken128,
		},
		{
			name:        "Failure - token below min length",
			query:       "?t=" + testShortToken,
			expectError: true,
		},
		{
			name:        "Failure - token query param missing entirely",
			query:       "",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest(http.MethodGet, "/v1/ams/login"+tt.query, nil)

			parsed, err := accessmanager.MapRequestToLoginUserRequest(req, newTestValidator())

			if tt.expectError {
				require.Error(t, err)
				assert.Equal(t, accessmanager.ErrKeyInvalidVerificationToken, err.Error())
				assert.Nil(t, parsed)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, parsed)
			assert.Equal(t, tt.expectToken, parsed.Token)
		})
	}
}

func TestMapRequestToCreateUserRequest(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		body           string
		expectError    bool
		expectFirst    string
		expectLast     string
		expectEmail    string
		expectMobile   bool
		expectRequest  string
		expectErrorKey string
	}{
		{
			name:          "Success - valid create user body",
			body:          `{"first_name":"Ada","last_name":"Lovelace","email":"ada@example.com","mobile":true,"request_url":"/welcome"}`,
			expectError:   false,
			expectFirst:   "Ada",
			expectLast:    "Lovelace",
			expectEmail:   "ada@example.com",
			expectMobile:  true,
			expectRequest: "/welcome",
		},
		{
			name:           "Failure - first name too short",
			body:           `{"first_name":"A","last_name":"Lovelace","email":"ada@example.com"}`,
			expectError:    true,
			expectErrorKey: accessmanager.ErrKeyInvalidUserBody,
		},
		{
			name:           "Failure - last name too short",
			body:           `{"first_name":"Ada","last_name":"L","email":"ada@example.com"}`,
			expectError:    true,
			expectErrorKey: accessmanager.ErrKeyInvalidUserBody,
		},
		{
			name:           "Failure - malformed json body",
			body:           `{"first_name":`,
			expectError:    true,
			expectErrorKey: accessmanager.ErrKeyInvalidUserBody,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest(http.MethodPost, "/v1/ams/users", bytes.NewBufferString(tt.body))
			req.Header.Set("Content-Type", "application/json")

			parsed, err := accessmanager.MapRequestToCreateUserRequest(req, newTestValidator())

			if tt.expectError {
				require.Error(t, err)
				assert.Equal(t, tt.expectErrorKey, err.Error())
				assert.Nil(t, parsed)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, parsed)
			assert.Equal(t, tt.expectFirst, parsed.FirstName)
			assert.Equal(t, tt.expectLast, parsed.LastName)
			assert.Equal(t, tt.expectEmail, parsed.Email)
			assert.Equal(t, tt.expectMobile, parsed.Mobile)
			assert.Equal(t, tt.expectRequest, parsed.RequestUrl)
		})
	}
}

func TestMapRequestToCreateInitalLoginOrVerificationTokenEmailRequest(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		body          string
		expectError   bool
		expectEmail   string
		expectMobile  bool
		expectDash    bool
		expectRequest string
	}{
		{
			name:        "Success - minimal email body",
			body:        `{"email":"` + testValidEmail + `"}`,
			expectError: false,
			expectEmail: testValidEmail,
		},
		{
			name:          "Success - full body with mobile + dashboard + request_url",
			body:          `{"email":"` + testValidEmail + `","mobile":true,"dashboard":true,"request_url":"/onboarding"}`,
			expectError:   false,
			expectEmail:   testValidEmail,
			expectMobile:  true,
			expectDash:    true,
			expectRequest: "/onboarding",
		},
		{
			name:        "Failure - malformed json body",
			body:        `{"email":`,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest(http.MethodPost, "/v1/ams/login-or-verify", bytes.NewBufferString(tt.body))
			req.Header.Set("Content-Type", "application/json")

			parsed, err := accessmanager.MapRequestToCreateInitalLoginOrVerificationTokenEmailRequest(req, newTestValidator())

			if tt.expectError {
				require.Error(t, err)
				assert.Equal(t, accessmanager.ErrKeyInvalidUserEmail, err.Error())
				assert.Nil(t, parsed)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, parsed)
			assert.Equal(t, tt.expectEmail, parsed.Email)
			assert.Equal(t, tt.expectMobile, parsed.Mobile)
			assert.Equal(t, tt.expectDash, parsed.Dashboard)
			assert.Equal(t, tt.expectRequest, parsed.RequestUrl)
		})
	}
}

func TestMapRequestToRefreshTokenRequest(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		refreshCookie    string
		accessCookie     string
		body             string
		expectError      bool
		expectRefreshTok string
		expectAccessTok  string
		expectErrorKey   string
	}{
		{
			name:             "Success - refresh token from cookie, no access cookie",
			refreshCookie:    testValidToken128,
			expectError:      false,
			expectRefreshTok: testValidToken128,
		},
		{
			name:             "Success - refresh + access tokens from cookies",
			refreshCookie:    testValidToken128,
			accessCookie:     "access-cookie-value",
			expectError:      false,
			expectRefreshTok: testValidToken128,
			expectAccessTok:  "access-cookie-value",
		},
		{
			name:             "Success - refresh token from body when cookie absent",
			body:             `{"refresh_token":"` + testValidToken128 + `"}`,
			expectError:      false,
			expectRefreshTok: testValidToken128,
		},
		{
			name:           "Failure - body refresh token too short and cookie absent",
			body:           `{"refresh_token":"too-short"}`,
			expectError:    true,
			expectErrorKey: accessmanager.ErrKeyInvalidRefreshToken,
		},
		{
			name:           "Failure - cookie refresh token too short fails validation",
			refreshCookie:  testShortToken,
			expectError:    true,
			expectErrorKey: accessmanager.ErrKeyInvalidRefreshToken,
		},
		{
			name:           "Failure - no cookie and unparseable body",
			body:           `not-json`,
			expectError:    true,
			expectErrorKey: accessmanager.ErrKeyInvalidRefreshToken,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var body *bytes.Buffer
			if tt.body != "" {
				body = bytes.NewBufferString(tt.body)
			} else {
				body = bytes.NewBufferString("")
			}

			req := httptest.NewRequest(http.MethodPost, "/v1/ams/refresh", body)
			req.Header.Set("Content-Type", "application/json")

			if tt.refreshCookie != "" {
				req.AddCookie(&http.Cookie{Name: testCookieRefresh, Value: tt.refreshCookie})
			}
			if tt.accessCookie != "" {
				req.AddCookie(&http.Cookie{Name: testCookieAuth, Value: tt.accessCookie})
			}

			parsed, err := accessmanager.MapRequestToRefreshTokenRequest(req, testCookieRefresh, testCookieAuth, newTestValidator())

			if tt.expectError {
				require.Error(t, err)
				assert.Equal(t, tt.expectErrorKey, err.Error())
				assert.Nil(t, parsed)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, parsed)
			assert.Equal(t, tt.expectRefreshTok, parsed.RefreshToken)
			assert.Equal(t, tt.expectAccessTok, parsed.AccessToken)
		})
	}
}
