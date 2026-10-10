package repository

import (
	"context"
	"errors"

	repositoryhelpers "github.com/ooaklee/ghatd/external/repository/helpers"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// RepositoryLogger defines interface for repository-level logging
type RepositoryLogger interface {
	// Error emits a repository-level error log for message with optional err and
	// structured fields. The Zap implementation appends the error as a zap field;
	// the helper delegates to LogError. Part of the RepositoryLogger contract.
	Error(ctx context.Context, message string, err error, fields ...Field)
	// Warn emits a repository-level warning log for message with optional err and
	// structured fields, per the RepositoryLogger contract. Implementations include
	// Zap logging, helper delegation, and a no-op discard.
	Warn(ctx context.Context, message string, err error, fields ...Field)
	// Info emits a repository-level informational log for message with optional err
	// and structured fields, per the RepositoryLogger contract. Implementations
	// include Zap logging, helper delegation, and a no-op discard.
	Info(ctx context.Context, message string, err error, fields ...Field)
	// Debug emits a repository-level debug log for message with optional err and
	// structured fields, per the RepositoryLogger contract. Implementations include
	// Zap logging, helper delegation, and a no-op discard.
	Debug(ctx context.Context, message string, err error, fields ...Field)
}

// Field represents a key-value pair for structured logging
type Field struct {
	Key   string
	Value interface{}
}

// CursorMapper defines interface for cursor mapping operations
type CursorMapper interface {
	// MapAllToResult decodes all documents from the supplied cursor into result as
	// part of the CursorMapper contract. The helper implementation closes the
	// cursor and wraps decode failures in a repository error with the native cause
	// retained.
	MapAllToResult(ctx context.Context, cursor *mongo.Cursor, result interface{}, objectName string) error
	// MapOneToResult decodes the first document from the supplied cursor into
	// result, closing the cursor, per the CursorMapper contract. The helper maps
	// absence to a not-found repository error and wraps other failures with their
	// causes.
	MapOneToResult(ctx context.Context, cursor *mongo.Cursor, result interface{}, objectName string) error
}

// CommonOperations defines interface for common MongoDB operations
type CommonOperations interface {
	// ExecuteCountDocuments returns the number of documents in collection matching
	// filter, honouring optional driver count options. The helper wraps count
	// failures in a repository error preserving the cause; part of
	// CommonOperations.
	ExecuteCountDocuments(ctx context.Context, collection *mongo.Collection, filter interface{}, opts ...options.Lister[options.CountOptions]) (int64, error)
	// ExecuteDeleteManyCommand removes all documents in collection matching filter;
	// targetObjectName names the affected object for diagnostics. The helper
	// discards the delete count and returns any driver error via CommonOperations.
	ExecuteDeleteManyCommand(ctx context.Context, collection *mongo.Collection, filter interface{}, targetObjectName string) error
	// ExecuteUpdateManyCommand applies updateFilter to all documents in collection
	// matching filter; resultObjectName names the affected object. The helper
	// discards the update result and returns any driver error via CommonOperations.
	ExecuteUpdateManyCommand(ctx context.Context, collection *mongo.Collection, filter interface{}, updateFilter interface{}, resultObjectName string) error
	// ExecuteUpdateOneCommand applies updateFilter to a single document in
	// collection matching filter; resultObjectName names the affected object. The
	// helper discards matched/modified counts and returns any driver error via
	// CommonOperations.
	ExecuteUpdateOneCommand(ctx context.Context, collection *mongo.Collection, filter interface{}, updateFilter interface{}, resultObjectName string) error
	// ExecuteDeleteOneCommand deletes the first document matching the filter from
	// the given collection, preserving the error-only legacy signature; callers
	// needing the delete result use the result-bearing helper.
	ExecuteDeleteOneCommand(ctx context.Context, collection *mongo.Collection, filter interface{}, targetObjectName string) error
	// ExecuteFindOneCommandDecodeResult runs a findOne on collection with filter
	// and decodes the document into result. logError controls error logging and
	// onFailureErr substitutes for absence; other errors keep their identity per
	// CommonOperations.
	ExecuteFindOneCommandDecodeResult(ctx context.Context, collection *mongo.Collection, filter interface{}, result interface{}, resultObjectName string, logError bool, onFailureErr error) error
	// ExecuteReplaceOneCommand replaces a single document in collection matching
	// filter with replacementObject; resultObjectName names the affected object.
	// The helper discards the update result and returns any driver error via
	// CommonOperations.
	ExecuteReplaceOneCommand(ctx context.Context, collection *mongo.Collection, filter interface{}, replacementObject interface{}, resultObjectName string) error
	// ExecuteFindCommand returns a cursor over documents in collection matching
	// filter with optional find options, per CommonOperations. The helper wraps
	// cursor-creation failures in a repository error; the caller owns cursor
	// closure.
	ExecuteFindCommand(ctx context.Context, collection *mongo.Collection, filter interface{}, opts ...options.Lister[options.FindOptions]) (*mongo.Cursor, error)
	// ExecuteAggregateCommand returns a cursor over the aggregation of
	// mongoPipeline on collection, per CommonOperations. The helper wraps failures
	// in a repository error with the native cause and never logs pipeline data.
	ExecuteAggregateCommand(ctx context.Context, collection *mongo.Collection, mongoPipeline []bson.D) (*mongo.Cursor, error)
	// ExecuteInsertOneCommand inserts document into collection and returns the
	// driver's InsertOneResult with the inserted key; resultObjectName names the
	// object for diagnostics. The helper preserves duplicate-key and other driver
	// error identities.
	ExecuteInsertOneCommand(ctx context.Context, collection *mongo.Collection, document interface{}, resultObjectName string) (*mongo.InsertOneResult, error)
	// ExecuteInsertManyCommand inserts documents into collection and returns the
	// driver's InsertManyResult; resultObjectName names the objects for
	// diagnostics. Ordered-write and retry semantics remain with the driver per
	// CommonOperations.
	ExecuteInsertManyCommand(ctx context.Context, collection *mongo.Collection, documents []interface{}, resultObjectName string) (*mongo.InsertManyResult, error)
}

// RepositoryHelper combines all repository utility interfaces
type RepositoryHelper interface {
	RepositoryLogger
	CursorMapper
	// GetClient returns the underlying mongo.Client for repository operations,
	// honouring ctx cancellation. The helper resolves it from its managed client
	// wrapper; the borrowed implementation returns the existing client without
	// opening a new pool.
	GetClient(ctx context.Context) (*mongo.Client, error)
	// GetDatabase returns the named database, falling back to the helper's default
	// when dbName is empty. The borrowed implementation rejects names outside its
	// bound database; part of the RepositoryHelper contract.
	GetDatabase(ctx context.Context, dbName string) (*mongo.Database, error)
	// Health returns a map of health information for the repository connection. The
	// helper delegates to its client wrapper; the borrowed implementation reports
	// only availability, excluding server or credential details.
	Health(ctx context.Context) map[string]interface{}
	// Stats returns connection statistics for the repository's MongoDB client. The
	// helper delegates to its client wrapper; the borrowed implementation returns
	// zero values because it owns no connection pool counters.
	Stats() repositoryhelpers.ConnectionStats

	// Explicit Log* methods for clear API
	LogError(ctx context.Context, message string, err error, fields ...Field)
	// LogWarn emits a warning-level repository log for message with optional err
	// and structured fields via the configured logger, per RepositoryHelper. No
	// output occurs when no logger is configured.
	LogWarn(ctx context.Context, message string, err error, fields ...Field)
	// LogInfo emits an info-level repository log for message with optional err and
	// structured fields via the configured logger, per RepositoryHelper. No output
	// occurs when no logger is configured.
	LogInfo(ctx context.Context, message string, err error, fields ...Field)
	// LogDebug emits a debug-level repository log for message with optional err and
	// structured fields via the configured logger, per RepositoryHelper. No output
	// occurs when no logger is configured.
	LogDebug(ctx context.Context, message string, err error, fields ...Field)

	CommonOperations
}

// MongoRepositoryHelper implements RepositoryHelper interface
type MongoRepositoryHelper struct {
	mongoClient repositoryhelpers.MongoClientManager
	logger      RepositoryLogger
	defaultDB   string
}

// NewMongoRepositoryHelper creates a new extensible repository helper
func NewMongoRepositoryHelper(
	mongoClient repositoryhelpers.MongoClientManager,
	logger RepositoryLogger,
	defaultDB string,
) *MongoRepositoryHelper {
	return &MongoRepositoryHelper{
		mongoClient: mongoClient,
		logger:      logger,
		defaultDB:   defaultDB,
	}
}

// GetClient returns MongoDB client
func (r *MongoRepositoryHelper) GetClient(ctx context.Context) (*mongo.Client, error) {
	if r == nil || r.mongoClient == nil || ctx == nil {
		return nil, ErrInvalidMongoOperation
	}
	client, err := r.mongoClient.GetClient(ctx)
	observeMongo(ctx, r, "get_client", err)
	return client, err
}

// GetDatabase returns MongoDB database
func (r *MongoRepositoryHelper) GetDatabase(ctx context.Context, dbName string) (*mongo.Database, error) {
	if r == nil || r.mongoClient == nil || ctx == nil {
		return nil, ErrInvalidMongoOperation
	}
	if dbName == "" {
		dbName = r.defaultDB
	}

	db, err := r.mongoClient.GetDatabase(ctx, dbName)
	observeMongo(ctx, r, "get_database", err)
	return db, err
}

// Health returns health information
func (r *MongoRepositoryHelper) Health(ctx context.Context) map[string]interface{} {
	return r.mongoClient.Health(ctx)
}

// Stats returns connection statistics
func (r *MongoRepositoryHelper) Stats() repositoryhelpers.ConnectionStats {
	return r.mongoClient.Stats()
}

// LogError logs error level messages
func (r *MongoRepositoryHelper) LogError(ctx context.Context, message string, err error, fields ...Field) {
	if r.logger != nil {
		r.logger.Error(ctx, message, err, fields...)
	}
}

// LogWarn logs warning level messages
func (r *MongoRepositoryHelper) LogWarn(ctx context.Context, message string, err error, fields ...Field) {
	if r.logger != nil {
		r.logger.Warn(ctx, message, err, fields...)
	}
}

// LogInfo logs info level messages
func (r *MongoRepositoryHelper) LogInfo(ctx context.Context, message string, err error, fields ...Field) {
	if r.logger != nil {
		r.logger.Info(ctx, message, err, fields...)
	}
}

// LogDebug logs debug level messages
func (r *MongoRepositoryHelper) LogDebug(ctx context.Context, message string, err error, fields ...Field) {
	if r.logger != nil {
		r.logger.Debug(ctx, message, err, fields...)
	}
}

// Interface methods (delegate to Log* methods for RepositoryLogger interface compatibility)

// Error implements RepositoryLogger interface
func (r *MongoRepositoryHelper) Error(ctx context.Context, message string, err error, fields ...Field) {
	r.LogError(ctx, message, err, fields...)
}

// Warn implements RepositoryLogger interface
func (r *MongoRepositoryHelper) Warn(ctx context.Context, message string, err error, fields ...Field) {
	r.LogWarn(ctx, message, err, fields...)
}

// Info implements RepositoryLogger interface
func (r *MongoRepositoryHelper) Info(ctx context.Context, message string, err error, fields ...Field) {
	r.LogInfo(ctx, message, err, fields...)
}

// Debug implements RepositoryLogger interface
func (r *MongoRepositoryHelper) Debug(ctx context.Context, message string, err error, fields ...Field) {
	r.LogDebug(ctx, message, err, fields...)
}

// MapAllToResult decodes and closes the cursor. Failures retain their native
// cause so errors.Is/As and the driver's transaction retry labels still work.
func (r *MongoRepositoryHelper) MapAllToResult(ctx context.Context, cursor *mongo.Cursor, result interface{}, objectName string) error {
	if r == nil || cursor == nil || ctx == nil {
		return ErrInvalidMongoOperation
	}
	err := cursor.All(ctx, result)
	observeMongo(ctx, r, "decode_all", err)
	if err != nil {
		return NewRepositoryErrorWithCause(ErrUnableToDecodeQueriedDocuments, "unable-to-decode-documents", err)
	}
	return nil
}

// MapOneToResult owns and closes the supplied cursor. It distinguishes cursor
// iteration failures from absence and never logs decoded payloads or driver text.
func (r *MongoRepositoryHelper) MapOneToResult(ctx context.Context, cursor *mongo.Cursor, result interface{}, objectName string) error {
	if r == nil || cursor == nil || ctx == nil {
		return ErrInvalidMongoOperation
	}
	defer closeMongoCursor(ctx, cursor)
	var err error
	if cursor.Next(ctx) {
		err = cursor.Decode(result)
	} else {
		err = cursor.Err()
		if err == nil {
			err = mongo.ErrNoDocuments
		}
	}
	observeMongo(ctx, r, "decode_one", err)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return NewRepositoryErrorWithCause(ErrResourceNotFound, "no-documents-found", err)
	}
	if err != nil {
		return NewRepositoryErrorWithCause(ErrUnableToDecodeQueriedDocuments, "unable-to-decode-document", err)
	}
	return nil
}

