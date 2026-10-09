// Package catalogue provides the shared record shape, audit semantics and
// bounded-list primitives used by the internationalisation child domains
// (currencycoder, telenumcoder, globalflagger, timezonecoder).
//
// These packages are trusted lower APIs. They own real validation and
// persistence semantics behind narrow typed repository ports, but they do
// not authorise callers: verified admin admission is owned by the
// internationalisation manager that composes them. ActorID on every command
// is command context (json:"-"), never accepted from a public body.
//
// Audit on every persisted record: revision, created_at, updated_at,
// deleted_at (nullable), created_by, updated_by, deleted_by. Services
// generate audit values with an injectable clock; original creation
// metadata is preserved on edit and restore. Admin writes require a
// matching expected revision; only a successful compare-and-swap mutation
// is honoured. Deletion is soft and restore is explicit.
package catalogue

import (
	"errors"
	"time"
)

// SystemSeedActor is the built-in actor attribution used by idempotent
// bootstrap seeding. Seed inserts never overwrite subsequent admin edits,
// disablement, hidden or deleted state, or audit attribution.
const SystemSeedActor = "system:catalogue-v1"

// Shared sentinel semantics used by every child-domain repository port.
// Absence (not found) is always distinct from failure; adapters must never
// flatten native storage errors into not-found.
var (
	// ErrNotFound reports a record absent by identity. It is not a failure
	// of the underlying store.
	ErrNotFound = errors.New("catalogue/not-found")
	// ErrStaleWrite reports the expected revision did not match; the
	// compare-and-swap mutation was not applied.
	ErrStaleWrite = errors.New("catalogue/stale-write")
	// ErrAlreadyExists reports a duplicate natural key.
	ErrAlreadyExists = errors.New("catalogue/already-exists")
	// ErrUnavailable reports unusable wiring or an uncertain outcome whose
	// underlying write state is unknown and must not be blindly retried.
	ErrUnavailable = errors.New("catalogue/unavailable")
	// ErrInvalidPayload reports domain validation failure before any I/O.
	ErrInvalidPayload = errors.New("catalogue/invalid-payload")
)

// Audit is persisted on every catalogue record. Revision drives optimistic
// concurrency for admin writes; deleted_at/deleted_by carry soft deletion.
// Time values are UTC.
type Audit struct {
	Revision  int        `json:"revision" bson:"revision"`
	CreatedAt time.Time  `json:"created_at" bson:"created_at"`
	UpdatedAt *time.Time `json:"updated_at,omitempty" bson:"updated_at,omitempty"`
	DeletedAt *time.Time `json:"deleted_at,omitempty" bson:"deleted_at,omitempty"`
	CreatedBy string     `json:"created_by" bson:"created_by"`
	UpdatedBy string     `json:"updated_by,omitempty" bson:"updated_by,omitempty"`
	DeletedBy string     `json:"deleted_by,omitempty" bson:"deleted_by,omitempty"`
}

// Clock supplies current instants. Services accept an injectable
// implementation so tests are deterministic; production uses RealClock.
type Clock interface {
	Now() time.Time
}

// RealClock returns the wall-clock time in UTC.
type RealClock struct{}

// Now implements Clock.
func (RealClock) Now() time.Time { return time.Now().UTC() }

// ClockFunc adapts a function to Clock.
type ClockFunc func() time.Time

// Now implements Clock.
func (f ClockFunc) Now() time.Time { return f() }

// MaxPageSize bounds list requests; catalogues are small but must not
// degrade into unbounded scans.
const (
	MaxPageSize     = 500
	DefaultPageSize = 100
	// MaxSupportedRecords is the bounded catalogue size ceiling enforced on
	// create/seed writes. Catalogues are reference data, not user content.
	MaxSupportedRecords = 1000
)

// ListQuery is the bounded list shape shared by catalogue services.
// Page/PageSize are 1-based and clamped to MaxPageSize. IncludeDeleted and
// IncludeHidden/IncludeDisabled are opt-in admin views; public selection
// lists always exclude soft-deleted, hidden and disabled records.
type ListQuery struct {
	Page            int
	PageSize        int
	IncludeDeleted  bool
	IncludeHidden   bool
	IncludeDisabled bool
	// Search optionally filters by stable key/code prefix (case-insensitive).
	Search string
}

// Sanitised returns a bounded copy of the query with defaults applied.
func (q ListQuery) Sanitised() ListQuery {
	if q.Page > 1000000 {
		q.Page = 1000000
	}
	if q.Page < 1 {
		q.Page = 1
	}
	if q.PageSize < 1 {
		q.PageSize = DefaultPageSize
	}
	if q.PageSize > MaxPageSize {
		q.PageSize = MaxPageSize
	}
	if len(q.Search) > 64 {
		q.Search = q.Search[:64]
	}
	return q
}

// Offset converts the sanitised page to a zero-based offset.
func (q ListQuery) Offset() int {
	s := q.Sanitised()
	return (s.Page - 1) * s.PageSize
}

// Selectable reports whether a record is available for NEW public choices.
// Soft-deleted, hidden and disabled records are excluded; archived records
// referencing them remain displayable with their immutable metadata.
func Selectable(enabled bool, hidden bool, deletedAt *time.Time) bool {
	return enabled && !hidden && deletedAt == nil
}
