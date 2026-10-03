package ephemeral

import (
	"context"
	"errors"
	"testing"

	"github.com/go-redis/redis/v7"
	"github.com/ooaklee/ghatd/external/logger"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

// deleteProbe supplies exact store receipts and records the selected key.
type deleteProbe struct {
	PersistentClient
	keys  []string
	count int64
	err   error
	after context.CancelFunc
}

func (p *deleteProbe) Del(keys ...string) *redis.IntCmd {
	p.keys = append(p.keys, keys...)
	if p.after != nil {
		p.after()
	}
	return redis.NewIntResult(p.count, p.err)
}

func TestDeleteAuthBoundary(t *testing.T) {
	driver := errors.New("private-driver-diagnostic")
	for _, name := range []string{"deleted", "absent", "driver error", "driver and cancellation", "negative count", "excess count", "missing separator", "empty owner", "empty token", "extra separator", "nil client", "typed nil client", "nil store", "nil context", "canceled", "cancel during delete"} {
		t.Run(name, func(t *testing.T) {
			core, logs := observer.New(zap.DebugLevel)
			ctx, cancel := context.WithCancel(logger.TransitWith(context.Background(), zap.New(core)))
			t.Cleanup(cancel)
			probe := &deleteProbe{count: 1}
			store := NewRedisStore(probe, 1, "fixture", "test")
			key := "owner:token"
			var want error
			calls := 1
			switch name {
			case "absent":
				probe.count = 0
			case "driver error":
				probe.err = driver
				want = driver
			case "driver and cancellation":
				probe.err = driver
				probe.after = cancel
				want = driver
			case "negative count":
				probe.count = -1
				want = ErrInvalidSessionCleanup
			case "excess count":
				probe.count = 2
				want = ErrInvalidSessionCleanup
			case "missing separator":
				key = "record"
				want = ErrInvalidSessionCleanup
				calls = 0
			case "empty owner":
				key = ":token"
				want = ErrInvalidSessionCleanup
				calls = 0
			case "empty token":
				key = "owner: "
				want = ErrInvalidSessionCleanup
				calls = 0
			case "extra separator":
				key = "owner:foreign:token"
				want = ErrInvalidSessionCleanup
				calls = 0
			case "nil client":
				store.client = nil
				want = ErrInvalidSessionCleanup
				calls = 0
			case "typed nil client":
				store.client = (*deleteProbe)(nil)
				want = ErrInvalidSessionCleanup
				calls = 0
			case "nil store":
				store = nil
				want = ErrInvalidSessionCleanup
				calls = 0
			case "nil context":
				ctx = nil
				want = ErrInvalidSessionCleanup
				calls = 0
			case "canceled":
				cancel()
				want = context.Canceled
				calls = 0
			case "cancel during delete":
				probe.after = cancel
				want = context.Canceled
			}
			count, err := store.DeleteAuth(ctx, key)
			require.ErrorIs(t, err, want)
			require.Len(t, probe.keys, calls)
			if want == nil {
				require.Equal(t, probe.count, count)
			} else {
				require.Zero(t, count)
			}
			if calls == 1 {
				require.Equal(t, []string{"fixture-test_owner:token"}, probe.keys)
			}
			for _, entry := range logs.All() {
				require.NotContains(t, entry.Message, driver.Error())
				require.NotContains(t, entry.ContextMap(), "error")
			}
		})
	}
}
