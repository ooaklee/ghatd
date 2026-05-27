package blueprint

import (
	"context"
	"strings"

	"github.com/ooaklee/ghatd/external/logger"
	"go.uber.org/zap"
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
	logger := logger.AcquireOperationFrom(ctx, "internal/blueprint", "create-blueprint")

	if req == nil || normaliseBlueprintName(req.Name) == "" {
		logger.Warn("blueprint-create-missing-name")
		return nil, ErrBlueprintNameIsRequired
	}
	if normaliseBlueprintKind(req.Kind) == "" {
		logger.Warn("blueprint-create-missing-kind", zap.String("name", normaliseBlueprintName(req.Name)))
		return nil, ErrBlueprintKindIsRequired
	}

	blueprint := NewBlueprint(req)
	blueprint.GenerateID()
	blueprint.GenerateNanoID()
	blueprint.SetCreatedAtTimeToNow()

	created, err := s.BlueprintRepository.CreateBlueprint(ctx, blueprint)
	if err != nil {
		logger.Error("blueprint-create-failed", zap.String("blueprint-id", blueprint.ID), zap.String("kind", blueprint.Kind), zap.Error(err))
		return nil, err
	}

	logger.Info("blueprint-created", zap.String("blueprint-id", created.ID), zap.String("kind", created.Kind))
	return &BlueprintResponse{Blueprint: created}, nil
}

// GetBlueprintByID retrieves a blueprint by ID.
func (s *Service) GetBlueprintByID(ctx context.Context, req *GetBlueprintByIDRequest) (*BlueprintResponse, error) {
	logger := logger.AcquireOperationFrom(ctx, "internal/blueprint", "get-blueprint-by-id")

	if req == nil || strings.TrimSpace(req.ID) == "" {
		logger.Warn("blueprint-get-by-id-missing-id")
		return nil, ErrBlueprintIDIsRequired
	}
	if strings.TrimSpace(req.UserID) == "" {
		logger.Warn("blueprint-get-by-id-missing-user-id", zap.String("blueprint-id", strings.TrimSpace(req.ID)))
		return nil, ErrBlueprintUserIDIsRequired
	}

	blueprint, err := s.BlueprintRepository.GetBlueprintByID(ctx, strings.TrimSpace(req.ID))
	if err != nil {
		logger.Error("blueprint-get-by-id-failed", zap.String("blueprint-id", strings.TrimSpace(req.ID)), zap.Error(err))
		return nil, err
	}

	logger.Debug("blueprint-get-by-id-completed", zap.String("blueprint-id", blueprint.ID), zap.String("kind", blueprint.Kind))
	return &BlueprintResponse{Blueprint: blueprint}, nil
}

// GetBlueprintByName retrieves a blueprint by name and kind.
func (s *Service) GetBlueprintByName(ctx context.Context, req *GetBlueprintByNameRequest) (*BlueprintResponse, error) {
	logger := logger.AcquireOperationFrom(ctx, "internal/blueprint", "get-blueprint-by-name")

	if req == nil || normaliseBlueprintName(req.Name) == "" {
		logger.Warn("blueprint-get-by-name-missing-name")
		return nil, ErrBlueprintNameIsRequired
	}
	if normaliseBlueprintKind(req.Kind) == "" {
		logger.Warn("blueprint-get-by-name-missing-kind", zap.String("name", normaliseBlueprintName(req.Name)))
		return nil, ErrBlueprintKindIsRequired
	}

	blueprint, err := s.BlueprintRepository.GetBlueprintByNameAndKind(ctx, req.Name, req.Kind)
	if err != nil {
		logger.Error("blueprint-get-by-name-failed", zap.String("name", normaliseBlueprintName(req.Name)), zap.String("kind", normaliseBlueprintKind(req.Kind)), zap.Error(err))
		return nil, err
	}

	logger.Debug("blueprint-get-by-name-completed", zap.String("blueprint-id", blueprint.ID), zap.String("kind", blueprint.Kind))
	return &BlueprintResponse{Blueprint: blueprint}, nil
}

// GetBlueprints retrieves a filtered page of blueprints.
func (s *Service) GetBlueprints(ctx context.Context, req *GetBlueprintsRequest) (*GetBlueprintsResponse, error) {
	logger := logger.AcquireOperationFrom(ctx, "internal/blueprint", "get-blueprints")

	blueprints, err := s.BlueprintRepository.GetBlueprints(ctx, req)
	if err != nil {
		logger.Error("blueprints-list-failed", zap.Error(err))
		return nil, err
	}

	total, err := s.BlueprintRepository.GetTotalBlueprints(ctx, req)
	if err != nil {
		logger.Error("blueprints-count-failed", zap.Error(err))
		return nil, err
	}

	logger.Debug("blueprints-list-completed", zap.Int("returned", len(blueprints)), zap.Int64("total", total))
	return &GetBlueprintsResponse{Blueprints: blueprints, Total: total}, nil
}

// UpdateBlueprint updates mutable fields on a blueprint.
func (s *Service) UpdateBlueprint(ctx context.Context, req *UpdateBlueprintRequest) (*BlueprintResponse, error) {
	logger := logger.AcquireOperationFrom(ctx, "internal/blueprint", "update-blueprint")

	if req == nil || strings.TrimSpace(req.ID) == "" {
		logger.Warn("blueprint-update-missing-id")
		return nil, ErrBlueprintIDIsRequired
	}

	current, err := s.BlueprintRepository.GetBlueprintByID(ctx, strings.TrimSpace(req.ID))
	if err != nil {
		logger.Error("blueprint-update-current-lookup-failed", zap.String("blueprint-id", strings.TrimSpace(req.ID)), zap.Error(err))
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
		logger.Error("blueprint-update-failed", zap.String("blueprint-id", current.ID), zap.Error(err))
		return nil, err
	}

	logger.Info("blueprint-updated", zap.String("blueprint-id", updated.ID), zap.String("kind", updated.Kind))
	return &BlueprintResponse{Blueprint: updated}, nil
}

// DeleteBlueprint deletes a blueprint by ID.
func (s *Service) DeleteBlueprint(ctx context.Context, req *DeleteBlueprintRequest) (*DeleteBlueprintResponse, error) {
	logger := logger.AcquireOperationFrom(ctx, "internal/blueprint", "delete-blueprint")

	if req == nil || strings.TrimSpace(req.ID) == "" {
		logger.Warn("blueprint-delete-missing-id")
		return nil, ErrBlueprintIDIsRequired
	}

	if err := s.BlueprintRepository.DeleteBlueprintByID(ctx, strings.TrimSpace(req.ID)); err != nil {
		logger.Error("blueprint-delete-failed", zap.String("blueprint-id", strings.TrimSpace(req.ID)), zap.Error(err))
		return nil, err
	}

	logger.Info("blueprint-deleted", zap.String("blueprint-id", strings.TrimSpace(req.ID)))
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
