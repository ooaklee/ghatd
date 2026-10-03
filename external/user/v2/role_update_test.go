package user

import (
	"context"
	"fmt"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
)

// roleProbe exercises domain boundaries; real Mongo tests establish atomicity.
type roleProbe struct {
	UserRepository
	stored  *UniversalUser
	fault   string
	err     error
	writes  int
	command SetAccountRolesRequest
	cancel  context.CancelFunc
}

func (p *roleProbe) GetUserByID(context.Context, string) (*UniversalUser, error) {
	if p.fault == "read failure" {
		return nil, p.err
	}
	if p.fault == "cancel read" {
		p.cancel()
	}
	return p.stored, nil
}

func (p *roleProbe) SetAccountRoles(_ context.Context, c *SetAccountRolesRequest) (*UniversalUser, error) {
	p.writes++
	p.command = copyRoleCommand(*c)
	if p.fault == "write failure" {
		return nil, p.err
	}
	v := copyUserForUpdate(p.stored)
	v.Roles = slices.Clone(c.Roles)
	if !slices.Equal(c.PreviousRoles, c.Roles) {
		if v.Metadata == nil {
			v.Metadata = &UserMetadata{}
		}
		v.Metadata.UpdatedAt = c.At
	}
	switch p.fault {
	case "nil receipt":
		return nil, nil
	case "mutated command":
		c.Roles[0] = "forged"
		v.Roles = c.Roles
	case "wrong ID":
		v.ID = "other"
	case "wrong email":
		v.Email = "changed@example.test"
	case "wrong revision":
		v.EmailRevision++
	case "wrong type":
		v.Type = "other"
	case "wrong status":
		v.Status = "other"
	case "wrong roles":
		v.Roles = []string{"other"}
	case "nil metadata":
		v.Metadata = nil
	case "wrong updated":
		v.Metadata.UpdatedAt = "other"
	case "cancel write":
		p.cancel()
	}
	return v, nil
}