// ExecuteCountDocuments counts the complete filter result unless explicit
// driver options bound it. Error wrapping preserves the underlying cause.
func (r *MongoRepositoryHelper) ExecuteCountDocuments(ctx context.Context, collection *mongo.Collection, filter interface{}, opts ...options.Lister[options.CountOptions]) (int64, error) {
	count, err := mongoCommand(ctx, r, "count_documents", collection, func() (int64, error) { return collection.CountDocuments(ctx, filter, opts...) })
	if err != nil {
		return 0, NewRepositoryErrorWithCause(ErrUnableToCountDocuments, "unable-to-count-documents", err)
	}
	return count, nil
}

// ExecuteDeleteManyCommand removes all matches. The legacy signature discards
// counts; revision-sensitive single deletes should use the result-bearing API.
func (r *MongoRepositoryHelper) ExecuteDeleteManyCommand(ctx context.Context, collection *mongo.Collection, filter interface{}, targetObjectName string) error {
	_, err := mongoCommand(ctx, r, "delete_many", collection, func() (*mongo.DeleteResult, error) { return collection.DeleteMany(ctx, filter) })
	return err
}

// ExecuteUpdateManyCommand updates all matches without logging filters or data.
func (r *MongoRepositoryHelper) ExecuteUpdateManyCommand(ctx context.Context, collection *mongo.Collection, filter interface{}, updateFilter interface{}, resultObjectName string) error {
	_, err := mongoCommand(ctx, r, "update_many", collection, func() (*mongo.UpdateResult, error) { return collection.UpdateMany(ctx, filter, updateFilter) })
	return err
}

