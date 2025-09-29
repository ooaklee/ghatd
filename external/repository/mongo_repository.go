package repository

import (
	"context"

	repositoryhelpers "github.com/ooaklee/ghatd/external/repository/helpers"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// MongoDbRepository uses the new extensible pattern
type MongoDbRepository struct {
	helper RepositoryHelper
}

// NewMongoDbRepository creates a new extensible mongo repository
func NewMongoDbRepository(
	mongoClient repositoryhelpers.MongoClientManager,
	logger RepositoryLogger,
	defaultDB string,
) *MongoDbRepository {

	helper := NewMongoRepositoryHelper(mongoClient, logger, defaultDB)

	return &MongoDbRepository{
		helper: helper,
	}
}

// NewMongoDbRepositoryWithDefaults creates repository with default zap logger
func NewMongoDbRepositoryWithDefaults(
	mongoClient repositoryhelpers.MongoClientManager,
	defaultDB string,
) *MongoDbRepository {

	logger := NewZapRepositoryLogger()
	helper := NewMongoRepositoryHelper(mongoClient, logger, defaultDB)

	return &MongoDbRepository{
		helper: helper,
	}
}

// GetHelper returns the repository helper for direct access
func (r *MongoDbRepository) GetHelper() RepositoryHelper {
	return r.helper
}

// Backward Compatibility Methods

// InitialiseClient maintains backward compatibility
func (r *MongoDbRepository) InitialiseClient(ctx context.Context) (*mongo.Client, error) {
	client, err := r.helper.GetClient(ctx)
	if err != nil {
		return nil, NewRepositoryError(ErrKeyUnableToInitialiseDBClient, "failed-to-initialise-client")
	}
	return client, nil
}

// GetDatabase returns database instance
func (r *MongoDbRepository) GetDatabase(ctx context.Context, dbName string) (*mongo.Database, error) {
	return r.helper.GetDatabase(ctx, dbName)
}

// Health returns repository health information
func (r *MongoDbRepository) Health(ctx context.Context) map[string]interface{} {
	return r.helper.Health(ctx)
}

// Stats returns connection statistics
func (r *MongoDbRepository) Stats() repositoryhelpers.ConnectionStats {
	return r.helper.Stats()
}

// Convenience Methods for Common Operations

// LogError logs an error message
func (r *MongoDbRepository) LogError(ctx context.Context, message string, err error, fields ...Field) {
	r.helper.LogError(ctx, message, err, fields...)
}

// LogWarn logs a warning message
func (r *MongoDbRepository) LogWarn(ctx context.Context, message string, err error, fields ...Field) {
	r.helper.LogWarn(ctx, message, err, fields...)
}

// LogInfo logs an info message
func (r *MongoDbRepository) LogInfo(ctx context.Context, message string, err error, fields ...Field) {
	r.helper.LogInfo(ctx, message, err, fields...)
}

// LogDebug logs a debug message
func (r *MongoDbRepository) LogDebug(ctx context.Context, message string, err error, fields ...Field) {
	r.helper.LogDebug(ctx, message, err, fields...)
}

// MapAllInCursorToResult maintains backward compatibility with improved logging
func (r *MongoDbRepository) MapAllInCursorToResult(ctx context.Context, cursor *mongo.Cursor, result interface{}, resultObjectName string) error {
	return r.helper.MapAllToResult(ctx, cursor, result, resultObjectName)
}

// MapOneInCursorToResult provides single document mapping
func (r *MongoDbRepository) MapOneInCursorToResult(ctx context.Context, cursor *mongo.Cursor, result interface{}, resultObjectName string) error {
	return r.helper.MapOneToResult(ctx, cursor, result, resultObjectName)
}

// ExecuteCountDocuments executes a count documents command
func (r *MongoDbRepository) ExecuteCountDocuments(ctx context.Context, collection *mongo.Collection, filter interface{}, opts ...*options.CountOptions) (int64, error) {
	return r.helper.ExecuteCountDocuments(ctx, collection, filter, opts...)
}

// ExecuteDeleteManyCommand executes a delete many command
func (r *MongoDbRepository) ExecuteDeleteManyCommand(ctx context.Context, collection *mongo.Collection, filter interface{}, targetObjectName string) error {
	return r.helper.ExecuteDeleteManyCommand(ctx, collection, filter, targetObjectName)
}

// ExecuteUpdateManyCommand executes a update many command
func (r *MongoDbRepository) ExecuteUpdateManyCommand(ctx context.Context, collection *mongo.Collection, filter interface{}, updateFilter interface{}, resultObjectName string) error {
	return r.helper.ExecuteUpdateManyCommand(ctx, collection, filter, updateFilter, resultObjectName)
}

// ExecuteUpdateOneCommand executes a update one command
func (r *MongoDbRepository) ExecuteUpdateOneCommand(ctx context.Context, collection *mongo.Collection, filter interface{}, updateFilter interface{}, resultObjectName string) error {
	return r.helper.ExecuteUpdateOneCommand(ctx, collection, filter, updateFilter, resultObjectName)
}

// ExecuteDeleteOneCommand executes a delete one command
func (r *MongoDbRepository) ExecuteDeleteOneCommand(ctx context.Context, collection *mongo.Collection, filter interface{}, targetObjectName string) error {
	return r.helper.ExecuteDeleteOneCommand(ctx, collection, filter, targetObjectName)
}

// ExecuteFindOneCommandDecodeResult executes a find one command and decodes the result
func (r *MongoDbRepository) ExecuteFindOneCommandDecodeResult(ctx context.Context, collection *mongo.Collection, filter interface{}, result interface{}, resultObjectName string, logError bool, onFailureErr error) error {
	return r.helper.ExecuteFindOneCommandDecodeResult(ctx, collection, filter, result, resultObjectName, logError, onFailureErr)
}

// ExecuteReplaceOneCommand executes a replace one command
func (r *MongoDbRepository) ExecuteReplaceOneCommand(ctx context.Context, collection *mongo.Collection, filter interface{}, replacementObject interface{}, resultObjectName string) error {
	return r.helper.ExecuteReplaceOneCommand(ctx, collection, filter, replacementObject, resultObjectName)
}

// ExecuteFindCommand executes a find command
func (r *MongoDbRepository) ExecuteFindCommand(ctx context.Context, collection *mongo.Collection, filter interface{}, opts ...*options.FindOptions) (*mongo.Cursor, error) {
	return r.helper.ExecuteFindCommand(ctx, collection, filter, opts...)
}

// ExecuteAggregateCommand executes an aggregate command
func (r *MongoDbRepository) ExecuteAggregateCommand(ctx context.Context, collection *mongo.Collection, mongoPipeline []bson.D) (*mongo.Cursor, error) {
	return r.helper.ExecuteAggregateCommand(ctx, collection, mongoPipeline)
}

// ExecuteInsertOneCommand executes an insert one command
func (r *MongoDbRepository) ExecuteInsertOneCommand(ctx context.Context, collection *mongo.Collection, document interface{}, resultObjectName string) (*mongo.InsertOneResult, error) {
	return r.helper.ExecuteInsertOneCommand(ctx, collection, document, resultObjectName)
}

// ExecuteInsertManyCommand executes an insert many command
func (r *MongoDbRepository) ExecuteInsertManyCommand(ctx context.Context, collection *mongo.Collection, documents []interface{}, resultObjectName string) (*mongo.InsertManyResult, error) {
	return r.helper.ExecuteInsertManyCommand(ctx, collection, documents, resultObjectName)
}
