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

	"github.com/gorilla/mux"
	"github.com/ooaklee/ghatd/external/accessmanager"
	accesshelpers "github.com/ooaklee/ghatd/external/accessmanager/helpers"
	"github.com/ooaklee/ghatd/external/apitoken"
	"github.com/ooaklee/ghatd/external/auth"
	"github.com/ooaklee/ghatd/external/logger"
	userv2 "github.com/ooaklee/ghatd/external/user/v2"
	"github.com/ooaklee/reply/v2"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

// tokenSessionContext models trusted middleware publication for service tests,
// not cryptographic credential verification. It preserves nil/canceled contexts.
func tokenSessionContext(ctx context.Context, owner string) context.Context {
	if ctx == nil {
		return nil
	}
	ctx = accesshelpers.TransitWith(ctx, owner)
	ctx = accesshelpers.TransitAuthenticatedWith(ctx, true)
	return accesshelpers.TransitSessionWith(ctx, &auth.TokenAccessDetails{UserID: owner, AccessUUID: "session", TokenUse: auth.TokenUseAccess, IsAuthorized: true})
}

// managementAPI provides deterministic lower-domain responses, never live
// credentials. Mutations record dispatch independently of manager admission.
type managementAPI struct {
	creationAPIStub
	calls        int
	result       *apitoken.GetAPITokensForResponse
	err          error
	query        *apitoken.GetAPITokensForRequest
	owner, token string
	after        context.CancelFunc
	nilResult    bool
}

func (a *managementAPI) GetAPITokensFor(_ context.Context, req *apitoken.GetAPITokensForRequest) (*apitoken.GetAPITokensForResponse, error) {
	a.calls++
	copy := *req
	a.query = &copy
	req.ID, req.Description = "adapter-edited", "adapter-edited"
	if a.after != nil {
		a.after()
	}
	if a.result != nil || a.err != nil || a.nilResult {
		return a.result, a.err
	}
	return &apitoken.GetAPITokensForResponse{APITokens: []apitoken.UserAPIToken{}}, nil
}
func (a *managementAPI) DeleteAPIToken(_ context.Context, req *apitoken.DeleteAPITokenRequest) error {
	a.calls++
	a.owner, a.token = req.UserID, req.APITokenID
	if a.after != nil {
		a.after()
	}
	return a.err
}
func (a *managementAPI) ActivateAPIToken(_ context.Context, req *apitoken.ActivateAPITokenRequest) error {
	a.calls++
	a.owner, a.token = req.UserID, req.ID
	if a.after != nil {
		a.after()
	}
	return a.err
}
func (a *managementAPI) RevokeAPIToken(_ context.Context, req *apitoken.RevokeAPITokenRequest) error {
	a.calls++
	a.owner, a.token = req.UserID, req.ID
	if a.after != nil {
		a.after()
	}
	return a.err
}

// managementUsers controls live account evidence separately from published
// authentication. Each case owns its records, call counters and cancellation.
type managementUsers struct {
	accessmanager.UserService
	account *userv2.UniversalUser
	err     error
	calls   int
	after   context.CancelFunc
}

func (u *managementUsers) GetUserByID(context.Context, *userv2.GetUserByIDRequest) (*userv2.GetUserByIDResponse, error) {
	u.calls++
	if u.after != nil {
		u.after()
	}
	return &userv2.GetUserByIDResponse{User: u.account}, u.err
}

