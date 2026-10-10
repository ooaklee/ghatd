// Package globalflagger owns the internationalisation flag catalogue: each
// canonical flag is stored once as sanitised SVG and referenced by
// identifier from other catalogue records (currency, phone country and
// timezone records may carry a flag_id; one flag serves many referers).
//
// globalflagger is a trusted lower API. It performs real validation and
// persistence semantics but does not authorise callers; verified admin
// admission is owned by the internationalisation manager that composes it.
// ActorID on every command is command context (json:"-") and is never
// trusted from a public body.
//
// Flags are decoration: a missing or hidden flag must never invalidate an
// otherwise valid currency or telephone number.
package globalflagger

import (
	"context"

	"github.com/ooaklee/ghatd/external/catalogue"
)

// Record is the public shape of a flag record. Code is the stable
// UPPERCASE identifier exposed to consumers (GB, EU, GB-SCT); the asset
// filename it was seeded from is lowercase. SVG is the sanitised,
// re-encoded canonical image; public list responses may omit it while the
// manager's GET endpoint serves it with safe headers.
type Record struct {
	ID      string `json:"id" bson:"-"`
	Code    string `json:"code" bson:"-"`
	Name    string `json:"name" bson:"-"`
	Enabled bool   `json:"enabled" bson:"-"`
	Hidden  bool   `json:"hidden" bson:"-"`
	SVG     string `json:"svg,omitempty" bson:"-"`

	catalogue.Audit `json:",inline" bson:"-"`
}

// Repository is the narrow typed persistence port for globalflagger. It is
// storage-only: all validation and audit generation live in the Service.
// Implementations must preserve absence (catalogue.ErrNotFound) versus
// native failure, expected-revision compare-and-swap semantics
// (catalogue.ErrStaleWrite) and never expose soft-deleted records in
// public lists.
type Repository interface {
	// Get returns one record by its UPPERCASE code, including soft-deleted
	// and hidden records so archived display remains possible.
	Get(ctx context.Context, code string) (*Record, error)
	// GetByID returns one record by identifier.
	GetByID(ctx context.Context, id string) (*Record, error)
	// List returns a bounded, ordered page plus the total matching count.
	// With publicSelectableOnly it excludes soft-deleted, hidden and
	// disabled records; the admin view is controlled via the query flags.
	List(ctx context.Context, query catalogue.ListQuery, publicSelectableOnly bool) ([]Record, int64, error)
	// Create persists a new record; a duplicate code is
	// catalogue.ErrAlreadyExists.
	Create(ctx context.Context, record *Record) (*Record, error)
	// ReplaceWithRevision persists an updated record when the expected
	// revision matches; otherwise catalogue.ErrStaleWrite.
	ReplaceWithRevision(ctx context.Context, record *Record, expectedRevision int) (*Record, error)
	// Count returns the number of non-soft-deleted records.
	Count(ctx context.Context) (int64, error)
	// InsertIfAbsent seeds one record keyed by code and never overwrites an
	// existing record, including admin edits, disablement, hidden or
	// deleted state, or audit attribution. The bool reports insertion.
	InsertIfAbsent(ctx context.Context, record *Record) (*Record, bool, error)
}
