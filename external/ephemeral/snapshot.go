package ephemeral

import (
	"context"
	"errors"
)

// ErrInvalidSnapshot rejects uninitialized stores, nil contexts/operations and
// nested operations on the same store using the callback's context.
var ErrInvalidSnapshot = errors.New("ephemeral/invalid-memory-snapshot")

// MemorySnapshot is a process-local copy-on-write fixture, not durable storage,
// a distributed lock, a TTL cache, or a replacement for Redis authentication.
// Callbacks are serialized. State is cloned on entry and before commit so a
// retained callback value cannot mutate committed state after returning.
// Do not copy the store after construction. Snapshot costs are O(state size).
type MemorySnapshot[T any] struct {
	// gate allows cancelled callers to stop waiting for another callback.
	gate chan struct{}
	// state is accessed only while gate is held.
	state T
	// clone must deeply detach ALL mutable state and must be deterministic,
	// non-reentrant and free of external side effects. Shallow clones are unsafe.
	clone func(T) T
}

// snapshotContextKey privately identifies stores entered by a callback context.
type snapshotContextKey struct{}

// snapshotEntry forms a chain so indirect nesting is rejected as well.
type snapshotEntry struct {
	store  any            // store is a comparable pointer identifying an entered snapshot.
	parent *snapshotEntry // parent preserves outer entries across different stores.
}

// NewMemorySnapshot creates detached local state. It requires an explicit deep
// clone instead of guessing how arbitrary application values should be copied.
func NewMemorySnapshot[T any](initial T, clone func(T) T) (*MemorySnapshot[T], error) {
	if clone == nil {
		return nil, ErrInvalidSnapshot
	}
	return &MemorySnapshot[T]{gate: make(chan struct{}, 1), state: clone(initial), clone: clone}, nil
}

// Update invokes callback on a detached snapshot and atomically replaces state
// only on success while the context remains live. Errors, panics and cancellation
// discard the snapshot. The callback must not spawn work using the snapshot or
// re-enter this store with a detached context; mutable callback data is not safe
// for concurrent access. Panics propagate after releasing the gate.
func (s *MemorySnapshot[T]) Update(ctx context.Context, callback func(context.Context, *T) error) error {
	return s.run(ctx, true, callback)
}

// View supplies a detached snapshot and never commits callback mutations. This
// intentionally spends a deep copy to prevent aliasing via escaped references.
func (s *MemorySnapshot[T]) View(ctx context.Context, callback func(context.Context, *T) error) error {
	return s.run(ctx, false, callback)
}

// run owns entry, cancellation and cloning for both read and write callbacks.
func (s *MemorySnapshot[T]) run(ctx context.Context, commit bool, callback func(context.Context, *T) error) error {
	if s == nil || s.gate == nil || s.clone == nil || ctx == nil || callback == nil {
		return ErrInvalidSnapshot
	}
	parent, _ := ctx.Value(snapshotContextKey{}).(*snapshotEntry)
	for entry := parent; entry != nil; entry = entry.parent {
		if entry.store == s {
			return ErrInvalidSnapshot
		}
	}
	select {
	case s.gate <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	defer func() { <-s.gate }()
	if err := ctx.Err(); err != nil {
		return err
	}
	snapshot := s.clone(s.state)
	if err := callback(context.WithValue(ctx, snapshotContextKey{}, &snapshotEntry{store: s, parent: parent}), &snapshot); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if commit {
		copy := s.clone(snapshot)
		if err := ctx.Err(); err != nil {
			return err
		}
		s.state = copy
	}
	return nil
}