// callTokenManagement exercises the same caller/target pair across the public
// service commands; operation-specific response assertions live in other tables.
func callTokenManagement(s *accessmanager.Service, ctx context.Context, operation, actor, owner string) error {
	switch operation {
	case "create":
		_, err := s.CreateUserAPIToken(ctx, &accessmanager.CreateUserAPITokenRequest{ActorID: actor, UserID: owner})
		return err
	case "list":
		_, err := s.GetSpecificUserAPITokens(ctx, &accessmanager.GetSpecificUserAPITokensRequest{ActorID: actor, UserID: owner, GetAPITokensForRequest: &apitoken.GetAPITokensForRequest{}})
		return err
	case "threshold":
		_, err := s.GetUserAPITokenThreshold(ctx, &accessmanager.GetUserAPITokenThresholdRequest{ActorID: actor, UserID: owner})
		return err
	case "delete":
		return s.DeleteUserAPIToken(ctx, &accessmanager.DeleteUserAPITokenRequest{ActorID: actor, UserID: owner, APITokenID: "token"})
	default:
		status := apitoken.UserTokenStatusKeyActive
		if operation == "revoke" {
			status = apitoken.UserTokenStatusKeyRevoked
		}
		return s.UpdateUserAPITokenStatus(ctx, &accessmanager.UserAPITokenStatusRequest{ActorID: actor, UserID: owner, APITokenID: "token", Status: status})
	}
}

func TestTokenManagementCallerAndLiveAccount(t *testing.T) {
	for _, operation := range []string{"create", "list", "threshold", "delete", "activate", "revoke"} {
		for _, tc := range []struct {
			name  string
			want  error
			reads int
		}{
			{"session", nil, 1}, {"legacy verified session", nil, 1},
			{"bare context", accessmanager.ErrUnauthorizedUnableToAttainRequestorID, 0},
			{"id only", accessmanager.ErrUnauthorizedUnableToAttainRequestorID, 0},
			{"anonymous placeholder", accessmanager.ErrUnauthorizedUnableToAttainRequestorID, 0},
			{"no actor", accessmanager.ErrUnauthorizedUnableToAttainRequestorID, 0},
			{"forged actor", accessmanager.ErrUnauthorizedUnableToAttainRequestorID, 0},
			{"other owner", accessmanager.ErrForbiddenUnableToAction, 0},
			{"administrator other owner", accessmanager.ErrForbiddenUnableToAction, 0},
			{"api credential", accessmanager.ErrUnauthorizedUnableToAttainRequestorID, 0},
			{"competing credentials", accessmanager.ErrUnauthorizedUnableToAttainRequestorID, 0},
			{"login proof", accessmanager.ErrUnauthorizedUnableToAttainRequestorID, 0},
			{"missing session ID", accessmanager.ErrUnauthorizedUnableToAttainRequestorID, 0},
			{"published account mismatch", accessmanager.ErrUnauthorizedUnableToAttainRequestorID, 0},
			{"published typed nil", accessmanager.ErrUnauthorizedUnableToAttainRequestorID, 0},
			{"inactive account", accessmanager.ErrForbiddenUnableToAction, 1},
			{"missing account", accessmanager.ErrForbiddenUnableToAction, 1},
			{"wrong live owner", accessmanager.ErrForbiddenUnableToAction, 1},
			{"email revision changed", accessmanager.ErrUnauthorizedUnableToAttainRequestorID, 1},
			{"signed type changed", accessmanager.ErrUnauthorizedUnableToAttainRequestorID, 1},
			{"typed nil users", accessmanager.ErrTokenPolicyUnavailable, 0},
			{"canceled entry", context.Canceled, 0},
			{"canceled user read", context.Canceled, 1},
		} {
			t.Run(operation+"/"+tc.name, func(t *testing.T) {
				base, cancel := context.WithCancel(context.Background())
				t.Cleanup(cancel)
				ctx := tokenSessionContext(base, "owner")
				actor, owner := "owner", "owner"
				users := &managementUsers{account: &userv2.UniversalUser{ID: "owner", Status: userv2.AccountStatusKeyActive, Roles: []string{"ADMIN"}}}
				api := &managementAPI{}
				s := &accessmanager.Service{UserService: users, ApitokenService: api}
				switch tc.name {
				case "legacy verified session":
					ctx = accesshelpers.TransitSessionWith(ctx, &auth.TokenAccessDetails{UserID: owner, AccessUUID: "session", IsAuthorized: true})
				case "bare context":
					ctx = base
				case "id only":
					ctx = accesshelpers.TransitWith(base, owner)
				case "anonymous placeholder":
					ctx = accesshelpers.TransitAuthenticatedWith(ctx, false)
				case "no actor":
					actor = ""
				case "forged actor":
					actor = "forged"
				case "other owner", "administrator other owner":
					owner = "other"
				case "api credential":
					ctx = accesshelpers.TransitSessionWith(ctx, nil)
					ctx = accesshelpers.TransitAPITokenWith(ctx, &apitoken.CredentialDetails{UserID: "owner", TokenID: "api"})
				case "competing credentials":
					ctx = accesshelpers.TransitAPITokenWith(ctx, &apitoken.CredentialDetails{UserID: "owner", TokenID: "api"})
				case "login proof":
					ctx = accesshelpers.TransitSessionWith(ctx, &auth.TokenAccessDetails{UserID: owner, AccessUUID: "proof", TokenUse: auth.TokenUseLogin, IsAuthorized: true})
				case "missing session ID":
					ctx = accesshelpers.TransitSessionWith(ctx, &auth.TokenAccessDetails{UserID: owner, TokenUse: auth.TokenUseAccess, IsAuthorized: true})
				case "published account mismatch":
					ctx = accesshelpers.TransitUserWith(ctx, &userv2.UniversalUser{ID: "other"})
				case "published typed nil":
					ctx = accesshelpers.TransitUserWith(ctx, nil)
				case "inactive account":
					users.account.Status = userv2.AccountStatusKeySuspended
				case "missing account":
					users.account = nil
				case "wrong live owner":
					users.account.ID = "other"
				case "email revision changed":
					users.account.EmailRevision = 1
				case "signed type changed":
					users.account.Type = "different"
					ctx = accesshelpers.TransitSessionWith(ctx, &auth.TokenAccessDetails{UserID: owner, AccessUUID: "session", TokenUse: auth.TokenUseAccess, UserType: "original", IsAuthorized: true})
				case "typed nil users":
					s.UserService = (*managementUsers)(nil)
				case "canceled entry":
					cancel()
				case "canceled user read":
					users.after = cancel
				}
				if tc.name == "other owner" {
					users.account.Roles = nil
				}
				err := callTokenManagement(s, ctx, operation, actor, owner)
				require.ErrorIs(t, err, tc.want)
				require.Equal(t, tc.reads, users.calls)
				if tc.want != nil {
					require.Zero(t, api.calls)
					require.Zero(t, api.created)
				}
			})
		}
	}
}

