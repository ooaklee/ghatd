package streaker

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
	executeFindFunc      func(ctx context.Context, collection *mongo.Collection, filter interface{}, opts ...*options.FindOptions) (*mongo.Cursor, error)
	mapAllFunc           func(ctx context.Context, cursor *mongo.Cursor, result interface{}, resultObjectName string) error
}

func (m *mockMongoDbStore) ExecuteCountDocuments(ctx context.Context, collection *mongo.Collection, filter interface{}, opts ...*options.CountOptions) (int64, error) {
	return 0, errors.New("not implemented")
}

func (m *mockMongoDbStore) ExecuteFindCommand(ctx context.Context, collection *mongo.Collection, filter interface{}, opts ...*options.FindOptions) (*mongo.Cursor, error) {
	if m.executeFindFunc != nil {
		return m.executeFindFunc(ctx, collection, filter, opts...)
	}
	return nil, errors.New("not implemented")
}

func (m *mockMongoDbStore) ExecuteInsertOneCommand(ctx context.Context, collection *mongo.Collection, document interface{}, resultObjectName string) (*mongo.InsertOneResult, error) {
	return nil, errors.New("not implemented")
}

func (m *mockMongoDbStore) ExecuteFindOneCommandDecodeResult(ctx context.Context, collection *mongo.Collection, filter interface{}, result interface{}, resultObjectName string, logError bool, onFailureErr error) error {
	return errors.New("not implemented")
}

func (m *mockMongoDbStore) ExecuteAggregateCommand(ctx context.Context, collection *mongo.Collection, mongoPipeline []bson.D) (*mongo.Cursor, error) {
	return nil, errors.New("not implemented")
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
	if m.mapAllFunc != nil {
		return m.mapAllFunc(ctx, cursor, result, resultObjectName)
	}
	return errors.New("not implemented")
}

func (m *mockMongoDbStore) MapOneInCursorToResult(ctx context.Context, cursor *mongo.Cursor, result interface{}, resultObjectName string) error {
	return errors.New("not implemented")
}

func TestRepository_GetStreakCollectionRetriesFailuresAndCachesSuccess(t *testing.T) {
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
			return client.Database("streaker_test"), nil
		},
	}
	repo := NewRepository(store)

	collection, err := repo.GetStreakCollection(context.Background())
	require.NoError(t, err)
	require.NotNil(t, collection)
	assert.Equal(t, StreakCollection, collection.Name())
	assert.Equal(t, 2, initialiseAttempts)

	collection, err = repo.GetStreakCollection(context.Background())
	require.NoError(t, err)
	require.NotNil(t, collection)
	assert.Equal(t, 2, initialiseAttempts)
}

func TestRepository_GetStreakCollectionReturnsAfterMaxAttempts(t *testing.T) {
	t.Parallel()

	initialiseAttempts := 0
	store := &mockMongoDbStore{
		initialiseClientFunc: func(ctx context.Context) (*mongo.Client, error) {
			initialiseAttempts++
			return nil, errors.New("init-error")
		},
	}
	repo := NewRepository(store)

	collection, err := repo.GetStreakCollection(context.Background())

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

	collection, err := repo.GetStreakCollection(context.Background())

	require.Error(t, err)
	assert.Nil(t, collection)
	assert.Equal(t, 2, initialiseAttempts)
	assert.Contains(t, err.Error(), "after 2 attempts")
}

func TestRepository_ListStreaksBuildsHistoryFilter(t *testing.T) {
	t.Parallel()

	client, err := mongo.NewClient(options.Client().ApplyURI("mongodb://localhost:27017"))
	require.NoError(t, err)

	var capturedFilter bson.M
	var capturedOptions *options.FindOptions
	store := &mockMongoDbStore{
		executeFindFunc: func(ctx context.Context, collection *mongo.Collection, filter interface{}, opts ...*options.FindOptions) (*mongo.Cursor, error) {
			capturedFilter = filter.(bson.M)
			require.Len(t, opts, 1)
			capturedOptions = opts[0]
			return nil, nil
		},
		mapAllFunc: func(ctx context.Context, cursor *mongo.Cursor, result interface{}, resultObjectName string) error {
			streaks := result.(*[]*Streak)
			*streaks = []*Streak{{Id: "streak-1", PeriodKey: "2026-05-21"}}
			return nil
		},
	}
	repo := NewRepository(store)
	repo.collection = client.Database("streaker_test").Collection(StreakCollection)

	res, err := repo.ListStreaks(context.Background(), &ListStreaksRequest{
		StreakStatsRequest: StreakStatsRequest{
			StreakType: "wms-saved-words",
			OwnerId:    "user-1",
			TargetType: "affirmation",
			TargetId:   "saved-words",
			PeriodType: StreakPeriodTypeDaily,
		},
		PeriodKeyFrom: "2026-05-20",
		PeriodKeyTo:   "2026-05-22",
		Page:          2,
		PerPage:       25,
		Sort:          "asc",
	})

	require.NoError(t, err)
	require.Len(t, res, 1)
	assert.Equal(t, "wms-saved-words", capturedFilter["streak_type"])
	assert.Equal(t, "user-1", capturedFilter["owner_id"])
	assert.Equal(t, "affirmation", capturedFilter["target_type"])
	assert.Equal(t, "saved-words", capturedFilter["target_id"])
	assert.Equal(t, StreakPeriodTypeDaily, capturedFilter["period_type"])
	assert.Equal(t, bson.M{"$gte": "2026-05-20", "$lte": "2026-05-22"}, capturedFilter["period_key"])
	require.NotNil(t, capturedOptions)
	assert.Equal(t, int64(25), *capturedOptions.Limit)
	assert.Equal(t, int64(25), *capturedOptions.Skip)
}
