package repository

import (
	"context"
	"errors"
	"fmt"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// ExecuteDeleteManyCommand attempts to remove all resources matching specified filter(s), if successful error is nil
func ExecuteDeleteManyCommand(ctx context.Context, collection *mongo.Collection, filter interface{}, targetObjectName string) error {

	var repoCtx = context.Background()

	// NICE_TO_HAVE: Wrap context with observability platform transaction

	_, err := collection.DeleteMany(repoCtx, filter)
	if err != nil {
		RepositoryLogEntry(ctx, logError, fmt.Sprintf("Unable to delete %s", targetObjectName), err)
		return err
	}

	return nil
}

// ExecuteUpdateManyCommand attempts to match and update document in collection, error on failure
func ExecuteUpdateManyCommand(ctx context.Context, collection *mongo.Collection, filter interface{}, updateFilter interface{}, resultObjectName string) error {

	var repoCtx = context.Background()

	// NICE_TO_HAVE: Wrap context with observability platform transaction

	_, err := collection.UpdateMany(repoCtx, filter, updateFilter)
	if err != nil {
		RepositoryLogEntry(ctx, logError, fmt.Sprintf("match-and-update-many-failure-%s:", resultObjectName), err)
		return err
	}

	return nil
}

// ExecuteUpdateOneCommand attempts to match and update document in collection, error on failure
func ExecuteUpdateOneCommand(ctx context.Context, collection *mongo.Collection, filter interface{}, updateFilter interface{}, resultObjectName string) error {

	var repoCtx = context.Background()

	// NICE_TO_HAVE: Wrap context with observability platform transaction

	_, err := collection.UpdateOne(repoCtx, filter, updateFilter)
	if err != nil {
		RepositoryLogEntry(ctx, logError, fmt.Sprintf("match-and-update-failure-%s:", resultObjectName), err)
		return err
	}

	return nil
}

// ExecuteDeleteOneCommand attempts to remove affirmation matching ID from repository, if successful error is nil
func ExecuteDeleteOneCommand(ctx context.Context, collection *mongo.Collection, filter interface{}, targetObjectName string) error {

	var repoCtx = context.Background()

	// NICE_TO_HAVE: Wrap context with observability platform transaction

	_, err := collection.DeleteOne(repoCtx, filter)
	if err != nil {
		RepositoryLogEntry(ctx, logError, fmt.Sprintf("Unable to delete %s", targetObjectName), err)
		return err
	}

	return nil
}

// ExecuteFindOneCommandDecodeResult if successful decodes document to passed result object, otherwise an error is returned
func ExecuteFindOneCommandDecodeResult(ctx context.Context, collection *mongo.Collection, filter interface{}, result interface{}, resultObjectName string, logError bool, errorType string) error {

	var repoCtx = context.Background()

	// NICE_TO_HAVE: Wrap context with observability platform transaction

	err := collection.FindOne(repoCtx, filter).Decode(result)
	if err != nil {
		if logError {
			RepositoryLogEntry(ctx, logWarn, fmt.Sprintf("Unable to find %s matching: %v", resultObjectName, filter), err)
		}
		return errors.New(errorType)
	}

	return nil
}

// ExecuteReplaceOneCommand if successful error is nil
func ExecuteReplaceOneCommand(ctx context.Context, collection *mongo.Collection, filter interface{}, replacementObject interface{}, resultObjectName string) error {

	var repoCtx = context.Background()

	// NICE_TO_HAVE: Wrap context with observability platform transaction

	_, err := collection.ReplaceOne(repoCtx, filter, replacementObject)
	if err != nil {
		RepositoryLogEntry(ctx, logError, fmt.Sprintf("Error updating %s:", resultObjectName), err)
		return err
	}

	return nil
}

// ExecuteFindCommand returns a cursor if successful, otherwise an error is returned
func ExecuteFindCommand(ctx context.Context, collection *mongo.Collection, filter interface{}, opts ...*options.FindOptions) (*mongo.Cursor, error) {

	var repoCtx = context.Background()

	// NICE_TO_HAVE: Wrap context with observability platform transaction

	c, err := collection.Find(repoCtx, filter, opts...)
	if err != nil {
		RepositoryLogEntry(ctx, logError, "Unable to generate cursor", err)
		return nil, errors.New(ErrKeyUnableToGenerateCollectionCursor)
	}

	return c, nil
}

// ExecuteAggregateCommand returns a cursor if successful, otherwise an error is returned
func ExecuteAggregateCommand(ctx context.Context, collection *mongo.Collection, mongoPipeline []bson.D) (*mongo.Cursor, error) {

	var repoCtx = context.Background()

	// NICE_TO_HAVE: Wrap context with observability platform transaction

	c, err := collection.Aggregate(repoCtx, mongoPipeline)
	if err != nil {
		RepositoryLogEntry(ctx, logError, "Unable to generate cursor", err)
		return nil, errors.New(ErrKeyUnableToGenerateCollectionCursor)
	}

	return c, nil
}

// ExecuteCountDocuments returns a int64 count if successful, otherwise an error is returned
func ExecuteCountDocuments(ctx context.Context, collection *mongo.Collection, filter interface{}, opts ...*options.CountOptions) (int64, error) {

	var repoCtx = context.Background()

	// NICE_TO_HAVE: Wrap context with observability platform transaction

	count, err := collection.CountDocuments(repoCtx, filter, opts...)
	if err != nil {
		RepositoryLogEntry(ctx, logError, "Unable to count records", err)
		return 0, errors.New(ErrKeyUnableToCountDocuments)
	}

	return count, nil
}
