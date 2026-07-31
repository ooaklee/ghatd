package vision

import (
	"context"
	"strings"

	"github.com/ooaklee/ghatd/external/logger"
	"github.com/ooaklee/ghatd/external/toolbox"
	"go.uber.org/zap"
)

// VisionRepository defines the persistence surface used by Service.
type VisionRepository interface {
	CreateVision(ctx context.Context, vision *Vision) (*Vision, error)
	DeleteVisionByID(ctx context.Context, id string) error
	GetVisionByNanoID(ctx context.Context, nanoID string) (*Vision, error)
	GetVisions(ctx context.Context, req *GetVisionsRequest) ([]Vision, error)
	GetTotalVisions(ctx context.Context, req *GetVisionsRequest) (int64, error)
	UpdateVision(ctx context.Context, vision *Vision) error
	UpdateVisionStatus(ctx context.Context, id string, status VisionStatus, updatedByUserID, updatedAt string) error
	SetVisionVote(ctx context.Context, id, userID string, vote VisionVote, updatedAt string) error
	RemoveVisionVote(ctx context.Context, id, userID, updatedAt string) error
	AddVisionComment(ctx context.Context, id string, comment *VisionComment) error
	SetVisionCommentVote(ctx context.Context, id, commentID, userID string, vote VisionVote, updatedAt string) error
	RemoveVisionCommentVote(ctx context.Context, id, commentID, userID, updatedAt string) error
}

// Service holds vision business logic.
type Service struct {
	VisionRepository VisionRepository
	Config           *VisionConfig
}

// NewService creates a configured vision service.
func NewService(visionRepository VisionRepository, configs ...*VisionConfig) (*Service, error) {
	config := DefaultVisionConfig()
	if len(configs) > 0 && configs[0] != nil {
		config = configs[0]
	}
	if err := config.Validate(); err != nil {
		return nil, err
	}

	return &Service{
		VisionRepository: visionRepository,
		Config:           config,
	}, nil
}

// CreateVision creates feedback or a bug report with no roadmap status.
func (s *Service) CreateVision(ctx context.Context, req *CreateVisionRequest) (*VisionResponse, error) {
	logger := logger.AcquireOperationFrom(ctx, "external/vision", "create-vision")
	if req == nil || normaliseVisionTitle(req.Title) == "" {
		return nil, ErrVisionTitleIsRequired
	}
	if !s.Config.IsValidType(req.Type) {
		return nil, ErrVisionInvalidType
	}
	if strings.TrimSpace(req.CreatedByUserID) == "" {
		return nil, ErrVisionUserIDIsRequired
	}

	vision := NewVision(req, s.Config)
	vision.GenerateID().GenerateNanoID().SetCreatedAtTimeToNow()

	created, err := s.VisionRepository.CreateVision(ctx, vision)
	if err != nil {
		logger.Error("vision-create-failed", zap.String("vision-id", vision.ID), zap.Error(err))
		return nil, err
	}
	created.SetConfig(s.Config)
	return &VisionResponse{Vision: created}, nil
}

// GetVisionByNanoID retrieves a full vision, including comments.
func (s *Service) GetVisionByNanoID(ctx context.Context, req *GetVisionByNanoIDRequest) (*VisionResponse, error) {
	if req == nil || strings.TrimSpace(req.NanoID) == "" {
		return nil, ErrVisionNanoIDIsRequired
	}

	vision, err := s.getVisionByNanoID(ctx, req.NanoID)
	if err != nil {
		return nil, err
	}
	return &VisionResponse{Vision: vision}, nil
}

// GetVisions retrieves a filtered page of vision summaries.
func (s *Service) GetVisions(ctx context.Context, req *GetVisionsRequest) (*GetVisionsResponse, error) {
	if req != nil {
		if req.Type != "" && !s.Config.IsValidType(req.Type) {
			return nil, ErrVisionInvalidType
		}
		if req.Status != "" && !s.Config.IsValidStatus(req.Status) {
			return nil, ErrVisionInvalidStatus
		}
	}

	visions, err := s.VisionRepository.GetVisions(ctx, req)
	if err != nil {
		return nil, err
	}
	total, err := s.VisionRepository.GetTotalVisions(ctx, req)
	if err != nil {
		return nil, err
	}
	for i := range visions {
		visions[i].SetConfig(s.Config)
	}
	return &GetVisionsResponse{Visions: visions, Total: total}, nil
}

