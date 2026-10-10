package browsersecurity

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestWindowLimiterValidation(t *testing.T) {
	for _, test := range []struct {
		name   string
		limit  int
		window time.Duration
		valid  bool
	}{
		{"minimum", 1, time.Second, true}, {"maximum", 10000, 24 * time.Hour, true},
		{"zero-limit", 0, time.Second, false}, {"large-limit", 10001, time.Second, false},
		{"short-window", 1, time.Second - time.Nanosecond, false}, {"long-window", 1, 24*time.Hour + time.Nanosecond, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := NewWindowLimiter(test.limit, test.window, nil)
			if test.valid {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, ErrConfigurationInvalid)
			}
		})
	}
	for _, test := range []struct {
		name, key             string
		nilContext, cancelled bool
		want                  error
	}{
		{"valid", "actor", false, false, nil}, {"empty-key", "", false, false, ErrInvalidRequest},
		{"long-key", strings.Repeat("x", 513), false, false, ErrInvalidRequest},
		{"nil-context", "actor", true, false, ErrInvalidRequest}, {"cancelled", "actor", false, true, context.Canceled},
	} {
		t.Run(test.name, func(t *testing.T) {
			limiter, err := NewWindowLimiter(1, time.Minute, nil)
			require.NoError(t, err)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if test.cancelled {
				cancel()
			}
			if test.nilContext {
				ctx = nil
			}
			_, err = limiter.Allow(ctx, test.key)
			require.ErrorIs(t, err, test.want)
		})
	}
	t.Run("zero-limiter", func(t *testing.T) {
		var limiter WindowLimiter
		_, err := limiter.Allow(context.Background(), "actor")
		require.ErrorIs(t, err, ErrConfigurationInvalid)
	})
}

func TestWindowLimiterConcurrentAdmissionAndCapacityRecovery(t *testing.T) {
	for _, test := range []struct {
		name            string
		limit, attempts int
	}{{"single", 1, 20}, {"five", 5, 30}, {"all", 20, 20}} {
		t.Run(test.name, func(t *testing.T) {
			at := time.Unix(1791547200, 0)
			limiter, err := NewWindowLimiter(test.limit, time.Minute, func() time.Time { return at })
			require.NoError(t, err)
			var count atomic.Int32
			var group sync.WaitGroup
			for range test.attempts {
				group.Add(1)
				go func() {
					defer group.Done()
					retry, err := limiter.Allow(context.Background(), "actor")
					if err == nil && retry == 0 {
						count.Add(1)
					}
				}()
			}
			group.Wait()
			require.Equal(t, int32(test.limit), count.Load())
			retry, err := limiter.Allow(context.Background(), "actor")
			require.NoError(t, err)
			require.Equal(t, time.Minute, retry)
		})
	}
	t.Run("capacity-expiry", func(t *testing.T) {
		now := time.Unix(1791547200, 0)
		limiter, err := NewWindowLimiter(1, time.Minute, func() time.Time { return now })
		require.NoError(t, err)
		for i := range 10000 {
			retry, err := limiter.Allow(context.Background(), fmt.Sprint("actor-", i))
			require.NoError(t, err)
			require.Zero(t, retry)
		}
		retry, err := limiter.Allow(context.Background(), "new-actor")
		require.NoError(t, err)
		require.Equal(t, time.Minute, retry)
		require.Len(t, limiter.buckets, 10000)
		now = now.Add(time.Minute)
		retry, err = limiter.Allow(context.Background(), "new-actor")
		require.NoError(t, err)
		require.Zero(t, retry)
		require.Len(t, limiter.buckets, 1)
	})
}

func TestRetrySecondsRoundsWithoutOverflow(t *testing.T) {
	for _, test := range []struct {
		name     string
		duration time.Duration
		want     string
	}{
		{"negative", -time.Second, "1"}, {"zero", 0, "1"},
		{"fraction", time.Nanosecond, "1"}, {"exact", time.Second, "1"},
		{"round-up", time.Second + time.Nanosecond, "2"},
		{"maximum-duration", time.Duration(1<<63 - 1), "9223372037"},
	} {
		t.Run(test.name, func(t *testing.T) { require.Equal(t, test.want, RetrySeconds(test.duration)) })
	}
}
