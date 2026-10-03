package accesspolicymanager

import (
	"context"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/accesspolicy"
	userv2 "github.com/ooaklee/ghatd/external/user/v2"
	"github.com/stretchr/testify/require"
)

// managementStore implements only audited reads/CAS; accidental enforcement calls
// panic rather than pretending this unit fixture is a transactional store.
type managementStore struct {
	accesspolicy.Store
	grant             *accesspolicy.Grant
	readErr, writeErr error
	writes            int
	actor             string
}

func (s *managementStore) Read(context.Context, accesspolicy.Subject) (accesspolicy.Grant, error) {
	if s.readErr != nil {
		return accesspolicy.Grant{}, s.readErr
	}
	if s.grant == nil {
		return accesspolicy.Grant{}, accesspolicy.ErrDenied
	}
	return *s.grant, nil
}
func (s *managementStore) Replace(_ context.Context, g accesspolicy.Grant, expected int64, actor string, _ time.Time) (accesspolicy.Grant, error) {
	if s.writeErr != nil {
		return accesspolicy.Grant{}, s.writeErr
	}
	if s.grant == nil && expected != 0 || s.grant != nil && s.grant.Revision != expected {
		return accesspolicy.Grant{}, accesspolicy.ErrConflict
	}
	s.grant = &g
	s.writes++
	s.actor = actor
	return g, nil
}

type selectedUser struct {
	user  *userv2.UniversalUser
	err   error
	reads int
}

func (u *selectedUser) GetUserByID(context.Context, string) (*userv2.UniversalUser, error) {
	u.reads++
	return u.user, u.err
}

type preparedInventory struct {
	owners []string
	err    error
	before func()
}

func (p *preparedInventory) PrepareTokenInventory(_ context.Context, id string) error {
	p.owners = append(p.owners, id)
	if p.before != nil {
		p.before()
	}
	return p.err
}

