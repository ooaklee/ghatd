package accesspolicymanager

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/accesspolicy"
	userv2 "github.com/ooaklee/ghatd/external/user/v2"
	"github.com/stretchr/testify/require"
)

// targetFailure models adapters whose Is classification must not establish
// authoritative absence when their actual cause is unknown or malformed.
type targetFailure struct{ cause error }

func (*targetFailure) Error() string     { return "private target diagnostic" }
func (e *targetFailure) Unwrap() error   { return e.cause }
func (*targetFailure) Is(err error) bool { return err == userv2.ErrUserNotFound }

func TestTargetLookupFailureBoundaries(t *testing.T) {
	var typedNil *targetFailure
	cycle := &targetFailure{}
	cycle.cause = cycle
	deep := error(userv2.ErrUserNotFound)
	for range 64 {
		deep = fmt.Errorf("private wrapper: %w", deep)
	}
	for _, tc := range []struct {
		name    string
		cause   error
		missing bool
	}{
		{"absent", userv2.ErrUserNotFound, true},
		{"wrapped absent", fmt.Errorf("private lookup: %w", userv2.ErrUserNotFound), true},
		{"joined outage", errors.Join(userv2.ErrUserNotFound, errors.New("private outage")), false},
		{"singleton join", errors.Join(userv2.ErrUserNotFound), false},
		{"custom Is outage", &targetFailure{cause: errors.New("private outage")}, false},
		{"typed nil", typedNil, false}, {"cycle", cycle, false}, {"over depth", deep, false},
	} {
		for _, mode := range []string{"preview", "apply"} {
			t.Run(tc.name+"/"+mode, func(t *testing.T) {
				store, inventory := &managementStore{}, &preparedInventory{}
				s, err := NewService(Config{System: "service", Store: store, Users: &selectedUser{err: tc.cause}, Inventory: inventory, Authorize: func(context.Context, string) (string, error) { return "operator", nil }})
				require.NoError(t, err)
				require.NotPanics(t, func() {
					if mode == "preview" {
						got, e := s.Preview(context.Background(), "selected", accesspolicy.TokenLimits{})
						require.Zero(t, got)
						err = e
					} else {
						got, e := s.Apply(context.Background(), "selected", 0, accesspolicy.TokenLimits{})
						require.Zero(t, got)
						err = e
					}
				})
				if tc.missing {
					require.Equal(t, ErrUserNotFound, err)
				} else {
					require.True(t, err == tc.cause, "retain original failure, not an absence classification")
				}
				require.Empty(t, inventory.owners)
				require.Zero(t, store.writes)
			})
		}
	}
}

// cancellationUser models an adapter that returns a result after cancellation.
type cancellationUser struct{ after func() }

func (u cancellationUser) GetUserByID(context.Context, string) (*userv2.UniversalUser, error) {
	u.after()
	return &userv2.UniversalUser{ID: "selected"}, nil
}

// committedManagementStore cancels only after its write has succeeded. This is
// distinct from a driver returning an unknown commit or cancellation error.
type committedManagementStore struct {
	*managementStore
	after func()
}

func (s committedManagementStore) Replace(ctx context.Context, g accesspolicy.Grant, rev int64, actor string, at time.Time) (accesspolicy.Grant, error) {
	result, err := s.managementStore.Replace(ctx, g, rev, actor, at)
	s.after()
	return result, err
}

func TestManagementCancellationBoundaries(t *testing.T) {
	for _, phase := range []string{"authorize", "lookup", "preparation", "known commit"} {
		t.Run(phase, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			t.Cleanup(cancel)
			store := &managementStore{}
			inventory := &preparedInventory{}
			users := &selectedUser{user: &userv2.UniversalUser{ID: "selected"}}
			config := Config{System: "service", Store: store, Users: users, Inventory: inventory, Authorize: func(context.Context, string) (string, error) {
				if phase == "authorize" {
					cancel()
				}
				return "operator", nil
			}}
			switch phase {
			case "lookup":
				config.Users = cancellationUser{after: cancel}
			case "preparation":
				inventory.before = cancel
			case "known commit":
				config.Store = committedManagementStore{managementStore: store, after: cancel}
			}
			s, err := NewService(config)
			require.NoError(t, err)
			grant, err := s.Apply(ctx, "selected", 0, accesspolicy.TokenLimits{Permanent: 1})
			if phase == "known commit" {
				require.NoError(t, err)
				require.Equal(t, int64(1), grant.Revision)
				require.Equal(t, 1, store.writes)
			} else {
				require.ErrorIs(t, err, context.Canceled)
				require.Zero(t, grant)
				require.Zero(t, store.writes)
			}
			if phase == "authorize" {
				require.Zero(t, users.reads)
			}
			if phase == "authorize" || phase == "lookup" {
				require.Empty(t, inventory.owners)
			}
			require.ErrorIs(t, ctx.Err(), context.Canceled)
		})
	}
}

func TestTargetLookupInconsistentResults(t *testing.T) {
	for _, tc := range []struct {
		name string
		user *userv2.UniversalUser
	}{
		{"nil result", nil}, {"wrong owner", &userv2.UniversalUser{ID: "other"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, err := NewService(Config{System: "service", Store: &managementStore{}, Users: &selectedUser{user: tc.user}, Inventory: &preparedInventory{}, Authorize: func(context.Context, string) (string, error) { return "operator", nil }})
			require.NoError(t, err)
			got, err := s.Preview(context.Background(), "selected", accesspolicy.TokenLimits{})
			require.ErrorIs(t, err, accesspolicy.ErrConfiguration)
			require.Zero(t, got)
		})
	}
}
