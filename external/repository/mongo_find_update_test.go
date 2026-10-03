package repository

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// atomicUpdateRecord keeps the fixture revision separate from private payloads
// whose accidental appearance in repository telemetry must fail the tests.
type atomicUpdateRecord struct {
	ID       string `bson:"_id"`
	Revision int64  `bson:"revision"`
	Secret   string `bson:"secret"`
}

func TestFindOneAndUpdateRejectsInvalidEntry(t *testing.T) {
	for _, tc := range []struct {
		name string
		want error
	}{
		{"nil repository", ErrInvalidMongoOperation},
		{"nil helper", ErrInvalidMongoOperation},
		{"typed nil helper", ErrInvalidMongoOperation},
		{"nil collection", ErrInvalidMongoOperation},
		{"nil context", ErrInvalidMongoOperation},
		{"nil destination", ErrInvalidMongoOperation},
		{"typed nil destination", ErrInvalidMongoOperation},
		{"value destination", ErrInvalidMongoOperation},
		{"map destination", ErrInvalidMongoOperation},
		{"cancelled context", context.Canceled},
		{"expired context", context.DeadlineExceeded},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// An unreachable server makes unintended driver work observable: it
			// cannot return the expected entry error without this validation.
			client, err := mongo.Connect(options.Client().ApplyURI("mongodb://127.0.0.1:1"))
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, client.Disconnect(context.Background())) })
			ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
			defer cancel()
			log := &findOneRecordingLogger{}
			repo := NewMongoDbRepository(nil, log, "")
			collection := client.Database("entry_fixture").Collection("records")
			var result any = &atomicUpdateRecord{}
			switch tc.name {
			case "nil repository":
				repo = nil
			case "nil helper":
				repo.helper = nil
			case "typed nil helper":
				repo.helper = (*MongoRepositoryHelper)(nil)
			case "nil collection":
				collection = nil
			case "nil context":
				ctx = nil
			case "nil destination":
				result = nil
			case "typed nil destination":
				result = (*atomicUpdateRecord)(nil)
			case "value destination":
				result = atomicUpdateRecord{}
			case "map destination":
				result = bson.M{}
			case "cancelled context":
				cancel()
			case "expired context":
				var stop context.CancelFunc
				ctx, stop = context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
				defer stop()
			}
			err = repo.ExecuteFindOneAndUpdateCommandDecodeResult(ctx, collection, bson.M{}, bson.M{"$inc": bson.M{"revision": 1}}, result)
			require.ErrorIs(t, err, tc.want)
			require.Empty(t, log.entries, "invalid calls must not reach a logger")
		})
	}
}