func TestExplicitUserPolicyManagement(t *testing.T) {
	for _, tc := range []struct {
		name, variant    string
		want             error
		prepared, writes int
	}{
		{"preview is read only", "preview", nil, 0, 0},
		{"explicit first provisioning", "create", nil, 1, 1},
		{"update preserves unrelated authority", "update", nil, 1, 1},
		{"disabled grant stays disabled", "disabled", nil, 1, 1},
		{"no-op prepares missing inventory", "same", nil, 1, 0},
		{"stale revision prepares nothing", "stale", accesspolicy.ErrConflict, 0, 0},
		{"denial hides target existence", "denied", accesspolicy.ErrDenied, 0, 0},
		{"invalid actor", "actor", accesspolicy.ErrDenied, 0, 0},
		{"missing target", "missing", ErrUserNotFound, 0, 0},
		{"wrong stored target", "wrong", accesspolicy.ErrConfiguration, 0, 0},
		{"target dependency failure", "user-error", context.DeadlineExceeded, 0, 0},
		{"policy dependency failure", "read-error", context.DeadlineExceeded, 0, 0},
		{"invalid limits", "limits", ErrInvalidRequest, 0, 0},
		{"invalid target", "id", ErrInvalidRequest, 0, 0},
		{"invalid revision", "revision", ErrInvalidRequest, 0, 0},
		{"preparation failure", "prepare-error", context.DeadlineExceeded, 1, 0},
		{"revoked during preparation", "revoke", accesspolicy.ErrDenied, 1, 0},
		{"grant changed during preparation", "changed", accesspolicy.ErrConflict, 1, 0},
		{"write failure no success receipt", "write-error", context.DeadlineExceeded, 1, 0},
		{"canceled", "canceled", context.Canceled, 0, 0},
		{"nil context", "nil-context", accesspolicy.ErrConfiguration, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &managementStore{}
			users := &selectedUser{user: &userv2.UniversalUser{ID: "selected"}}
			inventory := &preparedInventory{}
			allowed := true
			actor := "operator"
			userID := "selected"
			expected := int64(0)
			ctx := context.Background()
			limits := accesspolicy.TokenLimits{Permanent: 3}
			original := accesspolicy.Grant{Subject: accesspolicy.Subject{System: "service", Kind: accesspolicy.UserSubject, ID: "selected"}, Revision: 4, Enabled: true, Scopes: []string{"items:read"}, Permissions: []string{"items:view"}, Limits: map[string]accesspolicy.Limit{"requests": {Maximum: 5, WindowSeconds: 60}}, Tokens: accesspolicy.TokenLimits{Permanent: 1}}
			switch tc.variant {
			case "update", "disabled", "same", "stale":
				store.grant = &original
				expected = 4
			}
			switch tc.variant {
			case "disabled":
				original.Enabled = false
			case "same":
				limits = original.Tokens
			case "stale":
				expected = 3
			case "denied":
				allowed = false
			case "actor":
				actor = ""
			case "missing":
				users.err = userv2.ErrUserNotFound
			case "wrong":
				users.user.ID = "other"
			case "user-error":
				users.err = context.DeadlineExceeded
			case "read-error":
				store.readErr = context.DeadlineExceeded
			case "limits":
				limits.Permanent = -1
			case "id":
				userID = "bad id"
			case "revision":
				expected = -1
			case "prepare-error":
				inventory.err = context.DeadlineExceeded
			case "revoke":
				inventory.before = func() { allowed = false }
			case "changed":
				inventory.before = func() { store.grant = &original }
			case "write-error":
				store.writeErr = context.DeadlineExceeded
			case "canceled":
				c, cancel := context.WithCancel(ctx)
				cancel()
				ctx = c
			case "nil-context":
				ctx = nil
			}
			service, err := NewService(Config{System: "service", Store: store, Users: users, Inventory: inventory, Authorize: func(_ context.Context, system string) (string, error) {
				require.Equal(t, "service", system)
				if !allowed {
					return "", accesspolicy.ErrDenied
				}
				return actor, nil
			}})
			require.NoError(t, err)
			if tc.variant == "preview" {
				preview, err := service.Preview(ctx, userID, limits)
				require.NoError(t, err)
				require.Nil(t, preview.Before)
				require.Equal(t, limits, preview.After.Tokens)
			} else {
				got, err := service.Apply(ctx, userID, expected, limits)
				require.ErrorIs(t, err, tc.want)
				if err != nil {
					require.Zero(t, got)
				} else {
					require.Equal(t, limits, got.Tokens)
					require.Equal(t, "selected", got.Subject.ID)
					if expected > 0 {
						require.Equal(t, original.Scopes, got.Scopes)
						require.Equal(t, original.Permissions, got.Permissions)
						require.Equal(t, original.Limits, got.Limits)
						require.Equal(t, original.Enabled, got.Enabled)
					} else {
						require.Empty(t, got.Permissions)
						require.Empty(t, got.Scopes)
						require.Empty(t, got.Limits)
					}
				}
			}
			require.Len(t, inventory.owners, tc.prepared)
			require.Equal(t, tc.writes, store.writes)
			if tc.writes > 0 {
				require.Equal(t, "operator", store.actor)
			}
			if tc.variant == "denied" {
				require.Zero(t, users.reads)
			}
		})
	}
}

func TestManagementConstruction(t *testing.T) {
	for _, missing := range []string{"system", "store", "authorizer", "users", "inventory"} {
		t.Run(missing, func(t *testing.T) {
			config := Config{System: "service", Store: &managementStore{}, Authorize: func(context.Context, string) (string, error) { return "operator", nil }, Users: &selectedUser{}, Inventory: &preparedInventory{}}
			switch missing {
			case "system":
				config.System = ""
			case "store":
				config.Store = nil
			case "authorizer":
				config.Authorize = nil
			case "users":
				config.Users = nil
			case "inventory":
				config.Inventory = nil
			}
			got, err := NewService(config)
			require.ErrorIs(t, err, accesspolicy.ErrConfiguration)
			require.Nil(t, got)
		})
	}
}
