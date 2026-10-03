package ephemeral

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-redis/redis/v7"
	"github.com/ooaklee/ghatd/external/logger"
	"github.com/ooaklee/ghatd/external/toolbox"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func TestSessionCleanupRedisIsolation(t *testing.T) {
	address := os.Getenv("GHATD_TEST_REDIS_ADDR")
	if address == "" {
		t.Skip("set GHATD_TEST_REDIS_ADDR for real Redis isolation checks")
	}
	for _, owner := range []string{"owner", "o*[x]?\\"} {
		t.Run(owner, func(t *testing.T) {
			client := redis.NewClient(&redis.Options{Addr: address})
			t.Cleanup(func() { require.NoError(t, client.Close()) })
			store := NewRedisStore(client, 1, "cleanup", toolbox.GenerateUuidV4())
			var keys []string
			t.Cleanup(func() {
				if len(keys) > 0 {
					require.NoError(t, client.Del(keys...).Err())
				}
			})
			// Enough keys to force multiple SCAN pages; no shared DB flush.
			for i := 0; i < 300; i++ {
				key := store.keyPrefix + owner + ":" + fmt.Sprint(i)
				keys = append(keys, key)
				require.NoError(t, client.Set(key, owner, time.Minute).Err())
			}
			protected := []string{store.keyPrefix + "foreign:one", store.keyPrefix + owner + ":nested:one", store.keyPrefix + owner + ":"}
			for _, key := range protected {
				keys = append(keys, key)
				require.NoError(t, client.Set(key, "keep", time.Minute).Err())
			}
			require.NoError(t, store.DeleteAllTokenExceptedSpecified(context.Background(), owner, []string{owner + ":0"}))
			for i, key := range keys[:300] {
				require.Equal(t, i == 0, client.Exists(key).Val() == 1, "key index %d", i)
			}
			for _, key := range protected {
				require.Equal(t, int64(1), client.Exists(key).Val())
			}
			require.NoError(t, store.DeleteAllTokenExceptedSpecified(context.Background(), owner, nil))
			require.Zero(t, client.Exists(keys[0]).Val())
		})
	}
}

// cleanupClientProbe records exactly the keys selected by the cleanup adapter.
type cleanupClientProbe struct {
	PersistentClient
	keys, patterns     []string
	deleted            [][]string
	scanErr, deleteErr error
	cancel             context.CancelFunc
	invalidCount       bool
}

func (p *cleanupClientProbe) Scan(_ uint64, pattern string, _ int64) *redis.ScanCmd {
	p.patterns = append(p.patterns, pattern)
	if p.cancel != nil {
		p.cancel()
	}
	return redis.NewScanCmdResult(p.keys, 0, p.scanErr)
}
func (p *cleanupClientProbe) Del(keys ...string) *redis.IntCmd {
	p.deleted = append(p.deleted, append([]string{}, keys...))
	count := int64(len(keys))
	if p.invalidCount {
		count++
	}
	return redis.NewIntResult(count, p.deleteErr)
}

func TestSessionCleanupSelectionAndFailures(t *testing.T) {
	driver := errors.New("private-driver-diagnostic")
	for _, kind := range []string{"all", "exempt", "empty", "foreign results", "glob characters", "delimiter rejected", "empty identity", "bounded batches", "scan failure", "delete failure", "invalid count", "canceled", "cancel after scan", "nil context", "nil store", "typed nil client"} {
		t.Run(kind, func(t *testing.T) {
			core, logs := observer.New(zap.DebugLevel)
			ctx, cancel := context.WithCancel(logger.TransitWith(context.Background(), zap.New(core)))
			defer cancel()
			p := &cleanupClientProbe{keys: []string{"sample-test_owner:one", "sample-test_owner:two"}}
			store := NewRedisStore(p, 1, "sample", "test")
			id := "owner"
			var exempt []string
			var want error
			count := 2
			switch kind {
			case "exempt":
				exempt = []string{"owner:one", "foreign:one"}
				count = 1
			case "empty":
				p.keys = nil
				count = 0
			case "foreign results":
				p.keys = append(p.keys, "sample-test_foreign:one", "sample-test_owner:foreign:one", "sample-test_owner:")
				count = 2
			case "glob characters":
				id = "o*[x]?\\"
				p.keys = []string{"sample-test_" + id + ":one"}
				count = 1
			case "delimiter rejected":
				id = "owner:other"
				want = ErrInvalidSessionCleanup
				count = 0
			case "empty identity":
				id = " "
				want = ErrInvalidSessionCleanup
				count = 0
			case "bounded batches":
				p.keys = nil
				for i := 0; i < 300; i++ {
					p.keys = append(p.keys, fmt.Sprintf("sample-test_owner:%d", i))
				}
				count = 300
			case "scan failure":
				p.scanErr = driver
				want = driver
				count = 0
			case "delete failure":
				p.deleteErr = driver
				want = driver
			case "invalid count":
				p.invalidCount = true
				want = ErrInvalidSessionCleanup
			case "canceled":
				cancel()
				want = context.Canceled
				count = 0
			case "cancel after scan":
				p.cancel = cancel
				want = context.Canceled
				count = 0
			case "nil context":
				ctx = nil
				want = ErrInvalidSessionCleanup
				count = 0
			case "nil store":
				store = nil
				want = ErrInvalidSessionCleanup
				count = 0
			case "typed nil client":
				store.client = (*cleanupClientProbe)(nil)
				want = ErrInvalidSessionCleanup
				count = 0
			}
			err := store.DeleteAllTokenExceptedSpecified(ctx, id, exempt)
			require.Equal(t, want, err)
			actual := 0
			for _, batch := range p.deleted {
				require.LessOrEqual(t, len(batch), 128)
				actual += len(batch)
				for _, key := range batch {
					require.True(t, strings.HasPrefix(key, "sample-test_"+id+":"))
					require.NotContains(t, strings.TrimPrefix(key, "sample-test_"+id+":"), ":")
				}
			}
			require.Equal(t, count, actual)
			if kind == "glob characters" {
				require.Equal(t, `sample-test_o\*\[x\]\?\\:*`, p.patterns[0])
			}
			if kind == "exempt" {
				require.Equal(t, [][]string{{"sample-test_owner:two"}}, p.deleted)
			}
			require.NotContains(t, fmt.Sprint(logs.All()), "private-driver-diagnostic")
			require.NotContains(t, fmt.Sprint(logs.All()), "sample-test_")
		})
	}
}
