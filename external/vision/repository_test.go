package vision

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
	lastCollectionName    string
	lastFilter            interface{}
	lastUpdate            interface{}
}

func (m *mockMongoStore) ExecuteCountDocuments(context.Context, *mongo.Collection, interface{}, ...options.Lister[options.CountOptions]) (int64, error) {
	return 1, nil
}
func (m *mockMongoStore) ExecuteDeleteOneCommand(_ context.Context, collection *mongo.Collection, filter interface{}, _ string) error {
	m.lastCollectionName, m.lastFilter = collection.Name(), filter
	return nil
}
func (m *mockMongoStore) ExecuteFindCommand(_ context.Context, collection *mongo.Collection, filter interface{}, _ ...options.Lister[options.FindOptions]) (*mongo.Cursor, error) {
	m.lastCollectionName, m.lastFilter = collection.Name(), filter
	return nil, nil
}
func (m *mockMongoStore) ExecuteFindOneCommandDecodeResult(_ context.Context, collection *mongo.Collection, filter interface{}, result interface{}, _ string, _ bool, _ error) error {
	m.lastCollectionName, m.lastFilter = collection.Name(), filter
	if item, ok := result.(*Vision); ok {
		*item = Vision{ID: "vision-1", Title: "Search", Type: VisionTypeFeedback}
	}
	return nil
}
func (m *mockMongoStore) ExecuteInsertOneCommand(_ context.Context, collection *mongo.Collection, _ interface{}, _ string) (*mongo.InsertOneResult, error) {
	m.lastCollectionName = collection.Name()
	return &mongo.InsertOneResult{}, nil
}
func (m *mockMongoStore) ExecuteUpdateOneCommand(_ context.Context, collection *mongo.Collection, filter interface{}, update interface{}, _ string) error {
	m.lastCollectionName, m.lastFilter, m.lastUpdate = collection.Name(), filter, update
	return nil
}
func (m *mockMongoStore) GetDatabase(context.Context, string) (*mongo.Database, error) {
	m.getDatabaseCalls++
	index := m.getDatabaseCalls - 1
	if index < len(m.getDatabaseErrs) && m.getDatabaseErrs[index] != nil {
		return nil, m.getDatabaseErrs[index]
	}
	client, err := mongo.Connect(options.Client().ApplyURI("mongodb://localhost:27017"))
	if err != nil {
		return nil, err
	}
	return client.Database("vision_test"), nil
}
func (m *mockMongoStore) InitialiseClient(context.Context) (*mongo.Client, error) {
	m.initialiseClientCalls++
	index := m.initialiseClientCalls - 1
	if index < len(m.initialiseClientErrs) && m.initialiseClientErrs[index] != nil {
		return nil, m.initialiseClientErrs[index]
	}
	return mongo.Connect(options.Client().ApplyURI("mongodb://localhost:27017"))
}
func (m *mockMongoStore) MapAllInCursorToResult(_ context.Context, _ *mongo.Cursor, result interface{}, _ string) error {
	if items, ok := result.(*[]Vision); ok {
		*items = []Vision{{ID: "vision-1", Title: "Search", Type: VisionTypeFeedback}}
	}
	return nil
}

func TestRepositoryGetVisionCollectionRetriesAndCaches(t *testing.T) {
	store := &mockMongoStore{initialiseClientErrs: []error{errors.New("temporary")}}
	repo := NewRepository(store)

	collection, err := repo.GetVisionCollection(context.Background())
	if err != nil {
		t.Fatalf("GetVisionCollection() error = %v", err)
	}
	if collection.Name() != VisionCollection || store.initialiseClientCalls != 2 {
		t.Fatalf("collection=%q init calls=%d", collection.Name(), store.initialiseClientCalls)
	}
	if _, err = repo.GetVisionCollection(context.Background()); err != nil {
		t.Fatalf("cached GetVisionCollection() error = %v", err)
	}
	if store.initialiseClientCalls != 2 {
		t.Fatalf("cache miss: init calls=%d", store.initialiseClientCalls)
	}
}

func TestRepositoryGetVisionCollectionReturnsWrappedError(t *testing.T) {
	store := &mockMongoStore{initialiseClientErrs: []error{errors.New("one"), errors.New("two")}}
	_, err := NewRepository(store).WithCollectionInitMaxAttemptsLimit(2).GetVisionCollection(context.Background())
	if !errors.Is(err, ErrVisionDatabaseError) {
		t.Fatalf("error = %v, want ErrVisionDatabaseError", err)
	}
}

