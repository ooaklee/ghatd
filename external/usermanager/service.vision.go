package usermanager

import (
	"context"
	"sort"
	"strings"

	accessmanagerhelpers "github.com/ooaklee/ghatd/external/accessmanager/helpers"
	userv2 "github.com/ooaklee/ghatd/external/user/v2"
	"github.com/ooaklee/ghatd/external/vision"
)

const visionUserBatchSize = 100

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
	users, err := s.enrichVisionUsers(ctx, []vision.Vision{*response.Vision})
	if err != nil {
		return nil, err
	}
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
	users, err := s.enrichVisionUsers(ctx, response.Visions)
	if err != nil {
		return nil, err
	}
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
	if s.VisionService == nil {
		return nil, ErrVisionServiceNotEnabled
	}
	if req == nil {
		return nil, vision.ErrVisionInvalidPayload
	}
	requesterID := strings.TrimSpace(accessmanagerhelpers.AcquireAuthenticatedUserIDFrom(ctx))
	if requesterID == "" {
		return nil, ErrVisionEditForbidden
	}
	req.UpdatedByUserID = requesterID

	current, err := s.VisionService.GetVisionByNanoID(
		ctx,
		&vision.GetVisionByNanoIDRequest{NanoID: req.NanoID},
	)
	if err != nil {
		return nil, err
	}
	if current == nil || current.Vision == nil ||
		!visionViewerCanManage(ctx, current.Vision) {
		return nil, ErrVisionEditForbidden
	}

	// Metadata is an internal extension surface and is intentionally excluded
	// from the owner/admin descriptive edit route.
	req.Metadata = nil
	response, err := s.VisionService.UpdateVision(ctx, req)
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
	if s.VisionService == nil {
		return nil, ErrVisionServiceNotEnabled
	}
	if req == nil {
		return nil, vision.ErrVisionNanoIDIsRequired
	}
	current, err := s.VisionService.GetVisionByNanoID(
		ctx,
		&vision.GetVisionByNanoIDRequest{NanoID: req.NanoID},
	)
	if err != nil {
		return nil, err
	}
	if current == nil || current.Vision == nil ||
		!visionViewerCanManage(ctx, current.Vision) {
		return nil, ErrVisionDeleteForbidden
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
	users, err := s.enrichVisionUsers(ctx, []vision.Vision{*item})
	if err != nil {
		return nil, err
	}
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

type visionUserLookup struct {
	byID   map[string]VisionUser
	public map[string]VisionUser
}

// enrichVisionUsers loads the public user summaries referenced by the given visions.
func (s *Service) enrichVisionUsers(ctx context.Context, visions []vision.Vision) (*visionUserLookup, error) {
	ids := visionUserIDs(visions)
	result := &visionUserLookup{
		byID:   make(map[string]VisionUser, len(ids)),
		public: make(map[string]VisionUser, len(ids)),
	}

	for start := 0; start < len(ids); start += visionUserBatchSize {
		end := min(start+visionUserBatchSize, len(ids))
		response, err := s.UserService.GetUsers(ctx, &userv2.GetUsersRequest{
			IDsFilter: ids[start:end],
			Page:      1,
			PerPage:   visionUserBatchSize,
		})
		if err != nil {
			return nil, err
		}
		for i := range response.Users {
			user := newVisionUser(&response.Users[i])
			if user.NanoID == "" {
				continue
			}
			result.byID[response.Users[i].ID] = user
			result.public[user.NanoID] = user
		}
	}

	return result, nil
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
		Votes:        summariseVisionVotes(item.Voters),
		ViewerVote:   findViewerVote(item.Voters, viewerID),
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
				Votes:           summariseVisionVotes(comment.Voters),
				ViewerVote:      findViewerVote(comment.Voters, viewerID),
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
	if item == nil {
		return false
	}
	viewerID := strings.TrimSpace(accessmanagerhelpers.AcquireAuthenticatedUserIDFrom(ctx))
	if viewerID == "" {
		return false
	}
	if item.CreatedByUserID == viewerID {
		return true
	}
	requester := accessmanagerhelpers.AcquireUserFrom(ctx)
	return requester != nil &&
		requester.GetUserId() == viewerID &&
		requester.IsAdmin()
}

// summariseVisionVotes counts upvotes and downvotes and calculates their net score.
func summariseVisionVotes(voters map[vision.VisionVote][]string) VisionVoteSummary {
	upvotes := len(voters[vision.VisionVoteUpvote])
	downvotes := len(voters[vision.VisionVoteDownvote])
	return VisionVoteSummary{
		Upvotes:   upvotes,
		Downvotes: downvotes,
		Score:     upvotes - downvotes,
	}
}

// findViewerVote returns the current viewer's vote, if present.
func findViewerVote(voters map[vision.VisionVote][]string, viewerID string) *vision.VisionVote {
	viewerID = strings.TrimSpace(viewerID)
	if viewerID == "" {
		return nil
	}
	for _, vote := range []vision.VisionVote{vision.VisionVoteDownvote, vision.VisionVoteUpvote} {
		for _, voterID := range voters[vote] {
			if voterID == viewerID {
				result := vote
				return &result
			}
		}
	}
	return nil
}
