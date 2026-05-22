package reminder

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type mockMongoDbStore struct {
	initialiseClientFunc func(ctx context.Context) (*mongo.Client, error)
	getDatabaseFunc      func(ctx context.Context, dbName string) (*mongo.Database, error)
}

func (m *mockMongoDbStore) ExecuteCountDocuments(ctx context.Context, collection *mongo.Collection, filter interface{}, opts ...*options.CountOptions) (int64, error) {
	return 0, errors.New("not implemented")
}

func (m *mockMongoDbStore) ExecuteFindCommand(ctx context.Context, collection *mongo.Collection, filter interface{}, opts ...*options.FindOptions) (*mongo.Cursor, error) {
	return nil, errors.New("not implemented")
}

func (m *mockMongoDbStore) ExecuteInsertOneCommand(ctx context.Context, collection *mongo.Collection, document interface{}, resultObjectName string) (*mongo.InsertOneResult, error) {
	return nil, errors.New("not implemented")
}

func (m *mockMongoDbStore) ExecuteFindOneCommandDecodeResult(ctx context.Context, collection *mongo.Collection, filter interface{}, result interface{}, resultObjectName string, logError bool, onFailureErr error) error {
	return errors.New("not implemented")
}

func (m *mockMongoDbStore) ExecuteUpdateOneCommand(ctx context.Context, collection *mongo.Collection, filter interface{}, update interface{}, resultObjectName string) error {
	return errors.New("not implemented")
}

func (m *mockMongoDbStore) ExecuteDeleteOneCommand(ctx context.Context, collection *mongo.Collection, filter interface{}, targetObjectName string) error {
	return errors.New("not implemented")
}

func (m *mockMongoDbStore) GetDatabase(ctx context.Context, dbName string) (*mongo.Database, error) {
	if m.getDatabaseFunc != nil {
		return m.getDatabaseFunc(ctx, dbName)
	}
	return nil, errors.New("not implemented")
}

func (m *mockMongoDbStore) InitialiseClient(ctx context.Context) (*mongo.Client, error) {
	if m.initialiseClientFunc != nil {
		return m.initialiseClientFunc(ctx)
	}
	return nil, errors.New("not implemented")
}

func (m *mockMongoDbStore) MapAllInCursorToResult(ctx context.Context, cursor *mongo.Cursor, result interface{}, resultObjectName string) error {
	return errors.New("not implemented")
}

func (m *mockMongoDbStore) MapOneInCursorToResult(ctx context.Context, cursor *mongo.Cursor, result interface{}, resultObjectName string) error {
	return errors.New("not implemented")
}

func TestRepository_GetReminderCollectionRetriesFailuresAndCachesSuccess(t *testing.T) {
	t.Parallel()

	client, err := mongo.NewClient(options.Client().ApplyURI("mongodb://localhost:27017"))
	require.NoError(t, err)

	initialiseAttempts := 0
	store := &mockMongoDbStore{
		initialiseClientFunc: func(ctx context.Context) (*mongo.Client, error) {
			initialiseAttempts++
			if initialiseAttempts == 1 {
				return nil, errors.New("temporary-init-error")
			}
			return client, nil
		},
		getDatabaseFunc: func(ctx context.Context, dbName string) (*mongo.Database, error) {
			return client.Database("reminder_test"), nil
		},
	}
	repo := NewRepository(store)

	collection, err := repo.GetReminderCollection(context.Background())
	require.NoError(t, err)
	require.NotNil(t, collection)
	assert.Equal(t, ReminderCollection, collection.Name())
	assert.Equal(t, 2, initialiseAttempts)

	collection, err = repo.GetReminderCollection(context.Background())
	require.NoError(t, err)
	require.NotNil(t, collection)
	assert.Equal(t, 2, initialiseAttempts)
}

func TestRepository_GetReminderCollectionReturnsAfterMaxAttempts(t *testing.T) {
	t.Parallel()

	initialiseAttempts := 0
	store := &mockMongoDbStore{
		initialiseClientFunc: func(ctx context.Context) (*mongo.Client, error) {
			initialiseAttempts++
			return nil, errors.New("init-error")
		},
	}
	repo := NewRepository(store)

	collection, err := repo.GetReminderCollection(context.Background())

	require.Error(t, err)
	assert.Nil(t, collection)
	assert.Equal(t, defaultCollectionInitMaxAttemptsLimit, initialiseAttempts)
	assert.ErrorIs(t, err, ErrDatabaseError)
}

