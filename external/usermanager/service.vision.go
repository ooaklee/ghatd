package usermanager

import (
	"context"
	"sort"
	"strings"

	accessmanagerhelpers "github.com/ooaklee/ghatd/external/accessmanager/helpers"
	"github.com/ooaklee/ghatd/external/vision"
	"github.com/ooaklee/ghatd/external/voter"
)

// CreateVision stores authenticated feedback or a bug report, then enriches it.
func (s *Service) CreateVision(ctx context.Context, req *vision.CreateVisionRequest) (*GetVisionResponse, error) {
	if s.VisionService == nil {
		return nil, ErrVisionServiceNotEnabled
	}
	response, err := s.VisionService.CreateVision(ctx, req)
	if err != nil {
		return nil, err
	}
	return s.enrichedVisionResponse(ctx, response.Vision)
}

// GetVisionByNanoID returns a privacy-safe vision with public user summaries.
func (s *Service) GetVisionByNanoID(ctx context.Context, req *vision.GetVisionByNanoIDRequest) (*GetVisionResponse, error) {
	if s.VisionService == nil {
		return nil, ErrVisionServiceNotEnabled
	}
	response, err := s.VisionService.GetVisionByNanoID(ctx, req)
	if err != nil {
		return nil, err
	}
	users := s.enrichVisionUsers(ctx, []vision.Vision{*response.Vision})
	return &GetVisionResponse{
		Vision:           projectVision(ctx, response.Vision, users.byID),
		Users:            users.public,
		ViewerUserNanoID: visionViewerNanoID(ctx, users.byID),
	}, nil
}

// GetVisions returns a page of vision summaries with associated users.
func (s *Service) GetVisions(ctx context.Context, req *vision.GetVisionsRequest) (*GetVisionsResponse, error) {
	if s.VisionService == nil {
		return nil, ErrVisionServiceNotEnabled
	}
	response, err := s.VisionService.GetVisions(ctx, req)
	if err != nil {
		return nil, err
	}
	users := s.enrichVisionUsers(ctx, response.Visions)
	items := make([]VisionView, 0, len(response.Visions))
	for i := range response.Visions {
		items = append(items, *projectVision(ctx, &response.Visions[i], users.byID))
	}
	return &GetVisionsResponse{Visions: items, Users: users.public, Total: response.Total}, nil
}

// GetVisionConfig returns the client-safe vision capabilities.
func (s *Service) GetVisionConfig(ctx context.Context) (*vision.GetVisionConfigResponse, error) {
	if s.VisionService == nil {
		return nil, ErrVisionServiceNotEnabled
	}
	return s.VisionService.GetVisionConfig(ctx)
}

