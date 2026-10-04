package adminaccess

import (
	"context"
	"encoding/json"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/go-redis/redis/v7"
	"github.com/ooaklee/ghatd/external/accesspolicy"
	"github.com/stretchr/testify/require"
)

// testStore owns a random namespace only. It never flushes shared Redis data.
func testStore(t *testing.T) (*RedisStore, context.Context) {
	t.Helper()
	addr := os.Getenv("GHATD_TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("set GHATD_TEST_REDIS_ADDR for atomic store checks")
	}
	ctx := context.Background()
	client := redis.NewClient(&redis.Options{Addr: addr})
	store, err := NewRedisStore(client, randomID())
	require.NoError(t, err)
	t.Cleanup(func() {
		keys, err := client.Keys(store.prefix + "*").Result()
		require.NoError(t, err)
		if len(keys) > 0 {
			require.NoError(t, client.Del(keys...).Err())
		}
		require.NoError(t, client.Close())
	})
	return store, ctx
}

func TestRedisReviewTransitions(t *testing.T) {
	for _, scenario := range []string{"revision precision", "binding mismatch", "review mismatch", "absolute expiry", "cancelled code", "new preview invalidates code", "one shot confirm", "concurrent consumption"} {
		t.Run(scenario, func(t *testing.T) {
			s, ctx := testStore(t)
			binding := "verified-session"
			page, err := s.Create(ctx, binding, "actor")
			require.NoError(t, err)
			r := review{ID: randomID(), Target: "selected", Revision: 9007199254740991, Limits: accesspolicy.TokenLimits{Permanent: 2}}
			raw, err := json.Marshal(r)
			require.NoError(t, err)
			stored, err := s.run(ctx, page, binding, "preview", r.ID, string(raw), "", 5*time.Minute)
			require.NoError(t, err)
			require.Equal(t, r.Revision, stored.Revision)
			require.InDelta(t, time.Now().Add(5*time.Minute).UnixMilli(), stored.Expires, 2000)
			_, err = s.run(ctx, page, binding, "challenge", r.ID, codeHash(r.ID, "012345ABCDEF"), "actor", 0)
			require.NoError(t, err)
			want := error(nil)
			switch scenario {
			case "binding mismatch":
				binding = "another-session"
				want = ErrReview
			case "review mismatch":
				r.ID = randomID()
				want = ErrReview
			case "absolute expiry":
				require.NoError(t, s.client.HSet(s.prefix+page, "expires", 1).Err())
				want = ErrReview
			case "cancelled code":
				_, err = s.run(ctx, page, binding, "cancel", r.ID, "", "", 0)
				require.NoError(t, err)
				want = ErrReview
			case "new preview invalidates code":
				_, err = s.run(ctx, page, binding, "preview", randomID(), string(raw), "", 5*time.Minute)
				require.NoError(t, err)
				want = ErrReview
			}
			_, err = s.run(ctx, page, binding, "confirm", r.ID, codeHash(r.ID, "012345ABCDEF"), "", 0)
			require.ErrorIs(t, err, want)
			if want != nil {
				return
			}
			require.False(t, s.client.HExists(s.prefix+page, "code").Val())
			read, err := s.run(ctx, page, binding, "read", r.ID, "", "", 0)
			require.NoError(t, err)
			require.Equal(t, stored.Expires, read.Expires)
			if scenario == "one shot confirm" {
				_, err = s.run(ctx, page, binding, "confirm", r.ID, codeHash(r.ID, "012345ABCDEF"), "", 0)
				require.ErrorIs(t, err, ErrReview)
			}
			if scenario == "concurrent consumption" {
				var wg sync.WaitGroup
				results := make(chan error, 10)
				for range 10 {
					wg.Go(func() { _, err := s.run(ctx, page, binding, "consume", r.ID, "", "", 0); results <- err })
				}
				wg.Wait()
				close(results)
				success := 0
				for err := range results {
					if err == nil {
						success++
					} else {
						require.ErrorIs(t, err, ErrReview)
					}
				}
				require.Equal(t, 1, success)
			}
		})
	}
}

func TestRedisAccountWideBounds(t *testing.T) {
	for _, scenario := range []string{"page context limit", "email cooldown", "hourly email limit", "expiry keeps context bound"} {
		t.Run(scenario, func(t *testing.T) {
			s, ctx := testStore(t)
			if scenario == "page context limit" {
				for range 10 {
					_, err := s.Create(ctx, randomID(), "same-actor")
					require.NoError(t, err)
				}
				_, err := s.Create(ctx, randomID(), "same-actor")
				require.ErrorIs(t, err, ErrCooldown)
				return
			}
			page, err := s.Create(ctx, "binding", "actor")
			require.NoError(t, err)
			if scenario == "expiry keeps context bound" {
				require.NoError(t, s.client.PExpire(s.prefix+page, 30*time.Second).Err())
			}
			var last review
			for i := 0; i < 11; i++ {
				r := review{ID: randomID(), Target: "target"}
				raw, _ := json.Marshal(r)
				last, err = s.run(ctx, page, "binding", "preview", r.ID, string(raw), "", 5*time.Minute)
				require.NoError(t, err)
				if scenario == "expiry keeps context bound" {
					require.LessOrEqual(t, last.Expires, time.Now().Add(30*time.Second).UnixMilli())
					return
				}
				_, err = s.run(ctx, page, "binding", "challenge", r.ID, codeHash(r.ID, "012345ABCDEF"), "actor", 0)
				if scenario == "email cooldown" && i == 1 || i == 10 {
					require.ErrorIs(t, err, ErrCooldown)
					return
				}
				require.NoError(t, err)
				if scenario == "hourly email limit" {
					require.NoError(t, s.client.Del(s.prefix+"cooldown:"+digest("actor")).Err())
				}
			}
		})
	}
}
