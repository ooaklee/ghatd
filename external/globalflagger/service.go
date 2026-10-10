package globalflagger

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	"github.com/ooaklee/ghatd/external/catalogue"
)

// Service owns flag validation, audit generation and lifecycle semantics.
// It has no datastore dependency: persistence is behind the narrow typed
// Repository port. Caller authorisation is out of scope; the composing
// internationalisation manager admits verified admins before commands
// reach this service.
type Service struct {
	repository Repository
	clock      catalogue.Clock
}

// NewService wires the service. A nil clock defaults to the real clock;
// a nil repository is a wiring error surfaced on first use as
// catalogue.ErrUnavailable.
func NewService(repository Repository, clock catalogue.Clock) *Service {
	if clock == nil {
		clock = catalogue.RealClock{}
	}
	if repository != nil && reflect.ValueOf(repository).Kind() == reflect.Pointer && reflect.ValueOf(repository).IsNil() {
		repository = nil
	}
	return &Service{repository: repository, clock: clock}
}

// CreateFlagRequest is a trusted command. ActorID is command context and
// is never accepted from a public body. Flags are enabled by default;
// Hidden is an explicit opt-in at creation time.
type CreateFlagRequest struct {
	Code string `json:"-"`
	Name string `json:"name"`
	SVG  string `json:"svg"`
	// Enabled is optional: nil means the enabled default (true); an
	// explicit false is respected.
	Enabled *bool  `json:"enabled,omitempty"`
	Hidden  bool   `json:"hidden"`
	ActorID string `json:"-"`
}

// UpdateFlagRequest is a trusted compare-and-swap command. ExpectedRevision
// must match the current revision or the mutation is rejected. SVG is
// optional; when present it is re-sanitised. Enable/Disable and Hide/Show
// are ordinary updates and ride the same revision semantics.
type UpdateFlagRequest struct {
	Code             string `json:"-"`
	Name             string `json:"name"`
	SVG              string `json:"svg,omitempty"`
	Hidden           bool   `json:"hidden"`
	Enabled          bool   `json:"enabled"`
	ExpectedRevision int    `json:"-"`
	ActorID          string `json:"-"`
}

// DeleteFlagRequest soft-deletes a flag: archived records referencing it
// keep rendering, new selections exclude it. The record is restored
// disabled; re-enabling requires a later explicit update.
type DeleteFlagRequest struct {
	Code             string `json:"-"`
	ExpectedRevision int    `json:"-"`
	ActorID          string `json:"-"`
}

// RestoreFlagRequest restores a soft-deleted flag as disabled unless
// EnabledAfterRestore is explicitly set. ExpectedRevision is the revision
// observed while deleted.
type RestoreFlagRequest struct {
	Code             string `json:"-"`
	ExpectedRevision int    `json:"-"`
	// EnabledAfterRestore is optional; nil (the default) restores the
	// record disabled so re-enabling is always an explicit later update.
	EnabledAfterRestore *bool  `json:"-"`
	ActorID             string `json:"-"`
}

// FlagListResult carries one bounded page plus the unpaged total.
type FlagListResult struct {
	Flags []Record `json:"flags"`
	Total int64    `json:"total"`
}

// Get returns one flag by UPPERCASE code, including hidden and
// soft-deleted records so archived display remains possible.
func (s *Service) Get(ctx context.Context, code string) (*Record, error) {
	normalised, err := validateCode(code)
	if err != nil {
		return nil, err
	}
	return s.repo().Get(ctx, normalised)
}

// GetByID returns one flag by identifier.
func (s *Service) GetByID(ctx context.Context, id string) (*Record, error) {
	if strings.TrimSpace(id) == "" {
		return nil, ErrCodeRequired
	}
	return s.repo().GetByID(ctx, id)
}

// List returns the admin view controlled by the query flags (deleted,
// hidden and disabled are opt-in).
func (s *Service) List(ctx context.Context, query catalogue.ListQuery) (*FlagListResult, error) {
	flags, total, err := s.repo().List(ctx, query, false)
	if err != nil {
		return nil, err
	}
	if flags == nil {
		flags = []Record{}
	}
	return &FlagListResult{Flags: flags, Total: total}, nil
}

