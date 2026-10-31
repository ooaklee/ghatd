package indexnow

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
)

func TestIntegration_JobRepositoryConcurrentClaimAndRestart(t *testing.T) {
	type testCase struct {
		name       string
		contenders int
	}
	for _, tc := range []testCase{{name: "single activation", contenders: 1}, {name: "concurrent initializers across replicas", contenders: 8}} {
		t.Run(tc.name, func(t *testing.T) {
			attempts, ctx := isolatedIndexnowMongo(t)
			now := time.Now().UTC().Truncate(time.Millisecond)
			type result struct {
				lease *JobLease
				err   error
			}
			results := make(chan result, tc.contenders)
			start := make(chan struct{})
			for i := 0; i < tc.contenders; i++ {
				go func(i int) {
					jobs := NewJobRepository(attempts.Store)
					<-start
					lease, err := jobs.Claim(ctx, testJobScope, fmt.Sprintf("%032x", i+1), now, time.Minute)
					results <- result{lease, err}
				}(i)
			}
			close(start)
			var winner *JobLease
			wins := 0
			for i := 0; i < tc.contenders; i++ {
				got := <-results
				if got.err == nil {
					wins++
					winner = got.lease
				} else {
					require.ErrorIs(t, got.err, ErrJobBusy)
				}
			}
			require.Equal(t, 1, wins)
			require.NotNil(t, winner)
			require.False(t, winner.Initialized)
			require.True(t, winner.Checkpoint.Equal(now))
			jobs := NewJobRepository(attempts.Store)
			require.NoError(t, jobs.Complete(ctx, winner, winner.Checkpoint))
			require.NoError(t, jobs.Release(ctx, winner))
			restarted, err := NewJobRepository(attempts.Store).Claim(ctx, testJobScope, strings.Repeat("f", 32), now.Add(12*time.Hour), time.Minute)
			require.NoError(t, err)
			require.True(t, restarted.Initialized)
			require.True(t, restarted.Checkpoint.Equal(now), "restart must not reset durable checkpoint")
			require.NoError(t, jobs.Complete(ctx, restarted, now.Add(time.Hour)))
			require.NoError(t, jobs.Release(ctx, restarted))
		})
	}
}

func TestIntegration_JobRepositoryExpiredHolderFencing(t *testing.T) {
	type testCase struct {
		name     string
		takeover bool
	}
	for _, tc := range []testCase{{name: "expired holder cannot complete or release"}, {name: "successor fences stale holder", takeover: true}} {
		t.Run(tc.name, func(t *testing.T) {
			attempts, ctx := isolatedIndexnowMongo(t)
			jobs := NewJobRepository(attempts.Store)
			now := time.Now().UTC().Truncate(time.Millisecond)
			lease, err := jobs.Claim(ctx, testJobScope, strings.Repeat("a", 32), now, time.Minute)
			require.NoError(t, err)
			require.NoError(t, jobs.Complete(ctx, lease, lease.Checkpoint))
			collection, err := jobs.collection(ctx)
			require.NoError(t, err)
			_, err = collection.UpdateOne(ctx, bson.M{"_id": testJobScope}, bson.M{"$set": bson.M{"lease_until": time.Now().Add(-time.Second)}})
			require.NoError(t, err)
			var successor *JobLease
			if tc.takeover {
				successor, err = jobs.Claim(ctx, testJobScope, strings.Repeat("b", 32), now.Add(time.Hour), time.Minute)
				require.NoError(t, err)
			}
			lease.Initialized = true
			require.ErrorIs(t, jobs.Complete(ctx, lease, now.Add(time.Minute)), ErrJobLeaseLost)
			require.ErrorIs(t, jobs.Release(ctx, lease), ErrJobLeaseLost)
			require.ErrorIs(t, jobs.Defer(ctx, lease, time.Hour), ErrJobLeaseLost)
			if tc.takeover {
				require.NoError(t, jobs.Complete(ctx, successor, now.Add(time.Minute)))
				require.NoError(t, jobs.Release(ctx, successor))
			}
			var stored bson.M
			require.NoError(t, collection.FindOne(ctx, bson.M{"_id": testJobScope}).Decode(&stored))
			for key := range stored {
				require.Contains(t, []string{"_id", "checkpoint_at", "initialized", "lease_token", "lease_until"}, key, "job state cannot include URL/body/key diagnostics")
			}
		})
	}
}

