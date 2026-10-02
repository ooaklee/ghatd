package repository

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// isolatedRepository only drops its own randomly named database, never a
// database from the supplied URI. Live tests require an isolated replica set.
func isolatedRepository(t *testing.T) (*MongoDbRepository, *mongo.Database, context.Context, *findOneRecordingLogger) {
	t.Helper()
	uri := os.Getenv("GHATD_TEST_MONGO_URI")
	if uri == "" {
		t.Skip("set GHATD_TEST_MONGO_URI to an isolated replica set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	client, err := mongo.Connect(options.Client().ApplyURI(uri))
	require.NoError(t, err)
	db := client.Database("ghatd_repository_test_" + strings.ReplaceAll(uuid.NewString(), "-", ""))
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		require.NoError(t, db.Drop(ctx))
		require.NoError(t, client.Disconnect(ctx))
	})
	log := &findOneRecordingLogger{}
	repo, err := NewMongoDbRepositoryFromDatabase(db, log)
	require.NoError(t, err)
	require.NoError(t, repo.EnsureMongoCollection(ctx, db, "records"))
	return repo, db, ctx, log
}

func TestMongoTransactionAtomicity(t *testing.T) {
	for _, tc := range []struct {
		name  string
		mode  string
		count int64
		calls int
		want  error
	}{
		{"commit", "commit", 1, 1, nil},
		{"rollback", "rollback", 0, 1, ErrInvalidMongoOperation},
		{"raw transient retry", "retry", 1, 2, nil},
		{"wrapped transient retry", "wrapped", 1, 2, nil},
		{"manual abort rejected", "abort", 0, 1, ErrInvalidMongoOperation},
		{"nested session rejected", "nested", 0, 1, ErrInvalidMongoOperation},
		{"cancel rollback", "cancel", 0, 1, context.Canceled},
		{"panic cleanup", "panic", 0, 1, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo, db, ctx, log := isolatedRepository(t)
			require.NoError(t, repo.ProbeMongoTransactions(ctx, db.Collection("records")))
			operation, cancel := context.WithCancel(ctx)
			defer cancel()
			calls := 0
			run := func() error {
				return repo.WithMongoTransaction(operation, db, func(tx context.Context) error {
					calls++
					_, err := repo.ExecuteInsertOneCommand(tx, db.Collection("records"), bson.M{"_id": "private-id", "secret": "private-value"}, "private-name")
					if err != nil {
						return err
					}
					switch tc.mode {
					case "rollback":
						return ErrInvalidMongoOperation
					case "retry", "wrapped":
						if calls == 1 {
							failure := mongo.CommandError{Code: 112, Message: "private-message", Labels: []string{"TransientTransactionError"}}
							if tc.mode == "wrapped" {
								return NewRepositoryErrorWithCause(ErrUnableToGenerateCollectionCursor, "retry-test", fmt.Errorf("wrapped: %w", failure))
							}
							return failure
						}
					case "abort":
						return mongo.SessionFromContext(tx).AbortTransaction(tx)
					case "nested":
						return repo.WithMongoTransaction(tx, db, func(context.Context) error { return nil })
					case "cancel":
						cancel()
					case "panic":
						panic("test panic")
					}
					return nil
				})
			}
			if tc.mode == "panic" {
				require.Panics(t, func() { _ = run() })
			} else {
				require.ErrorIs(t, run(), tc.want)
			}
			require.Equal(t, tc.calls, calls)
			count, err := repo.ExecuteCountDocuments(ctx, db.Collection("records"), bson.M{})
			require.NoError(t, err)
			require.Equal(t, tc.count, count)
			for _, entry := range log.entries {
				require.Nil(t, entry.err)
				require.Len(t, entry.fields, 2)
				require.Equal(t, "operation", entry.fields[0].Key)
				require.Equal(t, "outcome", entry.fields[1].Key)
				require.NotContains(t, fmt.Sprint(entry), "private")
			}
		})
	}
}

func TestMongoResultCountsAndErrors(t *testing.T) {
	for _, tc := range []struct {
		name   string
		exists bool
		upsert bool
	}{
		{"existing", true, false}, {"missing", false, false}, {"explicit upsert", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo, db, ctx, _ := isolatedRepository(t)
			collection := db.Collection("records")
			if tc.exists {
				_, err := repo.ExecuteInsertOneCommand(ctx, collection, bson.M{"_id": "one", "revision": 1}, "")
				require.NoError(t, err)
			}
			result, err := repo.ExecuteReplaceOneCommandResult(ctx, collection, bson.M{"_id": "one", "revision": 1}, bson.M{"_id": "one", "revision": 2}, options.Replace().SetUpsert(tc.upsert))
			require.NoError(t, err)
			if tc.exists {
				require.EqualValues(t, 1, result.MatchedCount)
			} else {
				require.Zero(t, result.MatchedCount)
			}
			if tc.upsert {
				require.EqualValues(t, 1, result.UpsertedCount)
			}
			result, err = repo.ExecuteUpdateOneCommandResult(ctx, collection, bson.M{"_id": "one", "revision": 2}, bson.M{"$set": bson.M{"revision": 3}})
			require.NoError(t, err)
			expected := int64(0)
			if tc.exists || tc.upsert {
				expected = 1
			}
			require.Equal(t, expected, result.MatchedCount)
			deleted, err := repo.ExecuteDeleteOneCommandResult(ctx, collection, bson.M{"_id": "one", "revision": 2})
			require.NoError(t, err)
			require.Zero(t, deleted.DeletedCount)
			deleted, err = repo.ExecuteDeleteOneCommandResult(ctx, collection, bson.M{"_id": "one", "revision": 3})
			require.NoError(t, err)
			require.Equal(t, expected, deleted.DeletedCount)
		})
	}
}