// ExecuteUpdateOneCommand preserves the legacy error-only signature. Use
// ExecuteUpdateOneCommandResult when matched/modified counts affect correctness.
func (r *MongoRepositoryHelper) ExecuteUpdateOneCommand(ctx context.Context, collection *mongo.Collection, filter interface{}, updateFilter interface{}, resultObjectName string) error {
	_, err := mongoCommand(ctx, r, "update_one", collection, func() (*mongo.UpdateResult, error) { return collection.UpdateOne(ctx, filter, updateFilter) })
	return err
}

// ExecuteDeleteOneCommand preserves the legacy error-only signature. Use the
// result-bearing helper for revision-qualified deletes.
func (r *MongoRepositoryHelper) ExecuteDeleteOneCommand(ctx context.Context, collection *mongo.Collection, filter interface{}, targetObjectName string) error {
	_, err := mongoCommand(ctx, r, "delete_one", collection, func() (*mongo.DeleteResult, error) { return collection.DeleteOne(ctx, filter) })
	return err
}

// ExecuteFindOneCommandDecodeResult maps only absence to onFailureErr. All
// other errors retain their identity; logError controls metadata-only logging.
func (r *MongoRepositoryHelper) ExecuteFindOneCommandDecodeResult(ctx context.Context, collection *mongo.Collection, filter interface{}, result interface{}, resultObjectName string, logError bool, onFailureErr error) error {
	if r == nil || ctx == nil || collection == nil {
		return ErrInvalidMongoOperation
	}
	err := collection.FindOne(ctx, filter).Decode(result)
	return r.handleFindOneDecodeError(ctx, err, "", nil, "", logError, onFailureErr)
}

