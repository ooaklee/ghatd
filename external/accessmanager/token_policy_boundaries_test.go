package accessmanager_test

import (
	"context"
	"errors"
	"fmt"
	"net/http/httptest"
	"testing"

	"github.com/ooaklee/ghatd/external/accessmanager"
	"github.com/ooaklee/ghatd/external/accesspolicy"
	"github.com/ooaklee/ghatd/external/apitoken"
	userv2 "github.com/ooaklee/ghatd/external/user/v2"
	"github.com/ooaklee/reply/v2"
	"github.com/stretchr/testify/require"
)

func TestTokenPolicyEntryBoundaries(t *testing.T) {
	for _, operation := range []string{"create", "display"} {
		for _, variant := range []string{"nil service", "nil context", "canceled", "nil request", "empty owner", "missing wiring"} {
			t.Run(operation+"/"+variant, func(t *testing.T) {
				// Empty embedded ports panic if a dependency is called. Boundary
				// rejection must happen before either lookup or issuance.
				s := &accessmanager.Service{UserService: &creationUserStub{}, ApitokenService: &creationAPIStub{}}
				ctx := context.Background()
				create := &accessmanager.CreateUserAPITokenRequest{UserID: "owner"}
				display := &accessmanager.GetUserAPITokenThresholdRequest{UserId: "owner"}
				want := accessmanager.ErrTokenPolicyUnavailable
				switch variant {
				case "nil service":
					s = nil
				case "nil context":
					ctx = nil
				case "canceled":
					canceled, cancel := context.WithCancel(ctx)
					cancel()
					ctx, want = canceled, context.Canceled
				case "nil request":
					create, display, want = nil, nil, accessmanager.ErrBadRequest
				case "empty owner":
					create.UserID, display.UserId, want = "", "", accessmanager.ErrBadRequest
				case "missing wiring":
					s = &accessmanager.Service{}
				}
				require.NotPanics(t, func() {
					if operation == "create" {
						got, err := s.CreateUserAPIToken(ctx, create)
						require.ErrorIs(t, err, want)
						require.Nil(t, got)
					} else {
						got, err := s.GetUserAPITokenThreshold(ctx, display)
						require.ErrorIs(t, err, want)
						require.Nil(t, got)
					}
				})
			})
		}
	}
}

// policyDisguisedFailure proves domain orchestration does not consult an
// adapter's custom Is method to replace a storage failure with a denial.
type policyDisguisedFailure struct{ cause error }

func (policyDisguisedFailure) Error() string        { return "private policy diagnostic" }
func (e policyDisguisedFailure) Unwrap() error      { return e.cause }
func (policyDisguisedFailure) Is(target error) bool { return target == accesspolicy.ErrDenied }

func TestTokenPolicyPreservesFailureCauses(t *testing.T) {
	outage := errors.New("private persistence diagnostic")
	for _, operation := range []string{"create", "display"} {
		for _, tc := range []struct {
			name  string
			cause error
		}{
			{"denial", accesspolicy.ErrDenied},
			{"wrapped configuration", fmt.Errorf("private configuration: %w", accesspolicy.ErrConfiguration)},
			{"joined outage", errors.Join(accesspolicy.ErrDenied, outage)},
			{"custom Is outage", policyDisguisedFailure{outage}},
			{"cancellation", context.Canceled},
		} {
			t.Run(operation+"/"+tc.name, func(t *testing.T) {
				s := accessmanager.NewService(&accessmanager.NewServiceRequest{TokenPolicy: &creationPolicyStub{err: tc.cause}, UserService: &creationUserStub{}, ApiTokenService: &creationAPIStub{}})
				var err error
				if operation == "create" {
					got, failure := s.CreateUserAPIToken(context.Background(), &accessmanager.CreateUserAPITokenRequest{UserID: "owner"})
					require.Nil(t, got)
					err = failure
				} else {
					got, failure := s.GetUserAPITokenThreshold(context.Background(), &accessmanager.GetUserAPITokenThresholdRequest{UserId: "owner"})
					require.Nil(t, got)
					err = failure
				}
				require.Equal(t, tc.cause, err, "orchestration must preserve the original failure graph")
			})
		}
	}
}

