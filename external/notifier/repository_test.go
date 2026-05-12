package notifier

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

// mockNotifierMongoDbStore is a small repository double used to test the
// notifier repository's MongoDB orchestration without opening a real database
// connection.
type mockNotifierMongoDbStore struct {
	initialiseClientFunc func(ctx context.Context) (*mongo.Client, error)
	getDatabaseFunc      func(ctx context.Context, dbName string) (*mongo.Database, error)
	countFunc            func(ctx context.Context, collection *mongo.Collection, filter interface{}, opts ...*options.CountOptions) (int64, error)
	findFunc             func(ctx context.Context, collection *mongo.Collection, filter interface{}, opts ...*options.FindOptions) (*mongo.Cursor, error)
	findOneFunc          func(ctx context.Context, collection *mongo.Collection, filter interface{}, result interface{}, resultObjectName string, logError bool, onFailureErr error) error
	deleteOneFunc        func(ctx context.Context, collection *mongo.Collection, filter interface{}, targetObjectName string) error
	deleteManyFunc       func(ctx context.Context, collection *mongo.Collection, filter interface{}, targetObjectName string) error
	updateOneFunc        func(ctx context.Context, collection *mongo.Collection, filter interface{}, update interface{}, targetObjectName string) error
	mapAllFunc           func(ctx context.Context, cursor *mongo.Cursor, result interface{}, resultObjectName string) error
}

func (m *mockNotifierMongoDbStore) ExecuteCountDocuments(ctx context.Context, collection *mongo.Collection, filter interface{}, opts ...*options.CountOptions) (int64, error) {
	if m.countFunc != nil {
		return m.countFunc(ctx, collection, filter, opts...)
	}
	return 0, errors.New("not implemented")
}

func (m *mockNotifierMongoDbStore) ExecuteFindCommand(ctx context.Context, collection *mongo.Collection, filter interface{}, opts ...*options.FindOptions) (*mongo.Cursor, error) {
	if m.findFunc != nil {
		return m.findFunc(ctx, collection, filter, opts...)
	}
	return nil, errors.New("not implemented")
}

func (m *mockNotifierMongoDbStore) ExecuteDeleteOneCommand(ctx context.Context, collection *mongo.Collection, filter interface{}, targetObjectName string) error {
	if m.deleteOneFunc != nil {
		return m.deleteOneFunc(ctx, collection, filter, targetObjectName)
	}
	return errors.New("not implemented")
}

func (m *mockNotifierMongoDbStore) ExecuteDeleteManyCommand(ctx context.Context, collection *mongo.Collection, filter interface{}, targetObjectName string) error {
	if m.deleteManyFunc != nil {
		return m.deleteManyFunc(ctx, collection, filter, targetObjectName)
	}
	return errors.New("not implemented")
}

func (m *mockNotifierMongoDbStore) ExecuteFindOneCommandDecodeResult(ctx context.Context, collection *mongo.Collection, filter interface{}, result interface{}, resultObjectName string, logError bool, onFailureErr error) error {
	if m.findOneFunc != nil {
		return m.findOneFunc(ctx, collection, filter, result, resultObjectName, logError, onFailureErr)
	}
	return errors.New("not implemented")
}

func (m *mockNotifierMongoDbStore) GetDatabase(ctx context.Context, dbName string) (*mongo.Database, error) {
	if m.getDatabaseFunc != nil {
		return m.getDatabaseFunc(ctx, dbName)
	}
	return nil, errors.New("not implemented")
}

func (m *mockNotifierMongoDbStore) InitialiseClient(ctx context.Context) (*mongo.Client, error) {
	if m.initialiseClientFunc != nil {
		return m.initialiseClientFunc(ctx)
	}
	return nil, errors.New("not implemented")
}

func (m *mockNotifierMongoDbStore) ExecuteUpdateOneCommand(ctx context.Context, collection *mongo.Collection, filter interface{}, update interface{}, targetObjectName string) error {
	if m.updateOneFunc != nil {
		return m.updateOneFunc(ctx, collection, filter, update, targetObjectName)
	}
	return errors.New("not implemented")
}

