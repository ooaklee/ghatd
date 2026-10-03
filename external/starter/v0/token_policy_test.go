package starter

import (
	"context"
	"testing"

	"github.com/ooaklee/ghatd/external/accessmanager"
	accesshelpers "github.com/ooaklee/ghatd/external/accessmanager/helpers"
	"github.com/ooaklee/ghatd/external/accesspolicy"
	"github.com/ooaklee/ghatd/external/auth"
	userv2 "github.com/ooaklee/ghatd/external/user/v2"
	"github.com/stretchr/testify/require"
)

// startupTokenPolicy makes policy forwarding observable without opening stores.
type startupTokenPolicy struct{ denied bool }

func (s startupTokenPolicy) TokenLimits(context.Context, string) (accesspolicy.TokenLimits, error) {
	if s.denied {
		return accesspolicy.TokenLimits{}, accesspolicy.ErrDenied
	}
	return accesspolicy.TokenLimits{Permanent: 4}, nil
}
func (s startupTokenPolicy) WithTokenCreation(context.Context, string, func(context.Context, accesspolicy.TokenLimits) error) error {
	return accesspolicy.ErrDenied
}

// startupTokenUser supplies a live account independently of the configured
// policy. Other calls remain unwired so this stays a policy-forwarding test.
type startupTokenUser struct{ accessmanager.UserService }

func (*startupTokenUser) GetUserByID(context.Context, *userv2.GetUserByIDRequest) (*userv2.GetUserByIDResponse, error) {
	return &userv2.GetUserByIDResponse{User: &userv2.UniversalUser{ID: "owner", Status: userv2.AccountStatusKeyActive}}, nil
}

func TestStarterForwardsTokenPolicy(t *testing.T) {
	for _, tc := range []struct {
		name   string
		denied bool
	}{{"live limits", false}, {"denial is not replaced by roles", true}} {
		t.Run(tc.name, func(t *testing.T) {
			request := validServicesRequest(t)
			request.TokenPolicy = startupTokenPolicy{denied: tc.denied}
			services, err := NewServices(request)
			require.NoError(t, err)
			services.AccessManager.UserService = &startupTokenUser{}
			// Publish already-verified fixture identity; this test does not
			// exercise cryptographic authentication or a live account store.
			ctx := accesshelpers.TransitWith(context.Background(), "owner")
			ctx = accesshelpers.TransitAuthenticatedWith(ctx, true)
			ctx = accesshelpers.TransitSessionWith(ctx, &auth.TokenAccessDetails{UserID: "owner", AccessUUID: "test-session", TokenUse: auth.TokenUseAccess, IsAuthorized: true})
			got, err := services.AccessManager.GetUserAPITokenThreshold(ctx, &accessmanager.GetUserAPITokenThresholdRequest{ActorID: "owner", UserID: "owner"})
			if tc.denied {
				require.ErrorIs(t, err, accesspolicy.ErrDenied)
				require.Nil(t, got)
			} else {
				require.NoError(t, err)
				require.Equal(t, int64(4), got.PermanentUserTokenLimit)
			}
		})
	}
}
