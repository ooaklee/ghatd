package observability

import (
	"context"
	"time"

	"go.uber.org/zap"
)

// ShutdownConfig orders a process's drain, work cancellation, dependency
// cleanup and telemetry flush. Each callback is optional. Drain must stop
// admission promptly and return a completion channel; it must not block.
// Cleanup must honor its context. Shutdown should normally be Runtime.Shutdown,
// which supplies a fresh telemetry timeout. Timeout bounds each earlier phase
// independently; zero or negative selects the runtime default of 15 seconds.
type ShutdownConfig struct {
	Timeout    time.Duration
	Drain      func() <-chan struct{}
	CancelWork context.CancelFunc
	Cleanup    func(context.Context) error
	Shutdown   func(context.Context) error
	Logger     *zap.Logger
}

// ShutdownResources preserves context values after process cancellation. It
// always attempts telemetry shutdown, including after a cleanup panic, and
// leaves the panic or the caller's business result intact. Diagnostics use
// fixed messages rather than arbitrary dependency/exporter error text.
// This does not force a context-ignoring cleanup function or job to terminate.
func ShutdownResources(ctx context.Context, config ShutdownConfig) {
	if ctx == nil {
		ctx = context.Background()
	}
	if config.Timeout <= 0 {
		config.Timeout = defaultRuntimeShutdownTimeout
	}
	log := config.Logger
	if log == nil {
		log = zap.NewNop()
	}
	defer func() {
		if config.Shutdown != nil {
			if err := config.Shutdown(ctx); err != nil {
				log.Error("observability-shutdown-failed")
			}
		}
	}()
	if config.CancelWork != nil {
		defer config.CancelWork()
	}
	if config.Drain != nil {
		func() {
			drainCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), config.Timeout)
			defer cancel()
			if done := config.Drain(); done != nil {
				select {
				case <-done:
				case <-drainCtx.Done():
					log.Warn("observability-work-drain-timeout")
				}
			}
		}()
	}
	if config.CancelWork != nil {
		config.CancelWork()
	}
	if config.Cleanup != nil {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), config.Timeout)
		defer cancel()
		if err := config.Cleanup(cleanupCtx); err != nil {
			log.Error("observability-resource-cleanup-failed")
		}
	}
}
