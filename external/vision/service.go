package vision

import (
	"context"
	"strings"

	accesshelpers "github.com/ooaklee/ghatd/external/accessmanager/helpers"
	"github.com/ooaklee/ghatd/external/logger"
	"github.com/ooaklee/ghatd/external/voter"
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
	AddVisionComment(ctx context.Context, id string, comment *VisionComment) error
}

// Service holds vision business logic.
type Service struct {
	VisionRepository VisionRepository
	Config           *VisionConfig
	// VoterService owns shared vote mechanics and storage, not vision permissions.
	VoterService VoterService
}

// NewService creates a configured vision service with a required shared voting
// port. Missing ports fail closed on use; configuration is checked immediately.
func NewService(visionRepository VisionRepository, votes VoterService, configs ...*VisionConfig) (*Service, error) {
	config := DefaultVisionConfig()
	if len(configs) > 0 && configs[0] != nil {
		config = configs[0]
	}
	if err := config.Validate(); err != nil {
		return nil, err
	}

	return &Service{
		VisionRepository: visionRepository,
		VoterService:     votes,
		Config:           config,
	}, nil
}

// CreateVision creates feedback or a bug report with no roadmap status.
func (s *Service) CreateVision(ctx context.Context, req *CreateVisionRequest) (*VisionResponse, error) {
	if err := s.validateEntry(ctx, req); err != nil {
		return nil, err
	}
	if !visionActorMatchesContext(ctx, req.ActorID) {
		return nil, ErrVisionUserIDIsRequired
	}
	logger := logger.AcquireOperationFrom(ctx, "external/vision", "create-vision")
	if req == nil || normaliseVisionTitle(req.Title) == "" {
		return nil, ErrVisionTitleIsRequired
	}
	if !s.Config.IsValidType(req.Type) {
		return nil, ErrVisionInvalidType
	}
	if strings.TrimSpace(req.ActorID) == "" {
		return nil, ErrVisionUserIDIsRequired
	}

	vision := NewVision(req, s.Config)
	vision.GenerateID().GenerateNanoID().SetCreatedAtTimeToNow()
	// Pin identity before the adapter call: a custom adapter may return or mutate
	// its input pointer, which must not redefine the expected receipt identity.
	expectedID, expectedNanoID := vision.ID, vision.NanoID

	created, err := s.VisionRepository.CreateVision(ctx, vision)
	if err != nil {
		logger.Error("vision-create-failed")
		return nil, err
	}
	if created == nil || created.ID != expectedID || created.NanoID != expectedNanoID {
		return nil, ErrVisionUnavailable
	}
	result := *created
	result.SetConfig(s.Config)
	return &VisionResponse{Vision: &result}, nil
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
	return s.projectVotes(ctx, vision, accesshelpers.AcquireAuthenticatedUserIDFrom(ctx))
}

// GetVisions retrieves a filtered page of vision summaries.
func (s *Service) GetVisions(ctx context.Context, req *GetVisionsRequest) (*GetVisionsResponse, error) {
	// A nil query retains the existing default-page behavior.
	if err := s.validateEntry(ctx, struct{}{}); err != nil {
		return nil, err
	}
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
	// Custom repositories may share their returned snapshots between callers.
	// Enrichment must not attach this viewer's state to those shared records.
	visions = append([]Vision{}, visions...)
	total, err := s.VisionRepository.GetTotalVisions(ctx, req)
	if err != nil {
		return nil, err
	}
	for i := range visions {
		visions[i].SetConfig(s.Config)
	}
	refs := make([]*Vision, len(visions))
	for i := range visions {
		refs[i] = &visions[i]
	}
	if err := s.hydrateVotes(ctx, refs, accesshelpers.AcquireAuthenticatedUserIDFrom(ctx)); err != nil {
		return nil, err
	}
	return &GetVisionsResponse{Visions: visions, Total: total}, nil
}