func TestTokenPolicyNativeErrorReplies(t *testing.T) {
	for _, tc := range []struct {
		name   string
		cause  error
		status int
		code   string
	}{
		{"denied", accesspolicy.ErrDenied, 403, "ACP0-001"},
		{"wrapped configuration", fmt.Errorf("private diagnostic: %w", accesspolicy.ErrConfiguration), 503, "ACP0-002"},
		{"conflict", accesspolicy.ErrConflict, 409, "ACP0-003"},
		{"joined outage", errors.Join(accesspolicy.ErrDenied, errors.New("private diagnostic")), 500, ""},
	} {
		for _, override := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/override=%t", tc.name, override), func(t *testing.T) {
				var maps []reply.ErrorManifest
				if override {
					maps = []reply.ErrorManifest{{accesspolicy.ErrDenied: {StatusCode: 422, Code: "HOST-POLICY"}}}
				}
				h := accessmanager.NewHandler(&accessmanager.NewHandlerRequest{ErrorMaps: maps})
				w := httptest.NewRecorder()
				require.NoError(t, h.NewHTTPErrorResponse(w, tc.cause))
				status, code := tc.status, tc.code
				if override && tc.name == "denied" {
					status, code = 422, "HOST-POLICY"
				}
				require.Equal(t, status, w.Code)
				if code != "" {
					require.Contains(t, w.Body.String(), code)
				}
				require.NotContains(t, w.Body.String(), "private diagnostic")
			})
		}
	}
}

// boundaryTokenPolicy models policy dispatch independently from real Mongo
// atomicity. Hooks simulate cancellation at known read/callback/commit points.
type boundaryTokenPolicy struct {
	limits         accesspolicy.TokenLimits
	before, after  func()
	nilTransaction bool
}

func (p *boundaryTokenPolicy) TokenLimits(context.Context, string) (accesspolicy.TokenLimits, error) {
	if p.before != nil {
		p.before()
	}
	return p.limits, nil
}

func (p *boundaryTokenPolicy) WithTokenCreation(ctx context.Context, _ string, create func(context.Context, accesspolicy.TokenLimits) error) error {
	if p.before != nil {
		p.before()
	}
	if p.nilTransaction {
		ctx = nil
	}
	if err := create(ctx, p.limits); err != nil {
		return err
	}
	if p.after != nil {
		p.after()
	}
	return nil
}

// boundaryTokenUser records whether a stopped request reached its owner read.
type boundaryTokenUser struct {
	accessmanager.UserService
	calls int
	after func()
}

func (u *boundaryTokenUser) GetUserByID(_ context.Context, r *userv2.GetUserByIDRequest) (*userv2.GetUserByIDResponse, error) {
	u.calls++
	if u.after != nil {
		u.after()
	}
	return &userv2.GetUserByIDResponse{User: &userv2.UniversalUser{ID: r.ID, NanoID: "prefix", Status: userv2.AccountStatusKeyActive, Roles: []string{"MAX"}}}, nil
}

// boundaryTokenAPI counts dispatches and owns the mutable result returned by a
// custom adapter. It deliberately does not honor cancellation on our behalf.
type boundaryTokenAPI struct {
	accessmanager.ApitokenService
	counts, creates         int
	afterCount, afterCreate func()
	result                  *apitoken.CreateAPITokenResponse
}

func (a *boundaryTokenAPI) CountTokenInventory(context.Context, string) (apitoken.Inventory, error) {
	a.counts++
	if a.afterCount != nil {
		a.afterCount()
	}
	return apitoken.Inventory{}, nil
}
func (a *boundaryTokenAPI) CountTokenInventoryFenced(ctx context.Context, owner string) (apitoken.Inventory, error) {
	return a.CountTokenInventory(ctx, owner)
}
func (a *boundaryTokenAPI) CreateAPIToken(context.Context, *apitoken.CreateAPITokenRequest) (*apitoken.CreateAPITokenResponse, error) {
	a.creates++
	if a.afterCreate != nil {
		a.afterCreate()
	}
	return a.result, nil
}

