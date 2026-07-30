package vision

import (
	"context"
	"errors"
	"slices"
	"testing"
)

type memoryVisionRepository struct {
	item *Vision
}

func (m *memoryVisionRepository) CreateVision(_ context.Context, item *Vision) (*Vision, error) {
	m.item = item
	return item, nil
}
func (m *memoryVisionRepository) DeleteVisionByID(context.Context, string) error {
	m.item = nil
	return nil
}
func (m *memoryVisionRepository) GetVisionByNanoID(context.Context, string) (*Vision, error) {
	if m.item == nil {
		return nil, ErrVisionResourceNotFound
	}
	return m.item, nil
}
func (m *memoryVisionRepository) GetVisions(context.Context, *GetVisionsRequest) ([]Vision, error) {
	if m.item == nil {
		return []Vision{}, nil
	}
	return []Vision{*m.item}, nil
}
func (m *memoryVisionRepository) GetTotalVisions(context.Context, *GetVisionsRequest) (int64, error) {
	if m.item == nil {
		return 0, nil
	}
	return 1, nil
}
func (m *memoryVisionRepository) UpdateVision(_ context.Context, item *Vision) error {
	m.item = item
	return nil
}
func (m *memoryVisionRepository) UpdateVisionStatus(_ context.Context, _ string, status VisionStatus, userID, updatedAt string) error {
	m.item.Status = status
	m.item.UpdatedByUserID = userID
	m.item.UpdatedAt = updatedAt
	return nil
}
func (m *memoryVisionRepository) SetVisionVote(_ context.Context, _ string, userID string, vote VisionVote, _ string) error {
	other := VisionVoteUpvote
	if vote == VisionVoteUpvote {
		other = VisionVoteDownvote
	}
	m.item.Voters[other] = slices.DeleteFunc(m.item.Voters[other], func(id string) bool { return id == userID })
	if !slices.Contains(m.item.Voters[vote], userID) {
		m.item.Voters[vote] = append(m.item.Voters[vote], userID)
	}
	return nil
}
func (m *memoryVisionRepository) RemoveVisionVote(_ context.Context, _, userID, _ string) error {
	for vote := range m.item.Voters {
		m.item.Voters[vote] = slices.DeleteFunc(m.item.Voters[vote], func(id string) bool { return id == userID })
	}
	return nil
}
func (m *memoryVisionRepository) AddVisionComment(_ context.Context, _ string, comment *VisionComment) error {
	m.item.Comments = append(m.item.Comments, *comment)
	return nil
}
func (m *memoryVisionRepository) SetVisionCommentVote(_ context.Context, _, commentID, userID string, vote VisionVote, _ string) error {
	for i := range m.item.Comments {
		if m.item.Comments[i].ID != commentID {
			continue
		}
		other := VisionVoteUpvote
		if vote == VisionVoteUpvote {
			other = VisionVoteDownvote
		}
		m.item.Comments[i].Voters[other] = slices.DeleteFunc(
			m.item.Comments[i].Voters[other],
			func(id string) bool { return id == userID },
		)
		if !slices.Contains(m.item.Comments[i].Voters[vote], userID) {
			m.item.Comments[i].Voters[vote] = append(m.item.Comments[i].Voters[vote], userID)
		}
	}
	return nil
}
func (m *memoryVisionRepository) RemoveVisionCommentVote(_ context.Context, _, commentID, userID, _ string) error {
	for i := range m.item.Comments {
		if m.item.Comments[i].ID != commentID {
			continue
		}
		for vote := range m.item.Comments[i].Voters {
			m.item.Comments[i].Voters[vote] = slices.DeleteFunc(
				m.item.Comments[i].Voters[vote],
				func(id string) bool { return id == userID },
			)
		}
	}
	return nil
}

func mustVisionService(t *testing.T, repo VisionRepository, config ...*VisionConfig) *Service {
	t.Helper()
	service, err := NewService(repo, config...)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	return service
}

func createTestVision(t *testing.T, service *Service) *Vision {
	t.Helper()
	response, err := service.CreateVision(context.Background(), &CreateVisionRequest{
		Title:           " Better search ",
		Type:            VisionTypeFeedback,
		Description:     "Search all records",
		CreatedByUserID: "user-1",
	})
	if err != nil {
		t.Fatalf("CreateVision() error = %v", err)
	}
	return response.Vision
}

func TestServiceCreateVisionInitialisesFeedback(t *testing.T) {
	service := mustVisionService(t, &memoryVisionRepository{})
	item := createTestVision(t, service)

	if item.Title != "Better search" || item.Status != "" || item.IsRoadmapItem() {
		t.Fatalf("created vision = %+v", item)
	}
	if item.ID == "" || item.NanoID == "" || item.CreatedAt == "" {
		t.Fatalf("generated fields missing: %+v", item)
	}
	if item.Voters[VisionVoteUpvote] == nil || item.Voters[VisionVoteDownvote] == nil {
		t.Fatalf("vote buckets not initialised: %#v", item.Voters)
	}
}

