package accessmanager_test

import (
	"context"
	"errors"
	"fmt"
	"math"
	"testing"

	"github.com/ooaklee/ghatd/external/accessmanager"
	"github.com/ooaklee/ghatd/external/accesspolicy"
	"github.com/ooaklee/ghatd/external/apitoken"
	userv2 "github.com/ooaklee/ghatd/external/user/v2"
	"github.com/stretchr/testify/require"
)

type tokenTransactionKey struct{}

// creationPolicyStub simulates callback retry and commit failure separately.
// It is not evidence of storage atomicity; the integration matrix proves that.
type creationPolicyStub struct {
	limits            accesspolicy.TokenLimits
	err, commitError  error
	noCallback, retry bool
}

func (s *creationPolicyStub) TokenLimits(context.Context, string) (accesspolicy.TokenLimits, error) {
	return s.limits, s.err
}
func (s *creationPolicyStub) WithTokenCreation(ctx context.Context, owner string, create func(context.Context, accesspolicy.TokenLimits) error) error {
	if s.err != nil {
		return s.err
	}
	if s.noCallback {
		return nil
	}
	tx := context.WithValue(ctx, tokenTransactionKey{}, owner)
	if s.retry {
		if err := create(tx, s.limits); err != nil {
			return err
		}
	}
	if err := create(tx, s.limits); err != nil {
		return err
	}
	return s.commitError
}

// creationUserStub makes the current target account deterministic per case.
type creationUserStub struct {
	accessmanager.UserService
	user               *userv2.UniversalUser
	requireTransaction bool
}

func (s *creationUserStub) GetUserByID(ctx context.Context, req *userv2.GetUserByIDRequest) (*userv2.GetUserByIDResponse, error) {
	if s.requireTransaction && ctx.Value(tokenTransactionKey{}) != req.ID {
		panic("user read escaped transaction")
	}
	return &userv2.GetUserByIDResponse{User: s.user}, nil
}

// creationAPIStub refuses accidental list-based counting and checks that counts
// and insertion receive the policy context, not the outer request context.
type creationAPIStub struct {
	accessmanager.ApitokenService
	count               apitoken.Inventory
	countErr, createErr error
	created             int
	requireTransaction  bool
	fullAfterFirst      bool
}

func (s *creationAPIStub) CountTokenInventory(ctx context.Context, owner string) (apitoken.Inventory, error) {
	if s.requireTransaction && ctx.Value(tokenTransactionKey{}) != owner {
		panic("count escaped transaction")
	}
	if s.fullAfterFirst && s.created > 0 {
		return apitoken.Inventory{Permanent: 2}, nil
	}
	return s.count, s.countErr
}

// CountTokenInventoryFenced models the transaction-aware adapter contract.
// Real shared-lock behavior is checked by the Mongo integration matrix.
func (s *creationAPIStub) CountTokenInventoryFenced(ctx context.Context, owner string) (apitoken.Inventory, error) {
	return s.CountTokenInventory(ctx, owner)
}
func (s *creationAPIStub) CreateAPIToken(ctx context.Context, req *apitoken.CreateAPITokenRequest) (*apitoken.CreateAPITokenResponse, error) {
	if s.requireTransaction && ctx.Value(tokenTransactionKey{}) != req.UserID {
		panic("insert escaped transaction")
	}
	s.created++
	if s.createErr != nil {
		return nil, s.createErr
	}
	return &apitoken.CreateAPITokenResponse{APIToken: apitoken.UserAPIToken{ID: fmt.Sprintf("token-%d", s.created), Value: "new-secret", CreatedByID: req.UserID, Status: apitoken.UserTokenStatusKeyActive}}, nil
}

