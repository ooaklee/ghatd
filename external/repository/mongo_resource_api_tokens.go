package repository

import (
	"context"

	"github.com/ooaklee/ghatd/external/apitoken"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// GetTotalApiTokens total api token from DB that match passed arguments
func (r MongoDbRepository) GetTotalApiTokens(ctx context.Context, userId string, to string, from string, onlyEphemeral bool, onlyPermanent bool) (int64, error) {

	// Example mongo query
	// /// get token total
	// db.getCollection("apitokens").countDocuments({created_by_id: "7fd7fa4f-9ccc-4bd6-8e80-0302077ea9eb" })
	// /// get ephemeral token total
	// db.getCollection("apitokens").countDocuments({created_by_id: "7fd7fa4f-9ccc-4bd6-8e80-0302077ea9eb", ttl_expires_at: { $exists: true}, created_at: {
	// 	$gt: '2023-07-04T00:00:00.000Z',
	// 	$lt: '2023-07-05T00:00:00.000Z'
	//   } })
	// /// get permanent token total
	// db.getCollection("apitokens").countDocuments({created_by_id: "7fd7fa4f-9ccc-4bd6-8e80-0302077ea9eb", ttl_expires_at: { $exists: false} })

	apiTokenFilter := bson.M{"created_by_id": userId}

	if onlyEphemeral {
		apiTokenFilter["ttl_expires_at"] = bson.M{"$exists": true}
	}

	if onlyPermanent {
		apiTokenFilter["ttl_expires_at"] = bson.M{"$exists": false}
	}

	if to != "" || from != "" {

		timeRangeFilter := bson.M{}

		if from != "" {
			timeRangeFilter["$gt"] = from
		}

		if to != "" {
			timeRangeFilter["$lt"] = to
		}

		apiTokenFilter["created_at"] = timeRangeFilter
	}

	// NICE_TO_HAVE: Wrap context with observability platform transaction

	client, err := r.InitialiseClient(ctx)
	if err != nil {
		return 0, err
	}

	collection := r.GetApiTokenCollection(client)

	return ExecuteCountDocuments(ctx, collection, apiTokenFilter)
}

// GetAPITokensForNanoId is returning apitokens created by user with matching nano Id from the DB
func (r MongoDbRepository) GetAPITokensForNanoId(ctx context.Context, userNanoId string, requestFilter *bson.D) ([]apitoken.UserAPIToken, error) {
	var result []apitoken.UserAPIToken
	findOptions := options.Find()

	// Sort by request field
	findOptions.SetSort(*requestFilter)

	queryFilter := bson.M{"created_by_nid": userNanoId}

	// NICE_TO_HAVE: Wrap context with observability platform transaction

	client, err := r.InitialiseClient(ctx)
	if err != nil {
		return nil, err
	}

	collection := r.GetApiTokenCollection(client)

	c, err := ExecuteFindCommand(ctx, collection, queryFilter, findOptions)
	if err != nil {
		return nil, err
	}

	if err = MapAllInCursorToResult(ctx, c, &result, "apitoken"); err != nil {
		return nil, err
	}

	return result, nil
}

// GetAPITokensFor returns apitokens created by user with matching ID from the DB
// Consolidate with GetAPITokensForNanoId
func (r MongoDbRepository) GetAPITokensFor(ctx context.Context, userID string, requestFilter *bson.D) ([]apitoken.UserAPIToken, error) {
	var result []apitoken.UserAPIToken
	findOptions := options.Find()

	// Sort by request field
	findOptions.SetSort(*requestFilter)

	queryFilter := bson.M{"created_by_id": userID}

	// NICE_TO_HAVE: Wrap context with observability platform transaction

	client, err := r.InitialiseClient(ctx)
	if err != nil {
		return nil, err
	}

	collection := r.GetApiTokenCollection(client)

	c, err := ExecuteFindCommand(ctx, collection, queryFilter, findOptions)
	if err != nil {
		return nil, err
	}

	if err = MapAllInCursorToResult(ctx, c, &result, "apitoken"); err != nil {
		return nil, err
	}

	return result, nil
}

// CreateUserAPIToken creates an user apitoken in the DB
func (r MongoDbRepository) CreateUserAPIToken(ctx context.Context, apiToken *apitoken.UserAPIToken) (*apitoken.UserAPIToken, error) {

	// NICE_TO_HAVE: Wrap context with observability platform transaction

	client, err := r.InitialiseClient(ctx)
	if err != nil {
		return nil, err
	}

	collection := r.GetApiTokenCollection(client)

	apiToken.Generate().GenerateNewCodename().GenerateNewUUID()

	_, err = collection.InsertOne(ctx, apiToken)
	if err != nil {
		return nil, err
	}

	return apiToken, nil
}

// UpdateAPIToken updates apitoken passed in the DB
func (r MongoDbRepository) UpdateAPIToken(ctx context.Context, apiToken *apitoken.UserAPIToken) (*apitoken.UserAPIToken, error) {

	updateFilter := bson.M{"_id": apiToken.ID}

	apiToken.SetUpdatedAtTimeToNow()

	// NICE_TO_HAVE: Wrap context with observability platform transaction

	client, err := r.InitialiseClient(ctx)
	if err != nil {
		return nil, err
	}

	collection := r.GetApiTokenCollection(client)

	err = ExecuteReplaceOneCommand(ctx, collection, updateFilter, apiToken, "ApiToken")
	if err != nil {
		return nil, err
	}

	return apiToken, nil

}

// DeleteAPITokenByID removes passed api token ID from DB
func (r MongoDbRepository) DeleteAPITokenByID(ctx context.Context, apiTokenID string) error {
	deleteFilter := bson.M{"_id": apiTokenID}

	// NICE_TO_HAVE: Wrap context with observability platform transaction

	client, err := r.InitialiseClient(ctx)
	if err != nil {
		return err
	}

	collection := r.GetApiTokenCollection(client)

	return ExecuteDeleteOneCommand(ctx, collection, deleteFilter, "ApiToken")

}

// GetAPITokenByID returns the apitoken with matching id
func (r MongoDbRepository) GetAPITokenByID(ctx context.Context, apiTokenID string) (*apitoken.UserAPIToken, error) {
	var result apitoken.UserAPIToken

	// NICE_TO_HAVE: Wrap context with observability platform transaction

	client, err := r.InitialiseClient(ctx)
	if err != nil {
		return nil, err
	}

	collection := r.GetApiTokenCollection(client)

	err = ExecuteFindOneCommandDecodeResult(ctx, collection, bson.M{"_id": apiTokenID}, &result, "ApiToken", true, ErrKeyResourceNotFound)
	if err != nil {
		return nil, err
	}

	return &result, nil

}

// GetAPITokens returns apitokens matching filters from the DB
func (r MongoDbRepository) GetAPITokens(ctx context.Context, queryFilter bson.D, requestFilter *bson.D) ([]apitoken.UserAPIToken, error) {
	var result []apitoken.UserAPIToken
	findOptions := options.Find()

	// Sort by request field
	findOptions.SetSort(*requestFilter)

	// NICE_TO_HAVE: Wrap context with observability platform transaction

	client, err := r.InitialiseClient(ctx)
	if err != nil {
		return nil, err
	}

	collection := r.GetApiTokenCollection(client)

	c, err := ExecuteFindCommand(ctx, collection, queryFilter, findOptions)
	if err != nil {
		return nil, err
	}

	if err = MapAllInCursorToResult(ctx, c, &result, "apitoken"); err != nil {
		return nil, err
	}

	return result, nil
}

// // DeleteAPITokenFor removes apitoken with passed ID from apitoken collection.
// // Also updates user collection to remove reference
// func (r MongoDbRepository) DeleteAPITokenFor(ctx context.Context, userID string, apiTokenID string) error {

// 	// NICE_TO_HAVE: Wrap context with observability platform transaction

// 	err := r.DeleteAPITokenByID(ctx, apiTokenID)
// 	if err != nil {
// 		return err
// 	}

// 	// Purge token reference from user (users collection)
// 	r.userPurgeAPITokenByID(ctx, userID, apiTokenID)

// 	return nil
// }
