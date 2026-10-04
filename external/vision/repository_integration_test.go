package vision_test

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/benweissmann/memongo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	accesshelpers "github.com/ooaklee/ghatd/external/accessmanager/helpers"
	"github.com/ooaklee/ghatd/external/repository"
	repositoryhelpers "github.com/ooaklee/ghatd/external/repository/helpers"
	"github.com/ooaklee/ghatd/external/vision"
	"github.com/ooaklee/ghatd/external/voter"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// This single stateful lifecycle deliberately stays sequential: later assertions
// inspect votes, replies and roadmap changes applied to the same persisted record.
func TestIntegration_VisionService_FullLifecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)
	mongoURI := strings.TrimSpace(os.Getenv("GHATD_TEST_MONGO_URI"))
	if mongoURI == "" {
		mongoServer, err := memongo.StartWithOptions(&memongo.Options{MongoVersion: "7.0.14"})
		if err != nil {
			t.Skipf("no explicit Mongo URI and memongo unavailable: %v", err)
		}
		t.Cleanup(mongoServer.Stop)
		mongoURI = mongoServer.URI()
	}

	dbName := memongo.RandomDatabase()
	mongoHandler, err := repositoryhelpers.NewHandler(repositoryhelpers.DefaultConfig(mongoURI, dbName))
	require.NoError(t, err)
	t.Cleanup(func() {
		cleanupCtx, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		db, err := mongoHandler.GetDatabase(cleanupCtx, dbName)
		if err == nil {
			err = db.Drop(cleanupCtx)
		}
		assert.NoError(t, err)
		assert.NoError(t, mongoHandler.Close(cleanupCtx))
	})

	store := repository.NewMongoDbRepositoryWithDefaults(mongoHandler, dbName)
	db, err := store.GetDatabase(ctx, "")
	require.NoError(t, err)
	require.NoError(t, voter.EnsureIndexes(ctx, db))
	service, err := vision.NewService(vision.NewRepository(store), voter.NewService(voter.NewRepository(store)))
	require.NoError(t, err)

	created, err := service.CreateVision(ctx, &vision.CreateVisionRequest{
		Title:       "Better search",
		Type:        vision.VisionTypeFeedback,
		Description: "Search every record",
		ActorID:     "user-1",
	})
	require.NoError(t, err)
	assert.False(t, created.Vision.IsRoadmapItem())
	verified := accesshelpers.TransitAuthenticatedWith(accesshelpers.TransitWith(ctx, "user-2"), true)
	_, err = service.SetVisionVote(verified, &vision.SetVisionVoteRequest{NanoID: created.Vision.NanoID, ActorID: "forged", Vote: vision.VisionVoteUpvote})
	require.ErrorIs(t, err, vision.ErrVisionUserIDIsRequired)
	unchanged, err := service.GetVisionByNanoID(ctx, &vision.GetVisionByNanoIDRequest{NanoID: created.Vision.NanoID})
	require.NoError(t, err)
	assert.Empty(t, unchanged.Vision.VoteSummary.Up)

	voted, err := service.SetVisionVote(ctx, &vision.SetVisionVoteRequest{
		NanoID: created.Vision.NanoID, ActorID: "user-2", Vote: vision.VisionVoteUpvote,
	})
	require.NoError(t, err)
	assert.Equal(t, 1, voted.Vision.VoteSummary.Up)

	voted, err = service.SetVisionVote(ctx, &vision.SetVisionVoteRequest{
		NanoID: created.Vision.NanoID, ActorID: "user-2", Vote: vision.VisionVoteDownvote,
	})
	require.NoError(t, err)
	assert.Empty(t, voted.Vision.VoteSummary.Up)
	assert.Equal(t, 1, voted.Vision.VoteSummary.Down)

	commented, err := service.AddVisionComment(ctx, &vision.AddVisionCommentRequest{
		NanoID: created.Vision.NanoID, ActorID: "user-3", Message: "Ask <@nano-user>",
	})
	require.NoError(t, err)
	require.Len(t, commented.Vision.Comments, 1)
	assert.Equal(t, "Ask <@nano-user>", commented.Vision.Comments[0].Message)
	rootCommentID := commented.Vision.Comments[0].ID

	commented, err = service.AddVisionComment(ctx, &vision.AddVisionCommentRequest{
		NanoID:          created.Vision.NanoID,
		ParentCommentID: rootCommentID,
		ActorID:         "user-4",
		Message:         "I agree with <@nano-user>",
	})
	require.NoError(t, err)
	require.Len(t, commented.Vision.Comments, 2)
	assert.Equal(t, 2, commented.Vision.CommentCount)
	assert.Equal(t, rootCommentID, commented.Vision.Comments[1].ParentCommentID)

	commented, err = service.SetVisionCommentVote(ctx, &vision.SetVisionCommentVoteRequest{
		NanoID:    created.Vision.NanoID,
		CommentID: rootCommentID,
		ActorID:   "user-5",
		Vote:      vision.VisionVoteUpvote,
	})
	require.NoError(t, err)
	assert.Equal(t, 1, commented.Vision.Comments[0].VoteSummary.Up)

	commented, err = service.SetVisionCommentVote(ctx, &vision.SetVisionCommentVoteRequest{
		NanoID:    created.Vision.NanoID,
		CommentID: rootCommentID,
		ActorID:   "user-5",
		Vote:      vision.VisionVoteDownvote,
	})
	require.NoError(t, err)
	assert.Empty(t, commented.Vision.Comments[0].VoteSummary.Up)
	assert.Equal(t, 1, commented.Vision.Comments[0].VoteSummary.Down)
	// Both parent and comment votes must live only in the shared collection.
	count, err := db.Collection(voter.Collection).CountDocuments(ctx, bson.M{"domain": "vision", "resource_id": created.Vision.ID})
	require.NoError(t, err)
	assert.EqualValues(t, 2, count)
	var stored bson.M
	require.NoError(t, db.Collection(vision.VisionCollection).FindOne(ctx, bson.M{"_id": created.Vision.ID}).Decode(&stored))
	assert.NotContains(t, stored, "voters")
	assert.NotContains(t, stored, "votes")
	for _, comment := range stored["comments"].(bson.A) {
		encoded, err := bson.Marshal(comment)
		require.NoError(t, err)
		var child bson.M
		require.NoError(t, bson.Unmarshal(encoded, &child))
		assert.NotContains(t, child, "voters")
		assert.NotContains(t, child, "votes")
	}

	roadmapped, err := service.UpdateVisionStatus(ctx, &vision.UpdateVisionStatusRequest{
		NanoID: created.Vision.NanoID, Status: vision.VisionStatusUnderReview, ActorID: "admin-1",
	})
	require.NoError(t, err)
	assert.True(t, roadmapped.Vision.IsRoadmapItem())

	list, err := service.GetVisions(ctx, &vision.GetVisionsRequest{
		Type: vision.VisionTypeFeedback, RoadmapOnly: true,
	})
	require.NoError(t, err)
	require.Len(t, list.Visions, 1)
	assert.Empty(t, list.Visions[0].Comments, "list projection should omit comments")
	assert.Equal(t, 2, list.Visions[0].CommentCount)

	deleted, err := service.DeleteVision(ctx, &vision.DeleteVisionRequest{NanoID: created.Vision.NanoID, ActorID: "admin-1"})
	require.NoError(t, err)
	assert.True(t, deleted.Deleted)
}