func TestRepository_WithCollectionInitMaxAttemptsLimitOverridesDefault(t *testing.T) {
	t.Parallel()

	initialiseAttempts := 0
	store := &mockMongoDbStore{
		initialiseClientFunc: func(ctx context.Context) (*mongo.Client, error) {
			initialiseAttempts++
			return nil, errors.New("init-error")
		},
	}
	repo := NewRepository(store).WithCollectionInitMaxAttemptsLimit(2)

	collection, err := repo.GetReminderCollection(context.Background())

	require.Error(t, err)
	assert.Nil(t, collection)
	assert.Equal(t, 2, initialiseAttempts)
	assert.Contains(t, err.Error(), "after 2 attempts")
}

func TestBuildReminderListFilter(t *testing.T) {
	t.Parallel()

	t.Run("nil filter returns base filter", func(t *testing.T) {
		f := buildReminderListFilter(nil)
		assert.Equal(t, bson.M{"_id": bson.M{"$exists": true}}, f)
	})

	t.Run("empty filter returns base filter", func(t *testing.T) {
		f := buildReminderListFilter(&ReminderFilter{})
		assert.Equal(t, bson.M{"_id": bson.M{"$exists": true}}, f)
	})

	t.Run("with user id", func(t *testing.T) {
		f := buildReminderListFilter(&ReminderFilter{UserID: "user-1"})
		assert.Equal(t, "user-1", f["user_id"])
	})

	t.Run("with user ids", func(t *testing.T) {
		f := buildReminderListFilter(&ReminderFilter{UserIDs: []string{"user-1", "user-2"}})
		assert.Equal(t, bson.M{"$in": []string{"user-1", "user-2"}}, f["user_id"])
	})

	t.Run("with user id and user ids", func(t *testing.T) {
		f := buildReminderListFilter(&ReminderFilter{UserID: "user-1", UserIDs: []string{"user-2"}})
		assert.Equal(t, bson.M{"$in": []string{"user-1", "user-2"}}, f["user_id"])
	})

	t.Run("with status", func(t *testing.T) {
		f := buildReminderListFilter(&ReminderFilter{Status: "active"})
		assert.Equal(t, "active", f["status"])
	})

	t.Run("with due before", func(t *testing.T) {
		f := buildReminderListFilter(&ReminderFilter{DueBefore: "2026-05-10T12:00:00"})
		assert.Equal(t, bson.M{"$lte": "2026-05-10T12:00:00"}, f["target_time"])
	})

	t.Run("with user and status", func(t *testing.T) {
		f := buildReminderListFilter(&ReminderFilter{UserID: "user-1", Status: "active"})
		assert.Equal(t, "user-1", f["user_id"])
		assert.Equal(t, "active", f["status"])
	})

	t.Run("with target type and id", func(t *testing.T) {
		f := buildReminderListFilter(&ReminderFilter{TargetType: "lesson", TargetId: "lesson-1"})
		assert.Equal(t, "lesson", f["target_type"])
		assert.Equal(t, "lesson-1", f["target_id"])
	})
}

func TestBuildReminderExecutionFilter(t *testing.T) {
	t.Parallel()

	t.Run("nil filter returns base filter", func(t *testing.T) {
		f := buildReminderExecutionFilter(nil)
		assert.Equal(t, bson.M{"_id": bson.M{"$exists": true}}, f)
	})

	t.Run("with reminder and target fields", func(t *testing.T) {
		f := buildReminderExecutionFilter(&ReminderExecutionFilter{
			ReminderId: "reminder-1",
			UserID:     "user-1",
			Status:     ReminderExecutionStatusFailed,
			TargetType: "lesson",
			TargetId:   "lesson-1",
		})
		assert.Equal(t, "reminder-1", f["reminder_id"])
		assert.Equal(t, "user-1", f["user_id"])
		assert.Equal(t, ReminderExecutionStatusFailed, f["status"])
		assert.Equal(t, "lesson", f["target_type"])
		assert.Equal(t, "lesson-1", f["target_id"])
	})
}

func TestBuildReminderPaginationOptions(t *testing.T) {
	t.Parallel()

	t.Run("default pagination", func(t *testing.T) {
		opts := buildReminderPaginationOptions(0, 0)
		skip := opts.Skip
		limit := opts.Limit
		require.NotNil(t, skip)
		require.NotNil(t, limit)
		assert.EqualValues(t, 0, *skip)
		assert.EqualValues(t, 25, *limit)
	})

	t.Run("custom pagination", func(t *testing.T) {
		opts := buildReminderPaginationOptions(2, 10)
		skip := opts.Skip
		limit := opts.Limit
		require.NotNil(t, skip)
		require.NotNil(t, limit)
		assert.EqualValues(t, 10, *skip)
		assert.EqualValues(t, 10, *limit)
	})
}
