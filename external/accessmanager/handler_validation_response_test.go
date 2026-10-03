package accessmanager_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ooaklee/ghatd/external/accessmanager"
	"github.com/stretchr/testify/require"
)

// TestLoginServiceErrorClassification pins the custom-adapter boundary: only
// one unambiguous domain cause may determine an authentication error response.
func TestLoginServiceErrorClassification(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		err    error
		status int
	}{
		{"single cause", accessmanager.ErrBadRequest, http.StatusBadRequest},
		{"wrapped cause", fmt.Errorf("private diagnostic: %w", accessmanager.ErrBadRequest), http.StatusBadRequest},
		{"singleton join", errors.Join(accessmanager.ErrBadRequest), http.StatusInternalServerError},
		{"mapped validation join", errors.Join(accessmanager.ErrBadRequest, accessmanager.ErrInvalidUserBody), http.StatusInternalServerError},
		{"unknown cause", errors.New("private diagnostic"), http.StatusInternalServerError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			calls := 0
			h := accessmanager.NewHandler(&accessmanager.NewHandlerRequest{
				Validator: newTestValidator(),
				Service: &mockAccessmanagerService{loginUserFunc: func(context.Context, *accessmanager.LoginUserRequest) (*accessmanager.LoginUserResponse, error) {
					calls++
					return nil, tc.err
				}},
			})
			w := httptest.NewRecorder()
			h.LoginUser(w, httptest.NewRequest(http.MethodGet, "/login?c=ABC12345", nil))
			require.Equal(t, 1, calls)
			require.Equal(t, tc.status, w.Code, w.Body.String())
			require.NotContains(t, w.Body.String(), "private diagnostic")
		})
	}
}

// TestHandlerValidationResponses exercises real request validation before the
// strict authentication response resolver. Multiple invalid fields must remain
// a mapped client failure, never an opaque server failure or service dispatch.
func TestHandlerValidationResponses(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, endpoint, method, path, body string
		want                               error
	}{
		{"invalid login fields", "login", http.MethodGet, "/login?t=short&c=bad", "", accessmanager.ErrInvalidVerificationToken},
		{"missing login proof", "login", http.MethodGet, "/login", "", accessmanager.ErrMissingVerificationCredentials},
		{"invalid signup fields", "signup", http.MethodPost, "/users", `{"email":"invalid","first_name":"","last_name":""}`, accessmanager.ErrInvalidUserBody},
		{"missing refresh proof", "refresh", http.MethodPost, "/refresh", `{}`, accessmanager.ErrInvalidRefreshToken},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// No service is supplied: unexpected dispatch fails the test immediately.
			h := accessmanager.NewHandler(&accessmanager.NewHandlerRequest{
				Validator: newTestValidator(), CookiePrefixAuthToken: "access", CookiePrefixRefreshToken: "refresh",
			})
			r := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			w := httptest.NewRecorder()
			switch tc.endpoint {
			case "login":
				h.LoginUser(w, r)
			case "signup":
				h.CreateUser(w, r)
			case "refresh":
				h.RefreshToken(w, r)
			default:
				t.Fatal("unknown test endpoint")
			}
			item := accessmanager.AccessmanagerErrorMap[tc.want]
			require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
			var envelope struct {
				Errors []struct {
					Code string `json:"code"`
				} `json:"errors"`
			}
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &envelope))
			require.Len(t, envelope.Errors, 1)
			require.NotEmpty(t, item.Code)
			require.Equal(t, item.Code, envelope.Errors[0].Code)
		})
	}
}