func TestTokenManagementNativeErrorsAndCancellation(t *testing.T) {
	for _, operation := range []string{"create", "list", "threshold", "delete", "activate", "revoke"} {
		for _, phase := range []string{"user", "domain", "domain canceled"} {
			if (operation == "threshold" && phase != "user") || (operation == "create" && phase == "domain canceled") {
				continue
			}
			t.Run(operation+"/"+phase, func(t *testing.T) {
				ctx, cancel := context.WithCancel(tokenSessionContext(context.Background(), "owner"))
				t.Cleanup(cancel)
				native := fmt.Errorf("private native diagnostic: %w", apitoken.ErrInventoryUnavailable)
				users := &managementUsers{account: &userv2.UniversalUser{ID: "owner", Status: userv2.AccountStatusKeyActive, Roles: []string{"ADMIN"}}}
				api := &managementAPI{}
				want := error(native)
				if phase == "user" {
					users.err = native
				} else {
					api.err = native
					api.createErr = native
				}
				if phase == "domain canceled" {
					// Creation's phase matrix is retained in token_policy_boundaries_test.go.
					api.err = nil
					api.after = cancel
					want = context.Canceled
				}
				err := callTokenManagement(&accessmanager.Service{UserService: users, ApitokenService: api}, ctx, operation, "owner", "owner")
				require.Equal(t, want, err)
				if phase == "user" {
					require.Zero(t, api.calls)
					require.Zero(t, api.created)
				}
			})
		}
	}
}

