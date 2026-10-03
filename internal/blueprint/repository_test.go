package blueprint

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

type mockMongoStore struct {
	client                   *mongo.Client
	nilClient, nilDatabase   bool
	afterInit, afterDatabase func()
	insertFunc               func(*Blueprint) (*mongo.InsertOneResult, error)
	updateFunc               func(bson.M, bson.M) (*mongo.UpdateResult, error)
	deleteFunc               func(bson.M) (*mongo.DeleteResult, error)
	findOneFunc              func(bson.M, *Blueprint) error
	findFunc                 func([]options.Lister[options.FindOptions]) (*mongo.Cursor, error)
	mapFunc                  func(*mongo.Cursor, *[]Blueprint) error
	countFunc                func() (int64, error)
	operations               []string
	initialiseClientErrs     []error
	getDatabaseErrs          []error

	initialiseClientCalls int
	getDatabaseCalls      int
	insertOneCalls        int
	countDocumentsCalls   int
	lastCollectionName    string
	lastFilter            interface{}
}

// newMockMongoStore owns a driver handle per case, without issuing real commands.
func newMockMongoStore(t *testing.T) *mockMongoStore {
	t.Helper()
	client, err := mongo.Connect(options.Client().ApplyURI("mongodb://127.0.0.1:27017"))
	require.NoError(t, err)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		require.NoError(t, client.Disconnect(ctx))
	})
	return &mockMongoStore{client: client}
}

func (m *mockMongoStore) ExecuteCountDocuments(ctx context.Context, collection *mongo.Collection, filter interface{}, opts ...options.Lister[options.CountOptions]) (int64, error) {
	m.operations = append(m.operations, "count")
	m.countDocumentsCalls++
	m.lastCollectionName = collection.Name()
	m.lastFilter = filter
	if m.countFunc != nil {
		return m.countFunc()
	}
	return 7, nil
}

func (m *mockMongoStore) ExecuteDeleteOneCommandResult(ctx context.Context, collection *mongo.Collection, filter any, opts ...options.Lister[options.DeleteOneOptions]) (*mongo.DeleteResult, error) {
	m.operations = append(m.operations, "delete")
	m.lastCollectionName = collection.Name()
	m.lastFilter = filter
	if m.deleteFunc != nil {
		return m.deleteFunc(filter.(bson.M))
	}
	return &mongo.DeleteResult{Acknowledged: true, DeletedCount: 1}, nil
}

func (m *mockMongoStore) ExecuteFindCommand(ctx context.Context, collection *mongo.Collection, filter interface{}, opts ...options.Lister[options.FindOptions]) (*mongo.Cursor, error) {
	m.operations = append(m.operations, "find")
	m.lastCollectionName = collection.Name()
	m.lastFilter = filter
	if m.findFunc != nil {
		return m.findFunc(opts)
	}
	return mongo.NewCursorFromDocuments(nil, nil, nil)
}

func (m *mockMongoStore) ExecuteFindOneCommandDecodeResult(ctx context.Context, collection *mongo.Collection, filter interface{}, result interface{}, resultObjectName string, logError bool, onFailureErr error) error {
	m.operations = append(m.operations, "find one")
	if onFailureErr != nil {
		return errors.New("unexpected replacement error")
	}
	m.lastCollectionName = collection.Name()
	m.lastFilter = filter
	if m.findOneFunc != nil {
		return m.findOneFunc(filter.(bson.M), result.(*Blueprint))
	}
	if blueprint, ok := result.(*Blueprint); ok {
		*blueprint = Blueprint{ID: "bp-1", Name: "Example", Kind: "demo"}
	}
	return nil
}

func (m *mockMongoStore) ExecuteInsertOneCommand(ctx context.Context, collection *mongo.Collection, document interface{}, resultObjectName string) (*mongo.InsertOneResult, error) {
	m.operations = append(m.operations, "insert")
	m.insertOneCalls++
	m.lastCollectionName = collection.Name()
	if m.insertFunc != nil {
		return m.insertFunc(document.(*Blueprint))
	}
	return &mongo.InsertOneResult{Acknowledged: true, InsertedID: document.(*Blueprint).ID}, nil
}

func (m *mockMongoStore) ExecuteUpdateOneCommandResult(ctx context.Context, collection *mongo.Collection, filter, update any, opts ...options.Lister[options.UpdateOneOptions]) (*mongo.UpdateResult, error) {
	m.operations = append(m.operations, "update")
	m.lastCollectionName = collection.Name()
	m.lastFilter = filter
	if m.updateFunc != nil {
		return m.updateFunc(filter.(bson.M), update.(bson.M))
	}
	return &mongo.UpdateResult{Acknowledged: true, MatchedCount: 1, ModifiedCount: 1}, nil
}

func (m *mockMongoStore) GetDatabase(ctx context.Context, dbName string) (*mongo.Database, error) {
	m.operations = append(m.operations, "database")
	if m.afterDatabase != nil {
		defer m.afterDatabase()
	}
	m.getDatabaseCalls++
	callIndex := m.getDatabaseCalls - 1
	if callIndex < len(m.getDatabaseErrs) && m.getDatabaseErrs[callIndex] != nil {
		return nil, m.getDatabaseErrs[callIndex]
	}

	if m.nilDatabase {
		return nil, nil
	}
	return m.client.Database("blueprint_test"), nil
}