// handleFindOneDecodeError keeps the compatibility mapping without exposing
// object names, query contents or error messages to even custom loggers.
func (r *MongoRepositoryHelper) handleFindOneDecodeError(ctx context.Context, err error, collectionName string, filter interface{}, resultObjectName string, logError bool, onFailureErr error) error {
	if logError {
		observeMongo(ctx, r, "find_one_decode", err)
	}
	if errors.Is(err, mongo.ErrNoDocuments) && onFailureErr != nil {
		return onFailureErr
	}
	return err
}

// ExecuteReplaceOneCommand retains the legacy error-only contract. Use
// ExecuteReplaceOneCommandResult for compare-and-swap or explicit upsert options.
func (r *MongoRepositoryHelper) ExecuteReplaceOneCommand(ctx context.Context, collection *mongo.Collection, filter interface{}, replacementObject interface{}, resultObjectName string) error {
	_, err := mongoCommand(ctx, r, "replace_one", collection, func() (*mongo.UpdateResult, error) { return collection.ReplaceOne(ctx, filter, replacementObject) })
	return err
}

// ExecuteFindCommand preserves native errors underneath the repository code.
// The caller owns cursor closure or delegates to MapAllInCursorToResult.
func (r *MongoRepositoryHelper) ExecuteFindCommand(ctx context.Context, collection *mongo.Collection, filter interface{}, opts ...options.Lister[options.FindOptions]) (*mongo.Cursor, error) {
	cursor, err := mongoCommand(ctx, r, "find", collection, func() (*mongo.Cursor, error) { return collection.Find(ctx, filter, opts...) })
	if err != nil {
		return nil, NewRepositoryErrorWithCause(ErrUnableToGenerateCollectionCursor, "unable-to-create-cursor", err)
	}
	return cursor, nil
}

