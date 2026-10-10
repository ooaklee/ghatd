package cataloguestore

import (
	"context"
	"errors"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/ooaklee/ghatd/external/catalogue"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

type fullStore interface {
	Store
	GetByID(context.Context, string) (Document, error)
	Count(context.Context) (int64, error)
}

func contract(t *testing.T, s fullStore) {
	t.Helper()
	ctx := context.Background()
	now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	input := Document{ID: "GB", Key: "GB", Enabled: true, Payload: bson.M{"name": "United Kingdom", "values": []string{"GB"}}, Audit: catalogue.Audit{Revision: 1, CreatedAt: now, CreatedBy: "seeder"}}
	first, inserted, err := s.InsertIfAbsent(ctx, input)
	require.NoError(t, err)
	require.True(t, inserted)
	require.Equal(t, "GB", first.Key)
	input.Payload.(bson.M)["name"] = "mutated caller"
	kept, err := s.GetByID(ctx, "GB")
	require.NoError(t, err)
	payload, err := DecodePayload[struct {
		Name string `bson:"name"`
	}](kept)
	require.NoError(t, err)
	require.Equal(t, "United Kingdom", payload.Name)
	rows, count, err := s.List(ctx, catalogue.ListQuery{}, true)
	require.NoError(t, err)
	require.EqualValues(t, 1, count)
	require.Len(t, rows, 1)
	// Two writers share the same precondition. Exactly one replacement may win.
	var won, stale atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			next := kept
			next.Hidden = true
			next.Revision = 2
			_, err := s.ReplaceWithRevision(ctx, next, 1)
			if err == nil {
				won.Add(1)
			} else if errors.Is(err, catalogue.ErrStaleWrite) {
				stale.Add(1)
			} else {
				t.Errorf("unexpected CAS outcome: %v", err)
			}
		}()
	}
	wg.Wait()
	require.EqualValues(t, 1, won.Load())
	require.EqualValues(t, 1, stale.Load())
	rows, count, err = s.List(ctx, catalogue.ListQuery{IncludeHidden: true, IncludeDisabled: true, IncludeDeleted: true}, true)
	require.NoError(t, err)
	require.Empty(t, rows)
	require.Zero(t, count)
	second, inserted, err := s.InsertIfAbsent(ctx, input)
	require.NoError(t, err)
	require.False(t, inserted)
	require.True(t, second.Hidden)
	require.Equal(t, 2, second.Revision)
	second.DeletedAt = &now
	second.Enabled = false
	second.Revision = 3
	_, err = s.ReplaceWithRevision(ctx, second, 2)
	require.NoError(t, err)
	rows, count, err = s.List(ctx, catalogue.ListQuery{IncludeHidden: true, IncludeDisabled: true, IncludeDeleted: true}, false)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.EqualValues(t, 1, count)
	count, err = s.Count(ctx)
	require.NoError(t, err)
	require.Zero(t, count)
	_, err = s.GetByKey(ctx, "missing")
	require.ErrorIs(t, err, catalogue.ErrNotFound)
	ctx, cancel := context.WithCancel(ctx)
	cancel()
	_, err = s.GetByKey(ctx, "GB")
	require.ErrorIs(t, err, context.Canceled)
	_, _, err = s.InsertIfAbsent(ctx, input)
	require.ErrorIs(t, err, context.Canceled)
}

// Both adapters run the same ordered CAS/reseed contract with separate stores.
func TestStoreContract(t *testing.T) {
	for _, adapter := range []string{"memory", "mongo"} {
		t.Run(adapter, func(t *testing.T) {
			if adapter == "memory" {
				contract(t, NewMemoryStore(nil))
				return
			}
			uri := os.Getenv("GHATD_TEST_MONGO_URI")
			if uri == "" {
				t.Skip("set GHATD_TEST_MONGO_URI")
			}
			client, err := mongo.Connect(options.Client().ApplyURI(uri))
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, client.Disconnect(context.Background())) })
			db := client.Database("cataloguestore_" + uuid.NewString())
			t.Cleanup(func() { require.NoError(t, db.Drop(context.Background())) })
			require.NoError(t, EnsureIndexes(context.Background(), db, "entries"))
			require.NoError(t, EnsureIndexes(context.Background(), db, "entries"))
			store, err := OpenMongo(db, "entries")
			require.NoError(t, err)
			contract(t, store)
		})
	}
}

