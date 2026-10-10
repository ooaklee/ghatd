package catalogue

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"unicode/utf8"
)

var ErrNotSelectable = errors.New("catalogue/not-selectable")

// Entry contains shared policy and attribution; domain records add their own
// immutable definitions. Storage tags describe the model, not a datastore port.
type Entry struct {
	Code        string   `json:"code" bson:"code"`
	Name        string   `json:"name" bson:"name"`
	Enabled     bool     `json:"enabled" bson:"enabled"`
	Hidden      bool     `json:"hidden" bson:"hidden"`
	FlagID      string   `json:"flag_id,omitempty" bson:"flag_id,omitempty"`
	RegionCodes []string `json:"region_codes,omitempty" bson:"region_codes,omitempty"`
	Audit       `bson:",inline"`
}

// Repository is the typed persistence port behind Lifecycle: keyed reads,
// insert-if-absent creation, revision-guarded replace and paged listing.
type Repository[T any] interface {
	// Get reads one typed record by its natural code key through the Repository
	// persistence port behind Lifecycle. Returns the record or an error;
	// implementations forward repository absence and failure semantics unchanged.
	Get(context.Context, string) (T, error)
	// InsertIfAbsent creates the typed record only when its key is absent, per the
	// Repository port's insert-if-absent contract. Returns the stored record,
	// whether it was newly inserted, or an error.
	InsertIfAbsent(context.Context, T) (T, bool, error)
	// Replace updates the typed record through the Repository port guarded by the
	// supplied expected revision. Returns the replaced record or an error when the
	// revision is stale; ctx governs cancellation.
	Replace(context.Context, T, int) (T, error)
	// List returns a page of typed records plus the total matching count through
	// the Repository port, applying ListQuery sanitisation before the underlying
	// read.
	List(context.Context, ListQuery) ([]T, int64, error)
}

// CreateRequest carries a new record and its verified-context actor; ActorID is
// excluded from JSON because it is never transport-decoded.
type CreateRequest[T any] struct {
	Record  T      `json:"record"`
	ActorID string `json:"-"`
}

// UpdateRequest carries a full replacement record with the caller's expected
// revision and verified-context actor for compare-and-swap updates.
type UpdateRequest[T any] struct {
	Record           T      `json:"record"`
	ExpectedRevision int    `json:"-"`
	ActorID          string `json:"-"`
}

// ChangeRequest selects a record by code with the expected revision and the
// verified-context actor requesting the state change.
type ChangeRequest struct {
	Code             string
	ExpectedRevision int
	ActorID          string `json:"-"`
}

// Lifecycle implements shared audit and availability rules behind a typed
// repository. Each child supplies its own definition validation and entry view.
type Lifecycle[T any] struct {
	repo     Repository[T]
	clock    Clock
	entry    func(*T) *Entry
	validate func(T) error
}

// NewLifecycle wires a typed lifecycle over a repository, entry accessor and
// record validator, defaulting to the real clock when none is supplied. Nil
// wiring returns ErrUnavailable.
func NewLifecycle[T any](repo Repository[T], clock Clock, entry func(*T) *Entry, validate func(T) error) (*Lifecycle[T], error) {
	if repo == nil || (reflect.ValueOf(repo).Kind() == reflect.Pointer && reflect.ValueOf(repo).IsNil()) || entry == nil || validate == nil {
		return nil, ErrUnavailable
	}
	if clock == nil {
		clock = RealClock{}
	}
	return &Lifecycle[T]{repo: repo, clock: clock, entry: entry, validate: validate}, nil
}

// Get reads one record by its natural code key, forwarding repository absence
// and failure semantics unchanged.
func (s *Lifecycle[T]) Get(ctx context.Context, code string) (T, error) { return s.repo.Get(ctx, code) }

// List returns a page of records plus the total matching count, applying
// ListQuery sanitisation before the repository read.
func (s *Lifecycle[T]) List(ctx context.Context, q ListQuery) ([]T, int64, error) {
	return s.repo.List(ctx, q.Sanitised())
}

// RequireSelectable reads a record by code and returns ErrNotSelectable unless
// its entry is enabled, unhidden and undeleted.
func (s *Lifecycle[T]) RequireSelectable(ctx context.Context, code string) (T, error) {
	record, err := s.Get(ctx, code)
	if err != nil {
		return record, err
	}
	base := s.entry(&record)
	if !Selectable(base.Enabled, base.Hidden, base.DeletedAt) {
		var zero T
		return zero, ErrNotSelectable
	}
	return record, nil
}

// validateActor accepts only a non-blank actor of at most 256 bytes that is
// already trimmed and free of control characters.
func validateActor(actor string) error {
	if actor == "" || len(actor) > 256 || strings.TrimSpace(actor) != actor || strings.ContainsAny(actor, "\r\n\x00") {
		return ErrInvalidPayload
	}
	return nil
}

