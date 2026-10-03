package accessmanager_test

import (
	"context"
	"fmt"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"
	"github.com/ooaklee/ghatd/external/accessmanager"
	accessmanagerhelpers "github.com/ooaklee/ghatd/external/accessmanager/helpers"
	"github.com/ooaklee/ghatd/external/apitoken"
	"github.com/ooaklee/reply/v2"
	"github.com/stretchr/testify/require"
)

// lifecycleAPI records owner-bound dispatch; its unwired embedded port makes
// any list scan, lookup or unexpected management operation fail the test.
type lifecycleAPI struct {
	accessmanager.ApitokenService
	owner, token, operation string
}

func (s *lifecycleAPI) ActivateAPIToken(_ context.Context, req *apitoken.ActivateAPITokenRequest) error {
	s.owner, s.token, s.operation = req.UserID, req.ID, "activate"
	return nil
}
func (s *lifecycleAPI) RevokeAPIToken(_ context.Context, req *apitoken.RevokeAPITokenRequest) error {
	s.owner, s.token, s.operation = req.UserID, req.ID, "revoke"
	return nil
}
func (s *lifecycleAPI) DeleteAPIToken(_ context.Context, req *apitoken.DeleteAPITokenRequest) error {
	s.owner, s.token, s.operation = req.UserID, req.APITokenID, "delete"
	return nil
}

func TestAPILifecycleOwnerPropagation(t *testing.T) {
	for _, operation := range []string{"activate", "revoke", "delete"} {
		for _, variant := range []string{"owner", "no owner", "no token", "nil request", "nil service", "nil adapter", "nil context", "canceled", "bad status"} {
			if operation == "delete" && variant == "bad status" {
				continue
			}
			t.Run(operation+"/"+variant, func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				t.Cleanup(cancel)
				api := &lifecycleAPI{}
				svc := &accessmanager.Service{ApitokenService: api}
				owner, token := "owner", "token"
				want := accessmanager.ErrAPITokenNotAssociatedWithUser
				switch variant {
				case "owner":
					want = nil
				case "no owner":
					owner = ""
				case "no token":
					token = ""
				case "nil service":
					svc = nil
					want = apitoken.ErrServiceUnavailable
				case "nil adapter":
					svc.ApitokenService = nil
					want = apitoken.ErrServiceUnavailable
				case "nil context":
					ctx = nil
					want = apitoken.ErrServiceUnavailable
				case "canceled":
					cancel()
					want = context.Canceled
				case "bad status":
					want = apitoken.ErrTokenStatusInvalid
				}
				var err error
				require.NotPanics(t, func() {
					if operation == "delete" {
						req := &accessmanager.DeleteUserAPITokenRequest{UserID: owner, APITokenID: token}
						if variant == "nil request" {
							req = nil
						}
						err = svc.DeleteUserAPIToken(ctx, req)
					} else {
						status := apitoken.UserTokenStatusKeyActive
						if operation == "revoke" {
							status = apitoken.UserTokenStatusKeyRevoked
						}
						if variant == "bad status" {
							status = "INVALID"
						}
						req := &accessmanager.UserAPITokenStatusRequest{UserID: owner, APITokenID: token, Status: status}
						if variant == "nil request" {
							req = nil
						}
						err = svc.UpdateUserAPITokenStatus(ctx, req)
					}
				})
				if want != nil {
					require.ErrorIs(t, err, want)
					require.Empty(t, api.operation)
				} else {
					require.NoError(t, err)
					require.Equal(t, owner, api.owner)
					require.Equal(t, token, api.token)
					require.Equal(t, operation, api.operation)
				}
			})
		}
	}
}

func TestAPILifecycleMappersBindActorToOwner(t *testing.T) {
	for _, operation := range []string{"activate", "revoke", "delete"} {
		for _, actor := range []string{"owner", "different", ""} {
			t.Run(operation+"/actor="+actor, func(t *testing.T) {
				owner := "f5a10f5e-6d67-4e10-b2be-4426e8122033"
				token := "a131e6fc-78db-4c0a-8b3a-68c28a92cb49"
				req := httptest.NewRequest("PUT", "/", nil)
				identity := actor
				if actor == "owner" {
					identity = owner
				}
				req = req.WithContext(accessmanagerhelpers.TransitWith(req.Context(), identity))
				req = mux.SetURLVars(req, map[string]string{accessmanager.UserURIVariableID: owner, accessmanager.APITokenURIVariableID: token})
				var err error
				var gotOwner, gotToken string
				switch operation {
				case "activate", "revoke":
					mapper := accessmanager.MapRequestToActivateUserAPITokenRequest
					if operation == "revoke" {
						mapper = accessmanager.MapRequestToRevokeUserAPITokenRequest
					}
					got, failure := mapper(req, newTestValidator())
					err = failure
					if got != nil {
						gotOwner, gotToken = got.UserID, got.APITokenID
					}
				case "delete":
					got, failure := accessmanager.MapRequestToDeleteUserAPITokenRequest(req, newTestValidator())
					err = failure
					if got != nil {
						gotOwner, gotToken = got.UserID, got.APITokenID
					}
				}
				if actor == "owner" {
					require.NoError(t, err)
					require.Equal(t, owner, gotOwner)
					require.Equal(t, token, gotToken)
				} else {
					want := accessmanager.ErrForbiddenUnableToAction
					if actor == "" {
						want = accessmanager.ErrUnauthorizedUnableToAttainRequestorID
					}
					require.ErrorIs(t, err, want)
					require.Empty(t, gotOwner)
				}
			})
		}
	}
}

func TestAPILifecycleErrorMapIntegration(t *testing.T) {
	for _, domainErr := range []error{apitoken.ErrInvalidTokenTTL, apitoken.ErrInvalidTokenQuery, apitoken.ErrInventoryUnavailable, apitoken.ErrServiceUnavailable} {
		for _, variant := range []string{"direct", "wrapped", "override"} {
			t.Run(domainErr.Error()+"/"+variant, func(t *testing.T) {
				err := domainErr
				opts := &accessmanager.NewHandlerRequest{}
				want := apitoken.ApitokenErrorMap[domainErr]
				if variant != "direct" {
					err = fmt.Errorf("private diagnostic: %w", domainErr)
				}
				if variant == "override" {
					want.StatusCode, want.Code = 422, "HOST-OVERRIDE"
					opts.ErrorMaps = []reply.ErrorManifest{{domainErr: want}}
				}
				rec := httptest.NewRecorder()
				require.NoError(t, accessmanager.NewHandler(opts).NewHTTPErrorResponse(rec, err))
				require.Equal(t, want.StatusCode, rec.Code)
				require.Contains(t, rec.Body.String(), want.Code)
				require.NotContains(t, rec.Body.String(), "private diagnostic")
			})
		}
	}
}
