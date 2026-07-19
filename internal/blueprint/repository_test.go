package blueprint

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

type mockMongoStore struct {
	initialiseClientErrs []error
	getDatabaseErrs      []error

	initialiseClientCalls int
	getDatabaseCalls      int
	insertOneCalls        int
	countDocumentsCalls   int
	lastCollectionName    string
	lastFilter            interface{}
}

func (m *mockMongoStore) ExecuteCountDocuments(ctx context.Context, collection *mongo.Collection, filter interface{}, opts ...options.Lister[options.CountOptions]) (int64, error) {
	m.countDocumentsCalls++
	m.lastCollectionName = collection.Name()
	m.lastFilter = filter
	return 7, nil
}

func (m *mockMongoStore) ExecuteDeleteOneCommand(ctx context.Context, collection *mongo.Collection, filter interface{}, targetObjectName string) error {
	m.lastCollectionName = collection.Name()
	m.lastFilter = filter
	return nil
}

func (m *mockMongoStore) ExecuteFindCommand(ctx context.Context, collection *mongo.Collection, filter interface{}, opts ...options.Lister[options.FindOptions]) (*mongo.Cursor, error) {
	m.lastCollectionName = collection.Name()
	m.lastFilter = filter
	return nil, nil
}

func (m *mockMongoStore) ExecuteFindOneCommandDecodeResult(ctx context.Context, collection *mongo.Collection, filter interface{}, result interface{}, resultObjectName string, logError bool, onFailureErr error) error {
	m.lastCollectionName = collection.Name()
	m.lastFilter = filter
	if blueprint, ok := result.(*Blueprint); ok {
		*blueprint = Blueprint{ID: "bp-1", Name: "Example", Kind: "demo"}
	}
	return nil
}

func (m *mockMongoStore) ExecuteInsertOneCommand(ctx context.Context, collection *mongo.Collection, document interface{}, resultObjectName string) (*mongo.InsertOneResult, error) {
	m.insertOneCalls++
	m.lastCollectionName = collection.Name()
	return &mongo.InsertOneResult{}, nil
}

func (m *mockMongoStore) ExecuteUpdateOneCommand(ctx context.Context, collection *mongo.Collection, filter interface{}, updateFilter interface{}, resultObjectName string) error {
	m.lastCollectionName = collection.Name()
	m.lastFilter = filter
	return nil
}

func (m *mockMongoStore) GetDatabase(ctx context.Context, dbName string) (*mongo.Database, error) {
	m.getDatabaseCalls++
	callIndex := m.getDatabaseCalls - 1
	if callIndex < len(m.getDatabaseErrs) && m.getDatabaseErrs[callIndex] != nil {
		return nil, m.getDatabaseErrs[callIndex]
	}

	client, err := mongo.Connect(options.Client().ApplyURI("mongodb://localhost:27017"))
	if err != nil {
		return nil, err
	}
	return client.Database("blueprint_test"), nil
}

func (m *mockMongoStore) InitialiseClient(ctx context.Context) (*mongo.Client, error) {
	m.initialiseClientCalls++
	callIndex := m.initialiseClientCalls - 1
	if callIndex < len(m.initialiseClientErrs) && m.initialiseClientErrs[callIndex] != nil {
		return nil, m.initialiseClientErrs[callIndex]
	}

	return mongo.Connect(options.Client().ApplyURI("mongodb://localhost:27017"))
}

func (m *mockMongoStore) MapAllInCursorToResult(ctx context.Context, cursor *mongo.Cursor, result interface{}, resultObjectName string) error {
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
			store := &mockMongoStore{
				initialiseClientErrs: tt.initErrs,
				getDatabaseErrs:      tt.dbErrs,
			}
			repo := NewRepository(store)
			if tt.limit > 0 {
				repo.WithCollectionInitMaxAttemptsLimit(tt.limit)
			}

			collection, err := repo.GetBlueprintCollection(context.Background())
			if tt.wantErr {
				if err == nil {
					t.Fatal("GetBlueprintCollection() expected error, got nil")
				}
				if !errors.Is(err, ErrBlueprintDatabaseError) {
					t.Fatalf("GetBlueprintCollection() error = %v, want ErrBlueprintDatabaseError", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("GetBlueprintCollection() error = %v", err)
			}
			if collection.Name() != BlueprintCollection {
				t.Fatalf("collection name = %s, want %s", collection.Name(), BlueprintCollection)
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

func TestRepositoryCreateBlueprintUsesBlueprintCollection(t *testing.T) {
	store := &mockMongoStore{}
	repo := NewRepository(store)

	created, err := repo.CreateBlueprint(context.Background(), &Blueprint{ID: "bp-1", Name: "Example", Kind: "demo"})
	if err != nil {
		t.Fatalf("CreateBlueprint() error = %v", err)
	}
	if created.ID != "bp-1" {
		t.Fatalf("created ID = %s, want bp-1", created.ID)
	}
	if store.insertOneCalls != 1 {
		t.Fatalf("insert calls = %d, want 1", store.insertOneCalls)
	}
	if store.lastCollectionName != BlueprintCollection {
		t.Fatalf("collection = %s, want %s", store.lastCollectionName, BlueprintCollection)
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
