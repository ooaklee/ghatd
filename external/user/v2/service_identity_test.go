package user

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

// identityRepositoryStub supplies independently controlled identity reads.
type identityRepositoryStub struct {
	UserRepository
	user   *UniversalUser
	err    error
	key    string
	calls  int
	cancel context.CancelFunc
}

func (r *identityRepositoryStub) read(key string) (*UniversalUser, error) {
	r.calls++
	r.key = key
	if r.cancel != nil {
		r.cancel()
	}
	return r.user, r.err
}
func (r *identityRepositoryStub) GetUserByID(_ context.Context, id string) (*UniversalUser, error) {
	return r.read(id)
}
func (r *identityRepositoryStub) GetUserByNanoID(_ context.Context, id string) (*UniversalUser, error) {
	return r.read(id)
}

func TestIdentityReadsPreserveFailureCauses(t *testing.T) {
	driverErr := errors.New("repository unavailable")
	for _, kind := range []string{"id", "nano-id"} {
		t.Run(kind, func(t *testing.T) {
			for _, tc := range []struct {
				name, variant string
				want          error
				calls         int
			}{
				{"current account", "", nil, 1},
				{"expected absence", "absent", ErrUserNotFound, 1},
				{"wrapped absence", "wrapped-absent", ErrUserNotFound, 1},
				{"nil result", "nil-result", ErrUserNotFound, 1},
				{"operational error", "driver", driverErr, 1},
				{"nil service", "nil-service", ErrDatabaseError, 0},
				{"nil repository", "nil-repository", ErrDatabaseError, 0},
				{"nil context", "nil-context", ErrDatabaseError, 0},
				{"canceled", "canceled", context.Canceled, 0},
				{"canceled during read", "cancel-read", context.Canceled, 1},
				{"nil request", "nil-request", ErrInvalidUserID, 0},
				{"empty identity", "empty-key", ErrInvalidUserID, 0},
			} {
				t.Run(tc.name, func(t *testing.T) {
					ctx, cancel := context.WithCancel(context.Background())
					defer cancel()
					repo := &identityRepositoryStub{user: &UniversalUser{ID: "stored-id", NanoID: "public-id"}}
					service := NewService(repo, nil, nil, nil, nil, nil, "")
					key, want := "lookup-id", tc.want
					switch tc.variant {
					case "absent":
						repo.err = ErrUserNotFound
					case "wrapped-absent":
						repo.err = fmt.Errorf("lookup: %w", ErrUserNotFound)
					case "nil-result":
						repo.user = nil
					case "driver":
						repo.err = fmt.Errorf("lookup: %w", driverErr)
					case "nil-service":
						service = nil
					case "nil-repository":
						service.UserRepository = nil
					case "nil-context":
						ctx = nil
					case "canceled":
						cancel()
					case "cancel-read":
						repo.cancel = cancel
					case "empty-key":
						key = ""
					}
					var got *UniversalUser
					var err error
					if kind == "id" {
						req := &GetUserByIDRequest{ID: key}
						if tc.variant == "nil-request" {
							req = nil
						}
						response, callErr := service.GetUserByID(ctx, req)
						err = callErr
						if response != nil {
							got = response.User
						}
					} else {
						if want == ErrInvalidUserID {
							want = ErrInvalidNanoID
						}
						req := &GetUserByNanoIDRequest{NanoID: key}
						if tc.variant == "nil-request" {
							req = nil
						}
						response, callErr := service.GetUserByNanoID(ctx, req)
						err = callErr
						if response != nil {
							got = response.User
						}
					}
					require.ErrorIs(t, err, want)
					require.Equal(t, tc.calls, repo.calls)
					if tc.calls > 0 {
						require.Equal(t, key, repo.key)
					}
					if want == nil {
						require.Same(t, repo.user, got)
						require.NotNil(t, got.config, "restore model dependencies")
					} else {
						require.Nil(t, got)
					}
				})
			}
		})
	}
}
