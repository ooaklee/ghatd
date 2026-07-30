package vision_test

import (
	"context"
	"testing"

	"github.com/benweissmann/memongo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ooaklee/ghatd/external/repository"
	repositoryhelpers "github.com/ooaklee/ghatd/external/repository/helpers"
	"github.com/ooaklee/ghatd/external/vision"
)

func TestIntegration_VisionService_FullLifecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()
	mongoServer, err := memongo.StartWithOptions(&memongo.Options{MongoVersion: "7.0.14"})
	if err != nil {
		t.Skipf("skipping integration test: unable to start memongo: %v", err)
	}
	t.Cleanup(mongoServer.Stop)

	dbName := memongo.RandomDatabase()
	mongoHandler, err := repositoryhelpers.NewHandler(repositoryhelpers.DefaultConfig(mongoServer.URI(), dbName))
	require.NoError(t, err)
	t.Cleanup(func() { _ = mongoHandler.Close(ctx) })

	store := repository.NewMongoDbRepositoryWithDefaults(mongoHandler, dbName)
	service, err := vision.NewService(vision.NewRepository(store))
	require.NoError(t, err)

	created, err := service.CreateVision(ctx, &vision.CreateVisionRequest{
		Title:           "Better search",
		Type:            vision.VisionTypeFeedback,
		Description:     "Search every record",
		CreatedByUserID: "user-1",
	})
	require.NoError(t, err)
	assert.False(t, created.Vision.IsRoadmapItem())

	voted, err := service.SetVisionVote(ctx, &vision.SetVisionVoteRequest{
		NanoID: created.Vision.NanoID, UserID: "user-2", Vote: vision.VisionVoteUpvote,
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"user-2"}, voted.Vision.Voters[vision.VisionVoteUpvote])

	voted, err = service.SetVisionVote(ctx, &vision.SetVisionVoteRequest{
		NanoID: created.Vision.NanoID, UserID: "user-2", Vote: vision.VisionVoteDownvote,
	})
	require.NoError(t, err)
	assert.Empty(t, voted.Vision.Voters[vision.VisionVoteUpvote])
	assert.Equal(t, []string{"user-2"}, voted.Vision.Voters[vision.VisionVoteDownvote])

	commented, err := service.AddVisionComment(ctx, &vision.AddVisionCommentRequest{
		NanoID: created.Vision.NanoID, UserID: "user-3", Message: "Ask <@nano-user>",
	})
	require.NoError(t, err)
	require.Len(t, commented.Vision.Comments, 1)
	assert.Equal(t, "Ask <@nano-user>", commented.Vision.Comments[0].Message)
	rootCommentID := commented.Vision.Comments[0].ID

	commented, err = service.AddVisionComment(ctx, &vision.AddVisionCommentRequest{
		NanoID:          created.Vision.NanoID,
		ParentCommentID: rootCommentID,
		UserID:          "user-4",
		Message:         "I agree with <@nano-user>",
	})
	require.NoError(t, err)
	require.Len(t, commented.Vision.Comments, 2)
	assert.Equal(t, rootCommentID, commented.Vision.Comments[1].ParentCommentID)

	commented, err = service.SetVisionCommentVote(ctx, &vision.SetVisionCommentVoteRequest{
		NanoID:    created.Vision.NanoID,
		CommentID: rootCommentID,
		UserID:    "user-5",
		Vote:      vision.VisionVoteUpvote,
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"user-5"}, commented.Vision.Comments[0].Voters[vision.VisionVoteUpvote])

	commented, err = service.SetVisionCommentVote(ctx, &vision.SetVisionCommentVoteRequest{
		NanoID:    created.Vision.NanoID,
		CommentID: rootCommentID,
		UserID:    "user-5",
		Vote:      vision.VisionVoteDownvote,
	})
	require.NoError(t, err)
	assert.Empty(t, commented.Vision.Comments[0].Voters[vision.VisionVoteUpvote])
	assert.Equal(t, []string{"user-5"}, commented.Vision.Comments[0].Voters[vision.VisionVoteDownvote])

	roadmapped, err := service.UpdateVisionStatus(ctx, &vision.UpdateVisionStatusRequest{
		NanoID: created.Vision.NanoID, Status: vision.VisionStatusUnderReview, UpdatedByUserID: "admin-1",
	})
	require.NoError(t, err)
	assert.True(t, roadmapped.Vision.IsRoadmapItem())

	list, err := service.GetVisions(ctx, &vision.GetVisionsRequest{
		Type: vision.VisionTypeFeedback, RoadmapOnly: true,
	})
	require.NoError(t, err)
	require.Len(t, list.Visions, 1)
	assert.Empty(t, list.Visions[0].Comments, "list projection should omit comments")

	deleted, err := service.DeleteVision(ctx, &vision.DeleteVisionRequest{NanoID: created.Vision.NanoID})
	require.NoError(t, err)
	assert.True(t, deleted.Deleted)
}
