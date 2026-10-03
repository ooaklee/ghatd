package starter

import (
	"context"
	"testing"

	"github.com/ooaklee/ghatd/external/accessmanager"
	"github.com/ooaklee/ghatd/external/accesspolicy"
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
			got, err := services.AccessManager.GetUserAPITokenThreshold(context.Background(), &accessmanager.GetUserAPITokenThresholdRequest{UserId: "owner"})
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