func (m *mockNotifierMongoDbStore) MapAllInCursorToResult(ctx context.Context, cursor *mongo.Cursor, result interface{}, resultObjectName string) error {
	if m.mapAllFunc != nil {
		return m.mapAllFunc(ctx, cursor, result, resultObjectName)
	}
	return errors.New("not implemented")
}

// TestRepository_GetNotificationAddressesCollectionRetriesFailuresAndCachesSuccess
// verifies that the addresses collection follows the same lazy init, retry, and
// cache behaviour used by other GHATD external repositories.
func TestRepository_GetNotificationAddressesCollectionRetriesFailuresAndCachesSuccess(t *testing.T) {
	t.Parallel()

	client, err := mongo.NewClient(options.Client().ApplyURI("mongodb://localhost:27017"))
	require.NoError(t, err)

	initialiseAttempts := 0
	store := &mockNotifierMongoDbStore{
		initialiseClientFunc: func(ctx context.Context) (*mongo.Client, error) {
			initialiseAttempts++
			if initialiseAttempts == 1 {
				return nil, errors.New("temporary-init-error")
			}
			return client, nil
		},
		getDatabaseFunc: func(ctx context.Context, dbName string) (*mongo.Database, error) {
			return client.Database("notifier_test"), nil
		},
	}
	repo := NewRepository(store)

	collection, err := repo.GetNotificationAddressesCollection(context.Background())
	require.NoError(t, err)
	require.NotNil(t, collection)
	assert.Equal(t, NotificationAddressesCollection, collection.Name())
	assert.Equal(t, 2, initialiseAttempts)

	collection, err = repo.GetNotificationAddressesCollection(context.Background())
	require.NoError(t, err)
	require.NotNil(t, collection)
	assert.Equal(t, 2, initialiseAttempts)
}

// TestRepository_GetNotificationPreferencesCollectionRetriesFailuresAndCachesSuccess
// checks that preferences use their own cached collection and retry path instead
// of sharing state with notification addresses.
func TestRepository_GetNotificationPreferencesCollectionRetriesFailuresAndCachesSuccess(t *testing.T) {
	t.Parallel()

	client, err := mongo.NewClient(options.Client().ApplyURI("mongodb://localhost:27017"))
	require.NoError(t, err)

	initialiseAttempts := 0
	store := &mockNotifierMongoDbStore{
		initialiseClientFunc: func(ctx context.Context) (*mongo.Client, error) {
			initialiseAttempts++
			if initialiseAttempts == 1 {
				return nil, errors.New("temporary-init-error")
			}
			return client, nil
		},
		getDatabaseFunc: func(ctx context.Context, dbName string) (*mongo.Database, error) {
			return client.Database("notifier_test"), nil
		},
	}
	repo := NewRepository(store)

	collection, err := repo.GetNotificationPreferencesCollection(context.Background())
	require.NoError(t, err)
	require.NotNil(t, collection)
	assert.Equal(t, NotificationPreferencesCollection, collection.Name())
	assert.Equal(t, 2, initialiseAttempts)

	collection, err = repo.GetNotificationPreferencesCollection(context.Background())
	require.NoError(t, err)
	require.NotNil(t, collection)
	assert.Equal(t, 2, initialiseAttempts)
}

// TestRepository_GetNotificationAddressesCollectionReturnsAfterMaxAttempts
// verifies that a broken database connection fails fast after the configured
// default number of retries and wraps the failure in ErrDatabaseError.
func TestRepository_GetNotificationAddressesCollectionReturnsAfterMaxAttempts(t *testing.T) {
	t.Parallel()

	initialiseAttempts := 0
	store := &mockNotifierMongoDbStore{
		initialiseClientFunc: func(ctx context.Context) (*mongo.Client, error) {
			initialiseAttempts++
			return nil, errors.New("init-error")
		},
	}
	repo := NewRepository(store)

	collection, err := repo.GetNotificationAddressesCollection(context.Background())

	require.Error(t, err)
	assert.Nil(t, collection)
	assert.Equal(t, defaultCollectionInitMaxAttemptsLimit, initialiseAttempts)
	assert.ErrorIs(t, err, ErrDatabaseError)
}

