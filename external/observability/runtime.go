package observability

import (
	"context"
	"errors"
	"time"

	ghatdlogger "github.com/ooaklee/ghatd/external/logger"
	"go.uber.org/zap"
)

const defaultRuntimeShutdownTimeout = 15 * time.Second

// RuntimeConfig supplies application identity, logging, and telemetry shutdown
// policy. A nil Logger uses the context logger. Zero ShutdownTimeout selects
// 15 seconds; negative timeouts are invalid.
type RuntimeConfig struct {
	Telemetry       Config
	Logger          *zap.Logger
	LogOptions      []LogOption
	ShutdownTimeout time.Duration
}

// Runtime owns an SDK and its correlated application logger. It does not own
// application servers, database connections, or the original logger's lifetime.
// Shut those dependencies down before the runtime to flush their final signals.
type Runtime struct {
	sdk             *SDK
	logger          *zap.Logger
	ctx             context.Context
	shutdownTimeout time.Duration
}

type runtimeContextKey struct{}

// StartRuntime starts telemetry and installs its logger and runtime in the
// returned context. Create it before instrumented application dependencies.
// Like Start, it installs global providers; shutdown does not reset globals.
func StartRuntime(ctx context.Context, config RuntimeConfig) (*Runtime, error) {
	if config.ShutdownTimeout < 0 {
		return nil, errors.New("observability: runtime shutdown timeout must not be negative")
	}
	if config.ShutdownTimeout == 0 {
		config.ShutdownTimeout = defaultRuntimeShutdownTimeout
	}
	if ctx == nil {
		ctx = context.Background()
	}
	baseLogger := config.Logger
	if baseLogger == nil {
		baseLogger = ghatdlogger.AcquireFrom(ctx)
	}
	sdk, err := Start(ctx, config.Telemetry)
	if err != nil {
		return nil, err
	}
	runtime := &Runtime{sdk: sdk, shutdownTimeout: config.ShutdownTimeout}
	runtime.ctx = context.WithValue(ctx, runtimeContextKey{}, runtime)
	runtime.logger = WithTraceContext(runtime.ctx, TeeLogger(baseLogger, sdk.LoggerProvider(), config.LogOptions...))
	runtime.ctx = ghatdlogger.TransitWith(runtime.ctx, runtime.logger)
	return runtime, nil
}

// SDK returns the runtime's explicitly configured providers, or nil when absent.
func (runtime *Runtime) SDK() *SDK {
	if runtime == nil {
		return nil
	}
	return runtime.sdk
}

// Logger returns the telemetry-enabled application logger. A nil runtime
// returns a no-op logger.
func (runtime *Runtime) Logger() *zap.Logger {
	if runtime == nil || runtime.logger == nil {
		return zap.NewNop()
	}
	return runtime.logger
}

// Context returns the starting context with the runtime and logger attached.
func (runtime *Runtime) Context() context.Context {
	if runtime == nil || runtime.ctx == nil {
		return context.Background()
	}
	return runtime.ctx
}

// RuntimeFromContext returns the runtime attached by StartRuntime, or nil.
// Contexts derived for requests, commands, or operations retain this value.
func RuntimeFromContext(ctx context.Context) *Runtime {
	if ctx == nil {
		return nil
	}
	runtime, _ := ctx.Value(runtimeContextKey{}).(*Runtime)
	return runtime
}

// Shutdown flushes providers with a fresh timeout created at shutdown time.
// It retains the supplied context's values while detaching cancellation and
// expired deadlines. Nil uses the runtime's starting context. The configured
// deadline is shared by all providers; exporters must honor their context.
// Repeated or concurrent calls return the first SDK shutdown result.
func (runtime *Runtime) Shutdown(ctx context.Context) error {
	if runtime == nil {
		return nil
	}
	if ctx == nil {
		ctx = runtime.Context()
	}
	timeout := runtime.shutdownTimeout
	if timeout == 0 {
		timeout = defaultRuntimeShutdownTimeout
	}
	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), timeout)
	defer cancel()
	return runtime.sdk.Shutdown(shutdownCtx)
}