func TestTokenManagementListProjectionAndQueryIsolation(t *testing.T) {
	for _, variant := range []string{"valid", "other owner", "missing token ID", "negative total", "negative pages", "negative page", "negative per page"} {
		t.Run(variant, func(t *testing.T) {
			row := apitoken.UserAPIToken{ID: "token", CreatedByID: "owner", Value: "private secret", ValueSHA: []byte{1, 2}, Description: "a token"}
			result := &apitoken.GetAPITokensForResponse{APITokens: []apitoken.UserAPIToken{row}, Total: 1, TotalPages: 1, Page: 1, APITokensPerPage: 25}
			switch variant {
			case "other owner":
				result.APITokens[0].CreatedByID = "other"
			case "missing token ID":
				result.APITokens[0].ID = ""
			case "negative total":
				result.Total = -1
			case "negative pages":
				result.TotalPages = -1
			case "negative page":
				result.Page = -1
			case "negative per page":
				result.APITokensPerPage = -1
			}
			api := &managementAPI{result: result}
			s := &accessmanager.Service{ApitokenService: api, UserService: &boundaryTokenUser{}}
			query := &apitoken.GetAPITokensForRequest{ID: "forged", NanoId: "forged", TotalCount: 99, Page: 2, PerPage: 25, Meta: true, Description: "search", OnlyPermanent: true}
			got, err := s.GetSpecificUserAPITokens(tokenSessionContext(context.Background(), "owner"), &accessmanager.GetSpecificUserAPITokensRequest{ActorID: "owner", UserID: "owner", GetAPITokensForRequest: query})
			require.Equal(t, "forged", query.ID)
			require.Equal(t, "search", query.Description)
			require.Equal(t, "owner", api.query.ID)
			require.Empty(t, api.query.NanoId)
			require.Zero(t, api.query.TotalCount)
			require.Equal(t, 2, api.query.Page)
			require.True(t, api.query.Meta)
			require.True(t, api.query.OnlyPermanent)
			require.Equal(t, "private secret", result.APITokens[0].Value)
			require.Equal(t, []byte{1, 2}, result.APITokens[0].ValueSHA)
			if variant != "valid" {
				require.ErrorIs(t, err, apitoken.ErrServiceUnavailable)
				require.Nil(t, got)
				return
			}
			require.NoError(t, err)
			require.Empty(t, got.UserAPITokens[0].Value)
			require.Nil(t, got.UserAPITokens[0].ValueSHA)
			got.UserAPITokens[0].Description = "changed"
			require.Equal(t, "a token", result.APITokens[0].Description)
		})
	}
}

// invokeTokenHandler keeps the transport matrix on the real mapper/handler path.
func invokeTokenHandler(h *accessmanager.Handler, operation string, w http.ResponseWriter, r *http.Request) {
	switch operation {
	case "create":
		h.CreateUserAPIToken(w, r)
	case "list":
		h.GetSpecificUserAPITokens(w, r)
	case "threshold":
		h.GetUserAPITokenThreshold(w, r)
	case "delete":
		h.DeleteUserAPIToken(w, r)
	case "activate":
		h.ActivateUserAPIToken(w, r)
	case "revoke":
		h.RevokeUserAPIToken(w, r)
	}
}