// TestRepository_WithCollectionInitMaxAttemptsLimitOverridesDefault confirms
// that tests and apps can tune startup retry behaviour in the same way as
// streaker and the other GHATD packages.
func TestRepository_WithCollectionInitMaxAttemptsLimitOverridesDefault(t *testing.T) {
	t.Parallel()

	initialiseAttempts := 0
	store := &mockNotifierMongoDbStore{
		initialiseClientFunc: func(ctx context.Context) (*mongo.Client, error) {
			initialiseAttempts++
			return nil, errors.New("init-error")
		},
	}
	repo := NewRepository(store).WithCollectionInitMaxAttemptsLimit(2)

	collection, err := repo.GetNotificationPreferencesCollection(context.Background())

	require.Error(t, err)
	assert.Nil(t, collection)
	assert.Equal(t, 2, initialiseAttempts)
	assert.Contains(t, err.Error(), "after 2 attempts")
	assert.ErrorIs(t, err, ErrDatabaseError)
}

// TestRepository_DeleteAddressByIDForUserUsesRepositoryHelpers verifies the
// ownership check and helper-backed delete path used when a signed-in user
// removes one of their devices.
func TestRepository_DeleteAddressByIDForUserUsesRepositoryHelpers(t *testing.T) {
	t.Parallel()

	client, err := mongo.NewClient(options.Client().ApplyURI("mongodb://localhost:27017"))
	require.NoError(t, err)

	findOneCalls := 0
	deleteOneCalls := 0
	store := &mockNotifierMongoDbStore{
		initialiseClientFunc: func(ctx context.Context) (*mongo.Client, error) {
			return client, nil
		},
		getDatabaseFunc: func(ctx context.Context, dbName string) (*mongo.Database, error) {
			return client.Database("notifier_test"), nil
		},
		findOneFunc: func(ctx context.Context, collection *mongo.Collection, filter interface{}, result interface{}, resultObjectName string, logError bool, onFailureErr error) error {
			findOneCalls++
			assert.Equal(t, NotificationAddressesCollection, collection.Name())
			assert.Equal(t, bson.M{"_id": "address-1", "user_id": "user-1"}, filter)
			assert.Equal(t, "notification_address", resultObjectName)
			return nil
		},
		deleteOneFunc: func(ctx context.Context, collection *mongo.Collection, filter interface{}, targetObjectName string) error {
			deleteOneCalls++
			assert.Equal(t, NotificationAddressesCollection, collection.Name())
			assert.Equal(t, bson.M{"_id": "address-1", "user_id": "user-1"}, filter)
			assert.Equal(t, "notification_address", targetObjectName)
			return nil
		},
	}
	repo := NewRepository(store)

	err = repo.DeleteAddressByIDForUser(context.Background(), "user-1", "address-1")

	require.NoError(t, err)
	assert.Equal(t, 1, findOneCalls)
	assert.Equal(t, 1, deleteOneCalls)
}

