package blueprint

import (
	"context"
	"strings"

	"github.com/ooaklee/ghatd/external/logger"
)

// BlueprintRepository is the domain persistence port. Implementations must
// preserve native failures and return the selected record, not a nil success.
// Callers and adapters treat nested Metadata values as read-only during a call.
type BlueprintRepository interface {
	CreateBlueprint(ctx context.Context, blueprint *Blueprint) (*Blueprint, error)
	DeleteBlueprintByID(ctx context.Context, id string) error
	GetBlueprintByID(ctx context.Context, id string) (*Blueprint, error)
	GetBlueprintByNameAndKind(ctx context.Context, name, kind string) (*Blueprint, error)
	GetBlueprints(ctx context.Context, req *GetBlueprintsRequest) ([]Blueprint, error)
	GetTotalBlueprints(ctx context.Context, req *GetBlueprintsRequest) (int64, error)
	UpdateBlueprint(ctx context.Context, blueprint *Blueprint) (*Blueprint, error)
}

// Service demonstrates lower-domain validation and persistence orchestration.
// It does not replace route admission or a manager's resource authorization.
type Service struct {
	// BlueprintRepository is required for CRUD; typed-nil adapters fail closed.
	BlueprintRepository BlueprintRepository
	// Registry owns optional package registrations independently of persistence.
	Registry *Registry
}

// NewService uses the supplied registry, or creates an empty registry by default.
func NewService(blueprintRepository BlueprintRepository, registry ...*Registry) *Service {
	resolvedRegistry := MustRegistry()
	if len(registry) > 0 && registry[0] != nil {
		resolvedRegistry = registry[0]
	}
	return &Service{BlueprintRepository: blueprintRepository, Registry: resolvedRegistry}
}

// CreateBlueprint generates identity and creation attribution for an authorized
// caller. Dependency failures are returned unchanged; writes are never retried.
func (s *Service) CreateBlueprint(ctx context.Context, req *CreateBlueprintRequest) (*BlueprintResponse, error) {
	if err := s.validateEntry(ctx, req); err != nil {
		return nil, err
	}
	input := *req
	if normaliseBlueprintName(input.Name) == "" {
		return nil, ErrBlueprintNameIsRequired
	}
	if normaliseBlueprintKind(input.Kind) == "" {
		return nil, ErrBlueprintKindIsRequired
	}
	if !blueprintActorMatchesContext(ctx, input.ActorID) {
		return nil, ErrBlueprintUserIDIsRequired
	}
	log := logger.AcquireOperationFrom(ctx, "internal/blueprint", "create-blueprint")
	value := NewBlueprint(&input).GenerateID().GenerateNanoID().SetCreatedAtTimeToNow()
	id, actor := value.ID, input.ActorID
	created, err := s.BlueprintRepository.CreateBlueprint(ctx, value)
	if err != nil {
		log.Error("blueprint-create-failed")
		return nil, err
	}
	if err := blueprintResult(ctx, created, id); err != nil {
		return nil, err
	}
	if created.CreatedByUserID != actor {
		return nil, ErrBlueprintUnavailable
	}
	log.Info("blueprint-created")
	return &BlueprintResponse{Blueprint: created}, nil
}

// GetBlueprintByID reads a selected record for an authorized caller. ActorID and
// ID are independent; no ownership or administrator authority is inferred here.
func (s *Service) GetBlueprintByID(ctx context.Context, req *GetBlueprintByIDRequest) (*BlueprintResponse, error) {
	if err := s.validateEntry(ctx, req); err != nil {
		return nil, err
	}
	id, actor := strings.TrimSpace(req.ID), req.ActorID
	if id == "" {
		return nil, ErrBlueprintIDIsRequired
	}
	if !blueprintActorMatchesContext(ctx, actor) {
		return nil, ErrBlueprintUserIDIsRequired
	}
	value, err := s.BlueprintRepository.GetBlueprintByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := blueprintResult(ctx, value, id); err != nil {
		return nil, err
	}
	return &BlueprintResponse{Blueprint: value}, nil
}