func TestServiceStatusTransitionsPromoteToRoadmap(t *testing.T) {
	service := mustVisionService(t, &memoryVisionRepository{})
	item := createTestVision(t, service)

	response, err := service.UpdateVisionStatus(context.Background(), &UpdateVisionStatusRequest{
		NanoID:          item.NanoID,
		Status:          VisionStatusUnderReview,
		UpdatedByUserID: "admin-1",
	})
	if err != nil {
		t.Fatalf("UpdateVisionStatus() error = %v", err)
	}
	if !response.Vision.IsRoadmapItem() {
		t.Fatal("non-empty status should make the vision a roadmap item")
	}

	_, err = service.UpdateVisionStatus(context.Background(), &UpdateVisionStatusRequest{
		NanoID:          item.NanoID,
		Status:          VisionStatusInProgress,
		UpdatedByUserID: "admin-1",
	})
	if !errors.Is(err, ErrVisionInvalidStatusTransition) {
		t.Fatalf("invalid transition error = %v", err)
	}
}

func TestServiceVotingHonoursConfigAndMovesBuckets(t *testing.T) {
	repo := &memoryVisionRepository{}
	config := DefaultVisionConfig().WithDownvoting(false)
	service := mustVisionService(t, repo, config)
	item := createTestVision(t, service)

	_, err := service.SetVisionVote(context.Background(), &SetVisionVoteRequest{
		NanoID: item.NanoID, UserID: "user-2", Vote: VisionVoteDownvote,
	})
	if !errors.Is(err, ErrVisionDownvotingDisabled) {
		t.Fatalf("downvote error = %v", err)
	}

	response, err := service.SetVisionVote(context.Background(), &SetVisionVoteRequest{
		NanoID: item.NanoID, UserID: "user-2", Vote: VisionVoteUpvote,
	})
	if err != nil {
		t.Fatalf("upvote error = %v", err)
	}
	if !slices.Contains(response.Vision.Voters[VisionVoteUpvote], "user-2") {
		t.Fatalf("upvote bucket = %#v", response.Vision.Voters)
	}

	commented, err := service.AddVisionComment(context.Background(), &AddVisionCommentRequest{
		NanoID: item.NanoID, UserID: "user-3", Message: "same",
	})
	if err != nil {
		t.Fatalf("AddVisionComment() error = %v", err)
	}
	_, err = service.SetVisionCommentVote(context.Background(), &SetVisionCommentVoteRequest{
		NanoID:    item.NanoID,
		CommentID: commented.Vision.Comments[0].ID,
		UserID:    "user-2",
		Vote:      VisionVoteDownvote,
	})
	if !errors.Is(err, ErrVisionDownvotingDisabled) {
		t.Fatalf("comment downvote error = %v", err)
	}
}

func TestServiceCommentStoresRepliesMentionsAndVotes(t *testing.T) {
	service := mustVisionService(t, &memoryVisionRepository{})
	item := createTestVision(t, service)
	message := "Please check with <@nano-user>"

	root, err := service.AddVisionComment(context.Background(), &AddVisionCommentRequest{
		NanoID: item.NanoID, UserID: "user-2", Message: message,
	})
	if err != nil {
		t.Fatalf("AddVisionComment() error = %v", err)
	}
	if len(root.Vision.Comments) != 1 || root.Vision.Comments[0].Message != message {
		t.Fatalf("comments = %#v", root.Vision.Comments)
	}
	rootCommentID := root.Vision.Comments[0].ID

	response, err := service.AddVisionComment(context.Background(), &AddVisionCommentRequest{
		NanoID:          item.NanoID,
		ParentCommentID: rootCommentID,
		UserID:          "user-3",
		Message:         "Agreed, <@nano-user>",
	})
	if err != nil {
		t.Fatalf("AddVisionComment(reply) error = %v", err)
	}
	if response.Vision.Comments[1].ParentCommentID != rootCommentID {
		t.Fatalf("reply = %#v", response.Vision.Comments[1])
	}
	if response.Vision.Comments[1].Voters[VisionVoteUpvote] == nil {
		t.Fatalf("reply vote buckets = %#v", response.Vision.Comments[1].Voters)
	}

	response, err = service.SetVisionCommentVote(context.Background(), &SetVisionCommentVoteRequest{
		NanoID: item.NanoID, CommentID: rootCommentID, UserID: "user-4", Vote: VisionVoteUpvote,
	})
	if err != nil {
		t.Fatalf("SetVisionCommentVote() error = %v", err)
	}
	if !slices.Contains(response.Vision.Comments[0].Voters[VisionVoteUpvote], "user-4") {
		t.Fatalf("comment voters = %#v", response.Vision.Comments[0].Voters)
	}

	_, err = service.AddVisionComment(context.Background(), &AddVisionCommentRequest{
		NanoID: item.NanoID, ParentCommentID: "missing", UserID: "user-3", Message: "orphan",
	})
	if !errors.Is(err, ErrVisionCommentNotFound) {
		t.Fatalf("missing parent error = %v", err)
	}
}

func TestVisionConfigValidation(t *testing.T) {
	invalid := NewCustomVisionConfig().
		WithValidTypes(VisionTypeFeedback).
		WithStatusTransition(VisionStatusPlanned, VisionStatus("UNKNOWN"))
	if _, err := NewService(&memoryVisionRepository{}, invalid); !errors.Is(err, ErrVisionConfigInvalid) {
		t.Fatalf("NewService() error = %v, want ErrVisionConfigInvalid", err)
	}
}