// Embed the port so unexpected operations panic instead of fabricating success.
type nativeFixture struct {
	MongoDbStore
	findErr, writeErr error
	outcome           *mongo.UpdateResult
	insert            *mongo.InsertOneResult
	writes, reads     int
}

func (f *nativeFixture) ExecuteFindOneCommandDecodeResult(_ context.Context, _ *mongo.Collection, _ interface{}, _ interface{}, _ string, _ bool, _ error) error {
	f.reads++
	return f.findErr
}
func (f *nativeFixture) ExecuteReplaceOneCommandResult(_ context.Context, _ *mongo.Collection, _, _ any, _ ...options.Lister[options.ReplaceOptions]) (*mongo.UpdateResult, error) {
	f.writes++
	return f.outcome, f.writeErr
}
func (f *nativeFixture) ExecuteInsertOneCommand(_ context.Context, _ *mongo.Collection, _ interface{}, _ string) (*mongo.InsertOneResult, error) {
	f.writes++
	return f.insert, f.writeErr
}
func TestNativeErrorsAndUncertainWritesAreNotRetried(t *testing.T) {
	native := errors.New("native connection reset")
	for _, test := range []struct {
		name                    string
		result                  *mongo.UpdateResult
		writeErr, readErr, want error
		reads                   int
	}{
		{"native", nil, native, nil, native, 0},
		{"no receipt", nil, nil, nil, catalogue.ErrUnavailable, 0},
		{"unacknowledged", &mongo.UpdateResult{}, nil, nil, catalogue.ErrUnavailable, 0},
		{"known stale", &mongo.UpdateResult{Acknowledged: true}, nil, nil, catalogue.ErrStaleWrite, 1},
		{"known absent", &mongo.UpdateResult{Acknowledged: true}, nil, mongo.ErrNoDocuments, catalogue.ErrNotFound, 1},
		{"read failure after no match", &mongo.UpdateResult{Acknowledged: true}, nil, native, native, 1},
		{"no modification", &mongo.UpdateResult{Acknowledged: true, MatchedCount: 1}, nil, nil, catalogue.ErrUnavailable, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := &nativeFixture{outcome: test.result, writeErr: test.writeErr, findErr: test.readErr}
			s, err := NewMongoStore(f, &mongo.Collection{})
			require.NoError(t, err)
			_, err = s.ReplaceWithRevision(context.Background(), Document{ID: "GB", Key: "GB", Audit: catalogue.Audit{Revision: 2}}, 1)
			require.ErrorIs(t, err, test.want)
			require.Equal(t, 1, f.writes)
			require.Equal(t, test.reads, f.reads)
		})
	}
	for _, tc := range []struct {
		name          string
		failure, want error
	}{{"native_read_failure", native, native}, {"known_absence", mongo.ErrNoDocuments, catalogue.ErrNotFound}} {
		t.Run(tc.name, func(t *testing.T) {
			f := &nativeFixture{findErr: tc.failure}
			s, err := NewMongoStore(f, &mongo.Collection{})
			require.NoError(t, err)
			_, err = s.GetByKey(t.Context(), "GB")
			require.ErrorIs(t, err, tc.want)
		})
	}
	for _, tc := range []struct {
		name          string
		receipt       *mongo.InsertOneResult
		failure, want error
	}{
		{"missing_insert_receipt", nil, nil, catalogue.ErrUnavailable},
		{"unacknowledged_insert", &mongo.InsertOneResult{}, nil, catalogue.ErrUnavailable},
		{"native_insert_failure", nil, native, native},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &nativeFixture{findErr: mongo.ErrNoDocuments, insert: tc.receipt, writeErr: tc.failure}
			s, err := NewMongoStore(f, &mongo.Collection{})
			require.NoError(t, err)
			_, _, err = s.InsertIfAbsent(t.Context(), Document{ID: "GB", Key: "GB"})
			require.ErrorIs(t, err, tc.want)
			require.Equal(t, 1, f.writes)
		})
	}
}
