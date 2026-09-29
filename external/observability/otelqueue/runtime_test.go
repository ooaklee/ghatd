package otelqueue

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/observability"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	otellogglobal "go.opentelemetry.io/otel/log/global"
	"go.uber.org/zap"
)

type runtimeTestKey struct{}

func TestWorkerRuntimePreservesValuesWhileJobsDrain(t *testing.T) {
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(name, "OTEL_") {
			t.Setenv(name, "")
		}
	}
	for _, signal := range []string{"TRACES", "METRICS", "LOGS"} {
		t.Setenv("OTEL_"+signal+"_EXPORTER", "none")
	}
	tp, mp, lp, propagator := otel.GetTracerProvider(), otel.GetMeterProvider(), otellogglobal.GetLoggerProvider(), otel.GetTextMapPropagator()
	t.Cleanup(func() {
		otel.SetTracerProvider(tp)
		otel.SetMeterProvider(mp)
		otellogglobal.SetLoggerProvider(lp)
		otel.SetTextMapPropagator(propagator)
	})
	root, cancelRoot := context.WithCancel(context.WithValue(context.Background(), runtimeTestKey{}, "retained"))
	defer cancelRoot()
	worker, err := StartRuntime(root, RuntimeConfig{
		Runtime: observability.RuntimeConfig{Telemetry: observability.Config{ServiceName: "example-worker"}, Logger: zap.NewNop(), ShutdownTimeout: time.Second},
		Queue:   Config{Operations: []string{"queue.document.consume"}},
	})
	require.NoError(t, err)
	job, jobCtx := worker.BeginJob("queue.document.consume")
	cancelRoot()
	require.ErrorIs(t, worker.Context().Err(), context.Canceled)
	require.NoError(t, jobCtx.Err())
	require.Equal(t, "retained", jobCtx.Value(runtimeTestKey{}))
	require.NotNil(t, worker.SDK())
	require.NotNil(t, worker.Logger())
	worker.ShutdownResources(root, func() <-chan struct{} {
		require.NoError(t, jobCtx.Err())
		job.Settle(OutcomeAcked, nil)
		done := make(chan struct{})
		close(done)
		return done
	}, func(ctx context.Context) error {
		require.NoError(t, ctx.Err())
		require.ErrorIs(t, jobCtx.Err(), context.Canceled)
		return nil
	})
}

func TestWorkerRuntimeRejectsMissingIdentityAndNegativeTimeout(t *testing.T) {
	_, err := StartRuntime(context.Background(), RuntimeConfig{})
	require.Error(t, err)
	_, err = StartRuntime(context.Background(), RuntimeConfig{Runtime: observability.RuntimeConfig{Telemetry: observability.Config{ServiceName: "example-worker"}, ShutdownTimeout: -time.Second}})
	require.Error(t, err)
}