func TestTokenManagementHTTPContractAndDiagnosticPrivacy(t *testing.T) {
	for _, operation := range []string{"create", "list", "threshold", "delete", "activate", "revoke"} {
		for _, variant := range []string{"success", "wrapped native", "joined unknown", "host override", "missing service", "id only", "nil result"} {
			if variant == "nil result" && (operation == "delete" || operation == "activate" || operation == "revoke") {
				continue
			}
			t.Run(operation+"/"+variant, func(t *testing.T) {
				core, logs := observer.New(zap.DebugLevel)
				ctx := tokenSessionContext(logger.TransitWith(context.Background(), zap.New(core)), "owner")
				var failure error
				want := 202
				if operation == "create" {
					want = 201
				} else if operation == "list" || operation == "threshold" {
					want = 200
				}
				var maps []reply.ErrorManifest
				switch variant {
				case "wrapped native":
					failure = fmt.Errorf("private diagnostic: %w", apitoken.ErrInvalidTokenTTL)
					want = 400
				case "joined unknown":
					failure = errors.Join(apitoken.ErrInvalidTokenTTL, errors.New("private diagnostic"))
					want = 500
				case "host override":
					failure = fmt.Errorf("private diagnostic: %w", apitoken.ErrInvalidTokenTTL)
					want = 422
					maps = []reply.ErrorManifest{{apitoken.ErrInvalidTokenTTL: {Title: "Invalid lifetime", StatusCode: 422, Code: "HOST-TTL"}}}
				case "missing service", "nil result":
					want = 503
				case "id only":
					ctx = accesshelpers.TransitWith(context.Background(), "owner")
					want = 401
				}
				calls := 0
				check := func(actor, owner string) { calls++; require.Equal(t, "owner", actor); require.Equal(t, "owner", owner) }
				service := &mockAccessmanagerService{
					createUserAPITokenFunc: func(_ context.Context, r *accessmanager.CreateUserAPITokenRequest) (*accessmanager.CreateUserAPITokenResponse, error) {
						check(r.ActorID, r.UserID)
						require.Equal(t, int64(60), r.Ttl)
						if variant == "nil result" {
							return nil, nil
						}
						return &accessmanager.CreateUserAPITokenResponse{UserAPIToken: apitoken.UserAPIToken{ID: "token", CreatedByID: "owner", Status: "ACTIVE", Value: "creation-only-secret"}}, failure
					},
					getSpecificUserAPITokensFunc: func(_ context.Context, r *accessmanager.GetSpecificUserAPITokensRequest) (*accessmanager.GetSpecificUserAPITokensResponse, error) {
						check(r.ActorID, r.UserID)
						require.Empty(t, r.ID)
						require.Empty(t, r.NanoId)
						if variant == "nil result" {
							return nil, nil
						}
						return &accessmanager.GetSpecificUserAPITokensResponse{UserAPITokens: []apitoken.UserAPIToken{{ID: "token", CreatedByID: "owner", Value: "never-list-secret", ValueSHA: []byte{1}}}, Total: 1, TotalPages: 1, Page: 1, ResourcesPerPage: 25}, failure
					},
					getUserAPITokenThresholdFunc: func(_ context.Context, r *accessmanager.GetUserAPITokenThresholdRequest) (*accessmanager.GetUserAPITokenThresholdResponse, error) {
						check(r.ActorID, r.UserID)
						if variant == "nil result" {
							return nil, nil
						}
						return &accessmanager.GetUserAPITokenThresholdResponse{PermanentUserTokenLimit: 1}, failure
					},
					deleteUserAPITokenFunc: func(_ context.Context, r *accessmanager.DeleteUserAPITokenRequest) error {
						check(r.ActorID, r.UserID)
						require.Equal(t, "token", r.APITokenID)
						return failure
					},
					updateUserAPITokenStatusFunc: func(_ context.Context, r *accessmanager.UserAPITokenStatusRequest) error {
						check(r.ActorID, r.UserID)
						require.Equal(t, "token", r.APITokenID)
						expected := "ACTIVE"
						if operation == "revoke" {
							expected = "REVOKED"
						}
						require.Equal(t, expected, r.Status)
						return failure
					},
				}
				var port accessmanager.AccessmanagerService = service
				if variant == "missing service" {
					port = (*mockAccessmanagerService)(nil)
				}
				h := accessmanager.NewHandler(&accessmanager.NewHandlerRequest{Service: port, Validator: newTestValidator(), ErrorMaps: maps})
				r := httptest.NewRequest("POST", "/?meta=true&ActorID=forged&UserID=forged&ID=forged&NanoId=forged&Status=forged", strings.NewReader(`{"ttl":60,"ActorID":"forged","UserID":"forged","APITokenID":"forged","Status":"forged"}`)).WithContext(ctx)
				r = mux.SetURLVars(r, map[string]string{accessmanager.UserURIVariableID: "owner", accessmanager.APITokenURIVariableID: "token"})
				w := httptest.NewRecorder()
				invokeTokenHandler(h, operation, w, r)
				require.Equal(t, want, w.Code, w.Body.String())
				require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
				require.NotContains(t, w.Body.String()+fmt.Sprint(logs.All()), "private diagnostic")
				require.NotContains(t, w.Body.String(), "never-list-secret")
				if variant == "success" && operation == "create" {
					require.Contains(t, w.Body.String(), "creation-only-secret")
				} else {
					require.NotContains(t, w.Body.String(), "creation-only-secret")
				}
				if variant == "success" && operation == "list" {
					require.Contains(t, w.Body.String(), `"meta"`)
				}
				if variant == "id only" || variant == "missing service" {
					require.Zero(t, calls)
				} else {
					require.Equal(t, 1, calls)
				}
			})
		}
	}
}