// UpdateVision authorizes an owner or platform administrator, restricts the
// mutation to descriptive fields, and enriches the result.
func (s *Service) UpdateVision(ctx context.Context, req *vision.UpdateVisionRequest) (*GetVisionResponse, error) {
	if s == nil || s.VisionService == nil {
		return nil, ErrVisionServiceNotEnabled
	}
	if req == nil || ctx == nil {
		return nil, vision.ErrVisionInvalidPayload
	}
	requesterID := accessmanagerhelpers.AcquireAuthenticatedUserIDFrom(ctx)
	if requesterID == "" || strings.TrimSpace(requesterID) != requesterID || req.ActorID != requesterID {
		return nil, ErrVisionEditForbidden
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	current, err := s.VisionService.GetVisionByNanoID(
		ctx,
		&vision.GetVisionByNanoIDRequest{NanoID: req.NanoID},
	)
	if err != nil {
		return nil, err
	}
	if current == nil || current.Vision == nil ||
		current.Vision.NanoID != req.NanoID ||
		!visionViewerCanManage(ctx, current.Vision) {
		return nil, ErrVisionEditForbidden
	}

	// Metadata is an internal extension surface and is intentionally excluded
	// from the owner/admin descriptive edit route.
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	editable := *req
	editable.Metadata = nil
	response, err := s.VisionService.UpdateVision(ctx, &editable)
	if err != nil {
		return nil, err
	}
	return s.enrichedVisionResponse(ctx, response.Vision)
}

// UpdateVisionStatus delegates an admin roadmap transition and enriches the result.
func (s *Service) UpdateVisionStatus(ctx context.Context, req *vision.UpdateVisionStatusRequest) (*GetVisionResponse, error) {
	if s.VisionService == nil {
		return nil, ErrVisionServiceNotEnabled
	}
	response, err := s.VisionService.UpdateVisionStatus(ctx, req)
	if err != nil {
		return nil, err
	}
	return s.enrichedVisionResponse(ctx, response.Vision)
}

// DeleteVision authorizes the owner or a platform administrator before
// delegating permanent deletion.
func (s *Service) DeleteVision(ctx context.Context, req *vision.DeleteVisionRequest) (*vision.DeleteVisionResponse, error) {
	if s == nil || s.VisionService == nil {
		return nil, ErrVisionServiceNotEnabled
	}
	if req == nil || ctx == nil {
		return nil, vision.ErrVisionNanoIDIsRequired
	}
	requesterID := accessmanagerhelpers.AcquireAuthenticatedUserIDFrom(ctx)
	if requesterID == "" || strings.TrimSpace(requesterID) != requesterID || req.ActorID != requesterID {
		return nil, ErrVisionDeleteForbidden
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	current, err := s.VisionService.GetVisionByNanoID(
		ctx,
		&vision.GetVisionByNanoIDRequest{NanoID: req.NanoID},
	)
	if err != nil {
		return nil, err
	}
	if current == nil || current.Vision == nil ||
		current.Vision.NanoID != req.NanoID ||
		!visionViewerCanManage(ctx, current.Vision) {
		return nil, ErrVisionDeleteForbidden
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return s.VisionService.DeleteVision(ctx, req)
}

// SetVisionVote delegates the atomic vote then enriches the result.
func (s *Service) SetVisionVote(ctx context.Context, req *vision.SetVisionVoteRequest) (*GetVisionResponse, error) {
	if s.VisionService == nil {
		return nil, ErrVisionServiceNotEnabled
	}
	response, err := s.VisionService.SetVisionVote(ctx, req)
	if err != nil {
		return nil, err
	}
	return s.enrichedVisionResponse(ctx, response.Vision)
}

// RemoveVisionVote delegates vote removal then enriches the result.
func (s *Service) RemoveVisionVote(ctx context.Context, req *vision.RemoveVisionVoteRequest) (*GetVisionResponse, error) {
	if s.VisionService == nil {
		return nil, ErrVisionServiceNotEnabled
	}
	response, err := s.VisionService.RemoveVisionVote(ctx, req)
	if err != nil {
		return nil, err
	}
	return s.enrichedVisionResponse(ctx, response.Vision)
}

// AddVisionComment delegates comment storage then enriches the result.
func (s *Service) AddVisionComment(ctx context.Context, req *vision.AddVisionCommentRequest) (*GetVisionResponse, error) {
	if s.VisionService == nil {
		return nil, ErrVisionServiceNotEnabled
	}
	response, err := s.VisionService.AddVisionComment(ctx, req)
	if err != nil {
		return nil, err
	}
	return s.enrichedVisionResponse(ctx, response.Vision)
}

// SetVisionCommentVote delegates the atomic comment vote then enriches the result.
func (s *Service) SetVisionCommentVote(ctx context.Context, req *vision.SetVisionCommentVoteRequest) (*GetVisionResponse, error) {
	if s.VisionService == nil {
		return nil, ErrVisionServiceNotEnabled
	}
	response, err := s.VisionService.SetVisionCommentVote(ctx, req)
	if err != nil {
		return nil, err
	}
	return s.enrichedVisionResponse(ctx, response.Vision)
}

// RemoveVisionCommentVote delegates comment vote removal then enriches the result.
func (s *Service) RemoveVisionCommentVote(ctx context.Context, req *vision.RemoveVisionCommentVoteRequest) (*GetVisionResponse, error) {
	if s.VisionService == nil {
		return nil, ErrVisionServiceNotEnabled
	}
	response, err := s.VisionService.RemoveVisionCommentVote(ctx, req)
	if err != nil {
		return nil, err
	}
	return s.enrichedVisionResponse(ctx, response.Vision)
}

// enrichedVisionResponse projects a vision and includes its public user summaries.
func (s *Service) enrichedVisionResponse(ctx context.Context, item *vision.Vision) (*GetVisionResponse, error) {
	if item == nil {
		return &GetVisionResponse{}, nil
	}
	users := s.enrichVisionUsers(ctx, []vision.Vision{*item})
	return &GetVisionResponse{
		Vision:           projectVision(ctx, item, users.byID),
		Users:            users.public,
		ViewerUserNanoID: visionViewerNanoID(ctx, users.byID),
	}, nil
}

// visionViewerNanoID resolves the current authenticated participant to their
// public NanoID without exposing a raw user UUID.
func visionViewerNanoID(ctx context.Context, usersByID map[string]VisionUser) string {
	return usersByID[accessmanagerhelpers.AcquireAuthenticatedUserIDFrom(ctx)].NanoID
}

// visionUserLookup indexes resolved users two ways: by raw internal ID and by
// public NanoID, for privacy-safe enrichment lookups.
type visionUserLookup struct {
	byID   map[string]VisionUser
	public map[string]VisionUser
}

// enrichVisionUsers loads public summaries for users referenced by the given
// visions. Enrichment is best effort: missing users and failed batches are
// omitted so profile decoration cannot make a core vision operation fail.
func (s *Service) enrichVisionUsers(ctx context.Context, visions []vision.Vision) *visionUserLookup {
	ids := visionUserIDs(visions)
	result := &visionUserLookup{
		byID:   make(map[string]VisionUser, len(ids)),
		public: make(map[string]VisionUser, len(ids)),
	}

	usersByID := s.loadUsersForEnrichment(ctx, ids, "vision-user-enrichment")
	for userID, persistedUser := range usersByID {
		user := newVisionUser(persistedUser)
		if user.NanoID == "" {
			continue
		}
		result.byID[userID] = user
		result.public[user.NanoID] = user
	}

	return result
}

// visionUserIDs returns the sorted unique user IDs referenced by the given visions.
func visionUserIDs(visions []vision.Vision) []string {
	unique := make(map[string]struct{})
	add := func(id string) {
		if id != "" {
			unique[id] = struct{}{}
		}
	}

	for i := range visions {
		add(visions[i].CreatedByUserID)
		add(visions[i].UpdatedByUserID)
		for _, comment := range visions[i].Comments {
			add(comment.UserID)
		}
	}

	ids := make([]string, 0, len(unique))
	for id := range unique {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// projectVision converts a vision into a privacy-safe view for the current viewer.
func projectVision(
	ctx context.Context,
	item *vision.Vision,
	usersByID map[string]VisionUser,
) *VisionView {
	if item == nil {
		return nil
	}

	viewerID := accessmanagerhelpers.AcquireAuthenticatedUserIDFrom(ctx)

	result := &VisionView{
		NanoID:       item.NanoID,
		Title:        item.Title,
		Type:         item.Type,
		Description:  item.Description,
		Status:       item.Status,
		Votes:        summariseVisionVotes(item.VoteSummary),
		ViewerVote:   findViewerVote(item.VoteSummary, item.VoteViewerID, viewerID),
		CommentCount: item.CommentCount,
		CreatedAt:    item.CreatedAt,
		UpdatedAt:    item.UpdatedAt,
	}
	result.CanEdit = visionViewerCanManage(ctx, item)
	result.CanDelete = result.CanEdit
	if user, ok := usersByID[item.CreatedByUserID]; ok {
		result.CreatedByUserNanoID = user.NanoID
	}
	if user, ok := usersByID[item.UpdatedByUserID]; ok {
		result.UpdatedByUserNanoID = user.NanoID
	}

	if len(item.Comments) > 0 {
		result.Comments = make([]VisionCommentView, 0, len(item.Comments))
		for i := range item.Comments {
			comment := item.Comments[i]
			projected := VisionCommentView{
				ID:              comment.ID,
				ParentCommentID: comment.ParentCommentID,
				Message:         comment.Message,
				Votes:           summariseVisionVotes(comment.VoteSummary),
				ViewerVote:      findViewerVote(comment.VoteSummary, comment.VoteViewerID, viewerID),
				CreatedAt:       comment.CreatedAt,
			}
			if user, ok := usersByID[comment.UserID]; ok {
				projected.UserNanoID = user.NanoID
			}
			result.Comments = append(result.Comments, projected)
		}
	}

	return result
}

// visionViewerCanManage reports whether the authenticated viewer owns the
// vision or is a platform administrator.
func visionViewerCanManage(ctx context.Context, item *vision.Vision) bool {
	if ctx == nil || item == nil {
		return false
	}
	viewerID := strings.TrimSpace(accessmanagerhelpers.AcquireAuthenticatedUserIDFrom(ctx))
	if viewerID == "" {
		return false
	}
	requester := accessmanagerhelpers.AcquireUserFrom(ctx)
	if ctx.Value(accessmanagerhelpers.RequestorUserKey) != nil && (requester == nil || requester.GetUserId() != viewerID) {
		return false
	}
	if item.CreatedByUserID == viewerID {
		return true
	}
	return requester != nil &&
		requester.GetUserId() == viewerID &&
		requester.IsAdmin()
}

// summariseVisionVotes counts upvotes and downvotes and calculates their net score.
func summariseVisionVotes(summary voter.Summary) VisionVoteSummary {
	upvotes := summary.Up
	downvotes := summary.Down
	return VisionVoteSummary{
		Upvotes:   upvotes,
		Downvotes: downvotes,
		Score:     upvotes - downvotes,
	}
}

// findViewerVote only exposes a summary prepared for this authenticated viewer.
func findViewerVote(summary voter.Summary, summaryViewerID, viewerID string) *vision.VisionVote {
	if viewerID == "" || viewerID != summaryViewerID || summary.ViewerVote == nil || !summary.ViewerVote.Valid() {
		return nil
	}
	value := vision.VisionVote(*summary.ViewerVote)
	return &value
}
