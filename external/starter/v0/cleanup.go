package starter

import (
	"context"
	"errors"
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
	var errs []error
	for _, fn := range g.cleanups {
		if err := fn(ctx); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