func TestTokenManagementIdentityExcludedFromJSON(t *testing.T) {
	for _, operation := range []string{"create", "list", "threshold", "delete", "status"} {
		t.Run(operation, func(t *testing.T) {
			var value any
			switch operation {
			case "create":
				value = &accessmanager.CreateUserAPITokenRequest{}
			case "list":
				value = &accessmanager.GetSpecificUserAPITokensRequest{}
			case "threshold":
				value = &accessmanager.GetUserAPITokenThresholdRequest{}
			case "delete":
				value = &accessmanager.DeleteUserAPITokenRequest{}
			case "status":
				value = &accessmanager.UserAPITokenStatusRequest{}
			}
			require.NoError(t, json.Unmarshal([]byte(`{"ActorID":"forged","UserID":"forged","APITokenID":"forged","Status":"forged","ID":"forged"}`), value))
			encoded, err := json.Marshal(value)
			require.NoError(t, err)
			require.NotContains(t, string(encoded), "forged")
		})
	}
}

// mapTokenManagement exercises each public mapper without dispatching a handler.
func mapTokenManagement(r *http.Request, validator accessmanager.AccessmanagerValidator, operation string) error {
	var err error
	switch operation {
	case "create":
		_, err = accessmanager.MapRequestToCreateUserAPITokenRequest(r, validator)
	case "list":
		_, err = accessmanager.MapRequestToGetSpecificUserAPITokensRequest(r, validator)
	case "threshold":
		_, err = accessmanager.MapRequestToGetUserAPITokenThresholdRequest(r, validator)
	case "delete":
		_, err = accessmanager.MapRequestToDeleteUserAPITokenRequest(r, validator)
	case "activate":
		_, err = accessmanager.MapRequestToActivateUserAPITokenRequest(r, validator)
	case "revoke":
		_, err = accessmanager.MapRequestToRevokeUserAPITokenRequest(r, validator)
	}
	return err
}

func TestTokenManagementMapperBoundaries(t *testing.T) {
	for _, operation := range []string{"create", "list", "threshold", "delete", "activate", "revoke"} {
		for _, variant := range []string{"nil request", "nil URL", "nil validator", "canceled", "blank owner", "other owner", "blank token", "nil body", "invalid body", "invalid query"} {
			if variant == "blank token" && (operation == "create" || operation == "list" || operation == "threshold") ||
				(variant == "nil body" || variant == "invalid body") && operation != "create" || variant == "invalid query" && operation != "list" {
				continue
			}
			t.Run(operation+"/"+variant, func(t *testing.T) {
				ctx, cancel := context.WithCancel(tokenSessionContext(context.Background(), "owner"))
				t.Cleanup(cancel)
				r := httptest.NewRequest("POST", "/", strings.NewReader(`{"ttl":60}`)).WithContext(ctx)
				vars := map[string]string{accessmanager.UserURIVariableID: "owner", accessmanager.APITokenURIVariableID: "token"}
				var validator accessmanager.AccessmanagerValidator = newTestValidator()
				want := accessmanager.ErrBadRequest
				switch variant {
				case "nil request":
					r = nil
				case "nil URL":
					r.URL = nil
				case "nil validator":
					validator = nil
				case "canceled":
					cancel()
					want = context.Canceled
				case "blank owner":
					vars[accessmanager.UserURIVariableID] = " "
					want = accessmanager.ErrInvalidUserID
				case "other owner":
					vars[accessmanager.UserURIVariableID] = "other"
					want = accessmanager.ErrForbiddenUnableToAction
				case "blank token":
					vars[accessmanager.APITokenURIVariableID] = " "
					want = accessmanager.ErrInvalidAPITokenID
				case "nil body":
					r.Body = nil
					want = accessmanager.ErrInvalidCreateUserAPITokenBody
				case "invalid body":
					r = httptest.NewRequest("POST", "/", strings.NewReader(`{"ttl":`)).WithContext(ctx)
					want = accessmanager.ErrInvalidCreateUserAPITokenBody
				case "invalid query":
					r.URL.RawQuery = "per_page=invalid"
					want = accessmanager.ErrInvalidResultQueryParam
				}
				if r != nil {
					r = mux.SetURLVars(r, vars)
				}
				require.ErrorIs(t, mapTokenManagement(r, validator, operation), want)
			})
		}
	}
}