func TestIntegration_JobRetryDeferralSurvivesRestart(t *testing.T) {
	type testCase struct {
		name  string
		delay time.Duration
	}
	cases := []testCase{{name: "protocol hour", delay: time.Hour}, {name: "maximum duration does not overflow", delay: time.Duration(1<<63 - 1)}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			attempts, ctx := isolatedIndexnowMongo(t)
			jobs := NewJobRepository(attempts.Store)
			now := time.Now().UTC().Truncate(time.Millisecond)
			lease, err := jobs.Claim(ctx, testJobScope, strings.Repeat("a", 32), now, time.Minute)
			require.NoError(t, err)
			require.NoError(t, jobs.Complete(ctx, lease, lease.Checkpoint))
			require.NoError(t, jobs.Defer(ctx, lease, tc.delay))
			collection, err := jobs.collection(ctx)
			require.NoError(t, err)
			var before struct {
				Next       time.Time `bson:"next_attempt_at"`
				Checkpoint time.Time `bson:"checkpoint_at"`
			}
			require.NoError(t, collection.FindOne(ctx, bson.M{"_id": testJobScope}).Decode(&before))
			require.True(t, before.Next.After(now))
			require.True(t, before.Checkpoint.Equal(now))
			// A shorter request may be a strict no-op receipt, but cannot lower the floor.
			err = jobs.Defer(ctx, lease, time.Second)
			require.ErrorIs(t, err, ErrJobUnavailable)
			var after struct {
				Next time.Time `bson:"next_attempt_at"`
			}
			require.NoError(t, collection.FindOne(ctx, bson.M{"_id": testJobScope}).Decode(&after))
			require.Equal(t, before.Next, after.Next)
			require.NoError(t, jobs.Release(ctx, lease))
			// Application time is deliberately beyond the requested floor. MongoDB's
			// current server clock, rather than that input, must govern acquisition.
			_, err = NewJobRepository(attempts.Store).Claim(ctx, testJobScope, strings.Repeat("b", 32), before.Next.Add(time.Hour), time.Minute)
			require.ErrorIs(t, err, ErrJobBusy)
			_, err = collection.UpdateOne(ctx, bson.M{"_id": testJobScope}, bson.M{"$set": bson.M{"next_attempt_at": now.Add(-time.Hour)}})
			require.NoError(t, err)
			next, err := NewJobRepository(attempts.Store).Claim(ctx, testJobScope, strings.Repeat("c", 32), now.Add(time.Hour), time.Minute)
			require.NoError(t, err)
			require.True(t, next.Checkpoint.Equal(now))
		})
	}
}

func TestIntegration_RunnerDurableActivationAndIncompleteWindow(t *testing.T) {
	type testCase struct {
		name       string
		incomplete bool
	}
	for _, tc := range []testCase{{name: "complete scan persists progress"}, {name: "incomplete scan preserves checkpoint", incomplete: true}} {
		t.Run(tc.name, func(t *testing.T) {
			attempts, ctx := isolatedIndexnowMongo(t)
			now := time.Now().UTC().Truncate(time.Millisecond)
			scans := 0
			scan := func(context.Context, time.Time, time.Time, int) (bool, error) { scans++; return !tc.incomplete, nil }
			runner, err := NewRunner(RunnerConfig{Scope: testJobScope}, NewJobRepository(attempts.Store), scan)
			require.NoError(t, err)
			runner.now = func() time.Time { return now }
			report, err := runner.RunOnce(ctx)
			require.NoError(t, err)
			require.Equal(t, RunActivated, report.State)
			require.Zero(t, scans)
			runner, err = NewRunner(RunnerConfig{Scope: testJobScope}, NewJobRepository(attempts.Store), scan)
			require.NoError(t, err)
			runner.now = func() time.Time { return now.Add(time.Hour) }
			_, err = runner.RunOnce(ctx)
			if tc.incomplete {
				require.True(t, errors.Is(err, ErrJobIncomplete))
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, 1, scans)
			claim, err := NewJobRepository(attempts.Store).Claim(ctx, testJobScope, strings.Repeat("c", 32), now.Add(2*time.Hour), time.Minute)
			require.NoError(t, err)
			want := now.Add(time.Hour)
			if tc.incomplete {
				want = now
			}
			require.True(t, claim.Checkpoint.Equal(want))
		})
	}
}
