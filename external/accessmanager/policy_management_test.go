package accessmanager_test

import (
	"context"
	"errors"
	"testing"

	"github.com/ooaklee/ghatd/external/accessmanager"
	helpers "github.com/ooaklee/ghatd/external/accessmanager/helpers"
	"github.com/ooaklee/ghatd/external/accesspolicy"
	"github.com/ooaklee/ghatd/external/apitoken"
	"github.com/ooaklee/ghatd/external/auth"
	"github.com/ooaklee/ghatd/external/ephemeral"
	userv2 "github.com/ooaklee/ghatd/external/user/v2"
	"github.com/stretchr/testify/require"
)

func TestPolicyManagementRequiresLiveAdministratorSession(t *testing.T) {
	for _, variant := range []string{"allowed", "signed admin false", "anonymous", "API credential", "mixed credentials", "wrong system", "revoked", "wrong session owner", "demoted", "disabled", "email changed", "type changed", "user missing", "user dependency failure", "canceled", "nil context"} {
		t.Run(variant, func(t *testing.T) {
			user := &userv2.UniversalUser{ID: "operator", Status: userv2.AccountStatusKeyActive, Roles: []string{"ADMIN"}, Type: "web_app", EmailRevision: 2}
			session := &auth.TokenAccessDetails{UserID: user.ID, AccessUUID: "live-session", TokenUse: auth.TokenUseAccess, UserType: user.Type, EmailRevision: 2, IsAdmin: true}
			ctx := helpers.TransitWith(context.Background(), user.ID)
			ctx = helpers.TransitAuthenticatedWith(ctx, true)
			ctx = helpers.TransitSessionWith(ctx, session)
			owner, fetchErr := user.ID, error(nil)
			users := &refreshUserServiceMock{user: user}
			service := &accessmanager.Service{UserService: users, EphemeralStore: &refreshEphemeralStoreMock{fetchAuthFunc: func(context.Context, ephemeral.TokenDetailsAccess) (string, error) { return owner, fetchErr }}}
			authorize, err := service.PolicyManagementAuthorizer("sample")
			require.NoError(t, err)
			system := "sample"
			switch variant {
			case "signed admin false":
				session.IsAdmin = false
				ctx = helpers.TransitSessionWith(ctx, session)
			case "anonymous":
				ctx = helpers.TransitAuthenticatedWith(ctx, false)
			case "API credential", "mixed credentials":
				ctx = helpers.TransitAPITokenWith(ctx, &apitoken.CredentialDetails{UserID: user.ID, TokenID: "credential"})
				if variant == "API credential" {
					ctx = helpers.TransitSessionWith(ctx, nil)
				}
			case "wrong system":
				system = "another"
			case "revoked":
				fetchErr = errors.New("session absent")
			case "wrong session owner":
				owner = "other"
			case "demoted":
				user.Roles = nil
			case "disabled":
				user.Status = userv2.AccountStatusKeySuspended
			case "email changed":
				user.EmailRevision++
			case "type changed":
				user.Type = "api_service"
			case "user missing":
				users.user = nil
			case "user dependency failure":
				users.getUserByIDFunc = func(context.Context, *userv2.GetUserByIDRequest) (*userv2.GetUserByIDResponse, error) {
					return nil, context.DeadlineExceeded
				}
			case "canceled":
				canceled, cancel := context.WithCancel(ctx)
				cancel()
				ctx = canceled
			case "nil context":
				ctx = nil
			}
			actor, err := authorize(ctx, system)
			if variant == "allowed" || variant == "signed admin false" {
				require.NoError(t, err)
				require.Equal(t, "operator", actor)
				user.Roles = nil
				actor, err = authorize(ctx, system)
				require.ErrorIs(t, err, accesspolicy.ErrDenied)
				require.Empty(t, actor)
			} else {
				require.Error(t, err)
				require.Empty(t, actor)
				if variant == "user dependency failure" {
					require.ErrorIs(t, err, context.DeadlineExceeded)
				}
			}
		})
	}
}