func TestRepositoryGetVisionByNanoIDUsesNanoIDOnly(t *testing.T) {
	store := &mockMongoStore{}
	repo := NewRepository(store)

	if _, err := repo.GetVisionByNanoID(context.Background(), " public-nano "); err != nil {
		t.Fatalf("GetVisionByNanoID() error = %v", err)
	}

	want := bson.M{"_nano_id": "public-nano"}
	if !reflect.DeepEqual(store.lastFilter, want) {
		t.Fatalf("NanoID filter = %#v, want %#v", store.lastFilter, want)
	}
}

func TestRepositorySetVisionVoteUsesAtomicBuckets(t *testing.T) {
	store := &mockMongoStore{}
	repo := NewRepository(store)

	if err := repo.SetVisionVote(context.Background(), "vision-1", "user-1", VisionVoteDownvote, "now"); err != nil {
		t.Fatalf("SetVisionVote() error = %v", err)
	}
	update := store.lastUpdate.(bson.M)
	add := update["$addToSet"].(bson.M)
	pull := update["$pull"].(bson.M)
	if add["voters.0"] != "user-1" || pull["voters.1"] != "user-1" {
		t.Fatalf("atomic vote update = %#v", update)
	}
}

func TestRepositorySetVisionCommentVoteUsesAtomicPositionalBuckets(t *testing.T) {
	store := &mockMongoStore{}
	repo := NewRepository(store)

	if err := repo.SetVisionCommentVote(
		context.Background(),
		"vision-1",
		"comment-1",
		"user-1",
		VisionVoteUpvote,
		"now",
	); err != nil {
		t.Fatalf("SetVisionCommentVote() error = %v", err)
	}
	if !reflect.DeepEqual(store.lastFilter, bson.M{"_id": "vision-1", "comments.id": "comment-1"}) {
		t.Fatalf("comment vote filter = %#v", store.lastFilter)
	}
	update := store.lastUpdate.(bson.M)
	add := update["$addToSet"].(bson.M)
	pull := update["$pull"].(bson.M)
	if add["comments.$.voters.1"] != "user-1" || pull["comments.$.voters.0"] != "user-1" {
		t.Fatalf("atomic comment vote update = %#v", update)
	}
}

func TestRepositoryAddVisionCommentAtomicallyIncrementsCount(t *testing.T) {
	store := &mockMongoStore{}
	repo := NewRepository(store)
	comment := NewVisionComment("user-1", "Hello", "")

	if err := repo.AddVisionComment(context.Background(), "vision-1", comment); err != nil {
		t.Fatalf("AddVisionComment() error = %v", err)
	}
	update := store.lastUpdate.(bson.M)
	push := update["$push"].(bson.M)
	increment := update["$inc"].(bson.M)
	if push["comments"] != comment || increment["comment_count"] != 1 {
		t.Fatalf("atomic comment update = %#v", update)
	}
}

func TestBuildVisionListFilter(t *testing.T) {
	got := buildVisionListFilter(&GetVisionsRequest{
		Query:       " search ",
		Type:        VisionTypeFeedback,
		RoadmapOnly: true,
	})
	if got["type"] != VisionTypeFeedback {
		t.Fatalf("type filter = %#v", got["type"])
	}
	if !reflect.DeepEqual(got["status"], bson.M{"$exists": true, "$ne": ""}) {
		t.Fatalf("roadmap filter = %#v", got["status"])
	}
	wantQuery := []bson.M{
		{"title": bson.M{"$regex": "search", "$options": "i"}},
		{"description": bson.M{"$regex": "search", "$options": "i"}},
	}
	if !reflect.DeepEqual(got["$or"], wantQuery) {
		t.Fatalf("query filter = %#v", got["$or"])
	}
}

func TestNormaliseStoredVisionPreservesStoredCommentCount(t *testing.T) {
	projected := &Vision{CommentCount: 7}
	normaliseStoredVision(projected)
	if projected.CommentCount != 7 {
		t.Fatalf("projected CommentCount = %d, want 7", projected.CommentCount)
	}

	detailed := &Vision{
		CommentCount: 7,
		Comments: []VisionComment{
			{ID: "comment-1"},
			{ID: "comment-2"},
		},
	}
	normaliseStoredVision(detailed)
	if detailed.CommentCount != 7 {
		t.Fatalf("detailed CommentCount = %d, want 7", detailed.CommentCount)
	}
}

func TestNormalisePaginationAppliesSafeBounds(t *testing.T) {
	if page, pageSize := normalisePagination(nil); page != 1 || pageSize != defaultVisionPageSize {
		t.Fatalf("default pagination = (%d, %d)", page, pageSize)
	}
	if page, pageSize := normalisePagination(&GetVisionsRequest{Page: -1, PageSize: 1000}); page != 1 || pageSize != maxVisionPageSize {
		t.Fatalf("bounded pagination = (%d, %d)", page, pageSize)
	}
}
