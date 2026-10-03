package accessmanager

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"
	"github.com/ooaklee/ghatd/external/logger"
	user "github.com/ooaklee/ghatd/external/user/v2"
	"github.com/ooaklee/ghatd/external/validator"
	"github.com/ooaklee/reply/v2"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

type proofHandlerService struct {
	AccessmanagerService
	err   error
	calls int
}

func (s *proofHandlerService) LoginUser(context.Context, *LoginUserRequest) (*LoginUserResponse, error) {
	s.calls++
	return nil, s.err
}
func (s *proofHandlerService) ValidateEmailVerificationCode(context.Context, *ValidateEmailVerificationCodeRequest) (*ValidateEmailVerificationCodeResponse, error) {
	s.calls++
	return nil, s.err
}
func (s *proofHandlerService) OAuthProviders() []string { return nil }
func (s *proofHandlerService) OAuthLink(context.Context, *OauthLoginRequest, string) (*OauthLoginResponse, error) {
	s.calls++
	return nil, s.err
}

func TestEmailProofHandlerErrorsAndPrivacy(t *testing.T) {
	for _, endpoint := range []string{"login", "verification"} {
		for _, tc := range []struct {
			name   string
			err    error
			status int
			code   string
		}{
			{"wrapped credential denial", fmt.Errorf("private-proof-diagnostic: %w", ErrOAuthReauthenticationRequired), 401, "OAuthReauthenticationRequired"},
			{"unknown outage", errors.New("private-proof-diagnostic"), 500, ""},
			{"mixed outage", errors.Join(ErrOAuthReauthenticationRequired, errors.New("private-proof-diagnostic")), 500, ""},
			{"host override", fmt.Errorf("private-proof-diagnostic: %w", ErrOAuthReauthenticationRequired), 403, "HOST_REAUTH"},
			{"nil success", nil, 503, "AM00-039"},
			{"state conflict", user.ErrLoginStateConflict, 409, "USV2-040"},
			{"state unavailable", fmt.Errorf("private-proof-diagnostic: %w", user.ErrLoginStateUnavailable), 503, "USV2-041"},
		} {
			t.Run(endpoint+"/"+tc.name, func(t *testing.T) {
				service := &proofHandlerService{err: tc.err}
				h := NewHandler(&NewHandlerRequest{Service: service, Validator: validator.NewValidator()})
				if tc.name == "host override" {
					h.errorMaps = []reply.ErrorManifest{{ErrOAuthReauthenticationRequired: {Title: "Sign in again", StatusCode: 403, Code: "HOST_REAUTH"}}}
				}
				core, logs := observer.New(zap.DebugLevel)
				request := httptest.NewRequest(http.MethodGet, "/?c=ABCD1234", nil).WithContext(logger.TransitWith(context.Background(), zap.New(core)))
				response := httptest.NewRecorder()
				if endpoint == "login" {
					h.LoginUser(response, request)
				} else {
					h.ValidateEmailVerificationCode(response, request)
				}
				require.Equal(t, 1, service.calls)
				require.Equal(t, tc.status, response.Code, response.Body.String())
				require.Equal(t, "no-store", response.Header().Get("Cache-Control"))
				if tc.code != "" {
					require.Contains(t, response.Body.String(), tc.code)
				}
				require.Empty(t, response.Result().Cookies())
				require.NotContains(t, response.Body.String()+fmt.Sprint(logs.All()), "private-proof-diagnostic")
				require.NotContains(t, fmt.Sprint(logs.All()), "ABCD1234")
			})
		}
	}
}

func TestOAuthLinkCookieSelectionAndMappedErrors(t *testing.T) {
	for _, tc := range []struct {
		name    string
		cookies []string
		calls   int
		status  int
	}{
		{"one cookie", []string{"session"}, 1, 503},
		{"missing", nil, 0, 401},
		{"empty", []string{""}, 0, 401},
		{"duplicate equal", []string{"session", "session"}, 0, 401},
		{"duplicate different", []string{"session", "other"}, 0, 401},
		{"empty then session", []string{"", "session"}, 0, 401},
	} {
		t.Run(tc.name, func(t *testing.T) {
			service := &proofHandlerService{err: fmt.Errorf("private-session-diagnostic: %w", ErrSessionVerificationUnavailable)}
			h := NewHandler(&NewHandlerRequest{Service: service, OAuthOrigin: "https://app.example", CookiePrefixAuthToken: "access"})
			r := httptest.NewRequest(http.MethodPost, "/oauth/google/link", strings.NewReader(`{"request_url":"/settings","browser":true}`))
			r.Header.Set("Origin", "https://app.example")
			r = mux.SetURLVars(r, map[string]string{"provider": "google"})
			for _, cookie := range tc.cookies {
				r.AddCookie(&http.Cookie{Name: "access", Value: cookie})
			}
			w := httptest.NewRecorder()
			h.OAuthLink(w, r)
			require.Equal(t, tc.calls, service.calls)
			require.Equal(t, tc.status, w.Code, w.Body.String())
			require.NotContains(t, w.Body.String(), "private-session-diagnostic")
			require.Empty(t, w.Result().Cookies())
		})
	}
}
