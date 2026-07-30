package vision

import (
	"context"
	"strings"

	"github.com/ooaklee/ghatd/external/logger"
	"go.uber.org/zap"
)

// VisionRepository defines the repository surface used by the service.
type VisionRepository interface {
	CreateVision(ctx context.Context, vision *Vision) (*Vision, error)
	DeleteVisionByID(ctx context.Context, id string) error
	GetVisionByID(ctx context.Context, id string) (*Vision, error)
	GetVisionByNameAndKind(ctx context.Context, name, kind string) (*Vision, error)
	GetVisions(ctx context.Context, req *GetVisionsRequest) ([]Vision, error)
	GetTotalVisions(ctx context.Context, req *GetVisionsRequest) (int64, error)
	UpdateVision(ctx context.Context, vision *Vision) (*Vision, error)
}

// Service holds and manages vision business logic.
type Service struct {
	VisionRepository VisionRepository
	Registry         *Registry
}

// NewService creates a vision service.
func NewService(visionRepository VisionRepository, registry ...*Registry) *Service {
	resolvedRegistry := MustRegistry()
	if len(registry) > 0 && registry[0] != nil {
		resolvedRegistry = registry[0]
	}

	return &Service{
		VisionRepository: visionRepository,
		Registry:         resolvedRegistry,
	}
}

// CreateVision creates a vision record.
func (s *Service) CreateVision(ctx context.Context, req *CreateVisionRequest) (*VisionResponse, error) {
	logger := logger.AcquireOperationFrom(ctx, "internal/vision", "create-vision")

	if req == nil || normaliseVisionName(req.Name) == "" {
		logger.Warn("vision-create-missing-name")
		return nil, ErrVisionNameIsRequired
	}
	if normaliseVisionKind(req.Kind) == "" {
		logger.Warn("vision-create-missing-kind", zap.String("name", normaliseVisionName(req.Name)))
		return nil, ErrVisionKindIsRequired
	}

	vision := NewVision(req)
	vision.GenerateID()
	vision.GenerateNanoID()
	vision.SetCreatedAtTimeToNow()

	created, err := s.VisionRepository.CreateVision(ctx, vision)
	if err != nil {
		logger.Error("vision-create-failed", zap.String("vision-id", vision.ID), zap.String("kind", vision.Kind), zap.Error(err))
		return nil, err
	}

	logger.Info("vision-created", zap.String("vision-id", created.ID), zap.String("kind", created.Kind))
	return &VisionResponse{Vision: created}, nil
}

// GetVisionByID retrieves a vision by ID.
func (s *Service) GetVisionByID(ctx context.Context, req *GetVisionByIDRequest) (*VisionResponse, error) {
	logger := logger.AcquireOperationFrom(ctx, "internal/vision", "get-vision-by-id")

	if req == nil || strings.TrimSpace(req.ID) == "" {
		logger.Warn("vision-get-by-id-missing-id")
		return nil, ErrVisionIDIsRequired
	}
	if strings.TrimSpace(req.UserID) == "" {
		logger.Warn("vision-get-by-id-missing-user-id", zap.String("vision-id", strings.TrimSpace(req.ID)))
		return nil, ErrVisionUserIDIsRequired
	}

	vision, err := s.VisionRepository.GetVisionByID(ctx, strings.TrimSpace(req.ID))
	if err != nil {
		logger.Error("vision-get-by-id-failed", zap.String("vision-id", strings.TrimSpace(req.ID)), zap.Error(err))
		return nil, err
	}

	logger.Debug("vision-get-by-id-completed", zap.String("vision-id", vision.ID), zap.String("kind", vision.Kind))
	return &VisionResponse{Vision: vision}, nil
}

// GetVisionByName retrieves a vision by name and kind.
func (s *Service) GetVisionByName(ctx context.Context, req *GetVisionByNameRequest) (*VisionResponse, error) {
	logger := logger.AcquireOperationFrom(ctx, "internal/vision", "get-vision-by-name")

	if req == nil || normaliseVisionName(req.Name) == "" {
		logger.Warn("vision-get-by-name-missing-name")
		return nil, ErrVisionNameIsRequired
	}
	if normaliseVisionKind(req.Kind) == "" {
		logger.Warn("vision-get-by-name-missing-kind", zap.String("name", normaliseVisionName(req.Name)))
		return nil, ErrVisionKindIsRequired
	}

	vision, err := s.VisionRepository.GetVisionByNameAndKind(ctx, req.Name, req.Kind)
	if err != nil {
		logger.Error("vision-get-by-name-failed", zap.String("name", normaliseVisionName(req.Name)), zap.String("kind", normaliseVisionKind(req.Kind)), zap.Error(err))
		return nil, err
	}

	logger.Debug("vision-get-by-name-completed", zap.String("vision-id", vision.ID), zap.String("kind", vision.Kind))
	return &VisionResponse{Vision: vision}, nil
}

