package serverhelpers

import (
	"context"
	"errors"
	"sync"
	"time"
)

// Cleanup releases or drains one resource. It must honor context cancellation.
type Cleanup func(context.Context) error

// LifecycleConfig controls cleanup budgets and the final telemetry shutdown.
type LifecycleConfig struct {
	// Timeout bounds each callback's context; nonpositive values select 15 seconds.
	Timeout time.Duration
	// Telemetry is called last, including when a resource callback panics.
	Telemetry Cleanup
}

type ownedResource struct {
	release Cleanup
	drains  []Cleanup
}

// cleanupError preserves classification without printing potentially sensitive
// resource or provider details in the lifecycle's aggregate error.
type cleanupError struct {
	phase string
	cause error
}

func (e cleanupError) Error() string { return "serverhelpers: " + e.phase + " failed" }
func (e cleanupError) Unwrap() error { return e.cause }

// Lifecycle owns registered resources, but does not start workers or services.
// Register resources during startup, before calling Close.
type Lifecycle struct {
	mu        sync.Mutex
	once      sync.Once
	closed    bool
	resources []ownedResource
	config    LifecycleConfig
	result    error
}

// NewLifecycle creates an empty lifecycle. Callbacks receive fresh bounded
// contexts preserving the values of the context supplied to Close.
func NewLifecycle(config LifecycleConfig) *Lifecycle {
	if config.Timeout <= 0 {
		config.Timeout = 15 * time.Second
	}
	return &Lifecycle{config: config}
}

// AddResource registers an acquired resource and the workers borrowing it.
// A worker drain failure prevents this resource's release. Registration fails
// after Close begins and does not invoke any callbacks.
func (l *Lifecycle) AddResource(release Cleanup, drains ...Cleanup) error {
	if l == nil || release == nil {
		return errors.New("serverhelpers: resource release required")
	}
	for _, drain := range drains {
		if drain == nil {
			return errors.New("serverhelpers: resource drain required")
		}
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return errors.New("serverhelpers: lifecycle already closed")
	}
	l.resources = append(l.resources, ownedResource{release: release, drains: append([]Cleanup(nil), drains...)})
	return nil
}

// Close drains and releases resources in reverse acquisition order, then shuts
// down telemetry. It is safe to call more than once, including concurrently.
// A failed drain suppresses only its resource's release; independent cleanup
// continues. Errors identify phases without exposing callback error details.
// Their causes remain available through errors.Is and errors.As.
// A callback panic propagates after telemetry runs. Callbacks must cooperate
// with their contexts; Close does not launch or abandon cleanup goroutines.
func (l *Lifecycle) Close(ctx context.Context) error {
	if l == nil {
		return nil
	}
	l.once.Do(func() {
		l.mu.Lock()
		l.closed = true
		resources := append([]ownedResource(nil), l.resources...)
		l.mu.Unlock()
		if ctx == nil {
			ctx = context.Background()
		}
		base := context.WithoutCancel(ctx)
		var result error
		finished := false
		resourcesFinished := false
		defer func() {
			if !finished {
				result = errors.Join(result, errors.New("serverhelpers: cleanup interrupted by panic"))
			}
			l.result = result
		}()
		defer func() {
			if l.config.Telemetry != nil {
				if err := runCleanup(base, l.config.Timeout, l.config.Telemetry); err != nil {
					result = errors.Join(result, cleanupError{phase: "telemetry shutdown", cause: err})
				}
			}
			finished = resourcesFinished
		}()
		for i := len(resources) - 1; i >= 0; i-- {
			resource := resources[i]
			failed := false
			for _, drain := range resource.drains {
				if err := runCleanup(base, l.config.Timeout, drain); err != nil {
					failed = true
					result = errors.Join(result, cleanupError{phase: "worker drain", cause: err})
				}
			}
			if !failed {
				if err := runCleanup(base, l.config.Timeout, resource.release); err != nil {
					result = errors.Join(result, cleanupError{phase: "resource release", cause: err})
				}
			}
		}
		resourcesFinished = true
	})
	return l.result
}
func runCleanup(base context.Context, timeout time.Duration, callback Cleanup) error {
	ctx, cancel := context.WithTimeout(base, timeout)
	defer cancel()
	err := callback(ctx)
	if err != nil {
		return err
	}
	return ctx.Err()
}
