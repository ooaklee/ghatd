package contacter_test

import (
	"context"
	"testing"

	"github.com/benweissmann/memongo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ooaklee/ghatd/external/contacter"
	"github.com/ooaklee/ghatd/external/repository"
	repositoryhelpers "github.com/ooaklee/ghatd/external/repository/helpers"
	"github.com/ooaklee/ghatd/external/toolbox"
)

func TestRepository_Integration_CommsLifecycleAndStats(t *testing.T) {
	ctx := context.Background()

	mongoServer, err := memongo.StartWithOptions(&memongo.Options{MongoVersion: "7.0.14"})
	if err != nil {
		t.Skipf("skipping integration test: unable to start memongo: %v", err)
	}
	t.Cleanup(func() {
		mongoServer.Stop()
	})

	dbName := memongo.RandomDatabase()
	mongoHandler, err := repositoryhelpers.NewHandler(repositoryhelpers.DefaultConfig(mongoServer.URI(), dbName))
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = mongoHandler.Close(ctx)
	})

	store := repository.NewMongoDbRepositoryWithDefaults(mongoHandler, dbName)
	repo := contacter.NewRepository(store)

	createdA, err := repo.CreateComms(ctx, &contacter.Comms{
		FullName:     "Jane Doe",
		Email:        "jane@example.com",
		Type:         contacter.CommsTypeFeedbackCompanion,
		Message:      "Companion feedback #1",
		UserLoggedIn: true,
	})
	require.NoError(t, err)
	require.NotEmpty(t, createdA.Id)
	require.NotEmpty(t, createdA.CreatedAt)

	createdB, err := repo.CreateComms(ctx, &contacter.Comms{
		FullName:     "John Doe",
		Email:        "john@example.com",
		Type:         contacter.CommsTypeFeedbackCompanion,
		Message:      "Companion feedback #2",
		UserLoggedIn: false,
	})
	require.NoError(t, err)

	createdC, err := repo.CreateComms(ctx, &contacter.Comms{
		FullName:     "Sam Doe",
		Email:        "sam@example.com",
		Type:         contacter.CommsTypeFeedback,
		Message:      "General feedback",
		UserLoggedIn: false,
	})
	require.NoError(t, err)

	t.Run("GetTotalComms with type filter", func(t *testing.T) {
		total, err := repo.GetTotalComms(ctx, &contacter.GetTotalCommsRequest{
			CommsTypes: []contacter.CommsType{contacter.CommsTypeFeedbackCompanion},
		})
		require.NoError(t, err)
		assert.Equal(t, int64(2), total)
	})

	t.Run("GetComms with with_types query", func(t *testing.T) {
		results, err := repo.GetComms(ctx, &contacter.GetCommsRequest{
			WithTypes: "feedback-companion",
			PerPage:   10,
			Page:      1,
			Order:     "created_at_desc",
		})
		require.NoError(t, err)
		require.Len(t, results, 2)

		for _, c := range results {
			assert.Equal(t, contacter.CommsTypeFeedbackCompanion, c.Type)
		}
	})

	t.Run("UpdateComms and verify stats aggregation", func(t *testing.T) {
		createdA.AdminNotes = "Followed up"
		createdA.AdminReply = "Thanks for the report"
		createdA.LinkedCommsIds = []string{createdB.Id}
		createdA.ReachedOutAt = toolbox.TimeNowUTC()

		updated, err := repo.UpdateComms(ctx, createdA)
		require.NoError(t, err)
		require.NotEmpty(t, updated.UpdatedAt)

		stats, err := repo.GetCommsStatsCounts(ctx, &contacter.GetCommsStatsRequest{})
		require.NoError(t, err)
		require.NotNil(t, stats)

		assert.Equal(t, int64(3), stats.Total)
		assert.Equal(t, int64(1), stats.RepliedTo)
		assert.Equal(t, int64(1), stats.ReachedOut)
		assert.Equal(t, int64(1), stats.WithAdminNotes)
		assert.Equal(t, int64(1), stats.WithLinkedComms)
		assert.Equal(t, int64(1), stats.FromLoggedInUsers)
		assert.Equal(t, int64(2), stats.FromGuests)
		assert.Equal(t, int64(2), stats.ByType.FeedbackCompanion)
		assert.Equal(t, int64(1), stats.ByType.Feedback)
		assert.Equal(t, int64(1), stats.ByStatus.ReachedOut)
		assert.Equal(t, int64(2), stats.ByStatus.NotReachedOut)
		assert.GreaterOrEqual(t, stats.AverageReplyTimeMinutes, 0.0)
		assert.NotEmpty(t, stats.MostRecentCommsAt)
	})

	t.Run("GetCommsByIds retrieves inserted resources", func(t *testing.T) {
		results, err := repo.GetCommsByIds(ctx, []string{createdA.Id, createdB.Id, createdC.Id})
		require.NoError(t, err)
		assert.Len(t, results, 3)
	})
}