func TestFindOneAndUpdateImagesAndErrors(t *testing.T) {
	for _, tc := range []struct {
		name          string
		imageRevision int64
		storedCount   int64
		storedVersion int64
		outcome       string
	}{
		{"default before image", 1, 1, 2, "success"},
		{"after image", 2, 1, 2, "success"},
		{"pipeline update", 3, 1, 3, "success"},
		{"projected image", 2, 1, 2, "success"},
		{"no-op update", 1, 1, 1, "success"},
		{"missing match", 0, 0, 0, "not_found"},
		{"stale revision", 0, 1, 1, "not_found"},
		{"after image upsert", 1, 1, 1, "success"},
		{"before image upsert", 0, 1, 1, "not_found"},
		{"duplicate key", 0, 2, 1, "duplicate_key"},
		{"invalid update", 0, 1, 1, "database"},
		{"decode failure after write", 0, 1, 2, "database"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo, db, ctx, log := isolatedRepository(t)
			collection := db.Collection("records")
			if tc.name != "missing match" && tc.name != "after image upsert" && tc.name != "before image upsert" {
				_, err := collection.InsertOne(ctx, atomicUpdateRecord{ID: "private-id", Revision: 1, Secret: "private-payload"})
				require.NoError(t, err)
			}
			filter := bson.M{"_id": "private-id"}
			var update any = bson.M{"$inc": bson.M{"revision": 1}}
			var decoded atomicUpdateRecord
			var destination any = &decoded
			var opts []options.Lister[options.FindOneAndUpdateOptions]
			switch tc.name {
			case "after image":
				opts = append(opts, options.FindOneAndUpdate().SetReturnDocument(options.After))
			case "pipeline update":
				update = mongo.Pipeline{{{Key: "$set", Value: bson.M{"revision": bson.M{"$add": bson.A{"$revision", 2}}}}}}
				opts = append(opts, options.FindOneAndUpdate().SetReturnDocument(options.After))
			case "projected image":
				opts = append(opts, options.FindOneAndUpdate().SetReturnDocument(options.After).SetProjection(bson.M{"_id": 0, "revision": 1}))
			case "no-op update":
				update = bson.M{"$set": bson.M{"revision": 1}}
			case "stale revision":
				filter["revision"] = 0
			case "after image upsert":
				opts = append(opts, options.FindOneAndUpdate().SetUpsert(true).SetReturnDocument(options.After))
			case "before image upsert":
				opts = append(opts, options.FindOneAndUpdate().SetUpsert(true))
			case "duplicate key":
				require.NoError(t, repo.EnsureMongoIndexes(ctx, collection, []mongo.IndexModel{{Keys: bson.D{{Key: "secret", Value: 1}}, Options: options.Index().SetUnique(true)}}))
				_, err := collection.InsertOne(ctx, atomicUpdateRecord{ID: "private-other", Revision: 1, Secret: "private-taken"})
				require.NoError(t, err)
				update = bson.M{"$set": bson.M{"secret": "private-taken"}}
			case "invalid update":
				update = bson.M{"$private-invalid-operator": 1}
			case "decode failure after write":
				// The destination is a valid pointer but has an incompatible schema.
				destination = &struct{ Secret int }{}
				opts = append(opts, options.FindOneAndUpdate().SetReturnDocument(options.After))
			}
			log.entries = nil
			err := repo.ExecuteFindOneAndUpdateCommandDecodeResult(ctx, collection, filter, update, destination, opts...)
			switch tc.outcome {
			case "success":
				require.NoError(t, err)
			case "not_found":
				require.ErrorIs(t, err, mongo.ErrNoDocuments)
			case "duplicate_key":
				require.True(t, mongo.IsDuplicateKeyError(err), "native duplicate identity lost: %v", err)
				var command mongo.CommandError
				require.ErrorAs(t, err, &command)
			case "database":
				require.Error(t, err)
				require.NotErrorIs(t, err, mongo.ErrNoDocuments)
			}
			require.Equal(t, tc.imageRevision, decoded.Revision)
			if tc.name == "projected image" {
				require.Empty(t, decoded.ID)
				require.Empty(t, decoded.Secret)
			}
			require.Len(t, log.entries, 1)
			entry := log.entries[0]
			require.Nil(t, entry.err, "raw driver/decode errors must not reach custom loggers")
			require.Equal(t, []Field{{Key: "operation", Value: "find_one_and_update_decode"}, {Key: "outcome", Value: tc.outcome}}, entry.fields)
			require.NotContains(t, fmt.Sprint(entry), "private")
			count, err := collection.CountDocuments(ctx, bson.M{})
			require.NoError(t, err)
			require.Equal(t, tc.storedCount, count)
			if tc.storedCount > 0 {
				var stored atomicUpdateRecord
				require.NoError(t, collection.FindOne(ctx, bson.M{"_id": "private-id"}).Decode(&stored))
				require.Equal(t, tc.storedVersion, stored.Revision)
			}
		})
	}
}

func TestFindOneAndUpdateUsesTransactionContext(t *testing.T) {
	rollback := errors.New("test/rollback")
	for _, tc := range []struct {
		name          string
		calls         int
		storedVersion int64
		want          error
	}{
		{"commit", 1, 2, nil},
		{"rollback", 1, 1, rollback},
		{"retry transaction", 2, 2, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo, db, ctx, _ := isolatedRepository(t)
			collection := db.Collection("records")
			_, err := collection.InsertOne(ctx, atomicUpdateRecord{ID: "record", Revision: 1})
			require.NoError(t, err)
			calls := 0
			err = repo.WithMongoTransaction(ctx, db, func(tx context.Context) error {
				calls++
				var image atomicUpdateRecord
				if err := repo.ExecuteFindOneAndUpdateCommandDecodeResult(tx, collection, bson.M{"_id": "record", "revision": 1}, bson.M{"$inc": bson.M{"revision": 1}}, &image, options.FindOneAndUpdate().SetReturnDocument(options.After)); err != nil {
					return err
				}
				if image.Revision != 2 {
					return fmt.Errorf("expected post-image revision 2, got %d", image.Revision)
				}
				if tc.name == "rollback" {
					return rollback
				}
				if tc.name == "retry transaction" && calls == 1 {
					return mongo.CommandError{Code: 112, Message: "private-transient", Labels: []string{"TransientTransactionError"}}
				}
				return nil
			})
			require.ErrorIs(t, err, tc.want)
			require.Equal(t, tc.calls, calls)
			var stored atomicUpdateRecord
			require.NoError(t, collection.FindOne(ctx, bson.M{"_id": "record"}).Decode(&stored))
			require.Equal(t, tc.storedVersion, stored.Revision)
		})
	}
}

