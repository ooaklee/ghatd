package repository

import (
	"context"
	"errors"
	"reflect"

	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// ErrInvalidMongoOperation indicates a missing repository, collection, context,
// callback or required decode destination. It is not a database outage and must
// not trigger a fallback store.
var ErrInvalidMongoOperation = errors.New("repository/invalid-mongo-operation")

// ErrUnacknowledgedMongoWrite means the driver did not provide an authoritative
// mutation receipt. The write may have committed; this is neither a confirmed
// missing match nor evidence that retrying is safe.
var ErrUnacknowledgedMongoWrite = errors.New("repository/unacknowledged-mongo-write")

// observeMongo logs only fixed operation and error-class metadata. Driver error
// messages (including duplicate-key values), queries and documents are private.
// The original error is returned by the caller, never reconstructed from text.
func observeMongo(ctx context.Context, log RepositoryLogger, operation string, err error) {
	if nilRepositoryLogger(log) || ctx == nil {
		return
	}
	noDocument := errors.Is(err, mongo.ErrNoDocuments)
	if operation == "find_one_and_update_decode" {
		// For this result-bearing mutation, a codec error that wraps the
		// sentinel is still a database failure, not an absent result image.
		noDocument = err == mongo.ErrNoDocuments
	}
	class := "database"
	switch {
	case err == nil:
		class = "success"
	case noDocument && operation == "find_one_and_update_decode":
		// A before-image upsert can commit without returning an image.
		// This classification is about the result, not mutation success.
		class = "no_document_image"
	case noDocument:
		class = "not_found"
	case errors.Is(err, context.Canceled):
		class = "cancelled"
	case errors.Is(err, context.DeadlineExceeded):
		class = "deadline"
	case mongo.IsDuplicateKeyError(err):
		class = "duplicate_key"
	case errors.Is(err, ErrInvalidMongoOperation):
		class = "invalid_operation"
	case errors.Is(err, ErrUnacknowledgedMongoWrite):
		class = "unacknowledged"
	}
	fields := []Field{{Key: "operation", Value: operation}, {Key: "outcome", Value: class}}
	if err == nil || noDocument {
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

// ExecuteFindOneAndUpdateCommandDecodeResult atomically updates one matching
// document and decodes the selected image into result, a non-nil pointer. It
// uses the caller's context, session and collection codec registry unchanged.
// Options retain driver semantics: the default returns the pre-update image;
// use SetReturnDocument(options.After) to obtain the exact post-update image.
// Upsert is disabled unless explicitly requested by the caller.
//
// Errors retain native identity and labels, including mongo.ErrNoDocuments for
// a missing match. A before-image upsert also returns mongo.ErrNoDocuments even
// though it inserts a document. Decode, network and write-concern errors do not
// prove the write was rolled back; discard any partially decoded result and
// reconcile uncertain outcomes instead of blindly retrying. Within a managed
// transaction, return the error so its owner can abort or retry appropriately.
// Unacknowledged writes return ErrUnacknowledgedMongoWrite rather than decoding
// an unreliable image or reporting ErrNoDocuments as an authoritative no-match.
// Other native driver failures retain precedence and their original identity.
//
// Nil and non-pointer destinations are rejected before writing, but this does
// not prevalidate their BSON schema or custom decoder. Filters, updates, result
// bytes and raw errors are excluded from automatic operation telemetry. Resource
// authorization, revision filters and error-to-domain mapping belong to callers.
func (r *MongoDbRepository) ExecuteFindOneAndUpdateCommandDecodeResult(ctx context.Context, collection *mongo.Collection, filter, update, result any, opts ...options.Lister[options.FindOneAndUpdateOptions]) error {
	if r == nil || r.helper == nil || result == nil {
		return ErrInvalidMongoOperation
	}
	destination := reflect.ValueOf(result)
	if destination.Kind() != reflect.Pointer || destination.IsNil() {
		return ErrInvalidMongoOperation
	}
	_, err := mongoCommand(ctx, r.helper, "find_one_and_update_decode", collection, func() (struct{}, error) {
		receipt := collection.FindOneAndUpdate(ctx, filter, update, opts...)
		// The pinned driver returns the bare sentinel for an absent image.
		// Do not use errors.Is: a caller's BSON codec can wrap that sentinel
		// in a genuine MarshalError, which must retain native precedence.
		if err := receipt.Err(); err != nil && err != mongo.ErrNoDocuments {
			return struct{}{}, err
		}
		if !receipt.Acknowledged {
			return struct{}{}, ErrUnacknowledgedMongoWrite
		}
		return struct{}{}, receipt.Decode(result)
	})
	return err
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