func TestRepository_GetAddressesBuildsAdminFilter(t *testing.T) {
	t.Parallel()

	client, err := mongo.NewClient(options.Client().ApplyURI("mongodb://localhost:27017"))
	require.NoError(t, err)

	findCalls := 0
	store := &mockNotifierMongoDbStore{
		initialiseClientFunc: func(ctx context.Context) (*mongo.Client, error) {
			return client, nil
		},
		getDatabaseFunc: func(ctx context.Context, dbName string) (*mongo.Database, error) {
			return client.Database("notifier_test"), nil
		},
		findFunc: func(ctx context.Context, collection *mongo.Collection, filter interface{}, opts ...*options.FindOptions) (*mongo.Cursor, error) {
			findCalls++
			assert.Equal(t, NotificationAddressesCollection, collection.Name())
			assert.Equal(t, bson.M{
				"user_id": "user-1",
				"channel": NotificationChannelWebPush,
				"status":  NotificationAddressStatusActive,
			}, filter)
			require.Len(t, opts, 1)
			require.NotNil(t, opts[0].Limit)
			require.NotNil(t, opts[0].Skip)
			assert.Equal(t, int64(25), *opts[0].Limit)
			assert.Equal(t, int64(50), *opts[0].Skip)
			return mongo.NewCursorFromDocuments([]interface{}{}, nil, nil)
		},
		mapAllFunc: func(ctx context.Context, cursor *mongo.Cursor, result interface{}, resultObjectName string) error {
			assert.Equal(t, "notification_addresses", resultObjectName)
			return nil
		},
	}
	repo := NewRepository(store)

	addresses, err := repo.GetAddresses(context.Background(), &ListNotificationAddressesRequest{
		UserID:  "user-1",
		Channel: NotificationChannelWebPush,
		Status:  NotificationAddressStatusActive,
		Page:    3,
		PerPage: 25,
	})

	require.NoError(t, err)
	assert.Empty(t, addresses)
	assert.Equal(t, 1, findCalls)
}

func TestRepository_CountAddressesBuildsAdminFilter(t *testing.T) {
	t.Parallel()

	client, err := mongo.NewClient(options.Client().ApplyURI("mongodb://localhost:27017"))
	require.NoError(t, err)

	countCalls := 0
	store := &mockNotifierMongoDbStore{
		initialiseClientFunc: func(ctx context.Context) (*mongo.Client, error) {
			return client, nil
		},
		getDatabaseFunc: func(ctx context.Context, dbName string) (*mongo.Database, error) {
			return client.Database("notifier_test"), nil
		},
		countFunc: func(ctx context.Context, collection *mongo.Collection, filter interface{}, opts ...*options.CountOptions) (int64, error) {
			countCalls++
			assert.Equal(t, NotificationAddressesCollection, collection.Name())
			assert.Equal(t, bson.M{
				"user_id": "user-1",
				"channel": NotificationChannelFCM,
				"status":  NotificationAddressStatusDisabled,
			}, filter)
			return 42, nil
		},
	}
	repo := NewRepository(store)

	total, err := repo.CountAddresses(context.Background(), &ListNotificationAddressesRequest{
		UserID:  "user-1",
		Channel: NotificationChannelFCM,
		Status:  NotificationAddressStatusDisabled,
	})

	require.NoError(t, err)
	assert.Equal(t, int64(42), total)
	assert.Equal(t, 1, countCalls)
}

// TestRepository_DeleteAddressByIDForUserStopsWhenAddressIsNotFound ensures
// the delete helper is not called when the ownership lookup fails.
func TestRepository_DeleteAddressByIDForUserStopsWhenAddressIsNotFound(t *testing.T) {
	t.Parallel()

	client, err := mongo.NewClient(options.Client().ApplyURI("mongodb://localhost:27017"))
	require.NoError(t, err)

	deleteOneCalls := 0
	store := &mockNotifierMongoDbStore{
		initialiseClientFunc: func(ctx context.Context) (*mongo.Client, error) {
			return client, nil
		},
		getDatabaseFunc: func(ctx context.Context, dbName string) (*mongo.Database, error) {
			return client.Database("notifier_test"), nil
		},
		findOneFunc: func(ctx context.Context, collection *mongo.Collection, filter interface{}, result interface{}, resultObjectName string, logError bool, onFailureErr error) error {
			return onFailureErr
		},
		deleteOneFunc: func(ctx context.Context, collection *mongo.Collection, filter interface{}, targetObjectName string) error {
			deleteOneCalls++
			return nil
		},
	}
	repo := NewRepository(store)

	err = repo.DeleteAddressByIDForUser(context.Background(), "user-1", "missing-address")

	require.ErrorIs(t, err, ErrNotificationAddressNotFound)
	assert.Equal(t, 0, deleteOneCalls)
}