func TestTokenManagementListMissingInputs(t *testing.T) {
	for _, variant := range []string{"nil service", "nil context", "nil request", "nil filters", "typed nil token service", "nil result"} {
		t.Run(variant, func(t *testing.T) {
			api := &managementAPI{}
			s := &accessmanager.Service{ApitokenService: api, UserService: &boundaryTokenUser{}}
			ctx := tokenSessionContext(context.Background(), "owner")
			r := &accessmanager.GetSpecificUserAPITokensRequest{ActorID: "owner", UserID: "owner", GetAPITokensForRequest: &apitoken.GetAPITokensForRequest{}}
			want := apitoken.ErrServiceUnavailable
			switch variant {
			case "nil service":
				s = nil
			case "nil context":
				ctx = nil
			case "nil request":
				r, want = nil, accessmanager.ErrBadRequest
			case "nil filters":
				r.GetAPITokensForRequest, want = nil, accessmanager.ErrBadRequest
			case "typed nil token service":
				s.ApitokenService = (*managementAPI)(nil)
			case "nil result":
				api.nilResult = true
			}
			got, err := s.GetSpecificUserAPITokens(ctx, r)
			require.ErrorIs(t, err, want)
			require.Nil(t, got)
			if variant != "nil result" {
				require.Zero(t, api.calls)
			}
		})
	}
}

func TestTokenManagementRejectsBareOwner(t *testing.T) {
	for _, operation := range []string{"create", "list", "threshold", "delete", "activate", "revoke"} {
		t.Run(operation, func(t *testing.T) {
			api := &managementAPI{}
			s := &accessmanager.Service{ApitokenService: api, UserService: &creationUserStub{user: &userv2.UniversalUser{ID: "owner", Status: userv2.AccountStatusKeyActive, Roles: []string{"ADMIN"}}}}
			ctx := context.Background()
			var err error
			switch operation {
			case "create":
				_, err = s.CreateUserAPIToken(ctx, &accessmanager.CreateUserAPITokenRequest{UserID: "owner"})
			case "list":
				_, err = s.GetSpecificUserAPITokens(ctx, &accessmanager.GetSpecificUserAPITokensRequest{UserID: "owner", GetAPITokensForRequest: &apitoken.GetAPITokensForRequest{}})
			case "threshold":
				_, err = s.GetUserAPITokenThreshold(ctx, &accessmanager.GetUserAPITokenThresholdRequest{UserID: "owner"})
			case "delete":
				err = s.DeleteUserAPIToken(ctx, &accessmanager.DeleteUserAPITokenRequest{UserID: "owner", APITokenID: "token"})
			default:
				status := apitoken.UserTokenStatusKeyActive
				if operation == "revoke" {
					status = apitoken.UserTokenStatusKeyRevoked
				}
				err = s.UpdateUserAPITokenStatus(ctx, &accessmanager.UserAPITokenStatusRequest{UserID: "owner", APITokenID: "token", Status: status})
			}
			require.ErrorIs(t, err, accessmanager.ErrUnauthorizedUnableToAttainRequestorID)
			require.Zero(t, api.calls)
			require.Zero(t, api.created)
		})
	}
}