// ListPublic returns the public selection view: only enabled, visible,
// non-deleted records.
func (s *Service) ListPublic(ctx context.Context, query catalogue.ListQuery) (*FlagListResult, error) {
	flags, total, err := s.repo().List(ctx, query, true)
	if err != nil {
		return nil, err
	}
	if flags == nil {
		flags = []Record{}
	}
	return &FlagListResult{Flags: flags, Total: total}, nil
}

// Create validates and persists a new flag. The SVG is sanitised (or
// rejected) here; only sanitised bytes are ever persisted.
func (s *Service) Create(ctx context.Context, req *CreateFlagRequest) (*Record, error) {
	if req == nil {
		return nil, fmt.Errorf("%w: request is required", catalogue.ErrInvalidPayload)
	}
	if err := validateActor(req.ActorID); err != nil {
		return nil, err
	}
	code, err := validateCode(req.Code)
	if err != nil {
		return nil, err
	}
	name, err := validateName(req.Name)
	if err != nil {
		return nil, err
	}
	sanitised, err := SanitiseSVG([]byte(req.SVG))
	if err != nil {
		return nil, err
	}

	count, err := s.repo().Count(ctx)
	if err != nil {
		return nil, err
	}
	if count >= catalogue.MaxSupportedRecords {
		return nil, ErrCatalogueFull
	}

	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	now := s.clock.Now().UTC()
	record := &Record{
		Code:    code,
		Name:    name,
		Enabled: enabled,
		Hidden:  req.Hidden,
		SVG:     string(sanitised),
		Audit: catalogue.Audit{
			Revision:  1,
			CreatedAt: now,
			CreatedBy: req.ActorID,
			UpdatedAt: &now,
			UpdatedBy: req.ActorID,
		},
	}
	return s.repo().Create(ctx, record)
}

// Update applies a compare-and-swap update. Original creation metadata is
// preserved; revision advances on success.
func (s *Service) Update(ctx context.Context, req *UpdateFlagRequest) (*Record, error) {
	if req == nil {
		return nil, fmt.Errorf("%w: request is required", catalogue.ErrInvalidPayload)
	}
	if err := validateActor(req.ActorID); err != nil {
		return nil, err
	}
	if req.ExpectedRevision < 1 {
		return nil, ErrExpectedRevision
	}
	code, err := validateCode(req.Code)
	if err != nil {
		return nil, err
	}
	name, err := validateName(req.Name)
	if err != nil {
		return nil, err
	}

	current, err := s.repo().Get(ctx, code)
	if err != nil {
		return nil, err
	}
	if current.DeletedAt != nil {
		// Soft-deleted records are not editable; restore first.
		return nil, fmt.Errorf("%w: flag is soft-deleted", catalogue.ErrInvalidPayload)
	}
	if current.Revision != req.ExpectedRevision {
		return nil, catalogue.ErrStaleWrite
	}

	updated := *current
	updated.Name = name
	updated.Hidden = req.Hidden
	updated.Enabled = req.Enabled
	if strings.TrimSpace(req.SVG) != "" {
		sanitised, err := SanitiseSVG([]byte(req.SVG))
		if err != nil {
			return nil, err
		}
		updated.SVG = string(sanitised)
	}

	now := s.clock.Now().UTC()
	updated.Revision = current.Revision + 1
	updated.UpdatedAt = &now
	updated.UpdatedBy = req.ActorID

	return s.repo().ReplaceWithRevision(ctx, &updated, req.ExpectedRevision)
}

// Delete soft-deletes a flag. Delete of an already-deleted record is
// reported as ErrAlreadyDeleted rather than a silent success.
func (s *Service) Delete(ctx context.Context, req *DeleteFlagRequest) (*Record, error) {
	if req == nil {
		return nil, fmt.Errorf("%w: request is required", catalogue.ErrInvalidPayload)
	}
	if err := validateActor(req.ActorID); err != nil {
		return nil, err
	}
	if req.ExpectedRevision < 1 {
		return nil, ErrExpectedRevision
	}
	code, err := validateCode(req.Code)
	if err != nil {
		return nil, err
	}

	current, err := s.repo().Get(ctx, code)
	if err != nil {
		return nil, err
	}
	if current.DeletedAt != nil {
		return nil, ErrAlreadyDeleted
	}
	if current.Revision != req.ExpectedRevision {
		return nil, catalogue.ErrStaleWrite
	}

	now := s.clock.Now().UTC()
	updated := *current
	updated.Revision = current.Revision + 1
	updated.DeletedAt = &now
	updated.DeletedBy = req.ActorID
	updated.UpdatedAt = &now
	updated.UpdatedBy = req.ActorID
	updated.Enabled = false // deleted records are never selectable

	return s.repo().ReplaceWithRevision(ctx, &updated, req.ExpectedRevision)
}