// UpdateVision updates descriptive fields without changing type or status.
func (s *Service) UpdateVision(ctx context.Context, req *UpdateVisionRequest) (*VisionResponse, error) {
	if req == nil || strings.TrimSpace(req.NanoID) == "" {
		return nil, ErrVisionNanoIDIsRequired
	}
	if strings.TrimSpace(req.UpdatedByUserID) == "" {
		return nil, ErrVisionUserIDIsRequired
	}
	if req.Title == nil && req.Description == nil && req.Metadata == nil {
		return nil, ErrVisionInvalidPayload
	}

	current, err := s.getVisionByNanoID(ctx, req.NanoID)
	if err != nil {
		return nil, err
	}
	if req.Title != nil {
		title := normaliseVisionTitle(*req.Title)
		if title == "" {
			return nil, ErrVisionTitleIsRequired
		}
		current.Title = title
	}
	if req.Description != nil {
		current.Description = strings.TrimSpace(*req.Description)
	}
	if req.Metadata != nil {
		current.Metadata = req.Metadata
	}
	current.UpdatedByUserID = strings.TrimSpace(req.UpdatedByUserID)
	current.SetUpdatedAtTimeToNow()

	if err = s.VisionRepository.UpdateVision(ctx, current); err != nil {
		return nil, err
	}
	return &VisionResponse{Vision: current}, nil
}

// UpdateVisionStatus validates and persists a roadmap transition.
func (s *Service) UpdateVisionStatus(ctx context.Context, req *UpdateVisionStatusRequest) (*VisionResponse, error) {
	if req == nil || strings.TrimSpace(req.NanoID) == "" {
		return nil, ErrVisionNanoIDIsRequired
	}
	if strings.TrimSpace(req.UpdatedByUserID) == "" {
		return nil, ErrVisionUserIDIsRequired
	}

	current, err := s.getVisionByNanoID(ctx, req.NanoID)
	if err != nil {
		return nil, err
	}
	if err = current.UpdateStatus(req.Status); err != nil {
		return nil, err
	}
	current.UpdatedByUserID = strings.TrimSpace(req.UpdatedByUserID)

	if err = s.VisionRepository.UpdateVisionStatus(
		ctx,
		current.ID,
		current.Status,
		current.UpdatedByUserID,
		current.UpdatedAt,
	); err != nil {
		return nil, err
	}
	return &VisionResponse{Vision: current}, nil
}

// SetVisionVote atomically sets or changes the requestor's vote.
func (s *Service) SetVisionVote(ctx context.Context, req *SetVisionVoteRequest) (*VisionResponse, error) {
	if req == nil || strings.TrimSpace(req.NanoID) == "" {
		return nil, ErrVisionNanoIDIsRequired
	}
	if strings.TrimSpace(req.UserID) == "" {
		return nil, ErrVisionUserIDIsRequired
	}
	if !isValidVisionVote(req.Vote) {
		return nil, ErrVisionInvalidVote
	}
	if req.Vote == VisionVoteDownvote && !s.Config.EnableDownvoting {
		return nil, ErrVisionDownvotingDisabled
	}

	// Check existence before issuing an update because the shared repository
	// helper does not expose Mongo's matched count.
	current, err := s.getVisionByNanoID(ctx, req.NanoID)
	if err != nil {
		return nil, err
	}
	now := newUpdatedAt()
	if err := s.VisionRepository.SetVisionVote(ctx, current.ID, strings.TrimSpace(req.UserID), req.Vote, now); err != nil {
		return nil, err
	}
	return s.GetVisionByNanoID(ctx, &GetVisionByNanoIDRequest{NanoID: req.NanoID})
}

// RemoveVisionVote removes the requestor from both vote buckets.
func (s *Service) RemoveVisionVote(ctx context.Context, req *RemoveVisionVoteRequest) (*VisionResponse, error) {
	if req == nil || strings.TrimSpace(req.NanoID) == "" {
		return nil, ErrVisionNanoIDIsRequired
	}
	if strings.TrimSpace(req.UserID) == "" {
		return nil, ErrVisionUserIDIsRequired
	}
	current, err := s.getVisionByNanoID(ctx, req.NanoID)
	if err != nil {
		return nil, err
	}
	if err := s.VisionRepository.RemoveVisionVote(ctx, current.ID, strings.TrimSpace(req.UserID), newUpdatedAt()); err != nil {
		return nil, err
	}
	return s.GetVisionByNanoID(ctx, &GetVisionByNanoIDRequest{NanoID: req.NanoID})
}

// AddVisionComment appends a raw user comment. Mention tokens are stored
// verbatim and are intentionally not resolved in the core package.
func (s *Service) AddVisionComment(ctx context.Context, req *AddVisionCommentRequest) (*VisionResponse, error) {
	if req == nil || strings.TrimSpace(req.NanoID) == "" {
		return nil, ErrVisionNanoIDIsRequired
	}
	if strings.TrimSpace(req.UserID) == "" {
		return nil, ErrVisionUserIDIsRequired
	}
	if strings.TrimSpace(req.Message) == "" {
		return nil, ErrVisionCommentMessageIsRequired
	}
	current, err := s.getVisionByNanoID(ctx, req.NanoID)
	if err != nil {
		return nil, err
	}
	parentCommentID := strings.TrimSpace(req.ParentCommentID)
	if parentCommentID != "" && !visionHasComment(current, parentCommentID) {
		return nil, ErrVisionCommentNotFound
	}

	comment := NewVisionComment(req.UserID, req.Message, parentCommentID)
	if err := s.VisionRepository.AddVisionComment(ctx, current.ID, comment); err != nil {
		return nil, err
	}
	return s.GetVisionByNanoID(ctx, &GetVisionByNanoIDRequest{NanoID: req.NanoID})
}