func TestMongoSafeHelperContracts(t *testing.T) {
	for _, tc := range []string{"setup idempotent", "duplicate privacy", "invalid find preserves cause", "cancel count preserves cause", "borrowed lifecycle", "cursor decode failure"} {
		t.Run(tc, func(t *testing.T) {
			repo, db, ctx, log := isolatedRepository(t)
			collection := db.Collection("records")
			switch tc {
			case "setup idempotent":
				for range 2 {
					require.NoError(t, repo.EnsureMongoCollection(ctx, db, "records"))
					require.NoError(t, repo.EnsureMongoIndexes(ctx, collection, []mongo.IndexModel{{Keys: bson.D{{Key: "lookup", Value: 1}}, Options: options.Index().SetName("lookup_unique").SetUnique(true)}}))
					require.NoError(t, repo.ProbeMongoTransactions(ctx, collection))
				}
				count, err := collection.CountDocuments(ctx, bson.M{})
				require.NoError(t, err)
				require.Zero(t, count)
			case "duplicate privacy":
				for i := range 2 {
					_, err := repo.ExecuteInsertOneCommand(ctx, collection, bson.M{"_id": "private-key", "email": "private-email"}, "private-object")
					if i == 0 {
						require.NoError(t, err)
					} else {
						require.True(t, mongo.IsDuplicateKeyError(err))
					}
				}
			case "invalid find preserves cause":
				_, err := repo.ExecuteFindCommand(ctx, collection, bson.M{"$bad-private-operator": 1})
				require.ErrorIs(t, err, ErrUnableToGenerateCollectionCursor)
				var driver mongo.CommandError
				require.ErrorAs(t, err, &driver)
			case "cancel count preserves cause":
				cancelled, cancel := context.WithCancel(ctx)
				cancel()
				_, err := repo.ExecuteCountDocuments(cancelled, collection, bson.M{})
				require.ErrorIs(t, err, context.Canceled)
				require.ErrorIs(t, err, ErrUnableToCountDocuments)
			case "borrowed lifecycle":
				borrowed := repo.GetHelper().(*MongoRepositoryHelper).mongoClient
				require.NoError(t, borrowed.Close(ctx))
				require.NoError(t, borrowed.Ping(ctx))
				require.ErrorIs(t, borrowed.Reconnect(ctx), ErrInvalidMongoOperation)
				_, err := repo.GetDatabase(ctx, "other")
				require.ErrorIs(t, err, ErrInvalidMongoOperation)
			case "cursor decode failure":
				cursor, err := mongo.NewCursorFromDocuments([]any{bson.M{"value": "private-not-an-int"}}, nil, nil)
				require.NoError(t, err)
				var out []struct{ Value int }
				err = repo.MapAllInCursorToResult(ctx, cursor, &out, "private-result")
				require.ErrorIs(t, err, ErrUnableToDecodeQueriedDocuments)
				require.NotNil(t, errors.Unwrap(err))
			}
			for _, entry := range log.entries {
				require.Nil(t, entry.err)
				require.NotContains(t, fmt.Sprint(entry), "private")
			}
		})
	}
}

// The legacy interface remains source-compatible: no new methods were added.
var _ CommonOperations = (*MongoDbRepository)(nil)

func TestMongoBoundaryValidation(t *testing.T) {
	for _, tc := range []string{"nil database", "nil transaction context", "nil callback", "nil collection", "nil borrowed context", "unmanaged getter"} {
		t.Run(tc, func(t *testing.T) {
			client, err := mongo.Connect(options.Client().ApplyURI("mongodb://127.0.0.1:1"))
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, client.Disconnect(context.Background())) })
			db := client.Database("boundary_fixture")
			repo, err := NewMongoDbRepositoryFromDatabase(db, NewNoOpRepositoryLogger())
			require.NoError(t, err)
			switch tc {
			case "nil database":
				_, err = NewMongoDbRepositoryFromDatabase(nil, nil)
			case "nil transaction context":
				err = repo.WithMongoTransaction(nil, db, func(context.Context) error { return nil })
			case "nil callback":
				err = repo.WithMongoTransaction(context.Background(), db, nil)
			case "nil collection":
				_, err = repo.ExecuteDeleteOneCommandResult(context.Background(), nil, bson.M{})
			case "nil borrowed context":
				_, err = repo.GetDatabase(nil, "")
				require.Equal(t, false, repo.Health(nil)["healthy"])
			case "unmanaged getter":
				_, err = NewMongoDbRepository(nil, nil, "").GetDatabase(context.Background(), "")
			}
			require.ErrorIs(t, err, ErrInvalidMongoOperation)
		})
	}
}