func TestTokenIssuanceCancellationBoundaries(t *testing.T) {
	for _, mode := range []string{"legacy", "policy"} {
		for _, tc := range []struct {
			phase                  string
			reads, counts, creates int
		}{
			{"entry", 0, 0, 0}, {"user", 1, 0, 0}, {"inventory", 1, 1, 0},
			{"create", 1, 1, 1}, {"callback", 0, 0, 0}, {"commit", 1, 1, 1},
		} {
			if mode == "legacy" && (tc.phase == "callback" || tc.phase == "commit") {
				continue
			}
			t.Run(mode+"/"+tc.phase, func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				t.Cleanup(cancel)
				users := &boundaryTokenUser{}
				api := &boundaryTokenAPI{result: &apitoken.CreateAPITokenResponse{APIToken: apitoken.UserAPIToken{ID: "credential", Value: "private-created-secret", CreatedByID: "owner", Status: apitoken.UserTokenStatusKeyActive}}}
				policy := &boundaryTokenPolicy{limits: accesspolicy.TokenLimits{Permanent: 2}}
				switch tc.phase {
				case "entry":
					cancel()
				case "user":
					users.after = cancel
				case "inventory":
					api.afterCount = cancel
				case "create":
					api.afterCreate = cancel
				case "callback":
					policy.before = cancel
				case "commit":
					policy.after = cancel
				}
				config := &accessmanager.NewServiceRequest{UserService: users, ApiTokenService: api}
				if mode == "policy" {
					config.TokenPolicy = policy
				}
				got, err := accessmanager.NewService(config).CreateUserAPIToken(ctx, &accessmanager.CreateUserAPITokenRequest{UserID: "owner"})
				require.ErrorIs(t, err, context.Canceled)
				require.Nil(t, got)
				require.Equal(t, tc.reads, users.calls)
				require.Equal(t, tc.counts, api.counts)
				require.Equal(t, tc.creates, api.creates)
			})
		}
	}
}

func TestTokenThresholdCancellationBoundaries(t *testing.T) {
	for _, mode := range []string{"legacy", "policy"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			t.Cleanup(cancel)
			config := &accessmanager.NewServiceRequest{UserService: &boundaryTokenUser{after: cancel}}
			if mode == "policy" {
				config.TokenPolicy = &boundaryTokenPolicy{before: cancel}
			}
			got, err := accessmanager.NewService(config).GetUserAPITokenThreshold(ctx, &accessmanager.GetUserAPITokenThresholdRequest{UserId: "owner"})
			require.ErrorIs(t, err, context.Canceled)
			require.Nil(t, got)
		})
	}
}

func TestTokenCreationAdapterResults(t *testing.T) {
	for _, mode := range []string{"legacy", "policy"} {
		for _, variant := range []string{"valid snapshot", "nil result", "missing ID", "missing secret", "wrong owner", "revoked", "nil transaction"} {
			if mode == "legacy" && variant == "nil transaction" {
				continue
			}
			t.Run(mode+"/"+variant, func(t *testing.T) {
				result := &apitoken.CreateAPITokenResponse{APIToken: apitoken.UserAPIToken{ID: "credential", Value: "private-created-secret", CreatedByID: "owner", Status: apitoken.UserTokenStatusKeyActive, ValueSHA: []byte{1, 2}}}
				switch variant {
				case "nil result":
					result = nil
				case "missing ID":
					result.APIToken.ID = ""
				case "missing secret":
					result.APIToken.Value = ""
				case "wrong owner":
					result.APIToken.CreatedByID = "other"
				case "revoked":
					result.APIToken.Status = apitoken.UserTokenStatusKeyRevoked
				}
				api := &boundaryTokenAPI{result: result}
				config := &accessmanager.NewServiceRequest{UserService: &boundaryTokenUser{}, ApiTokenService: api}
				if mode == "policy" {
					config.TokenPolicy = &boundaryTokenPolicy{limits: accesspolicy.TokenLimits{Permanent: 2}, nilTransaction: variant == "nil transaction"}
				}
				got, err := accessmanager.NewService(config).CreateUserAPIToken(context.Background(), &accessmanager.CreateUserAPITokenRequest{UserID: "owner"})
				if variant == "valid snapshot" {
					require.NoError(t, err)
					result.APIToken.ValueSHA[0] = 9
					require.Equal(t, []byte{1, 2}, got.UserAPIToken.ValueSHA)
				} else {
					require.ErrorIs(t, err, accessmanager.ErrTokenPolicyUnavailable)
					require.Nil(t, got)
				}
				if variant == "nil transaction" {
					require.Zero(t, api.creates)
				}
			})
		}
	}
}
