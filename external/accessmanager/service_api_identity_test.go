package accessmanager_test

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ooaklee/ghatd/external/accessmanager"
	"github.com/ooaklee/ghatd/external/apitoken"
	userv2 "github.com/ooaklee/ghatd/external/user/v2"
	"github.com/stretchr/testify/require"
)

// verifiedAPIStub controls only the verified credential and telemetry ports.
type verifiedAPIStub struct {
	accessmanager.ApitokenService
	verified *apitoken.APITokenRequester
	err      error
	touched  *apitoken.UpdateAPITokenLastUsedAtRequest
	touchErr error
	cancel   context.CancelFunc
	cancelAt string
}

func (s *verifiedAPIStub) ExtractValidateUserAPITokenMetadata(context.Context, *http.Request) (*apitoken.APITokenRequester, error) {
	if s.cancelAt == "verify" {
		s.cancel()
	}
	return s.verified, s.err
}
func (s *verifiedAPIStub) UpdateAPITokenLastUsedAt(_ context.Context, req *apitoken.UpdateAPITokenLastUsedAtRequest) error {
	s.touched = req
	if s.cancelAt == "touch" {
		s.cancel()
	}
	return s.touchErr
}

// apiOwnerStub makes stored-owner resolution observable without credentials.
type apiOwnerStub struct {
	accessmanager.UserService
	response *userv2.GetUserByNanoIDResponse
	err      error
	prefix   string
	cancel   context.CancelFunc
}

func (s *apiOwnerStub) GetUserByNanoID(_ context.Context, req *userv2.GetUserByNanoIDRequest) (*userv2.GetUserByNanoIDResponse, error) {
	s.prefix = req.NanoID
	if s.cancel != nil {
		s.cancel()
	}
	return s.response, s.err
}

func TestAPIAuthenticationBoundaryGuards(t *testing.T) {
	for _, admin := range []bool{false, true} {
		for _, variant := range []string{"nil service", "nil request", "nil verifier", "nil users", "canceled", "verify", "owner", "touch"} {
			t.Run(fmt.Sprintf("admin=%t/%s", admin, variant), func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				t.Cleanup(cancel)
				api := &verifiedAPIStub{verified: &apitoken.APITokenRequester{TokenID: "token", UserID: "owner", NanoId: "prefix", IsValid: true}, cancel: cancel, cancelAt: variant}
				users := &apiOwnerStub{response: &userv2.GetUserByNanoIDResponse{User: &userv2.UniversalUser{ID: "owner", Status: userv2.AccountStatusKeyActive, Roles: []string{userv2.UserRoleAdmin}}}}
				svc := &accessmanager.Service{ApitokenService: api, UserService: users}
				req := httptest.NewRequest("GET", "/", nil).WithContext(ctx)
				want := apitoken.ErrServiceUnavailable
				switch variant {
				case "nil service":
					svc = nil
				case "nil request":
					req = nil
				case "nil verifier":
					svc.ApitokenService = nil
				case "nil users":
					svc.UserService = nil
				case "canceled":
					cancel()
					want = context.Canceled
				case "verify", "touch":
					want = context.Canceled
				case "owner":
					users.cancel = cancel
					want = context.Canceled
				}
				var result *accessmanager.MiddlewareAuthedUserResponse
				var err error
				require.NotPanics(t, func() {
					if admin {
						result, err = svc.MiddlewareAdminAPITokenRequired(req)
					} else {
						result, err = svc.MiddlewareValidAPITokenRequired(req)
					}
				})
				require.ErrorIs(t, err, want)
				require.Nil(t, result)
				if variant != "touch" {
					require.Nil(t, api.touched)
				}
				if variant != "owner" && variant != "touch" {
					require.Empty(t, users.prefix)
				}
			})
		}
	}
}