func TestAccountRoleDomain(t *testing.T) {
	native := fmt.Errorf("private-native-diagnostic: %w", ErrRoleUpdateConflict)
	for _, remove := range []bool{false, true} {
		mode := "add"
		if remove {
			mode = "remove"
		}
		for _, tc := range []struct {
			name   string
			want   error
			writes int
		}{
			{"success", nil, 1}, {"legacy type", nil, 1}, {"legacy objects", nil, 1}, {"cancel write", nil, 1},
			{"nil service", ErrRoleUpdateUnavailable, 0}, {"nil context", ErrRoleUpdateUnavailable, 0}, {"nil repository", ErrRoleUpdateUnavailable, 0}, {"typed nil repository", ErrRoleUpdateUnavailable, 0}, {"missing capability", ErrRoleUpdateUnavailable, 0}, {"nil clock", ErrRoleUpdateUnavailable, 0}, {"zero clock", ErrRoleUpdateUnavailable, 0}, {"nil strings", ErrRoleUpdateUnavailable, 0},
			{"empty ID", ErrInvalidUserID, 0}, {"blank role", ErrUserInvalidRole, 0}, {"read failure", native, 0}, {"write failure", native, 1}, {"nil stored", ErrRoleUpdateUnavailable, 0}, {"wrong stored", ErrRoleUpdateUnavailable, 0}, {"negative revision", ErrRoleUpdateUnavailable, 0}, {"empty status", ErrRoleUpdateUnavailable, 0},
			{"canceled", context.Canceled, 0}, {"cancel read", context.Canceled, 0},
			{"nil receipt", ErrRoleUpdateUnavailable, 1}, {"mutated command", ErrRoleUpdateUnavailable, 1}, {"wrong ID", ErrRoleUpdateUnavailable, 1}, {"wrong email", ErrRoleUpdateUnavailable, 1}, {"wrong revision", ErrRoleUpdateUnavailable, 1}, {"wrong type", ErrRoleUpdateUnavailable, 1}, {"wrong status", ErrRoleUpdateUnavailable, 1}, {"wrong roles", ErrRoleUpdateUnavailable, 1}, {"nil metadata", ErrRoleUpdateUnavailable, 1}, {"wrong updated", ErrRoleUpdateUnavailable, 1},
		} {
			t.Run(mode+"/"+tc.name, func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				p := &roleProbe{stored: genericAccount(), fault: tc.name, err: native, cancel: cancel}
				p.stored.Roles = []string{"USER"}
				if remove {
					p.stored.Roles = []string{"USER", "ADMIN", "ADMIN"}
				}
				s := NewService(p, nil, nil, &DefaultIDGenerator{}, profileClock{stamp: "2026-10-03T18:00:00Z"}, &DefaultStringUtils{}, "")
				id, role := "selected", "ADMIN"
				switch tc.name {
				case "legacy type":
					p.stored.Type = ""
				case "legacy objects":
					p.stored.Metadata = nil
					p.stored.Verification = nil
				case "nil service":
					s = nil
				case "nil context":
					ctx = nil
				case "nil repository":
					s.UserRepository = nil
				case "typed nil repository":
					s.UserRepository = (*roleProbe)(nil)
				case "missing capability":
					s.UserRepository = &genericUpdateProbe{}
				case "nil clock":
					s.TimeProvider = nil
				case "zero clock":
					s.TimeProvider = profileClock{}
				case "nil strings":
					s.StringUtils = nil
				case "empty ID":
					id = " "
				case "blank role":
					role = " "
				case "nil stored":
					p.stored = nil
				case "wrong stored":
					p.stored.ID = "other"
				case "negative revision":
					p.stored.EmailRevision = -1
				case "empty status":
					p.stored.Status = ""
				case "canceled":
					cancel()
				}
				before := copyUserForUpdate(p.stored)
				v, changed, err := s.updateAccountRole(ctx, id, role, remove)
				require.Equal(t, tc.want, err)
				require.Equal(t, tc.writes, p.writes)
				require.Equal(t, before, p.stored)
				if err != nil {
					require.Nil(t, v)
					require.False(t, changed)
					return
				}
				require.True(t, changed)
				require.Equal(t, !remove, slices.Contains(v.Roles, "ADMIN"))
				require.Equal(t, "USER", v.Roles[0])
				require.Equal(t, before.Type, p.command.Account.Type)
				require.Equal(t, "default", v.Type)
			})
		}
	}
}

func TestAccountRoleConfiguredSemantics(t *testing.T) {
	for _, tc := range []struct {
		name    string
		before  []string
		role    string
		remove  bool
		allowed []string
		after   []string
		changed bool
		want    error
	}{
		{"add", []string{"USER"}, "ADMIN", false, []string{"USER", "ADMIN"}, []string{"USER", "ADMIN"}, true, nil},
		{"duplicate addition", []string{"ADMIN", "USER"}, "ADMIN", false, []string{"ADMIN"}, []string{"ADMIN", "USER"}, false, nil},
		{"absent removal", []string{"USER"}, "ADMIN", true, nil, []string{"USER"}, false, nil},
		{"empty removal", nil, "ADMIN", true, nil, nil, false, nil},
		{"remove duplicates", []string{"ADMIN", "USER", "ADMIN", "VIEWER"}, "ADMIN", true, nil, []string{"USER", "VIEWER"}, true, nil},
		{"remove obsolete", []string{"OLD"}, "OLD", true, []string{"USER"}, []string{}, true, nil},
		{"reject invalid", []string{"USER"}, "OTHER", false, []string{"USER"}, nil, false, ErrUserInvalidRole},
		{"reject already present invalid", []string{"OTHER"}, "OTHER", false, []string{"USER"}, nil, false, ErrUserInvalidRole},
		{"unrestricted config", nil, "CUSTOM", false, nil, []string{"CUSTOM"}, true, nil},
		{"case sensitive", nil, "admin", false, []string{"ADMIN"}, nil, false, ErrUserInvalidRole},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := &roleProbe{stored: genericAccount()}
			p.stored.Roles = slices.Clone(tc.before)
			p.stored.Metadata.UpdatedAt = "prior"
			config := DefaultUserConfig()
			config.ValidRoles = tc.allowed
			s := NewService(p, nil, config, &DefaultIDGenerator{}, profileClock{stamp: "2026-10-03T18:00:00Z"}, &DefaultStringUtils{}, "")
			var v *UniversalUser
			var changed bool
			var err error
			if tc.remove {
				var r *RemoveUserRoleResponse
				r, err = s.RemoveUserRole(context.Background(), &RemoveUserRoleRequest{ID: "selected", Role: tc.role})
				if r != nil {
					v, changed = r.User, r.Changed
				}
			} else {
				var r *AddUserRoleResponse
				r, err = s.AddUserRole(context.Background(), &AddUserRoleRequest{ID: "selected", Role: tc.role})
				if r != nil {
					v, changed = r.User, r.Changed
				}
			}
			require.Equal(t, tc.want, err)
			if err != nil {
				require.Zero(t, p.writes)
				return
			}
			require.Equal(t, tc.changed, changed)
			require.True(t, slices.Equal(tc.after, v.Roles))
			require.True(t, slices.Equal(tc.before, p.stored.Roles))
			if !changed {
				require.Equal(t, "prior", v.Metadata.UpdatedAt)
			}
		})
	}
}