// ExecuteAggregateCommand returns a caller-owned cursor and retains driver
// error causes. Pipeline data is never included in helper telemetry.
func (r *MongoRepositoryHelper) ExecuteAggregateCommand(ctx context.Context, collection *mongo.Collection, mongoPipeline []bson.D) (*mongo.Cursor, error) {
	cursor, err := mongoCommand(ctx, r, "aggregate", collection, func() (*mongo.Cursor, error) { return collection.Aggregate(ctx, mongoPipeline) })
	if err != nil {
		return nil, NewRepositoryErrorWithCause(ErrUnableToGenerateCollectionCursor, "unable-to-create-cursor", err)
	}
	return cursor, nil
}

// ExecuteInsertOneCommand preserves the driver result and duplicate/retry error
// identity. Encrypted documents and raw error messages are never logged.
func (r *MongoRepositoryHelper) ExecuteInsertOneCommand(ctx context.Context, collection *mongo.Collection, document interface{}, resultObjectName string) (*mongo.InsertOneResult, error) {
	return mongoCommand(ctx, r, "insert_one", collection, func() (*mongo.InsertOneResult, error) { return collection.InsertOne(ctx, document) })
}

// ExecuteInsertManyCommand inserts documents using the provided context and
// leaves ordered-write and retry semantics with the driver.
func (r *MongoRepositoryHelper) ExecuteInsertManyCommand(ctx context.Context, collection *mongo.Collection, documents []interface{}, resultObjectName string) (*mongo.InsertManyResult, error) {
	return mongoCommand(ctx, r, "insert_many", collection, func() (*mongo.InsertManyResult, error) { return collection.InsertMany(ctx, documents) })
}

////

// GetPaginationLimit gets the pagination limit from passed params and returns
// a pointer
func GetPaginationLimit(numberOfResourcePerPage int64) *int64 {
	var paginationLimit int64 = 0

	paginationLimit = numberOfResourcePerPage

	return &paginationLimit
}

// GetPaginationSkip calculates the skip value for pagination based on the
// page number and limit passed
func GetPaginationSkip(pageNumber int64, paginationLimit *int64) *int64 {
	var skip int64 = 0

	if pageNumber > 1 {
		skip = (pageNumber - 1) * *paginationLimit
	}

	return &skip
}