// UpdateVision updates descriptive fields without changing type or status.
func (s *Service) UpdateVision(ctx context.Context, req *UpdateVisionRequest) (*VisionResponse, error) {
	if err := s.validateEntry(ctx, req); err != nil {
		return nil, err
	}
	if !visionActorMatchesContext(ctx, req.ActorID) {
		return nil, ErrVisionUserIDIsRequired
	}
	if req == nil || strings.TrimSpace(req.NanoID) == "" {
		return nil, ErrVisionNanoIDIsRequired
	}
	if strings.TrimSpace(req.ActorID) == "" {
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
	current.UpdatedByUserID = strings.TrimSpace(req.ActorID)
	current.SetUpdatedAtTimeToNow()

	if err = s.VisionRepository.UpdateVision(ctx, current); err != nil {
		return nil, err
	}
	return s.projectVotes(ctx, current, req.ActorID)
}

// UpdateVisionStatus validates and persists a roadmap transition.
func (s *Service) UpdateVisionStatus(ctx context.Context, req *UpdateVisionStatusRequest) (*VisionResponse, error) {
	if err := s.validateEntry(ctx, req); err != nil {
		return nil, err
	}
	if !visionActorMatchesContext(ctx, req.ActorID) {
		return nil, ErrVisionUserIDIsRequired
	}
	if req == nil || strings.TrimSpace(req.NanoID) == "" {
		return nil, ErrVisionNanoIDIsRequired
	}
	if strings.TrimSpace(req.ActorID) == "" {
		return nil, ErrVisionUserIDIsRequired
	}

	current, err := s.getVisionByNanoID(ctx, req.NanoID)
	if err != nil {
		return nil, err
	}
	if err = current.UpdateStatus(req.Status); err != nil {
		return nil, err
	}
	current.UpdatedByUserID = strings.TrimSpace(req.ActorID)

	if err = s.VisionRepository.UpdateVisionStatus(
		ctx,
		current.ID,
		current.Status,
		current.UpdatedByUserID,
		current.UpdatedAt,
	); err != nil {
		return nil, err
	}
	return s.projectVotes(ctx, current, req.ActorID)
}

// SetVisionVote atomically sets or changes the requestor's vote.
func (s *Service) SetVisionVote(ctx context.Context, req *SetVisionVoteRequest) (*VisionResponse, error) {
	if err := s.validateEntry(ctx, req); err != nil {
		return nil, err
	}
	if !visionActorMatchesContext(ctx, req.ActorID) {
		return nil, ErrVisionUserIDIsRequired
	}
	if req == nil || strings.TrimSpace(req.NanoID) == "" {
		return nil, ErrVisionNanoIDIsRequired
	}
	if strings.TrimSpace(req.ActorID) == "" {
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
	if nilVisionDependency(s.VoterService) {
		return nil, ErrVisionUnavailable
	}
	if err := s.VoterService.SetVote(ctx, &voter.SetVoteRequest{ActorID: req.ActorID, Target: visionVoteTarget(current.ID, ""), Vote: voter.Value(req.Vote)}); err != nil {
		return nil, err
	}
	return s.getVoteResponse(ctx, req.NanoID, req.ActorID)
}

// RemoveVisionVote removes only the requestor's shared vote on the vision.
func (s *Service) RemoveVisionVote(ctx context.Context, req *RemoveVisionVoteRequest) (*VisionResponse, error) {
	if err := s.validateEntry(ctx, req); err != nil {
		return nil, err
	}
	if !visionActorMatchesContext(ctx, req.ActorID) {
		return nil, ErrVisionUserIDIsRequired
	}
	if req == nil || strings.TrimSpace(req.NanoID) == "" {
		return nil, ErrVisionNanoIDIsRequired
	}
	if strings.TrimSpace(req.ActorID) == "" {
		return nil, ErrVisionUserIDIsRequired
	}
	current, err := s.getVisionByNanoID(ctx, req.NanoID)
	if err != nil {
		return nil, err
	}
	if nilVisionDependency(s.VoterService) {
		return nil, ErrVisionUnavailable
	}
	if err := s.VoterService.RemoveVote(ctx, &voter.RemoveVoteRequest{ActorID: req.ActorID, Target: visionVoteTarget(current.ID, "")}); err != nil {
		return nil, err
	}
	return s.getVoteResponse(ctx, req.NanoID, req.ActorID)
}

// AddVisionComment appends a raw user comment. Mention tokens are stored
// verbatim and are intentionally not resolved in the core package.
func (s *Service) AddVisionComment(ctx context.Context, req *AddVisionCommentRequest) (*VisionResponse, error) {
	if err := s.validateEntry(ctx, req); err != nil {
		return nil, err
	}
	if !visionActorMatchesContext(ctx, req.ActorID) {
		return nil, ErrVisionUserIDIsRequired
	}
	if req == nil || strings.TrimSpace(req.NanoID) == "" {
		return nil, ErrVisionNanoIDIsRequired
	}
	if strings.TrimSpace(req.ActorID) == "" {
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

	comment := NewVisionComment(req.ActorID, req.Message, parentCommentID)
	if err := s.VisionRepository.AddVisionComment(ctx, current.ID, comment); err != nil {
		return nil, err
	}
	return s.getVoteResponse(ctx, req.NanoID, req.ActorID)
}

// SetVisionCommentVote atomically sets or changes the requestor's vote on a comment.
func (s *Service) SetVisionCommentVote(ctx context.Context, req *SetVisionCommentVoteRequest) (*VisionResponse, error) {
	if err := s.validateEntry(ctx, req); err != nil {
		return nil, err
	}
	if !visionActorMatchesContext(ctx, req.ActorID) {
		return nil, ErrVisionUserIDIsRequired
	}
	if req == nil || strings.TrimSpace(req.NanoID) == "" {
		return nil, ErrVisionNanoIDIsRequired
	}
	if strings.TrimSpace(req.CommentID) == "" {
		return nil, ErrVisionCommentNotFound
	}
	if strings.TrimSpace(req.ActorID) == "" {
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
	if nilVisionDependency(s.VoterService) {
		return nil, ErrVisionUnavailable
	}
	if err = s.VoterService.SetVote(ctx, &voter.SetVoteRequest{ActorID: req.ActorID, Target: visionVoteTarget(current.ID, strings.TrimSpace(req.CommentID)), Vote: voter.Value(req.Vote)}); err != nil {
		return nil, err
	}
	return s.getVoteResponse(ctx, req.NanoID, req.ActorID)
}

// RemoveVisionCommentVote removes only the requestor's shared comment vote.
func (s *Service) RemoveVisionCommentVote(ctx context.Context, req *RemoveVisionCommentVoteRequest) (*VisionResponse, error) {
	if err := s.validateEntry(ctx, req); err != nil {
		return nil, err
	}
	if !visionActorMatchesContext(ctx, req.ActorID) {
		return nil, ErrVisionUserIDIsRequired
	}
	if req == nil || strings.TrimSpace(req.NanoID) == "" {
		return nil, ErrVisionNanoIDIsRequired
	}
	if strings.TrimSpace(req.CommentID) == "" {
		return nil, ErrVisionCommentNotFound
	}
	if strings.TrimSpace(req.ActorID) == "" {
		return nil, ErrVisionUserIDIsRequired
	}

	current, err := s.getVisionByNanoID(ctx, req.NanoID)
	if err != nil {
		return nil, err
	}
	if !visionHasComment(current, req.CommentID) {
		return nil, ErrVisionCommentNotFound
	}
	if nilVisionDependency(s.VoterService) {
		return nil, ErrVisionUnavailable
	}
	if err = s.VoterService.RemoveVote(ctx, &voter.RemoveVoteRequest{ActorID: req.ActorID, Target: visionVoteTarget(current.ID, strings.TrimSpace(req.CommentID))}); err != nil {
		return nil, err
	}
	return s.getVoteResponse(ctx, req.NanoID, req.ActorID)
}

// DeleteVision deletes a vision addressed by public NanoID.
func (s *Service) DeleteVision(ctx context.Context, req *DeleteVisionRequest) (*DeleteVisionResponse, error) {
	if err := s.validateEntry(ctx, req); err != nil {
		return nil, err
	}
	if !visionActorMatchesContext(ctx, req.ActorID) {
		return nil, ErrVisionUserIDIsRequired
	}
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

// getVisionByNanoID validates the selected record and detaches scalar state before
// attaching configuration or applying edits. Nested values remain read-only here;
// custom repositories must not mutate shared maps or slices returned to callers.
func (s *Service) getVisionByNanoID(ctx context.Context, nanoID string) (*Vision, error) {
	if err := s.validateEntry(ctx, nanoID); err != nil {
		return nil, err
	}
	vision, err := s.VisionRepository.GetVisionByNanoID(ctx, strings.TrimSpace(nanoID))
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if vision == nil || strings.TrimSpace(vision.ID) == "" || vision.NanoID != strings.TrimSpace(nanoID) {
		return nil, ErrVisionUnavailable
	}
	result := *vision
	result.SetConfig(s.Config)
	return &result, nil
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
