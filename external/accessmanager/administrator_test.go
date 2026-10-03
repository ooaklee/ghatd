package accessmanager_test

import (
	"context"
	"errors"
	"testing"

	"github.com/ooaklee/ghatd/external/accessmanager"
	helpers "github.com/ooaklee/ghatd/external/accessmanager/helpers"
	"github.com/ooaklee/ghatd/external/apitoken"
	"github.com/ooaklee/ghatd/external/auth"
	"github.com/ooaklee/ghatd/external/ephemeral"
	user "github.com/ooaklee/ghatd/external/user/v2"
	"github.com/stretchr/testify/require"
)

func TestAdministratorAuthority(t *testing.T) {
	for _, name := range []string{"current", "signed admin false", "anonymous", "bare identity", "API", "mixed", "demoted", "disabled", "revision changed", "type changed", "revoked", "wrong store owner", "native failure", "nil service", "nil context", "typed nil users", "typed nil store", "canceled"} {
		t.Run(name, func(t *testing.T) {
			account := &user.UniversalUser{ID: "operator", Type: "default", Status: "ACTIVE", EmailRevision: 2, Roles: []string{"ADMIN"}}
			token := &auth.TokenAccessDetails{UserID: "operator", AccessUUID: "session", TokenUse: auth.TokenUseAccess, UserType: "default", EmailRevision: 2, IsAdmin: name != "signed admin false"}
			ctx := helpers.TransitWith(context.Background(), "operator")
			ctx = helpers.TransitAuthenticatedWith(ctx, true)
			ctx = helpers.TransitSessionWith(ctx, token)
			owner := "operator"
			var fetchErr error
			s := &accessmanager.Service{UserService: &refreshUserServiceMock{user: account}, EphemeralStore: &refreshEphemeralStoreMock{fetchAuthFunc: func(context.Context, ephemeral.TokenDetailsAccess) (string, error) { return owner, fetchErr }}}
			native := errors.New("private-store-failure")
			switch name {
			case "anonymous":
				ctx = helpers.TransitAuthenticatedWith(ctx, false)
			case "bare identity":
				ctx = helpers.TransitSessionWith(ctx, nil)
			case "API", "mixed":
				ctx = helpers.TransitAPITokenWith(ctx, &apitoken.CredentialDetails{UserID: "operator", TokenID: "api"})
				if name == "API" {
					ctx = helpers.TransitSessionWith(ctx, nil)
				}
			case "demoted":
				account.Roles = nil
			case "disabled":
				account.Status = "SUSPENDED"
			case "revision changed":
				account.EmailRevision++
			case "type changed":
				account.Type = "other"
			case "revoked":
				fetchErr = ephemeral.ErrAuthNotFound
			case "wrong store owner":
				owner = "other"
			case "native failure":
				fetchErr = native
			case "nil service":
				s = nil
			case "nil context":
				ctx = nil
			case "typed nil users":
				s.UserService = (*refreshUserServiceMock)(nil)
			case "typed nil store":
				s.EphemeralStore = (*refreshEphemeralStoreMock)(nil)
			case "canceled":
				c, cancel := context.WithCancel(ctx)
				cancel()
				ctx = c
			}
			got, err := s.AuthorizeAdministrator(ctx)
			if name == "current" || name == "signed admin false" {
				require.NoError(t, err)
				require.Equal(t, "operator", got)
			} else {
				require.Error(t, err)
				require.Empty(t, got)
			}
			if name == "native failure" {
				require.Same(t, native, err)
			}
		})
	}
}