// Restore un-deletes a soft-deleted flag. The restored record starts
// disabled unless EnabledAfterRestore is set; creation audit is preserved.
func (s *Service) Restore(ctx context.Context, req *RestoreFlagRequest) (*Record, error) {
	if req == nil {
		return nil, fmt.Errorf("%w: request is required", catalogue.ErrInvalidPayload)
	}
	if err := validateActor(req.ActorID); err != nil {
		return nil, err
	}
	if req.ExpectedRevision < 1 {
		return nil, ErrExpectedRevision
	}
	code, err := validateCode(req.Code)
	if err != nil {
		return nil, err
	}

	current, err := s.repo().Get(ctx, code)
	if err != nil {
		return nil, err
	}
	if current.DeletedAt == nil {
		return nil, ErrNotDeleted
	}
	if current.Revision != req.ExpectedRevision {
		return nil, catalogue.ErrStaleWrite
	}

	enabledAfterRestore := false
	if req.EnabledAfterRestore != nil {
		enabledAfterRestore = *req.EnabledAfterRestore
	}
	now := s.clock.Now().UTC()
	updated := *current
	updated.Revision = current.Revision + 1
	updated.DeletedAt = nil
	updated.DeletedBy = ""
	updated.Enabled = enabledAfterRestore
	updated.UpdatedAt = &now
	updated.UpdatedBy = req.ActorID

	return s.repo().ReplaceWithRevision(ctx, &updated, req.ExpectedRevision)
}

// repo returns the wired repository or an availability error.
func (s *Service) repo() Repository {
	if s == nil || s.repository == nil {
		return unavailableRepository{}
	}
	return s.repository
}

// unavailableRepository fails every call with ErrUnavailable.
type unavailableRepository struct{}

// Get reports the wiring failure catalogue.ErrUnavailable for this repository
// stub.
func (unavailableRepository) Get(context.Context, string) (*Record, error) {
	return nil, catalogue.ErrUnavailable
}

// GetByID reports the wiring failure catalogue.ErrUnavailable for this
// repository stub.
func (unavailableRepository) GetByID(context.Context, string) (*Record, error) {
	return nil, catalogue.ErrUnavailable
}

// List reports the wiring failure catalogue.ErrUnavailable for this repository
// stub.
func (unavailableRepository) List(context.Context, catalogue.ListQuery, bool) ([]Record, int64, error) {
	return nil, 0, catalogue.ErrUnavailable
}

// Create reports the wiring failure catalogue.ErrUnavailable for this
// repository stub.
func (unavailableRepository) Create(context.Context, *Record) (*Record, error) {
	return nil, catalogue.ErrUnavailable
}

// ReplaceWithRevision reports the wiring failure catalogue.ErrUnavailable for
// this repository stub.
func (unavailableRepository) ReplaceWithRevision(context.Context, *Record, int) (*Record, error) {
	return nil, catalogue.ErrUnavailable
}

// Count reports the wiring failure catalogue.ErrUnavailable for this repository
// stub.
func (unavailableRepository) Count(context.Context) (int64, error) {
	return 0, catalogue.ErrUnavailable
}

// InsertIfAbsent reports the wiring failure catalogue.ErrUnavailable for this
// repository stub.
func (unavailableRepository) InsertIfAbsent(context.Context, *Record) (*Record, bool, error) {
	return nil, false, catalogue.ErrUnavailable
}

// ErrFlagReferenceNotFound is a convenience sentinel for consumers
// validating foreign flag_id references. It is an alias of the shared
// catalogue absence sentinel so errors.Is keeps working across domains.
var ErrFlagReferenceNotFound = catalogue.ErrNotFound