func (m *mockMongoStore) InitialiseClient(ctx context.Context) (*mongo.Client, error) {
	m.operations = append(m.operations, "initialize")
	if m.afterInit != nil {
		defer m.afterInit()
	}
	m.initialiseClientCalls++
	callIndex := m.initialiseClientCalls - 1
	if callIndex < len(m.initialiseClientErrs) && m.initialiseClientErrs[callIndex] != nil {
		return nil, m.initialiseClientErrs[callIndex]
	}

	if m.nilClient {
		return nil, nil
	}
	return m.client, nil
}

func (m *mockMongoStore) MapAllInCursorToResult(ctx context.Context, cursor *mongo.Cursor, result interface{}, resultObjectName string) error {
	m.operations = append(m.operations, "map")
	if m.mapFunc != nil {
		return m.mapFunc(cursor, result.(*[]Blueprint))
	}
	if blueprints, ok := result.(*[]Blueprint); ok {
		*blueprints = []Blueprint{{ID: "bp-1", Name: "Example", Kind: "demo"}}
	}
	return nil
}

func TestRepositoryGetBlueprintCollection(t *testing.T) {
	tests := []struct {
		name                string
		initErrs            []error
		dbErrs              []error
		limit               int
		wantErr             bool
		wantInitCallCount   int
		wantDBCallCount     int
		secondCallUsesCache bool
	}{
		{
			name:                "SUCCESS - initialises collection",
			wantInitCallCount:   1,
			wantDBCallCount:     1,
			secondCallUsesCache: true,
		},
		{
			name:                "SUCCESS - retries initial client failure",
			initErrs:            []error{errors.New("first failure")},
			wantInitCallCount:   2,
			wantDBCallCount:     1,
			secondCallUsesCache: true,
		},
		{
			name:                "SUCCESS - retries database lookup failure",
			dbErrs:              []error{errors.New("first database failure")},
			wantInitCallCount:   2,
			wantDBCallCount:     2,
			secondCallUsesCache: true,
		},
		{
			name:              "FAILURE - returns database error after limit",
			initErrs:          []error{errors.New("one"), errors.New("two")},
			limit:             2,
			wantErr:           true,
			wantInitCallCount: 2,
			wantDBCallCount:   0,
		},
		{
			name:              "FAILURE - retries database lookup failure",
			dbErrs:            []error{errors.New("db one"), errors.New("db two")},
			limit:             2,
			wantErr:           true,
			wantInitCallCount: 2,
			wantDBCallCount:   2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := newMockMongoStore(t)
			store.initialiseClientErrs = tt.initErrs
			store.getDatabaseErrs = tt.dbErrs
			repo := NewRepository(store)
			if tt.limit > 0 {
				repo.WithCollectionInitMaxAttemptsLimit(tt.limit)
			}

			collection, err := repo.GetBlueprintCollection(context.Background())
			if tt.wantErr {
				if err == nil {
					t.Fatal("GetBlueprintCollection() expected error, got nil")
				}
				var cause error
				if len(tt.initErrs) > 0 {
					cause = tt.initErrs[len(tt.initErrs)-1]
				} else {
					cause = tt.dbErrs[len(tt.dbErrs)-1]
				}
				require.ErrorIs(t, err, cause)
				require.NotErrorIs(t, err, ErrBlueprintDatabaseError)
			} else {
				require.NoError(t, err)
				require.Equal(t, BlueprintCollection, collection.Name())
			}
			if store.initialiseClientCalls != tt.wantInitCallCount {
				t.Fatalf("InitialiseClient calls = %d, want %d", store.initialiseClientCalls, tt.wantInitCallCount)
			}
			if store.getDatabaseCalls != tt.wantDBCallCount {
				t.Fatalf("GetDatabase calls = %d, want %d", store.getDatabaseCalls, tt.wantDBCallCount)
			}

			if tt.secondCallUsesCache {
				_, err = repo.GetBlueprintCollection(context.Background())
				if err != nil {
					t.Fatalf("second GetBlueprintCollection() error = %v", err)
				}
				if store.initialiseClientCalls != tt.wantInitCallCount {
					t.Fatalf("second call InitialiseClient calls = %d, want cached %d", store.initialiseClientCalls, tt.wantInitCallCount)
				}
			}
		})
	}
}

func TestBuildBlueprintListFilter(t *testing.T) {
	tests := []struct {
		name string
		req  *GetBlueprintsRequest
		want map[string]interface{}
	}{
		{name: "SUCCESS - nil request", req: nil, want: map[string]interface{}{}},
		{name: "SUCCESS - kind filter", req: &GetBlueprintsRequest{Kind: " Demo "}, want: map[string]interface{}{"kind": "demo"}},
		{name: "SUCCESS - status filter", req: &GetBlueprintsRequest{Status: " Active "}, want: map[string]interface{}{"status": "active"}},
		{name: "SUCCESS - query filter", req: &GetBlueprintsRequest{Query: " Starter API "}, want: map[string]interface{}{"$or": []bson.M{
			{"name": bson.M{"$regex": "Starter API", "$options": "i"}},
			{"kind": bson.M{"$regex": "Starter API", "$options": "i"}},
			{"description": bson.M{"$regex": "Starter API", "$options": "i"}},
		}}},
		{name: "SUCCESS - combined filters", req: &GetBlueprintsRequest{Kind: " Demo ", Status: " Active "}, want: map[string]interface{}{"kind": "demo", "status": "active"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildBlueprintListFilter(tt.req)
			for key, wantValue := range tt.want {
				if !reflect.DeepEqual(got[key], wantValue) {
					t.Fatalf("filter[%s] = %v, want %v", key, got[key], wantValue)
				}
			}
		})
	}
}