func TestFindOneAndUpdateCollectionCodec(t *testing.T) {
	for _, tc := range []struct {
		name       string
		custom     bool
		wantSecret string
	}{
		{"default codec", false, "private-payload"},
		{"collection codec", true, "PRIVATE-PAYLOAD"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo, db, ctx, _ := isolatedRepository(t)
			// A case-local named type avoids changing every string's decoder.
			type secretText string
			registry := bson.NewRegistry()
			calls := 0
			if tc.custom {
				registry.RegisterTypeDecoder(reflect.TypeFor[secretText](), bson.ValueDecoderFunc(func(_ bson.DecodeContext, reader bson.ValueReader, destination reflect.Value) error {
					calls++
					value, err := reader.ReadString()
					if err == nil {
						destination.SetString(strings.ToUpper(value))
					}
					return err
				}))
			}
			collection := db.Collection("records", options.Collection().SetRegistry(registry))
			_, err := collection.InsertOne(ctx, atomicUpdateRecord{ID: "record", Revision: 1, Secret: "private-payload"})
			require.NoError(t, err)
			var image struct {
				Secret   secretText
				Revision int64
			}
			err = repo.ExecuteFindOneAndUpdateCommandDecodeResult(ctx, collection, bson.M{"_id": "record"}, bson.M{"$inc": bson.M{"revision": 1}}, &image, options.FindOneAndUpdate().SetReturnDocument(options.After))
			require.NoError(t, err)
			require.Equal(t, tc.wantSecret, string(image.Secret))
			require.EqualValues(t, 2, image.Revision)
			require.Equal(t, tc.custom, calls == 1)
		})
	}
}

func TestFindOneAndUpdateConcurrentCAS(t *testing.T) {
	for _, writers := range []int{2, 8} {
		t.Run(fmt.Sprintf("%d writers", writers), func(t *testing.T) {
			_, db, ctx, _ := isolatedRepository(t)
			collection := db.Collection("records")
			_, err := collection.InsertOne(ctx, atomicUpdateRecord{ID: "record", Revision: 1})
			require.NoError(t, err)
			// Each worker owns its logger. The shared client/database are safe
			// for concurrent use, but sessions and mutable fixtures are not shared.
			type outcome struct {
				image atomicUpdateRecord
				err   error
			}
			results := make([]outcome, writers)
			start := make(chan struct{})
			var group sync.WaitGroup
			for i := range writers {
				group.Add(1)
				go func() {
					defer group.Done()
					<-start
					repo, err := NewMongoDbRepositoryFromDatabase(db, &findOneRecordingLogger{})
					if err != nil {
						results[i].err = err
						return
					}
					results[i].err = repo.ExecuteFindOneAndUpdateCommandDecodeResult(ctx, collection, bson.M{"_id": "record", "revision": 1}, bson.M{"$inc": bson.M{"revision": 1}}, &results[i].image, options.FindOneAndUpdate().SetReturnDocument(options.After))
				}()
			}
			close(start)
			group.Wait()
			winners := 0
			for _, result := range results {
				if result.err == nil {
					winners++
					require.EqualValues(t, 2, result.image.Revision)
				} else {
					require.ErrorIs(t, result.err, mongo.ErrNoDocuments)
				}
			}
			require.Equal(t, 1, winners)
			var stored atomicUpdateRecord
			require.NoError(t, collection.FindOne(ctx, bson.M{"_id": "record"}).Decode(&stored))
			require.EqualValues(t, 2, stored.Revision)
		})
	}
}
