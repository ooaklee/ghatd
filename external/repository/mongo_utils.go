package repository

import (
	"context"
	"fmt"
	"strings"

	repositoryhelpers "github.com/ooaklee/ghatd/external/repository/helpers"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// RepositoryLogger defines interface for repository-level logging
type RepositoryLogger interface {
	Error(ctx context.Context, message string, err error, fields ...Field)
	Warn(ctx context.Context, message string, err error, fields ...Field)
	Info(ctx context.Context, message string, err error, fields ...Field)
	Debug(ctx context.Context, message string, err error, fields ...Field)
}

// Field represents a key-value pair for structured logging
type Field struct {
	Key   string
	Value interface{}
}

// CursorMapper defines interface for cursor mapping operations
type CursorMapper interface {
	MapAllToResult(ctx context.Context, cursor *mongo.Cursor, result interface{}, objectName string) error
	MapOneToResult(ctx context.Context, cursor *mongo.Cursor, result interface{}, objectName string) error
}

// CommonOperations defines interface for common MongoDB operations
type CommonOperations interface {
	ExecuteCountDocuments(ctx context.Context, collection *mongo.Collection, filter interface{}, opts ...*options.CountOptions) (int64, error)
	ExecuteDeleteManyCommand(ctx context.Context, collection *mongo.Collection, filter interface{}, targetObjectName string) error
	ExecuteUpdateManyCommand(ctx context.Context, collection *mongo.Collection, filter interface{}, updateFilter interface{}, resultObjectName string) error
	ExecuteUpdateOneCommand(ctx context.Context, collection *mongo.Collection, filter interface{}, updateFilter interface{}, resultObjectName string) error
	ExecuteDeleteOneCommand(ctx context.Context, collection *mongo.Collection, filter interface{}, targetObjectName string) error
	ExecuteFindOneCommandDecodeResult(ctx context.Context, collection *mongo.Collection, filter interface{}, result interface{}, resultObjectName string, logError bool, onFailureErr error) error
	ExecuteReplaceOneCommand(ctx context.Context, collection *mongo.Collection, filter interface{}, replacementObject interface{}, resultObjectName string) error
	ExecuteFindCommand(ctx context.Context, collection *mongo.Collection, filter interface{}, opts ...*options.FindOptions) (*mongo.Cursor, error)
	ExecuteAggregateCommand(ctx context.Context, collection *mongo.Collection, mongoPipeline []bson.D) (*mongo.Cursor, error)
	ExecuteInsertOneCommand(ctx context.Context, collection *mongo.Collection, document interface{}, resultObjectName string) (*mongo.InsertOneResult, error)
	ExecuteInsertManyCommand(ctx context.Context, collection *mongo.Collection, documents []interface{}, resultObjectName string) (*mongo.InsertManyResult, error)
}

// RepositoryHelper combines all repository utility interfaces
type RepositoryHelper interface {
	RepositoryLogger
	CursorMapper
	GetClient(ctx context.Context) (*mongo.Client, error)
	GetDatabase(ctx context.Context, dbName string) (*mongo.Database, error)
	Health(ctx context.Context) map[string]interface{}
	Stats() repositoryhelpers.ConnectionStats

	// Explicit Log* methods for clear API
	LogError(ctx context.Context, message string, err error, fields ...Field)
	LogWarn(ctx context.Context, message string, err error, fields ...Field)
	LogInfo(ctx context.Context, message string, err error, fields ...Field)
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
	client, err := r.mongoClient.GetClient(ctx)
	if err != nil {
		r.LogError(ctx, "error-initialising-db-client", err, Field{Key: "operation", Value: "get_client"})
	}
	return client, err
}

// GetDatabase returns MongoDB database
func (r *MongoRepositoryHelper) GetDatabase(ctx context.Context, dbName string) (*mongo.Database, error) {
	if dbName == "" {
		dbName = r.defaultDB
	}

	db, err := r.mongoClient.GetDatabase(ctx, dbName)
	if err != nil {
		r.LogError(ctx, "error-getting-database", err,
			Field{Key: "operation", Value: "get_database"},
			Field{Key: "database", Value: dbName},
		)
	}
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

// MapAllToResult maps all documents in cursor to result
func (r *MongoRepositoryHelper) MapAllToResult(ctx context.Context, cursor *mongo.Cursor, result interface{}, objectName string) error {
	if cursor == nil {
		err := fmt.Errorf("cursor-is-nil")
		r.LogError(ctx, "cannot-decode-documents-from-nil-cursor", err,
			Field{Key: "operation", Value: "map_all_to_result"},
			Field{Key: "object_name", Value: objectName},
		)
		return NewRepositoryError(ErrKeyUnableToDecodeQueriedDocuments, "cursor is nil")
	}

	if err := cursor.All(ctx, result); err != nil {
		r.LogError(ctx, fmt.Sprintf("unable-to-decode-%s", objectName), err,
			Field{Key: "operation", Value: "map_all_to_result"},
			Field{Key: "object_name", Value: objectName},
		)
		return NewRepositoryError(ErrKeyUnableToDecodeQueriedDocuments, err.Error())
	}

	r.LogDebug(ctx, fmt.Sprintf("successfully-decoded-%s", objectName), nil,
		Field{Key: "operation", Value: "map_all_to_result"},
		Field{Key: "object_name", Value: objectName},
	)

	return nil
}

// MapOneToResult maps one document from cursor to result
func (r *MongoRepositoryHelper) MapOneToResult(ctx context.Context, cursor *mongo.Cursor, result interface{}, objectName string) error {
	if cursor == nil {
		err := fmt.Errorf("cursor-is-nil")
		r.LogError(ctx, "cannot-decode-document-from-nil-cursor", err,
			Field{Key: "operation", Value: "map_one_to_result"},
			Field{Key: "object_name", Value: objectName},
		)
		return NewRepositoryError(ErrKeyUnableToDecodeQueriedDocuments, "cursor is nil")
	}

	if cursor.Next(ctx) {
		if err := cursor.Decode(result); err != nil {
			r.LogError(ctx, fmt.Sprintf("unable-to-decode-%s", objectName), err,
				Field{Key: "operation", Value: "map_one_to_result"},
				Field{Key: "object_name", Value: objectName},
			)
			return NewRepositoryError(ErrKeyUnableToDecodeQueriedDocuments, err.Error())
		}

		r.LogDebug(ctx, fmt.Sprintf("successfully-decoded-%s", objectName), nil,
			Field{Key: "operation", Value: "map_one_to_result"},
			Field{Key: "object_name", Value: objectName},
		)
		return nil
	}

	// No documents found
	err := fmt.Errorf("no-documents-found")
	r.LogWarn(ctx, fmt.Sprintf("no-%s-found-in-cursor", objectName), err,
		Field{Key: "operation", Value: "map_one_to_result"},
		Field{Key: "object_name", Value: objectName},
	)
	return NewRepositoryError(ErrKeyResourceNotFound, "no-documents-found")
}

// ExecuteCountDocuments returns a int64 count if successful, otherwise an error is returned
func (r *MongoRepositoryHelper) ExecuteCountDocuments(ctx context.Context, collection *mongo.Collection, filter interface{}, opts ...*options.CountOptions) (int64, error) {

	count, err := collection.CountDocuments(ctx, filter, opts...)
	if err != nil {
		r.LogError(ctx, "unable-to-count-records", err,
			Field{Key: "operation", Value: "count_documents"},
			Field{Key: "collection", Value: collection.Name()},
			Field{Key: "query_filter", Value: filter},
		)
		return 0, NewRepositoryError(ErrKeyUnableToCountDocuments, err.Error())
	}

	return count, nil
}

// ExecuteDeleteManyCommand attempts to remove all resources matching specified filter(s), if successful error is nil
func (r *MongoRepositoryHelper) ExecuteDeleteManyCommand(ctx context.Context, collection *mongo.Collection, filter interface{}, targetObjectName string) error {

	targetObjectName = strings.ToLower(targetObjectName)

	_, err := collection.DeleteMany(ctx, filter)
	if err != nil {
		r.LogError(ctx, fmt.Sprintf("unable-to-delete-%s", targetObjectName), err,
			Field{Key: "operation", Value: "delete_many"},
			Field{Key: "collection", Value: collection.Name()},
			Field{Key: "query_filter", Value: filter},
		)
		return err
	}

	return nil
}

// ExecuteUpdateManyCommand attempts to match and update document in collection, error on failure
func (r *MongoRepositoryHelper) ExecuteUpdateManyCommand(ctx context.Context, collection *mongo.Collection, filter interface{}, updateFilter interface{}, resultObjectName string) error {

	resultObjectName = strings.ToLower(resultObjectName)

	_, err := collection.UpdateMany(ctx, filter, updateFilter)
	if err != nil {
		r.LogError(ctx, fmt.Sprintf("match-and-update-many-failure-%s", resultObjectName), err,
			Field{Key: "operation", Value: "update_many"},
			Field{Key: "collection", Value: collection.Name()},
			Field{Key: "query_filter", Value: filter},
			Field{Key: "update_filter", Value: updateFilter},
		)
		return err
	}

	return nil
}

// ExecuteUpdateOneCommand attempts to match and update document in collection, error on failure
func (r *MongoRepositoryHelper) ExecuteUpdateOneCommand(ctx context.Context, collection *mongo.Collection, filter interface{}, updateFilter interface{}, resultObjectName string) error {

	resultObjectName = strings.ToLower(resultObjectName)

	_, err := collection.UpdateOne(ctx, filter, updateFilter)
	if err != nil {
		r.LogError(ctx, fmt.Sprintf("match-and-update-failure-%s", resultObjectName), err,
			Field{Key: "operation", Value: "update_one"},
			Field{Key: "collection", Value: collection.Name()},
			Field{Key: "query_filter", Value: filter},
			Field{Key: "update_filter", Value: updateFilter},
		)
		return err
	}

	return nil
}

// ExecuteDeleteOneCommand attempts to remove resource matching filter from repository, if successful error is nil
func (r *MongoRepositoryHelper) ExecuteDeleteOneCommand(ctx context.Context, collection *mongo.Collection, filter interface{}, targetObjectName string) error {

	targetObjectName = strings.ToLower(targetObjectName)
	_, err := collection.DeleteOne(ctx, filter)
	if err != nil {
		r.LogError(ctx, fmt.Sprintf("unable-to-delete-%s", targetObjectName), err,
			Field{Key: "operation", Value: "delete_one"},
			Field{Key: "collection", Value: collection.Name()},
			Field{Key: "query_filter", Value: filter},
		)
		return err
	}

	return nil
}

// ExecuteFindOneCommandDecodeResult if successful decodes document to passed result object, otherwise an error is returned
func (r *MongoRepositoryHelper) ExecuteFindOneCommandDecodeResult(ctx context.Context, collection *mongo.Collection, filter interface{}, result interface{}, resultObjectName string, logError bool, onFailureErr error) error {
	resultObjectName = strings.ToLower(resultObjectName)

	err := collection.FindOne(ctx, filter).Decode(result)
	if err != nil {
		if logError {
			r.LogWarn(ctx, fmt.Sprintf("unable-to-find-and-decode-%s-matching-provided-filter", resultObjectName), err,
				Field{Key: "operation", Value: "find_one_decode"},
				Field{Key: "collection", Value: collection.Name()},
				Field{Key: "query_filter", Value: filter},
			)
		}
		if onFailureErr != nil {
			return onFailureErr
		}
		return err
	}

	return nil
}

// ExecuteReplaceOneCommand handles replacing a document in a collection, error on failure
func (r *MongoRepositoryHelper) ExecuteReplaceOneCommand(ctx context.Context, collection *mongo.Collection, filter interface{}, replacementObject interface{}, resultObjectName string) error {

	resultObjectName = strings.ToLower(resultObjectName)
	_, err := collection.ReplaceOne(ctx, filter, replacementObject)
	if err != nil {
		r.LogError(ctx, fmt.Sprintf("error-updating-%s", resultObjectName), err,
			Field{Key: "operation", Value: "replace_one"},
			Field{Key: "collection", Value: collection.Name()},
			Field{Key: "query_filter", Value: filter},
		)

		return err
	}

	return nil
}

// ExecuteFindCommand returns a cursor if successful, otherwise an error is returned
func (r *MongoRepositoryHelper) ExecuteFindCommand(ctx context.Context, collection *mongo.Collection, filter interface{}, opts ...*options.FindOptions) (*mongo.Cursor, error) {

	c, err := collection.Find(ctx, filter, opts...)
	if err != nil {
		r.LogError(ctx, "error-generating-cursor", err,
			Field{Key: "operation", Value: "find"},
			Field{Key: "collection", Value: collection.Name()},
			Field{Key: "query_filter", Value: filter},
		)
		return nil, NewRepositoryError(ErrKeyUnableToGenerateCollectionCursor, err.Error())
	}

	return c, nil
}

// ExecuteAggregateCommand returns a cursor if successful, otherwise an error is returned
func (r *MongoRepositoryHelper) ExecuteAggregateCommand(ctx context.Context, collection *mongo.Collection, mongoPipeline []bson.D) (*mongo.Cursor, error) {

	c, err := collection.Aggregate(ctx, mongoPipeline)
	if err != nil {
		r.LogError(ctx, "error-generating-cursor-for-aggregation", err,
			Field{Key: "operation", Value: "aggregate"},
			Field{Key: "collection", Value: collection.Name()},
			Field{Key: "pipeline", Value: mongoPipeline},
		)
		return nil, NewRepositoryError(ErrKeyUnableToGenerateCollectionCursor, err.Error())
	}

	return c, nil
}

// ExecuteInsertOneCommand executes an insert one command
func (r *MongoRepositoryHelper) ExecuteInsertOneCommand(ctx context.Context, collection *mongo.Collection, document interface{}, resultObjectName string) (*mongo.InsertOneResult, error) {
	resultObjectName = strings.ToLower(resultObjectName)
	res, err := collection.InsertOne(ctx, document)
	if err != nil {
		r.LogError(ctx, fmt.Sprintf("error-inserting-%s", resultObjectName), err,
			Field{Key: "operation", Value: "insert_one"},
			Field{Key: "collection", Value: collection.Name()},
			Field{Key: "document", Value: document},
		)

		return nil, err
	}

	return res, nil
}

// ExecuteInsertManyCommand executes an insert many command
func (r *MongoRepositoryHelper) ExecuteInsertManyCommand(ctx context.Context, collection *mongo.Collection, documents []interface{}, resultObjectName string) (*mongo.InsertManyResult, error) {
	resultObjectName = strings.ToLower(resultObjectName)
	res, err := collection.InsertMany(ctx, documents)
	if err != nil {
		r.LogError(ctx, fmt.Sprintf("error-inserting-%s", resultObjectName), err,
			Field{Key: "operation", Value: "insert_many"},
			Field{Key: "collection", Value: collection.Name()},
			Field{Key: "documents", Value: documents},
		)

		return nil, err
	}

	return res, nil
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