func TestPolicyTokenCreationBoundaries(t *testing.T) {
	storageFailure := errors.New("storage unavailable")
	for _, tc := range []struct {
		name                                                                                             string
		ttl                                                                                              int64
		permanent, ephemeral                                                                             int64
		policyErr, commitErr                                                                             error
		inactive, wrongOwner, countFailure, createFailure, retry, noCallback, disabled, invalidIncrement bool
		want                                                                                             error
		creates                                                                                          int
		retryThenDeny                                                                                    bool
	}{
		{name: "permanent", creates: 1}, {name: "minimum ttl", ttl: 60, creates: 1}, {name: "maximum ttl", ttl: 3600, creates: 1},
		{name: "too short", ttl: 59, want: accessmanager.ErrCreateUserAPITokenRequestTtlTooShort}, {name: "too long", ttl: 3601, want: accessmanager.ErrCreateUserAPITokenRequestTtlTooLong},
		{name: "wrong increment", ttl: 61, want: accessmanager.ErrCreateUserAPITokenRequestTtlOutsideAllowedIncrement}, {name: "negative", ttl: -1, want: apitoken.ErrInvalidTokenTTL},
		{name: "overflow", ttl: math.MaxInt64, want: accessmanager.ErrCreateUserAPITokenRequestTtlTooLong},
		{name: "at permanent limit", permanent: 2, want: accessmanager.ErrPermanentAPITokenLimitReached}, {name: "beyond first page", permanent: 150, want: accessmanager.ErrPermanentAPITokenLimitReached},
		{name: "at ephemeral limit", ttl: 60, ephemeral: 2, want: accessmanager.ErrEphemeralAPITokenLimitReached},
		{name: "missing grant never falls back to ADMIN", policyErr: accesspolicy.ErrDenied, want: accesspolicy.ErrDenied},
		{name: "configuration fails closed", policyErr: accesspolicy.ErrConfiguration, want: accesspolicy.ErrConfiguration},
		{name: "policy outage never falls back", policyErr: storageFailure, want: storageFailure},
		{name: "commit failure hides secret", commitErr: storageFailure, want: storageFailure, creates: 1},
		{name: "callback retry publishes last attempt", retry: true, creates: 2},
		{name: "retry becomes full and hides prior secret", retry: true, retryThenDeny: true, creates: 1, want: accessmanager.ErrPermanentAPITokenLimitReached},
		{name: "negative permanent count", permanent: -1, want: accessmanager.ErrTokenPolicyUnavailable},
		{name: "negative ephemeral count", ephemeral: -1, want: accessmanager.ErrTokenPolicyUnavailable},
		{name: "no callback cannot report success", noCallback: true, want: accessmanager.ErrTokenPolicyUnavailable},
		{name: "inactive account", inactive: true, want: accessmanager.ErrForbiddenUnableToAction}, {name: "wrong stored owner", wrongOwner: true, want: accessmanager.ErrForbiddenUnableToAction},
		{name: "count failure", countFailure: true, want: storageFailure}, {name: "create failure", createFailure: true, want: storageFailure, creates: 1},
		{name: "disabled ephemeral", ttl: 60, disabled: true, want: accessmanager.ErrEphemeralAPITokenLimitReached}, {name: "zero increment cannot panic", ttl: 60, invalidIncrement: true, want: accessmanager.ErrTokenPolicyUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			policy := &creationPolicyStub{limits: accesspolicy.TokenLimits{Permanent: 2, Ephemeral: 2, MinimumTTL: 60, MaximumTTL: 3600, TTLIncrement: 60}, err: tc.policyErr, commitError: tc.commitErr, retry: tc.retry, noCallback: tc.noCallback}
			if tc.disabled {
				policy.limits.Ephemeral = 0
			}
			if tc.invalidIncrement {
				policy.limits.TTLIncrement = 0
			}
			user := &userv2.UniversalUser{ID: "owner", NanoID: "prefix", Status: userv2.AccountStatusKeyActive, Roles: []string{"ADMIN"}}
			if tc.inactive {
				user.Status = userv2.AccountStatusKeyProvisioned
			}
			if tc.wrongOwner {
				user.ID = "other"
			}
			api := &creationAPIStub{count: apitoken.Inventory{Permanent: tc.permanent, Ephemeral: tc.ephemeral}, requireTransaction: true, fullAfterFirst: tc.retryThenDeny}
			if tc.countFailure {
				api.countErr = storageFailure
			}
			if tc.createFailure {
				api.createErr = storageFailure
			}
			service := accessmanager.NewService(&accessmanager.NewServiceRequest{TokenPolicy: policy, ApiTokenService: api, UserService: &creationUserStub{user: user, requireTransaction: true}})
			got, err := service.CreateUserAPIToken(context.Background(), &accessmanager.CreateUserAPITokenRequest{UserID: "owner", Ttl: tc.ttl})
			if tc.want != nil {
				require.ErrorIs(t, err, tc.want)
				require.Nil(t, got)
			} else {
				require.NoError(t, err)
				require.Equal(t, fmt.Sprintf("token-%d", tc.creates), got.UserAPIToken.ID)
			}
			require.Equal(t, tc.creates, api.created)
		})
	}
}

