package blueprint

import (
	"context"
	"strings"
)

// BlueprintRepository defines the repository surface used by the service.
type BlueprintRepository interface {
	CreateBlueprint(ctx context.Context, blueprint *Blueprint) (*Blueprint, error)
	DeleteBlueprintByID(ctx context.Context, id string) error
	GetBlueprintByID(ctx context.Context, id string) (*Blueprint, error)
	GetBlueprintByNameAndKind(ctx context.Context, name, kind string) (*Blueprint, error)
	GetBlueprints(ctx context.Context, req *GetBlueprintsRequest) ([]Blueprint, error)
	GetTotalBlueprints(ctx context.Context, req *GetBlueprintsRequest) (int64, error)
	UpdateBlueprint(ctx context.Context, blueprint *Blueprint) (*Blueprint, error)
}

// Service holds and manages blueprint business logic.
type Service struct {
	BlueprintRepository BlueprintRepository
	Registry            *Registry
}

// NewService creates a blueprint service.
func NewService(blueprintRepository BlueprintRepository, registry ...*Registry) *Service {
	resolvedRegistry := MustRegistry()
	if len(registry) > 0 && registry[0] != nil {
		resolvedRegistry = registry[0]
	}

	return &Service{
		BlueprintRepository: blueprintRepository,
		Registry:            resolvedRegistry,
	}
}

// CreateBlueprint creates a blueprint record.
func (s *Service) CreateBlueprint(ctx context.Context, req *CreateBlueprintRequest) (*BlueprintResponse, error) {
	if req == nil || normaliseBlueprintName(req.Name) == "" {
		return nil, ErrBlueprintNameIsRequired
	}
	if normaliseBlueprintKind(req.Kind) == "" {
		return nil, ErrBlueprintKindIsRequired
	}

	blueprint := NewBlueprint(req)
	blueprint.GenerateID()
	blueprint.GenerateNanoID()
	blueprint.SetCreatedAtTimeToNow()

	created, err := s.BlueprintRepository.CreateBlueprint(ctx, blueprint)
	if err != nil {
		return nil, err
	}

	return &BlueprintResponse{Blueprint: created}, nil
}

// GetBlueprintByID retrieves a blueprint by ID.
func (s *Service) GetBlueprintByID(ctx context.Context, req *GetBlueprintByIDRequest) (*BlueprintResponse, error) {
	if req == nil || strings.TrimSpace(req.ID) == "" {
		return nil, ErrBlueprintIDIsRequired
	}
	if strings.TrimSpace(req.UserID) == "" {
		return nil, ErrBlueprintUserIDIsRequired
	}

	blueprint, err := s.BlueprintRepository.GetBlueprintByID(ctx, strings.TrimSpace(req.ID))
	if err != nil {
		return nil, err
	}

	return &BlueprintResponse{Blueprint: blueprint}, nil
}

// GetBlueprintByName retrieves a blueprint by name and kind.
func (s *Service) GetBlueprintByName(ctx context.Context, req *GetBlueprintByNameRequest) (*BlueprintResponse, error) {
	if req == nil || normaliseBlueprintName(req.Name) == "" {
		return nil, ErrBlueprintNameIsRequired
	}
	if normaliseBlueprintKind(req.Kind) == "" {
		return nil, ErrBlueprintKindIsRequired
	}

	blueprint, err := s.BlueprintRepository.GetBlueprintByNameAndKind(ctx, req.Name, req.Kind)
	if err != nil {
		return nil, err
	}

	return &BlueprintResponse{Blueprint: blueprint}, nil
}

// GetBlueprints retrieves a filtered page of blueprints.
func (s *Service) GetBlueprints(ctx context.Context, req *GetBlueprintsRequest) (*GetBlueprintsResponse, error) {
	blueprints, err := s.BlueprintRepository.GetBlueprints(ctx, req)
	if err != nil {
		return nil, err
	}

	total, err := s.BlueprintRepository.GetTotalBlueprints(ctx, req)
	if err != nil {
		return nil, err
	}

	return &GetBlueprintsResponse{Blueprints: blueprints, Total: total}, nil
}

// UpdateBlueprint updates mutable fields on a blueprint.
func (s *Service) UpdateBlueprint(ctx context.Context, req *UpdateBlueprintRequest) (*BlueprintResponse, error) {
	if req == nil || strings.TrimSpace(req.ID) == "" {
		return nil, ErrBlueprintIDIsRequired
	}

	current, err := s.BlueprintRepository.GetBlueprintByID(ctx, strings.TrimSpace(req.ID))
	if err != nil {
		return nil, err
	}

	if name := normaliseBlueprintName(req.Name); name != "" {
		current.Name = name
	}
	if kind := normaliseBlueprintKind(req.Kind); kind != "" {
		current.Kind = kind
	}
	if req.Description != "" {
		current.Description = strings.TrimSpace(req.Description)
	}
	if status := normaliseBlueprintStatus(req.Status); status != "" {
		current.Status = status
	}
	if req.Metadata != nil {
		current.Metadata = req.Metadata
	}
	current.UpdatedByUserID = strings.TrimSpace(req.UpdatedByUserID)
	current.SetUpdatedAtTimeToNow()

	updated, err := s.BlueprintRepository.UpdateBlueprint(ctx, current)
	if err != nil {
		return nil, err
	}

	return &BlueprintResponse{Blueprint: updated}, nil
}

// DeleteBlueprint deletes a blueprint by ID.
func (s *Service) DeleteBlueprint(ctx context.Context, req *DeleteBlueprintRequest) (*DeleteBlueprintResponse, error) {
	if req == nil || strings.TrimSpace(req.ID) == "" {
		return nil, ErrBlueprintIDIsRequired
	}

	if err := s.BlueprintRepository.DeleteBlueprintByID(ctx, strings.TrimSpace(req.ID)); err != nil {
		return nil, err
	}

	return &DeleteBlueprintResponse{Deleted: true}, nil
}

// RegisterBlueprint adds a blueprint registration to the package registry.
func (s *Service) RegisterBlueprint(entry Registration) error {
	return s.Registry.Register(entry)
}

// GetBlueprintRegistration retrieves a blueprint registration by key.
func (s *Service) GetBlueprintRegistration(key string) (Registration, error) {
	return s.Registry.MustGet(key)
}