// validateRecord enforces shared entry invariants (code, name, region codes,
// flag length) before delegating to the domain-specific validator supplied at
// construction.
func (s *Lifecycle[T]) validateRecord(record T) error {
	base := s.entry(&record)
	if base.Code == "" || len(base.Code) > 100 || strings.TrimSpace(base.Code) != base.Code || strings.ContainsAny(base.Code, "\r\n\x00") || strings.TrimSpace(base.Name) == "" || utf8.RuneCountInString(base.Name) > 200 || strings.ContainsAny(base.Name, "\r\n\x00") {
		return ErrInvalidPayload
	}
	if len(base.RegionCodes) > 250 || len(base.FlagID) > 40 {
		return ErrInvalidPayload
	}
	for _, region := range base.RegionCodes {
		if len(region) != 2 || region[0] < 'A' || region[0] > 'Z' || region[1] < 'A' || region[1] > 'Z' {
			return ErrInvalidPayload
		}
	}
	return s.validate(record)
}

// prepare validates actor and record, then stamps a fresh audit block with
// revision 1 and the UTC creation time for insertion.
func (s *Lifecycle[T]) prepare(record T, actor string) (T, error) {
	if err := validateActor(actor); err != nil {
		return record, err
	}
	if err := s.validateRecord(record); err != nil {
		return record, err
	}
	now := s.clock.Now().UTC()
	s.entry(&record).Audit = Audit{Revision: 1, CreatedAt: now, UpdatedAt: &now, CreatedBy: actor, UpdatedBy: actor}
	return record, nil
}

// Create validates and prepares a new record, then inserts it if absent; an
// existing code returns ErrAlreadyExists with a zero record rather than
// overwriting administrator edits.
func (s *Lifecycle[T]) Create(ctx context.Context, req CreateRequest[T]) (T, error) {
	record, err := s.prepare(req.Record, req.ActorID)
	if err != nil {
		return record, err
	}
	stored, inserted, err := s.repo.InsertIfAbsent(ctx, record)
	if err != nil {
		return stored, err
	}
	if !inserted {
		var zero T
		return zero, ErrAlreadyExists
	}
	return stored, nil
}

// Update replaces a record after validating actor, payload, expected revision
// and non-deleted current state, preserving original audit attribution while
// bumping revision and updated-by. Stale revisions return ErrStaleWrite.
func (s *Lifecycle[T]) Update(ctx context.Context, req UpdateRequest[T]) (T, error) {
	var zero T
	if req.ExpectedRevision < 1 {
		return zero, ErrStaleWrite
	}
	if err := validateActor(req.ActorID); err != nil {
		return zero, err
	}
	if err := s.validateRecord(req.Record); err != nil {
		return zero, err
	}
	input := req.Record
	base := s.entry(&input)
	previous, err := s.repo.Get(ctx, base.Code)
	if err != nil {
		return zero, err
	}
	old := s.entry(&previous)
	if old.Revision != req.ExpectedRevision {
		return zero, ErrStaleWrite
	}
	if old.DeletedAt != nil {
		return zero, ErrNotSelectable
	}
	base.Audit = old.Audit
	now := s.clock.Now().UTC()
	base.Revision++
	base.UpdatedAt = &now
	base.UpdatedBy = req.ActorID
	return s.repo.Replace(ctx, input, req.ExpectedRevision)
}

// Delete soft-deletes a record selected by ChangeRequest, disabling it and
// recording the deleting actor under revision guard.
func (s *Lifecycle[T]) Delete(ctx context.Context, req ChangeRequest) (T, error) {
	return s.changeState(ctx, req, false)
}

// Restore clears the soft-delete marker of a deleted record under revision
// guard, leaving it disabled until separately enabled.
func (s *Lifecycle[T]) Restore(ctx context.Context, req ChangeRequest) (T, error) {
	return s.changeState(ctx, req, true)
}

// changeState implements Delete and Restore: it validates revision and actor,
// requires the record's deleted state to match the requested transition, then
// bumps revision and rewrites the soft-delete markers via compare-and-swap.
func (s *Lifecycle[T]) changeState(ctx context.Context, req ChangeRequest, restore bool) (T, error) {
	var zero T
	if req.ExpectedRevision < 1 {
		return zero, ErrStaleWrite
	}
	if err := validateActor(req.ActorID); err != nil {
		return zero, err
	}
	record, err := s.repo.Get(ctx, req.Code)
	if err != nil {
		return zero, err
	}
	base := s.entry(&record)
	if base.Revision != req.ExpectedRevision {
		return zero, ErrStaleWrite
	}
	if restore != (base.DeletedAt != nil) {
		return zero, ErrInvalidPayload
	}
	now := s.clock.Now().UTC()
	base.Revision++
	base.UpdatedAt = &now
	base.UpdatedBy = req.ActorID
	base.Enabled = false
	if restore {
		base.DeletedAt = nil
		base.DeletedBy = ""
	} else {
		base.DeletedAt = &now
		base.DeletedBy = req.ActorID
	}
	return s.repo.Replace(ctx, record, req.ExpectedRevision)
}

// Seed is insertion-only and safe to call again after interrupted migrations.
// A known duplicate is preserved; native/uncertain failures stop the migration.
func (s *Lifecycle[T]) Seed(ctx context.Context, records []T) error {
	for _, record := range records {
		if err := ctx.Err(); err != nil {
			return err
		}
		prepared, err := s.prepare(record, SystemSeedActor)
		if err != nil {
			return err
		}
		if _, _, err = s.repo.InsertIfAbsent(ctx, prepared); err != nil {
			return err
		}
	}
	return nil
}
