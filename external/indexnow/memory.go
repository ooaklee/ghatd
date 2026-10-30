package indexnow

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"sync"
)

// In-memory repository errors. They carry no attempt content.
var (
	// ErrMemoryRepositoryInvalid is returned for nil contexts, nil records, or
	// records missing identity/timing fields.
	ErrMemoryRepositoryInvalid = errors.New("indexnow: invalid memory repository request")

	// ErrMemoryRepositoryDuplicate is returned when RecordAttempt reuses an
	// existing attempt ID.
	ErrMemoryRepositoryDuplicate = errors.New("indexnow: duplicate attempt id")

	// ErrMemoryRepositoryNotFound is returned when an attempt ID is unknown.
	ErrMemoryRepositoryNotFound = errors.New("indexnow: attempt not found")

	// ErrMemoryRepositoryFinished is returned when FinishAttempt completes an
	// attempt that was already completed with a different final state.
	ErrMemoryRepositoryFinished = errors.New("indexnow: attempt already finished")
)

// memoryListLimit bounds ListAttempts results.
const memoryListLimit = 1000

// MemoryRepository is a concurrency-safe in-memory AttemptRepository. It is
// intended for tests, demos, and single-process deployments without a
// database. State is lost on process restart; there is no queue, deduplication,
// or exactly-once guarantee.
type MemoryRepository struct {
	mutex    sync.RWMutex
	attempts map[string]*Attempt
}

// NewMemoryRepository returns an empty in-memory repository.
func NewMemoryRepository() *MemoryRepository {
	return &MemoryRepository{attempts: make(map[string]*Attempt)}
}

// RecordAttempt stores a copy of the in-flight attempt before any external
// side effect. A nil context or record, or a record without an ID, is invalid;
// duplicate IDs are rejected. The stored record's URL slice is copied so later
// caller mutation cannot rewrite history.
func (m *MemoryRepository) RecordAttempt(ctx context.Context, attempt *Attempt) error {
	if err := m.validateAttempt(ctx, attempt); err != nil {
		return err
	}

	m.mutex.Lock()
	defer m.mutex.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if !attempt.FinishedAt.IsZero() || attempt.Outcome != OutcomeInFlight || attempt.StatusCode != 0 || attempt.Success || attempt.Retryable || attempt.RetryAfter != 0 || attempt.FailureMessage != "" {
		return ErrMemoryRepositoryInvalid
	}
	if m.attempts == nil {
		m.attempts = make(map[string]*Attempt)
	}

	if _, exists := m.attempts[attempt.ID]; exists {
		return fmt.Errorf("%w: %s", ErrMemoryRepositoryDuplicate, "id already recorded")
	}
	m.attempts[attempt.ID] = copyAttempt(attempt)
	return nil
}

// FinishAttempt completes the stored attempt selected by ID with the supplied
// final fields. It never upserts: an unknown ID is an error, and no receipt is
// fabricated. The first completion is final; replaying the identical final
// state is idempotent, while replaying a different final state for the same ID
// reports an explicit conflict.
func (m *MemoryRepository) FinishAttempt(ctx context.Context, attempt *Attempt) error {
	if err := m.validateAttempt(ctx, attempt); err != nil {
		return err
	}

	m.mutex.Lock()
	defer m.mutex.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if attempt.FinishedAt.IsZero() || attempt.FinishedAt.Before(attempt.StartedAt) || attempt.Outcome == OutcomeInFlight || attempt.Outcome == "" {
		return ErrMemoryRepositoryInvalid
	}

	stored, exists := m.attempts[attempt.ID]
	if !exists {
		return ErrMemoryRepositoryNotFound
	}
	if !stored.StartedAt.Equal(attempt.StartedAt) || stored.Method != attempt.Method || !slices.Equal(stored.URLs, attempt.URLs) {
		return ErrMemoryRepositoryInvalid
	}
	if !stored.FinishedAt.IsZero() {
		if sameFinalState(stored, attempt) {
			return nil
		}
		return ErrMemoryRepositoryFinished
	}
	m.attempts[attempt.ID] = copyAttempt(attempt)
	return nil
}

// GetAttempt returns a copy of the attempt selected by id, or
// ErrMemoryRepositoryNotFound.
func (m *MemoryRepository) GetAttempt(ctx context.Context, id string) (*Attempt, error) {
	if m == nil || ctx == nil || id == "" {
		return nil, ErrMemoryRepositoryInvalid
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	stored, exists := m.attempts[id]
	if !exists {
		return nil, ErrMemoryRepositoryNotFound
	}
	result := copyAttempt(stored)
	return result, nil
}

// ListAttempts returns up to limit attempts ordered by start time descending,
// with attempt ID ascending as the stable tiebreaker. Nonpositive limits use
// the bounded default of 1000; limits above 1000 are capped at 1000. Returned
// records are copies.
func (m *MemoryRepository) ListAttempts(ctx context.Context, limit int) ([]Attempt, error) {
	if m == nil || ctx == nil {
		return nil, ErrMemoryRepositoryInvalid
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if limit <= 0 || limit > memoryListLimit {
		limit = memoryListLimit
	}

	m.mutex.RLock()
	if err := ctx.Err(); err != nil {
		m.mutex.RUnlock()
		return nil, err
	}
	records := make([]Attempt, 0, len(m.attempts))
	for _, stored := range m.attempts {
		records = append(records, *copyAttempt(stored))
	}
	m.mutex.RUnlock()

	sort.SliceStable(records, func(i, j int) bool {
		if !records[i].StartedAt.Equal(records[j].StartedAt) {
			return records[i].StartedAt.After(records[j].StartedAt)
		}
		return records[i].ID < records[j].ID
	})

	if len(records) > limit {
		records = records[:limit]
	}
	return records, nil
}

// validateAttempt rejects nil wiring and records before taking the lock.
func (m *MemoryRepository) validateAttempt(ctx context.Context, attempt *Attempt) error {
	if m == nil || ctx == nil {
		return ErrMemoryRepositoryInvalid
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if attempt == nil || attempt.ID == "" || attempt.StartedAt.IsZero() {
		return ErrMemoryRepositoryInvalid
	}
	return nil
}

// copyAttempt returns a deep-enough copy: the URL slice is duplicated so the
// stored history cannot be mutated through the caller's slice.
func copyAttempt(a *Attempt) *Attempt {
	result := *a
	result.URLs = append([]string(nil), a.URLs...)
	return &result
}

// sameFinalState compares the completion-relevant fields of two attempts.
func sameFinalState(a, b *Attempt) bool {
	return a.ID == b.ID && a.StartedAt.Equal(b.StartedAt) && a.Method == b.Method && slices.Equal(a.URLs, b.URLs) && a.RetryAfter == b.RetryAfter && a.Outcome == b.Outcome &&
		a.StatusCode == b.StatusCode &&
		a.Success == b.Success &&
		a.Retryable == b.Retryable &&
		a.FailureMessage == b.FailureMessage &&
		a.FinishedAt.Equal(b.FinishedAt)
}