func TestPolicyTokenThresholds(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want error
	}{
		{"current grant", nil, nil}, {"missing grant", accesspolicy.ErrDenied, accesspolicy.ErrDenied}, {"invalid configuration", accesspolicy.ErrConfiguration, accesspolicy.ErrConfiguration},
	} {
		t.Run(tc.name, func(t *testing.T) {
			policy := &creationPolicyStub{limits: accesspolicy.TokenLimits{Permanent: 7, Ephemeral: 3, MinimumTTL: 120, MaximumTTL: 7200, TTLIncrement: 60}, err: tc.err}
			service := accessmanager.NewService(&accessmanager.NewServiceRequest{TokenPolicy: policy}) // no UserService: role fallback would panic
			got, err := service.GetUserAPITokenThreshold(context.Background(), &accessmanager.GetUserAPITokenThresholdRequest{UserId: "owner"})
			if tc.want != nil {
				require.ErrorIs(t, err, tc.want)
				require.Nil(t, got)
			} else {
				require.NoError(t, err)
				require.Equal(t, int64(7), got.PermanentUserTokenLimit)
				require.Equal(t, int64(3), got.EphemeralUserTokenLimit)
				require.Equal(t, int64(120), got.EphemeralMinimumAllowedTime)
			}
		})
	}
}

func TestInvalidLimitsFailClosedForDisplay(t *testing.T) {
	for _, tc := range []struct {
		name   string
		limits accesspolicy.TokenLimits
	}{
		{"negative permanent", accesspolicy.TokenLimits{Permanent: -1}},
		{"ephemeral without ttl policy", accesspolicy.TokenLimits{Ephemeral: 1}},
		{"negative ttl metadata", accesspolicy.TokenLimits{MinimumTTL: -1}},
		{"no valid increment within range", accesspolicy.TokenLimits{Ephemeral: 1, MinimumTTL: 61, MaximumTTL: 119, TTLIncrement: 60}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			service := accessmanager.NewService(&accessmanager.NewServiceRequest{TokenPolicy: &creationPolicyStub{limits: tc.limits}})
			got, err := service.GetUserAPITokenThreshold(context.Background(), &accessmanager.GetUserAPITokenThresholdRequest{UserId: "owner"})
			require.ErrorIs(t, err, accessmanager.ErrTokenPolicyUnavailable)
			require.Nil(t, got)
		})
	}
}

func TestLegacyAdmissionRequiresExactInventoryAndPresentOwner(t *testing.T) {
	for _, tc := range []struct {
		name                                string
		missingUser, missingInventory, full bool
		want                                error
	}{
		{"missing account", true, false, false, accessmanager.ErrForbiddenUnableToAction},
		{"custom adapter without exact count", false, true, false, accessmanager.ErrTokenPolicyUnavailable},
		{"count beyond display page", false, false, true, accessmanager.ErrPermanentAPITokenLimitReached},
	} {
		t.Run(tc.name, func(t *testing.T) {
			user := &userv2.UniversalUser{ID: "owner", NanoID: "prefix", Status: userv2.AccountStatusKeyActive, Roles: []string{"MAX"}}
			if tc.missingUser {
				user = nil
			}
			api := &creationAPIStub{}
			if tc.full {
				api.count.Permanent = 150
			}
			var port accessmanager.ApitokenService = api
			if tc.missingInventory {
				port = &verifiedAPIStub{}
			}
			service := accessmanager.NewService(&accessmanager.NewServiceRequest{UserService: &creationUserStub{user: user}, ApiTokenService: port})
			got, err := service.CreateUserAPIToken(context.Background(), &accessmanager.CreateUserAPITokenRequest{UserID: "owner"})
			require.ErrorIs(t, err, tc.want)
			require.Nil(t, got)
			require.Zero(t, api.created)
			if tc.missingUser {
				display, err := service.GetUserAPITokenThreshold(context.Background(), &accessmanager.GetUserAPITokenThresholdRequest{UserId: "owner"})
				require.ErrorIs(t, err, accessmanager.ErrForbiddenUnableToAction)
				require.Nil(t, display)
			}
		})
	}
}

// countOnlyAPI deliberately exposes the old capability without a write fence.
type countOnlyAPI struct{ accessmanager.ApitokenService }

func (*countOnlyAPI) CountTokenInventory(context.Context, string) (apitoken.Inventory, error) {
	return apitoken.Inventory{}, nil
}

func TestPolicyAdmissionRejectsUnfencedAdapters(t *testing.T) {
	for _, tc := range []struct {
		name string
		api  accessmanager.ApitokenService
	}{
		{"no inventory capability", &verifiedAPIStub{}},
		{"exact count but no inventory fence", &countOnlyAPI{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := accessmanager.NewService(&accessmanager.NewServiceRequest{ApiTokenService: tc.api, TokenPolicy: &creationPolicyStub{limits: accesspolicy.TokenLimits{Permanent: 1}}, UserService: &creationUserStub{user: &userv2.UniversalUser{ID: "owner", Status: userv2.AccountStatusKeyActive}}})
			got, err := s.CreateUserAPIToken(context.Background(), &accessmanager.CreateUserAPITokenRequest{UserID: "owner"})
			require.ErrorIs(t, err, accessmanager.ErrTokenPolicyUnavailable)
			require.Nil(t, got)
		})
	}
}
