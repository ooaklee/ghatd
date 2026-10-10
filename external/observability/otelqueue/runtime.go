package otelqueue

import (
	"context"
	"errors"
	"time"

	"github.com/ooaklee/ghatd/external/observability"
	"go.uber.org/zap"
)

// RuntimeConfig combines the telemetry runtime settings with queue observer
// configuration.
type RuntimeConfig struct {
	Runtime observability.RuntimeConfig
	Queue   Config
}

// Runtime owns process providers, detached job contexts and a queue observer.
// Broker connections, database clients and business callbacks remain host-owned.
type Runtime struct {
	runtime         *observability.Runtime
	jobsCtx         context.Context
	cancelJobs      context.CancelFunc
	queue           *Observer
	shutdownTimeout time.Duration
}

// StartRuntime validates the context, service name and timeout, starts the
// telemetry runtime, and binds queue instruments to that runtime's providers so
// later global changes do not affect them. On queue construction failure the
// started runtime is shut down. Job contexts derive from the runtime without
// its cancellation.
func StartRuntime(ctx context.Context, config RuntimeConfig) (*Runtime, error) {
	if ctx == nil || config.Runtime.Telemetry.ServiceName == "" || config.Runtime.ShutdownTimeout < 0 {
		return nil, errors.New("otelqueue: root context, service name and nonnegative shutdown timeout required")
	}
	runtime, err := observability.StartRuntime(ctx, config.Runtime)
	if err != nil {
		return nil, err
	}
	// Instruments are bound to this runtime even when global providers change.
	config.Queue.TracerProvider = runtime.SDK().TracerProvider()
	config.Queue.MeterProvider = runtime.SDK().MeterProvider()
	queue, err := New(config.Queue)
	if err != nil {
		_ = runtime.Shutdown(ctx)
		return nil, err
	}
	jobsCtx, cancel := context.WithCancel(context.WithoutCancel(runtime.Context()))
	return &Runtime{runtime: runtime, queue: queue, jobsCtx: jobsCtx, cancelJobs: cancel, shutdownTimeout: config.Runtime.ShutdownTimeout}, nil
}

// Context returns the runtime's starting context with runtime and logger
// attached.
func (r *Runtime) Context() context.Context { return r.runtime.Context() }

// Logger returns the telemetry-enabled application logger.
func (r *Runtime) Logger() *zap.Logger { return r.runtime.Logger() }

// SDK returns the runtime's explicitly configured providers, or nil when
// absent.
func (r *Runtime) SDK() *observability.SDK { return r.runtime.SDK() }

// BeginJob starts a job trace on the runtime's detached job context,
// independent of the starting context's cancellation.
func (r *Runtime) BeginJob(operation string) (*Job, context.Context) {
	return r.queue.Begin(r.jobsCtx, operation)
}

// ShutdownResources first stops admission and drains jobs, then cancels remaining
// jobs, closes dependencies with a fresh deadline, and finally flushes telemetry.
// A context-ignoring job can outlive the drain budget; the host's broker decides
// redelivery. This helper does not promise exactly-once business side effects.
func (r *Runtime) ShutdownResources(ctx context.Context, stop func() <-chan struct{}, cleanup func(context.Context) error) {
	observability.ShutdownResources(ctx, observability.ShutdownConfig{
		Timeout: r.shutdownTimeout, Drain: stop, CancelWork: r.cancelJobs,
		Cleanup: cleanup, Shutdown: r.runtime.Shutdown, Logger: r.Logger(),
	})
}