func TestAPIAuthenticationBindsCredentialToCurrentOwner(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name                                                                                                                                                                         string
		adminRoute, adminRole, inactive, mismatch, missingID, missingOwner, missingPrefix, unverified, nilCredential, nilUser, nilResponse, verifyFailure, userFailure, touchFailure bool
		wantError                                                                                                                                                                    bool
	}{
		{name: "active owner"},
		{name: "active administrator", adminRoute: true, adminRole: true},
		{name: "admin denied ordinary owner", adminRoute: true, wantError: true},
		{name: "inactive owner", inactive: true, wantError: true},
		{name: "inactive administrator", adminRoute: true, adminRole: true, inactive: true, wantError: true},
		{name: "stored owner mismatch", mismatch: true, wantError: true},
		{name: "missing credential ID", missingID: true, wantError: true},
		{name: "missing stored owner", missingOwner: true, wantError: true},
		{name: "missing namespace", missingPrefix: true, wantError: true},
		{name: "unverified credential", unverified: true, wantError: true},
		{name: "nil verifier result", nilCredential: true, wantError: true},
		{name: "nil resolved user", nilUser: true, wantError: true},
		{name: "nil user response", nilResponse: true, wantError: true},
		{name: "verification failed", verifyFailure: true, wantError: true},
		{name: "owner lookup failed", userFailure: true, wantError: true},
		{name: "telemetry failure does not revoke authenticated result", touchFailure: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			digest := sha256.Sum256([]byte("test-secret"))
			credential := &apitoken.APITokenRequester{TokenID: "credential", UserID: "owner", NanoId: "prefix", IsValid: true, UserAPITokenEncoded: digest[:]}
			if tc.missingID {
				credential.TokenID = ""
			}
			if tc.missingOwner {
				credential.UserID = ""
			}
			if tc.missingPrefix {
				credential.NanoId = ""
			}
			if tc.unverified {
				credential.IsValid = false
			}
			if tc.nilCredential {
				credential = nil
			}
			api := &verifiedAPIStub{verified: credential}
			if tc.verifyFailure {
				api.err = errors.New("verification unavailable")
			}
			if tc.touchFailure {
				api.touchErr = errors.New("telemetry unavailable")
			}
			user := &userv2.UniversalUser{ID: "owner", Status: userv2.AccountStatusKeyActive, Type: "person"}
			if tc.adminRole {
				user.Roles = []string{userv2.UserRoleAdmin}
			}
			if tc.inactive {
				user.Status = userv2.AccountStatusKeyProvisioned
			}
			if tc.mismatch {
				user.ID = "other"
			}
			if tc.nilUser {
				user = nil
			}
			owner := &apiOwnerStub{response: &userv2.GetUserByNanoIDResponse{User: user}}
			if tc.nilResponse {
				owner.response = nil
			}
			if tc.userFailure {
				owner.err = errors.New("user store unavailable")
			}
			service := &accessmanager.Service{ApitokenService: api, UserService: owner}
			req := httptest.NewRequest("GET", "/items", nil)
			var result *accessmanager.MiddlewareAuthedUserResponse
			var err error
			if tc.adminRoute {
				result, err = service.MiddlewareAdminAPITokenRequired(req)
			} else {
				result, err = service.MiddlewareValidAPITokenRequired(req)
			}
			if tc.wantError {
				require.Error(t, err)
				require.Nil(t, result)
				require.Nil(t, api.touched)
			} else {
				require.NoError(t, err)
				require.True(t, result.Authenticated)
				require.Equal(t, "owner", result.UserID)
				require.Nil(t, result.Token)
				require.Equal(t, &apitoken.CredentialDetails{TokenID: "credential", UserID: "owner"}, result.APIToken)
				require.Equal(t, "prefix", owner.prefix)
				require.Equal(t, &apitoken.UpdateAPITokenLastUsedAtRequest{TokenID: "credential", ClientID: "owner", APITokenEncoded: digest[:]}, api.touched)
			}
			if tc.verifyFailure || tc.nilCredential || tc.unverified || tc.missingID || tc.missingOwner || tc.missingPrefix {
				require.Empty(t, owner.prefix, "unverified identities must not reach account lookup")
			}
		})
	}
}