// GetBlueprintByName is a trusted lower-domain natural-key lookup. A manager
// exposing it must enforce its own permission policy before calling the service.
func (s *Service) GetBlueprintByName(ctx context.Context, req *GetBlueprintByNameRequest) (*BlueprintResponse, error) {
	if err := s.validateEntry(ctx, req); err != nil {
		return nil, err
	}
	name, kind := normaliseBlueprintName(req.Name), normaliseBlueprintKind(req.Kind)
	if name == "" {
		return nil, ErrBlueprintNameIsRequired
	}
	if kind == "" {
		return nil, ErrBlueprintKindIsRequired
	}
	value, err := s.BlueprintRepository.GetBlueprintByNameAndKind(ctx, name, kind)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if value == nil || strings.TrimSpace(value.ID) == "" || value.Name != name || value.Kind != kind {
		return nil, ErrBlueprintUnavailable
	}
	return &BlueprintResponse{Blueprint: value}, nil
}

// GetBlueprints performs independent list/count reads, not a transactional
// snapshot. A nil request retains the unfiltered-query convention. Route or
// manager authorization owns access to this actor-independent lower query.
func (s *Service) GetBlueprints(ctx context.Context, req *GetBlueprintsRequest) (*GetBlueprintsResponse, error) {
	if req == nil {
		req = &GetBlueprintsRequest{}
	}
	if err := s.validateEntry(ctx, req); err != nil {
		return nil, err
	}
	input := *req
	listInput := input
	values, err := s.BlueprintRepository.GetBlueprints(ctx, &listInput)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	total, err := s.BlueprintRepository.GetTotalBlueprints(ctx, &input)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if total < 0 {
		return nil, ErrBlueprintUnavailable
	}
	return &GetBlueprintsResponse{Blueprints: values, Total: total}, nil
}

// UpdateBlueprint applies nonempty scalar fields and an optional metadata map
// to a copy of the selected record. Empty strings retain existing values. Nested
// metadata is read-only by convention, not deeply copied; no CAS is implied.
func (s *Service) UpdateBlueprint(ctx context.Context, req *UpdateBlueprintRequest) (*BlueprintResponse, error) {
	if err := s.validateEntry(ctx, req); err != nil {
		return nil, err
	}
	input := *req
	id := strings.TrimSpace(input.ID)
	if id == "" {
		return nil, ErrBlueprintIDIsRequired
	}
	if !blueprintActorMatchesContext(ctx, input.ActorID) {
		return nil, ErrBlueprintUserIDIsRequired
	}
	current, err := s.BlueprintRepository.GetBlueprintByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := blueprintResult(ctx, current, id); err != nil {
		return nil, err
	}
	value := *current
	if name := normaliseBlueprintName(input.Name); name != "" {
		value.Name = name
	}
	if kind := normaliseBlueprintKind(input.Kind); kind != "" {
		value.Kind = kind
	}
	if input.Description != "" {
		value.Description = strings.TrimSpace(input.Description)
	}
	if status := normaliseBlueprintStatus(input.Status); status != "" {
		value.Status = status
	}
	if input.Metadata != nil {
		value.Metadata = input.Metadata
	}
	value.UpdatedByUserID = input.ActorID
	value.SetUpdatedAtTimeToNow()
	updated, err := s.BlueprintRepository.UpdateBlueprint(ctx, &value)
	if err != nil {
		return nil, err
	}
	if err := blueprintResult(ctx, updated, id); err != nil {
		return nil, err
	}
	if updated.UpdatedByUserID != input.ActorID {
		return nil, ErrBlueprintUnavailable
	}
	return &BlueprintResponse{Blueprint: updated}, nil
}

// DeleteBlueprint removes a selected record after caller validation. Adapters
// must report absence or operational failures; this service does not retry them.
func (s *Service) DeleteBlueprint(ctx context.Context, req *DeleteBlueprintRequest) (*DeleteBlueprintResponse, error) {
	if err := s.validateEntry(ctx, req); err != nil {
		return nil, err
	}
	id, actor := strings.TrimSpace(req.ID), req.ActorID
	if id == "" {
		return nil, ErrBlueprintIDIsRequired
	}
	if !blueprintActorMatchesContext(ctx, actor) {
		return nil, ErrBlueprintUserIDIsRequired
	}
	if err := s.BlueprintRepository.DeleteBlueprintByID(ctx, id); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return &DeleteBlueprintResponse{Deleted: true}, nil
}

// RegisterBlueprint adds a registration independently of repository wiring.
func (s *Service) RegisterBlueprint(entry Registration) error {
	if s == nil || s.Registry == nil {
		return ErrBlueprintUnavailable
	}
	return s.Registry.Register(entry)
}

// GetBlueprintRegistration resolves a registration independently of persistence.
func (s *Service) GetBlueprintRegistration(key string) (Registration, error) {
	if s == nil || s.Registry == nil {
		return Registration{}, ErrBlueprintUnavailable
	}
	return s.Registry.MustGet(key)
}