// GetVisions retrieves a filtered page of visions.
func (s *Service) GetVisions(ctx context.Context, req *GetVisionsRequest) (*GetVisionsResponse, error) {
	logger := logger.AcquireOperationFrom(ctx, "internal/vision", "get-visions")

	visions, err := s.VisionRepository.GetVisions(ctx, req)
	if err != nil {
		logger.Error("visions-list-failed", zap.Error(err))
		return nil, err
	}

	total, err := s.VisionRepository.GetTotalVisions(ctx, req)
	if err != nil {
		logger.Error("visions-count-failed", zap.Error(err))
		return nil, err
	}

	logger.Debug("visions-list-completed", zap.Int("returned", len(visions)), zap.Int64("total", total))
	return &GetVisionsResponse{Visions: visions, Total: total}, nil
}

// UpdateVision updates mutable fields on a vision.
func (s *Service) UpdateVision(ctx context.Context, req *UpdateVisionRequest) (*VisionResponse, error) {
	logger := logger.AcquireOperationFrom(ctx, "internal/vision", "update-vision")

	if req == nil || strings.TrimSpace(req.ID) == "" {
		logger.Warn("vision-update-missing-id")
		return nil, ErrVisionIDIsRequired
	}

	current, err := s.VisionRepository.GetVisionByID(ctx, strings.TrimSpace(req.ID))
	if err != nil {
		logger.Error("vision-update-current-lookup-failed", zap.String("vision-id", strings.TrimSpace(req.ID)), zap.Error(err))
		return nil, err
	}

	if name := normaliseVisionName(req.Name); name != "" {
		current.Name = name
	}
	if kind := normaliseVisionKind(req.Kind); kind != "" {
		current.Kind = kind
	}
	if req.Description != "" {
		current.Description = strings.TrimSpace(req.Description)
	}
	if status := normaliseVisionStatus(req.Status); status != "" {
		current.Status = status
	}
	if req.Metadata != nil {
		current.Metadata = req.Metadata
	}
	current.UpdatedByUserID = strings.TrimSpace(req.UpdatedByUserID)
	current.SetUpdatedAtTimeToNow()

	updated, err := s.VisionRepository.UpdateVision(ctx, current)
	if err != nil {
		logger.Error("vision-update-failed", zap.String("vision-id", current.ID), zap.Error(err))
		return nil, err
	}

	logger.Info("vision-updated", zap.String("vision-id", updated.ID), zap.String("kind", updated.Kind))
	return &VisionResponse{Vision: updated}, nil
}

// DeleteVision deletes a vision by ID.
func (s *Service) DeleteVision(ctx context.Context, req *DeleteVisionRequest) (*DeleteVisionResponse, error) {
	logger := logger.AcquireOperationFrom(ctx, "internal/vision", "delete-vision")

	if req == nil || strings.TrimSpace(req.ID) == "" {
		logger.Warn("vision-delete-missing-id")
		return nil, ErrVisionIDIsRequired
	}

	if err := s.VisionRepository.DeleteVisionByID(ctx, strings.TrimSpace(req.ID)); err != nil {
		logger.Error("vision-delete-failed", zap.String("vision-id", strings.TrimSpace(req.ID)), zap.Error(err))
		return nil, err
	}

	logger.Info("vision-deleted", zap.String("vision-id", strings.TrimSpace(req.ID)))
	return &DeleteVisionResponse{Deleted: true}, nil
}

// RegisterVision adds a vision registration to the package registry.
func (s *Service) RegisterVision(entry Registration) error {
	return s.Registry.Register(entry)
}

// GetVisionRegistration retrieves a vision registration by key.
func (s *Service) GetVisionRegistration(key string) (Registration, error) {
	return s.Registry.MustGet(key)
}
