package ephemeral

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/go-redis/redis/v7"
	ghatdlogger "github.com/ooaklee/ghatd/external/logger"
	"github.com/ooaklee/ghatd/external/toolbox"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

// authLookupIdentity supplies only the identity needed for a store lookup.
type authLookupIdentity struct{ user, session string }

func (i authLookupIdentity) GetUserId() string          { return i.user }
func (i authLookupIdentity) GetTokenAccessUuid() string { return i.session }
func (i authLookupIdentity) IsUserAdmin() bool          { return false }
func (i authLookupIdentity) IsUserAuthorized() bool     { return true }

// authLookupClient records the exact namespaced key and propagated context.
type authLookupClient struct {
	PersistentClient
	value, key string
	err        error
	ctx        context.Context
	cancel     context.CancelFunc
	calls      int
}

func (c *authLookupClient) WithContext(ctx context.Context) PersistentClient {
	c.ctx = ctx
	return c
}
func (c *authLookupClient) Get(key string) *redis.StringCmd {
	c.calls++
	c.key = key
	if c.cancel != nil {
		c.cancel()
	}
	return redis.NewStringResult(c.value, c.err)
}

func TestFetchAuthBoundariesAndErrorCauses(t *testing.T) {
	driverErr := errors.New("private-driver-diagnostic")
	for _, tc := range []struct {
		name, variant string
		want          error
		calls         int
	}{
		{"existing owner", "", nil, 1},
		{"missing session", "missing", ErrAuthNotFound, 1},
		{"wrapped Redis absence", "wrapped-missing", ErrAuthNotFound, 1},
		{"storage failure", "driver", driverErr, 1},
		{"nil client", "nil-client", ErrInvalidAuthLookup, 0},
		{"nil store", "nil-store", ErrInvalidAuthLookup, 0},
		{"nil context", "nil-context", ErrInvalidAuthLookup, 0},
		{"nil identity", "nil-identity", ErrInvalidAuthLookup, 0},
		{"empty user", "empty-user", ErrInvalidAuthLookup, 0},
		{"empty session", "empty-session", ErrInvalidAuthLookup, 0},
		{"already canceled", "canceled", context.Canceled, 0},
		{"canceled during read", "cancel-read", context.Canceled, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			core, logs := observer.New(zap.DebugLevel)
			ctx, cancel := context.WithCancel(ghatdlogger.TransitWith(context.Background(), zap.New(core)))
			defer cancel()
			client := &authLookupClient{value: "private-owner"}
			store := NewRedisStore(client, 10, "sample", "test")
			var identity TokenDetailsAccess = authLookupIdentity{"private-owner", "private-session"}
			switch tc.variant {
			case "missing":
				client.err = redis.Nil
			case "wrapped-missing":
				client.err = fmt.Errorf("lookup: %w", redis.Nil)
			case "driver":
				client.err = driverErr
			case "nil-client":
				store.client = nil
			case "nil-store":
				store = nil
			case "nil-context":
				ctx = nil
			case "nil-identity":
				identity = nil
			case "empty-user":
				identity = authLookupIdentity{session: "session"}
			case "empty-session":
				identity = authLookupIdentity{user: "user"}
			case "canceled":
				cancel()
			case "cancel-read":
				client.cancel = cancel
			}
			owner, err := store.FetchAuth(ctx, identity)
			require.ErrorIs(t, err, tc.want)
			require.Equal(t, tc.calls, client.calls)
			if tc.want == nil {
				require.Equal(t, "private-owner", owner)
			} else {
				require.Empty(t, owner)
			}
			if tc.calls > 0 {
				require.Equal(t, ctx, client.ctx)
				require.Equal(t, "sample-test_"+toolbox.CombinedUuidFormat("private-owner", "private-session"), client.key)
			}
			if tc.want == ErrAuthNotFound {
				require.ErrorIs(t, err, redis.Nil, "preserve legacy errors.Is compatibility")
				require.True(t, IsAuthNotFound(err))
				require.Zero(t, logs.Len(), "expected absence is not an operational error")
			}
			for _, log := range logs.All() {
				encoded := fmt.Sprint(log.Message, log.ContextMap())
				for _, private := range []string{"private-owner", "private-session", "private-driver-diagnostic"} {
					require.NotContains(t, encoded, private)
				}
			}
		})
	}
}

func TestIsAuthNotFound(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"sentinel", ErrAuthNotFound, true},
		{"wrapped sentinel", fmt.Errorf("lookup: %w", ErrAuthNotFound), true},
		{"legacy Redis", redis.Nil, true},
		{"lookalike", errors.New(ErrAuthNotFound.Error()), false},
		{"outage", context.DeadlineExceeded, false},
	} {
		t.Run(tc.name, func(t *testing.T) { require.Equal(t, tc.want, IsAuthNotFound(tc.err)) })
	}
}