func TestAccountRoleEntryGuards(t *testing.T) {
	for _, remove := range []bool{false, true} {
		t.Run(fmt.Sprintf("remove=%v", remove), func(t *testing.T) {
			var s *Service
			if remove {
				r, err := s.RemoveUserRole(context.Background(), nil)
				require.Nil(t, r)
				require.ErrorIs(t, err, ErrInvalidUserBody)
			} else {
				r, err := s.AddUserRole(context.Background(), nil)
				require.Nil(t, r)
				require.ErrorIs(t, err, ErrInvalidUserBody)
			}
		})
	}
}

func TestAccountRoleRepositoryGuards(t *testing.T) {
	for _, tc := range []struct {
		name string
		want error
	}{
		{"nil receiver", ErrRoleUpdateUnavailable}, {"nil context", ErrRoleUpdateUnavailable}, {"nil store", ErrRoleUpdateUnavailable}, {"typed nil store", ErrRoleUpdateUnavailable}, {"missing capability", ErrRoleUpdateUnavailable},
		{"nil command", ErrInvalidUserBody}, {"blank ID", ErrInvalidUserBody}, {"empty status", ErrInvalidUserBody}, {"negative revision", ErrInvalidUserBody}, {"invalid time", ErrInvalidUserBody}, {"non UTC", ErrInvalidUserBody}, {"zero time", ErrInvalidUserBody}, {"canceled", context.Canceled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			p := &emailStorageSetupProbe{}
			r := NewRepository(p)
			c := &SetAccountRolesRequest{Account: AccountSnapshot{UserID: "target", Status: "ACTIVE"}, At: "2026-10-03T18:00:00Z"}
			switch tc.name {
			case "nil receiver":
				r = nil
			case "nil context":
				ctx = nil
			case "nil store":
				r.Store = nil
			case "typed nil store":
				r.Store = (*emailStorageSetupProbe)(nil)
			case "nil command":
				c = nil
			case "blank ID":
				c.Account.UserID = " "
			case "empty status":
				c.Account.Status = ""
			case "negative revision":
				c.Account.EmailRevision = -1
			case "invalid time":
				c.At = "bad"
			case "non UTC":
				c.At = "2026-10-03T19:00:00+01:00"
			case "zero time":
				c.At = "0001-01-01T00:00:00Z"
			case "canceled":
				cancel()
			}
			v, err := r.SetAccountRoles(ctx, c)
			require.Nil(t, v)
			require.Equal(t, tc.want, err)
			require.Zero(t, p.initCalls)
		})
	}
}
