package repository

import (
	"context"
	"errors"
	"reflect"

	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// ErrInvalidMongoOperation indicates a missing repository, collection, context
// or callback. It is not a database outage and must not trigger a fallback store.
var ErrInvalidMongoOperation = errors.New("repository/invalid-mongo-operation")

// observeMongo logs only fixed operation and error-class metadata. Driver error
// messages (including duplicate-key values), queries and documents are private.
// The original error is returned by the caller, never reconstructed from text.
func observeMongo(ctx context.Context, log RepositoryLogger, operation string, err error) {
	if nilRepositoryLogger(log) || ctx == nil {
		return
	}
	class := "database"
	switch {
	case err == nil:
		class = "success"
	case errors.Is(err, mongo.ErrNoDocuments):
		class = "not_found"
	case errors.Is(err, context.Canceled):
		class = "cancelled"
	case errors.Is(err, context.DeadlineExceeded):
		class = "deadline"
	case mongo.IsDuplicateKeyError(err):
		class = "duplicate_key"
	case errors.Is(err, ErrInvalidMongoOperation):
		class = "invalid_operation"
	}
	fields := []Field{{Key: "operation", Value: operation}, {Key: "outcome", Value: class}}
	if err == nil || errors.Is(err, mongo.ErrNoDocuments) {
		log.Debug(ctx, "mongo-operation", nil, fields...)
	} else {
		log.Error(ctx, "mongo-operation-failed", nil, fields...)
	}
}

// mongoCommand runs one driver operation, preserving its result and error identity.
// Callers must pass the transaction context; no session or client is substituted.
// Nil helper validation happens before any driver or log call, including helpers
// held in an interface. Custom non-nil helpers retain their logging contract.
func mongoCommand[T any](ctx context.Context, log RepositoryLogger, operation string, collection *mongo.Collection, run func() (T, error)) (T, error) {
	var zero T
	if nilRepositoryLogger(log) || ctx == nil || collection == nil || run == nil {
		return zero, ErrInvalidMongoOperation
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	result, err := run()
	observeMongo(ctx, log, operation, err)
	return result, err
}

// nilRepositoryLogger detects typed-nil adapters without invoking their methods.
// A non-nil adapter still owns the safety of its implementation and dependencies.
func nilRepositoryLogger(log RepositoryLogger) bool {
	if log == nil {
		return true
	}
	switch value := reflect.ValueOf(log); value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

// ExecuteReplaceOneCommandResult retains MatchedCount for domain-owned revision
// checks. A zero match is not a driver error; callers decide its business meaning.
// Options are passed unchanged, including explicit upsert policy.
func (r *MongoDbRepository) ExecuteReplaceOneCommandResult(ctx context.Context, collection *mongo.Collection, filter, replacement any, opts ...options.Lister[options.ReplaceOptions]) (*mongo.UpdateResult, error) {
	if r == nil || r.helper == nil {
		return nil, ErrInvalidMongoOperation
	}
	return mongoCommand(ctx, r.helper, "replace_one", collection, func() (*mongo.UpdateResult, error) {
		return collection.ReplaceOne(ctx, filter, replacement, opts...)
	})
}

// ExecuteUpdateOneCommandResult exposes matched/modified counts without turning
// a no-op update into success or failure on behalf of the owning domain.
func (r *MongoDbRepository) ExecuteUpdateOneCommandResult(ctx context.Context, collection *mongo.Collection, filter, update any, opts ...options.Lister[options.UpdateOneOptions]) (*mongo.UpdateResult, error) {
	if r == nil || r.helper == nil {
		return nil, ErrInvalidMongoOperation
	}
	return mongoCommand(ctx, r.helper, "update_one", collection, func() (*mongo.UpdateResult, error) {
		return collection.UpdateOne(ctx, filter, update, opts...)
	})
}

// ExecuteDeleteOneCommandResult retains DeletedCount so stale revisions or
// missing resources cannot be confused with successful deletion.
func (r *MongoDbRepository) ExecuteDeleteOneCommandResult(ctx context.Context, collection *mongo.Collection, filter any, opts ...options.Lister[options.DeleteOneOptions]) (*mongo.DeleteResult, error) {
	if r == nil || r.helper == nil {
		return nil, ErrInvalidMongoOperation
	}
	return mongoCommand(ctx, r.helper, "delete_one", collection, func() (*mongo.DeleteResult, error) {
		return collection.DeleteOne(ctx, filter, opts...)
	})
}

// EnsureMongoIndexes creates only the caller-declared indexes. It never drops,
// silently replaces or repairs conflicting indexes. Call at explicit startup,
// outside transactions; names, keys and retention policy belong to the domain.
func (r *MongoDbRepository) EnsureMongoIndexes(ctx context.Context, collection *mongo.Collection, models []mongo.IndexModel) error {
	if r == nil || r.helper == nil || ctx == nil || mongo.SessionFromContext(ctx) != nil || len(models) == 0 {
		return ErrInvalidMongoOperation
	}
	_, err := mongoCommand(ctx, r.helper, "create_indexes", collection, func() ([]string, error) {
		return collection.Indexes().CreateMany(ctx, models)
	})
	return err
}

// EnsureMongoCollection creates one caller-selected collection, accepting only
// NamespaceExists as an idempotent result. It never changes existing options.
func (r *MongoDbRepository) EnsureMongoCollection(ctx context.Context, db *mongo.Database, name string) error {
	if r == nil || r.helper == nil || ctx == nil || db == nil || name == "" || mongo.SessionFromContext(ctx) != nil {
		return ErrInvalidMongoOperation
	}
	err := db.CreateCollection(ctx, name)
	var command mongo.CommandError
	if errors.As(err, &command) && command.Code == 48 {
		err = nil
	}
	observeMongo(ctx, r.helper, "create_collection", err)
	return err
}