// TestRepository_DeleteAddressesByUserIDUsesDeleteManyHelper checks that
// account-cleanup deletes flow through the shared Mongo repository helper.
func TestRepository_DeleteAddressesByUserIDUsesDeleteManyHelper(t *testing.T) {
	t.Parallel()

	client, err := mongo.NewClient(options.Client().ApplyURI("mongodb://localhost:27017"))
	require.NoError(t, err)

	deleteManyCalls := 0
	store := &mockNotifierMongoDbStore{
		initialiseClientFunc: func(ctx context.Context) (*mongo.Client, error) {
			return client, nil
		},
		getDatabaseFunc: func(ctx context.Context, dbName string) (*mongo.Database, error) {
			return client.Database("notifier_test"), nil
		},
		deleteManyFunc: func(ctx context.Context, collection *mongo.Collection, filter interface{}, targetObjectName string) error {
			deleteManyCalls++
			assert.Equal(t, NotificationAddressesCollection, collection.Name())
			assert.Equal(t, bson.M{"user_id": "user-1"}, filter)
			assert.Equal(t, "notification_addresses", targetObjectName)
			return nil
		},
	}
	repo := NewRepository(store)

	err = repo.DeleteAddressesByUserID(context.Background(), "user-1")

	require.NoError(t, err)
	assert.Equal(t, 1, deleteManyCalls)
}

// TestRepository_DisableAddressByHashUsesExecuteUpdateOneCommand verifies
// that DisableAddressByHash delegates to ExecuteUpdateOneCommand with the
// correct filter, update document, and target object name, and wraps errors.
func TestRepository_DisableAddressByHashUsesExecuteUpdateOneCommand(t *testing.T) {
	t.Parallel()

	client, err := mongo.NewClient(options.Client().ApplyURI("mongodb://localhost:27017"))
	require.NoError(t, err)

	updateOneCalls := 0
	store := &mockNotifierMongoDbStore{
		initialiseClientFunc: func(ctx context.Context) (*mongo.Client, error) {
			return client, nil
		},
		getDatabaseFunc: func(ctx context.Context, dbName string) (*mongo.Database, error) {
			return client.Database("notifier_test"), nil
		},
		updateOneFunc: func(ctx context.Context, collection *mongo.Collection, filter interface{}, update interface{}, targetObjectName string) error {
			updateOneCalls++
			assert.Equal(t, NotificationAddressesCollection, collection.Name())
			assert.Equal(t, bson.M{"address_hash": "hash-abc"}, filter)
			updateDoc, ok := update.(bson.M)
			require.True(t, ok)
			setDoc, ok := updateDoc["$set"].(bson.M)
			require.True(t, ok)
			assert.Equal(t, NotificationAddressStatusDisabled, setDoc["status"])
			assert.NotEmpty(t, setDoc["metadata.updated_at"])
			assert.Equal(t, "notification_address", targetObjectName)
			return nil
		},
	}
	repo := NewRepository(store)

	err = repo.DisableAddressByHash(context.Background(), "hash-abc")

	require.NoError(t, err)
	assert.Equal(t, 1, updateOneCalls)
}

// TestRepository_DisableAddressByHashWrapsExecutorError checks that errors
// from ExecuteUpdateOneCommand are wrapped in ErrDatabaseError.
func TestRepository_DisableAddressByHashWrapsExecutorError(t *testing.T) {
	t.Parallel()

	client, err := mongo.NewClient(options.Client().ApplyURI("mongodb://localhost:27017"))
	require.NoError(t, err)

	store := &mockNotifierMongoDbStore{
		initialiseClientFunc: func(ctx context.Context) (*mongo.Client, error) {
			return client, nil
		},
		getDatabaseFunc: func(ctx context.Context, dbName string) (*mongo.Database, error) {
			return client.Database("notifier_test"), nil
		},
		updateOneFunc: func(ctx context.Context, collection *mongo.Collection, filter interface{}, update interface{}, targetObjectName string) error {
			return errors.New("connection refused")
		},
	}
	repo := NewRepository(store)

	err = repo.DisableAddressByHash(context.Background(), "hash-abc")

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrDatabaseError)
	assert.Contains(t, err.Error(), "connection refused")
}
