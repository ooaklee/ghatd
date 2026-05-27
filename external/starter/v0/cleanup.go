package starter

import (
	"context"
	"errors"

	"github.com/ooaklee/ghatd/external/logger"
	"go.uber.org/zap"
)

// CleanupGroup aggregates multiple Cleanup functions into a single Cleanup.
// Every added cleanup is invoked when Run is called; nil functions are silently
// skipped. All non-nil cleanups run even when earlier ones return errors.
// Errors are joined with errors.Join.
type CleanupGroup struct {
	cleanups []Cleanup
}

// Add appends one or more Cleanup functions. Nil entries are silently ignored.
func (g *CleanupGroup) Add(fns ...Cleanup) {
	if g == nil {
		return
	}
	for _, fn := range fns {
		if fn != nil {
			g.cleanups = append(g.cleanups, fn)
		}
	}
}

// Run invokes every registered Cleanup in insertion order and returns all
// errors collected via errors.Join. Every cleanup is called even when
// earlier ones fail.
func (g *CleanupGroup) Run(ctx context.Context) error {
	if g == nil {
		return nil
	}
	logger := logger.AcquireOperationFrom(ctx, "external/starter/v0", "cleanup-run")
	logger.Info("starter-cleanup-started", zap.Int("cleanup-count", len(g.cleanups)))

	var errs []error
	for index, fn := range g.cleanups {
		if err := fn(ctx); err != nil {
			logger.Error("starter-cleanup-failed", zap.Int("cleanup-index", index), zap.Error(err))
			errs = append(errs, err)
			continue
		}
		logger.Debug("starter-cleanup-completed", zap.Int("cleanup-index", index))
	}

	joinedErr := errors.Join(errs...)
	if joinedErr != nil {
		logger.Error("starter-cleanup-completed-with-errors", zap.Int("error-count", len(errs)), zap.Error(joinedErr))
		return joinedErr
	}

	logger.Info("starter-cleanup-completed")
	return nil
}