// SetVisionCommentVote atomically sets or changes the requestor's vote on a comment.
func (s *Service) SetVisionCommentVote(ctx context.Context, req *SetVisionCommentVoteRequest) (*VisionResponse, error) {
	if req == nil || strings.TrimSpace(req.NanoID) == "" {
		return nil, ErrVisionNanoIDIsRequired
	}
	if strings.TrimSpace(req.CommentID) == "" {
		return nil, ErrVisionCommentNotFound
	}
	if strings.TrimSpace(req.UserID) == "" {
		return nil, ErrVisionUserIDIsRequired
	}
	if !isValidVisionVote(req.Vote) {
		return nil, ErrVisionInvalidVote
	}
	if req.Vote == VisionVoteDownvote && !s.Config.EnableDownvoting {
		return nil, ErrVisionDownvotingDisabled
	}

	current, err := s.getVisionByNanoID(ctx, req.NanoID)
	if err != nil {
		return nil, err
	}
	if !visionHasComment(current, req.CommentID) {
		return nil, ErrVisionCommentNotFound
	}
	if err = s.VisionRepository.SetVisionCommentVote(
		ctx,
		current.ID,
		strings.TrimSpace(req.CommentID),
		strings.TrimSpace(req.UserID),
		req.Vote,
		newUpdatedAt(),
	); err != nil {
		return nil, err
	}
	return s.GetVisionByNanoID(ctx, &GetVisionByNanoIDRequest{NanoID: req.NanoID})
}

// RemoveVisionCommentVote removes the requestor from both comment vote buckets.
func (s *Service) RemoveVisionCommentVote(ctx context.Context, req *RemoveVisionCommentVoteRequest) (*VisionResponse, error) {
	if req == nil || strings.TrimSpace(req.NanoID) == "" {
		return nil, ErrVisionNanoIDIsRequired
	}
	if strings.TrimSpace(req.CommentID) == "" {
		return nil, ErrVisionCommentNotFound
	}
	if strings.TrimSpace(req.UserID) == "" {
		return nil, ErrVisionUserIDIsRequired
	}

	current, err := s.getVisionByNanoID(ctx, req.NanoID)
	if err != nil {
		return nil, err
	}
	if !visionHasComment(current, req.CommentID) {
		return nil, ErrVisionCommentNotFound
	}
	if err = s.VisionRepository.RemoveVisionCommentVote(
		ctx,
		current.ID,
		strings.TrimSpace(req.CommentID),
		strings.TrimSpace(req.UserID),
		newUpdatedAt(),
	); err != nil {
		return nil, err
	}
	return s.GetVisionByNanoID(ctx, &GetVisionByNanoIDRequest{NanoID: req.NanoID})
}

// DeleteVision deletes a vision addressed by public NanoID.
func (s *Service) DeleteVision(ctx context.Context, req *DeleteVisionRequest) (*DeleteVisionResponse, error) {
	if req == nil || strings.TrimSpace(req.NanoID) == "" {
		return nil, ErrVisionNanoIDIsRequired
	}
	current, err := s.getVisionByNanoID(ctx, req.NanoID)
	if err != nil {
		return nil, err
	}
	if err := s.VisionRepository.DeleteVisionByID(ctx, current.ID); err != nil {
		return nil, err
	}
	return &DeleteVisionResponse{Deleted: true}, nil
}

// GetVisionConfig returns the client-safe configuration.
func (s *Service) GetVisionConfig(context.Context) (*GetVisionConfigResponse, error) {
	if s.Config == nil {
		return nil, ErrVisionConfigNotSet
	}
	return &GetVisionConfigResponse{Config: s.Config.toCapabilities()}, nil
}

// getVisionByNanoID retrieves a vision and attaches this service's configuration.
func (s *Service) getVisionByNanoID(ctx context.Context, nanoID string) (*Vision, error) {
	vision, err := s.VisionRepository.GetVisionByNanoID(ctx, strings.TrimSpace(nanoID))
	if err != nil {
		return nil, err
	}
	vision.SetConfig(s.Config)
	return vision, nil
}

// newUpdatedAt returns the current UTC timestamp in the platform format.
func newUpdatedAt() string {
	return toolbox.TimeNowUTC()
}

// visionHasComment reports whether a vision contains the supplied comment ID.
func visionHasComment(item *Vision, commentID string) bool {
	commentID = strings.TrimSpace(commentID)
	if item == nil || commentID == "" {
		return false
	}
	for i := range item.Comments {
		if item.Comments[i].ID == commentID {
			return true
		}
	}
	return false
}
