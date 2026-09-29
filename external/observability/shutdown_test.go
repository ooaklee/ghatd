package observability

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

type cleanupContextKey struct{}

func TestShutdownResourcesOrdersDrainCleanupAndFlush(t *testing.T) {
	root, cancelRoot := context.WithCancel(context.WithValue(context.Background(), cleanupContextKey{}, "retained"))
	jobs, cancelJobs := context.WithCancel(context.WithoutCancel(root))
	cancelRoot()
	var events []string
	core, logs := observer.New(zap.InfoLevel)
	ShutdownResources(root, ShutdownConfig{
		Timeout: time.Second, Logger: zap.New(core), CancelWork: cancelJobs,
		Drain: func() <-chan struct{} {
			require.NoError(t, jobs.Err())
			events = append(events, "drain")
			done := make(chan struct{})
			close(done)
			return done
		},
		Cleanup: func(ctx context.Context) error {
			events = append(events, "dependencies")
			require.NoError(t, ctx.Err())
			require.Equal(t, "retained", ctx.Value(cleanupContextKey{}))
			deadline, ok := ctx.Deadline()
			require.True(t, ok)
			require.Positive(t, time.Until(deadline))
			require.ErrorIs(t, jobs.Err(), context.Canceled)
			return errors.New("private-dependency-error")
		},
		Shutdown: func(ctx context.Context) error {
			events = append(events, "telemetry")
			require.Same(t, root, ctx, "Runtime.Shutdown supplies its own fresh budget")
			return errors.New("private-exporter-error")
		},
	})
	require.Equal(t, []string{"drain", "dependencies", "telemetry"}, events)
	require.Len(t, logs.FilterMessage("observability-resource-cleanup-failed").All(), 1)
	require.Len(t, logs.FilterMessage("observability-shutdown-failed").All(), 1)
	for _, entry := range logs.All() {
		require.Empty(t, entry.Context)
	}
}

func TestShutdownResourcesBoundsDrainAndRenewsCleanupBudget(t *testing.T) {
	root, cancelRoot := context.WithCancel(context.Background())
	cancelRoot()
	jobs, cancelJobs := context.WithCancel(context.WithoutCancel(root))
	flushed := false
	core, logs := observer.New(zap.WarnLevel)
	ShutdownResources(root, ShutdownConfig{
		Timeout: time.Millisecond, Logger: zap.New(core), CancelWork: cancelJobs,
		Drain: func() <-chan struct{} { return make(chan struct{}) },
		Cleanup: func(ctx context.Context) error {
			require.NoError(t, ctx.Err())
			require.ErrorIs(t, jobs.Err(), context.Canceled)
			return nil
		},
		Shutdown: func(context.Context) error { flushed = true; return nil },
	})
	require.True(t, flushed)
	require.Len(t, logs.FilterMessage("observability-work-drain-timeout").All(), 1)
}

func TestShutdownResourcesFlushesAfterPanicAndAllowsOptionalCallbacks(t *testing.T) {
	for _, phase := range []string{"drain", "cleanup"} {
		t.Run(phase, func(t *testing.T) {
			flushed, cancelled := false, false
			failure := &struct{}{}
			require.PanicsWithValue(t, failure, func() {
				ShutdownResources(context.Background(), ShutdownConfig{
					Drain: func() <-chan struct{} {
						if phase == "drain" {
							panic(failure)
						}
						return nil
					},
					Cleanup:    func(context.Context) error { panic(failure) },
					CancelWork: func() { cancelled = true },
					Shutdown:   func(context.Context) error { flushed = true; return nil },
				})
			})
			require.True(t, flushed)
			require.True(t, cancelled)
		})
	}
	ShutdownResources(context.Background(), ShutdownConfig{})
}
