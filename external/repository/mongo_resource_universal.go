package repository

import (
	"context"
	"errors"
	"fmt"
	"reflect"

	"github.com/ooaklee/ghatd/external/apitoken"

	"github.com/ooaklee/ghatd/external/logger"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.uber.org/zap"
)

// DeleteResourcesByOwnerId deletes all of specified resource type that belongs to the specified user id
func (r MongoDbRepository) DeleteResourcesByOwnerId(ctx context.Context, resourceType interface{}, ownerId string) error {

	var deleteFilter bson.M
	var resourceTypeCollection *mongo.Collection
	var resourceTypeString string
	loggr := logger.AcquireFrom(ctx)

	// NICE_TO_HAVE: Wrap context with observability platform transaction

	client, err := r.InitialiseClient(ctx)
	if err != nil {
		return err
	}

	switch reflect.TypeOf(resourceType) {
	case reflect.TypeOf(&apitoken.UserAPIToken{}):
		deleteFilter = bson.M{"created_by_id": ownerId}
		resourceTypeCollection = r.GetApiTokenCollection(client)
		resourceTypeString = "ApiTokens"
	default:
		loggr.Error(fmt.Sprintf("unsupported-resource-type-passed: %e", reflect.TypeOf(resourceType)), zap.String("user-id", ownerId))
		return errors.New("ErrKeyAttemptedDeletionOfUnsupportedResourceTpye")
	}

	return ExecuteDeleteManyCommand(ctx, resourceTypeCollection, deleteFilter, resourceTypeString)
}
