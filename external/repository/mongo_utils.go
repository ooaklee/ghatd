package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/ooaklee/ghatd/external/logger"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.uber.org/zap"
)

// InitialiseClient returns initialised mongo client, or error if unsuccessful
func (r MongoDbRepository) InitialiseClient(ctx context.Context) (*mongo.Client, error) {
	client, err := r.ClientHandler.GetClient(ctx)
	if err != nil {
		RepositoryLogEntry(ctx, logError, "Error initialising DB client", err)
		return nil, errors.New(ErrKeyUnableToInitialiseDBClient)
	}

	return client, nil
}

// GenerateSampleFilter returns the filter used to pull 1 sample document from collection. Without a query filter,
// sample uses entire DB.
func GenerateSampleFilter(queryFilter []bson.D, sampleSize int) []bson.D {

	finalisedFilter := []bson.D{}

	sampleAggregation := bson.E{Key: MongoAggregationKeySample, Value: bson.D{bson.E{Key: MongoAggregationKeySampleOptionSize, Value: sampleSize}}}

	if len(queryFilter) == 0 {
		// Run sample on entire collection
		return append(finalisedFilter, bson.D{sampleAggregation})
	}

	// Creates pipeline that limits pool to meet query filter(s) before running finishing off with sample
	finalisedFilter = append(finalisedFilter, bson.D{bson.E{Key: "$match", Value: bson.D{
		bson.E{Key: "$or", Value: queryFilter}}}})

	finalisedFilter = append(finalisedFilter, bson.D{sampleAggregation})

	return finalisedFilter
}

// RepositoryLogEntry handles logging passed message, error from repository
func RepositoryLogEntry(ctx context.Context, logLevel string, logMessage string, err error) {
	logger := logger.AcquireFrom(ctx)

	switch logLevel {
	case logWarn:
		logger.Warn(logMessage, zap.Error(err))
		if err != nil {
			logger.Warn(logMessage)
		}
	case logError:
		if err != nil {
			logger.Error(logMessage, zap.Error(err))
		}
		logger.Error(logMessage)

	default:
		if err != nil {
			logger.Info(logMessage, zap.Error(err))
			return
		}
		logger.Info(logMessage)
	}

}

// MapAllInCursorToResult handles decoding All documents found in cursour to passed result object, otherwise an error is returned
func MapAllInCursorToResult(ctx context.Context, cursor *mongo.Cursor, result interface{}, resultObjectName string) error {
	if err := cursor.All(ctx, result); err != nil {
		RepositoryLogEntry(ctx, logError, fmt.Sprintf("Unable to decode %s", resultObjectName), err)
		return errors.New(ErrKeyUnableToDecodeQueriedDocuments)
	}

	return nil
}
