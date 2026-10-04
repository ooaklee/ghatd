package vision

import (
	"context"
	"errors"
	"testing"

	"github.com/ooaklee/ghatd/external/voter"
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
func (m *memoryVisionRepository) AddVisionComment(_ context.Context, _ string, comment *VisionComment) error {
	m.item.Comments = append(m.item.Comments, *comment)
	m.item.CommentCount++
	return nil
}

func mustVisionService(t *testing.T, repo VisionRepository, config ...*VisionConfig) *Service {
	t.Helper()
	votes := &memoryVoteRepository{values: map[voteKey]voter.Value{}}
	if recorded, ok := repo.(*actorVisionStore); ok {
		votes.onWrite = recorded.record
	}
	service, err := NewService(repo, voter.NewService(votes), config...)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	return service
}

func createTestVision(t *testing.T, service *Service) *Vision {
	t.Helper()
	response, err := service.CreateVision(context.Background(), &CreateVisionRequest{
		Title:       " Better search ",
		Type:        VisionTypeFeedback,
		Description: "Search all records",
		ActorID:     "user-1",
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
	if item.VoteSummary.Up != 0 || item.VoteSummary.Down != 0 || item.VoteSummary.ViewerVote != nil {
		t.Fatalf("initial summary = %#v", item.VoteSummary)
	}
	if item.CommentCount != 0 {
		t.Fatalf("CommentCount = %d, want 0", item.CommentCount)
	}
}

func TestServiceStatusTransitionsPromoteToRoadmap(t *testing.T) {
	service := mustVisionService(t, &memoryVisionRepository{})
	item := createTestVision(t, service)

	response, err := service.UpdateVisionStatus(context.Background(), &UpdateVisionStatusRequest{
		NanoID:  item.NanoID,
		Status:  VisionStatusUnderReview,
		ActorID: "admin-1",
	})
	if err != nil {
		t.Fatalf("UpdateVisionStatus() error = %v", err)
	}
	if !response.Vision.IsRoadmapItem() {
		t.Fatal("non-empty status should make the vision a roadmap item")
	}

	_, err = service.UpdateVisionStatus(context.Background(), &UpdateVisionStatusRequest{
		NanoID:  item.NanoID,
		Status:  VisionStatusInProgress,
		ActorID: "admin-1",
	})
	if !errors.Is(err, ErrVisionInvalidStatusTransition) {
		t.Fatalf("invalid transition error = %v", err)
	}
}

func TestServiceUpdateVisionChangesOnlyRequestedDescriptiveFields(t *testing.T) {
	repo := &memoryVisionRepository{}
	service := mustVisionService(t, repo)
	item := createTestVision(t, service)
	repo.item.Description = "Original description"
	repo.item.Status = VisionStatusUnderReview
	title := "  Updated title  "
	description := ""

	response, err := service.UpdateVision(context.Background(), &UpdateVisionRequest{
		NanoID:      item.NanoID,
		Title:       &title,
		Description: &description,
		ActorID:     "user-1",
	})
	if err != nil {
		t.Fatalf("UpdateVision() error = %v", err)
	}
	if response.Vision.Title != "Updated title" {
		t.Fatalf("Title = %q, want %q", response.Vision.Title, "Updated title")
	}
	if response.Vision.Description != "" {
		t.Fatalf("Description = %q, want empty", response.Vision.Description)
	}
	if response.Vision.Status != VisionStatusUnderReview {
		t.Fatalf("Status = %q, want %q", response.Vision.Status, VisionStatusUnderReview)
	}
}

func TestServiceUpdateVisionRejectsEmptyTitle(t *testing.T) {
	service := mustVisionService(t, &memoryVisionRepository{})
	item := createTestVision(t, service)
	title := "   "

	_, err := service.UpdateVision(context.Background(), &UpdateVisionRequest{
		NanoID:  item.NanoID,
		Title:   &title,
		ActorID: "user-1",
	})
	if !errors.Is(err, ErrVisionTitleIsRequired) {
		t.Fatalf("UpdateVision() error = %v, want %v", err, ErrVisionTitleIsRequired)
	}
}

func TestServiceVotingHonoursConfigAndSharedSummary(t *testing.T) {
	for _, tc := range []struct {
		name               string
		child, downEnabled bool
		vote               VisionVote
		want               error
	}{
		{"parent up", false, false, VisionVoteUpvote, nil},
		{"parent down enabled", false, true, VisionVoteDownvote, nil},
		{"parent down disabled", false, false, VisionVoteDownvote, ErrVisionDownvotingDisabled},
		{"comment up", true, false, VisionVoteUpvote, nil},
		{"comment down enabled", true, true, VisionVoteDownvote, nil},
		{"comment down disabled", true, false, VisionVoteDownvote, ErrVisionDownvotingDisabled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			service := mustVisionService(t, &memoryVisionRepository{}, DefaultVisionConfig().WithDownvoting(tc.downEnabled))
			item := createTestVision(t, service)
			var response *VisionResponse
			var err error
			if tc.child {
				commented, addErr := service.AddVisionComment(context.Background(), &AddVisionCommentRequest{NanoID: item.NanoID, ActorID: "author", Message: "same"})
				if addErr != nil {
					t.Fatal(addErr)
				}
				response, err = service.SetVisionCommentVote(context.Background(), &SetVisionCommentVoteRequest{NanoID: item.NanoID, ActorID: "voter", CommentID: commented.Vision.Comments[0].ID, Vote: tc.vote})
			} else {
				response, err = service.SetVisionVote(context.Background(), &SetVisionVoteRequest{NanoID: item.NanoID, ActorID: "voter", Vote: tc.vote})
			}
			if !errors.Is(err, tc.want) {
				t.Fatalf("vote error = %v, want %v", err, tc.want)
			}
			if tc.want != nil {
				return
			}
			summary, viewer := response.Vision.VoteSummary, response.Vision.VoteViewerID
			if tc.child {
				summary, viewer = response.Vision.Comments[0].VoteSummary, response.Vision.Comments[0].VoteViewerID
			}
			if summary.Up+summary.Down != 1 || summary.ViewerVote == nil || VisionVote(*summary.ViewerVote) != tc.vote || viewer != "voter" {
				t.Fatalf("summary=%+v viewer=%q", summary, viewer)
			}
		})
	}
}

// This ordered reply/mention/vote lifecycle asserts changes to one discussion.

func TestServiceCommentStoresRepliesMentionsAndVotes(t *testing.T) {
	service := mustVisionService(t, &memoryVisionRepository{})
	item := createTestVision(t, service)
	message := "Please check with <@nano-user>"

	root, err := service.AddVisionComment(context.Background(), &AddVisionCommentRequest{
		NanoID: item.NanoID, ActorID: "user-2", Message: message,
	})
	if err != nil {
		t.Fatalf("AddVisionComment() error = %v", err)
	}
	if len(root.Vision.Comments) != 1 || root.Vision.Comments[0].Message != message {
		t.Fatalf("comments = %#v", root.Vision.Comments)
	}
	if root.Vision.CommentCount != 1 {
		t.Fatalf("CommentCount = %d, want 1", root.Vision.CommentCount)
	}
	rootCommentID := root.Vision.Comments[0].ID

	response, err := service.AddVisionComment(context.Background(), &AddVisionCommentRequest{
		NanoID:          item.NanoID,
		ParentCommentID: rootCommentID,
		ActorID:         "user-3",
		Message:         "Agreed, <@nano-user>",
	})
	if err != nil {
		t.Fatalf("AddVisionComment(reply) error = %v", err)
	}
	if response.Vision.Comments[1].ParentCommentID != rootCommentID {
		t.Fatalf("reply = %#v", response.Vision.Comments[1])
	}
	if response.Vision.CommentCount != 2 {
		t.Fatalf("CommentCount = %d, want 2", response.Vision.CommentCount)
	}
	if response.Vision.Comments[1].VoteSummary.Up != 0 {
		t.Fatalf("reply summary = %#v", response.Vision.Comments[1].VoteSummary)
	}

	response, err = service.SetVisionCommentVote(context.Background(), &SetVisionCommentVoteRequest{
		NanoID: item.NanoID, CommentID: rootCommentID, ActorID: "user-4", Vote: VisionVoteUpvote,
	})
	if err != nil {
		t.Fatalf("SetVisionCommentVote() error = %v", err)
	}
	if response.Vision.Comments[0].VoteSummary.Up != 1 || response.Vision.Comments[0].VoteViewerID != "user-4" {
		t.Fatalf("comment summary = %#v", response.Vision.Comments[0].VoteSummary)
	}

	_, err = service.AddVisionComment(context.Background(), &AddVisionCommentRequest{
		NanoID: item.NanoID, ParentCommentID: "missing", ActorID: "user-3", Message: "orphan",
	})
	if !errors.Is(err, ErrVisionCommentNotFound) {
		t.Fatalf("missing parent error = %v", err)
	}
}

func TestVisionConfigValidation(t *testing.T) {
	invalid := NewCustomVisionConfig().
		WithValidTypes(VisionTypeFeedback).
		WithStatusTransition(VisionStatusPlanned, VisionStatus("UNKNOWN"))
	if _, err := NewService(&memoryVisionRepository{}, nil, invalid); !errors.Is(err, ErrVisionConfigInvalid) {
		t.Fatalf("NewService() error = %v, want ErrVisionConfigInvalid", err)
	}
}
