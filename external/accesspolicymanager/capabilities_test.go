package accesspolicymanager

import (
	"context"
	"errors"
	"github.com/ooaklee/ghatd/external/accesspolicy"
	user "github.com/ooaklee/ghatd/external/user/v2"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestCapabilityManagementSelectedAuthority(t *testing.T) {
	for _, tc := range []struct {
		name, variant string
		want          error
		reads, writes int
	}{
		{"explicit stored user provisioning", "create", nil, 1, 1},
		{"read only known absence", "review", nil, 1, 0},
		{"denial hides identity", "denied", accesspolicy.ErrDenied, 0, 0},
		{"missing target cannot provision", "missing", ErrUserNotFound, 1, 0},
		{"joined missing target and outage stays unavailable", "joined", context.DeadlineExceeded, 1, 0},
		{"wrong stored identity fails closed", "wrong", accesspolicy.ErrConfiguration, 1, 0},
		{"authority revoked after selected identity", "late", accesspolicy.ErrDenied, 1, 0},
		{"stale revision cannot provision", "stale", accesspolicy.ErrConflict, 1, 0},
		{"duplicate authority rejected before identity", "duplicate", ErrInvalidRequest, 0, 0},
		{"canceled request stops identity", "canceled", context.Canceled, 0, 0},
		{"nil context unavailable", "nil", accesspolicy.ErrConfiguration, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &managementStore{}
			inventory := &preparedInventory{}
			users := &selectedUser{user: &user.UniversalUser{ID: "selected"}}
			calls := 0
			ctx := context.Background()
			expected := int64(0)
			patch := accesspolicy.Capabilities{Enabled: true, Scopes: []string{"reviewed-scope"}, Permissions: []string{"exact-action"}}
			switch tc.variant {
			case "missing":
				users.err = user.ErrUserNotFound
			case "joined":
				users.err = errors.Join(user.ErrUserNotFound, context.DeadlineExceeded)
			case "wrong":
				users.user.ID = "another"
			case "stale":
				expected = 1
			case "duplicate":
				patch.Permissions = []string{"exact-action", "exact-action"}
			case "canceled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			case "nil":
				ctx = nil
			}
			service, err := NewService(Config{System: "platform", Store: store, Users: users, Inventory: inventory, Authorize: func(context.Context, string) (string, error) {
				calls++
				if tc.variant == "denied" || tc.variant == "late" && calls > 1 {
					return "", accesspolicy.ErrDenied
				}
				return "verified-operator", nil
			}})
			require.NoError(t, err)
			if tc.variant == "review" {
				result, e := service.ReviewCapabilities(ctx, "selected")
				err = e
				require.Nil(t, result)
			} else {
				result, e := service.ApplyCapabilities(ctx, "selected", expected, patch)
				err = e
				if tc.want != nil {
					require.Zero(t, result)
				} else {
					require.Equal(t, "selected", result.Subject.ID)
					require.Equal(t, "platform", result.Subject.System)
					require.Zero(t, result.Tokens)
				}
			}
			require.ErrorIs(t, err, tc.want)
			require.Equal(t, tc.reads, users.reads)
			require.Equal(t, tc.writes, store.writes)
			require.Empty(t, inventory.owners, "capability grants cannot provision token inventory")
		})
	}
}
