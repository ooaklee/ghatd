package repository

import (
	"context"
	"fmt"

	"github.com/ooaklee/ghatd/external/apitoken"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// GetTotalApiTokens total api token from DB that match passed arguments
func (r MongoDbRepository) GetTotalApiTokens(ctx context.Context, userId, userNanoId, descriptionFilter, statusFilter, to, from string, onlyEphemeral bool, onlyPermanent bool) (int64, error) {

	// Example mongo query
	// /// get token total
	// db.getCollection("apitokens").countDocuments({_id: { $exists : true }, created_by_id: "7fd7fa4f-9ccc-4bd6-8e80-0302077ea9eb" })
	// /// get token with status "x" total
	// db.getCollection("apitokens").countDocuments({_id: { $exists : true }, created_by_id: "7fd7fa4f-9ccc-4bd6-8e80-0302077ea9eb", status: /^x$/i })
	// /// get ephemeral token total
	// db.getCollection("apitokens").countDocuments({_id: { $exists : true }, created_by_id: "7fd7fa4f-9ccc-4bd6-8e80-0302077ea9eb", ttl_expires_at: { $exists: true}, created_at: {
	// 	$gt: '2023-07-04T00:00:00.000Z',
	// 	$lt: '2023-07-05T00:00:00.000Z'
	//   } })
	// /// get permanent token total
	// db.getCollection("apitokens").countDocuments({_id: { $exists : true }, created_by_id: "7fd7fa4f-9ccc-4bd6-8e80-0302077ea9eb", ttl_expires_at: { $exists: false} })

	apiTokenFilter := bson.M{"_id": bson.M{"$exists": true}}

	if userId != "" {
		apiTokenFilter["created_by_id"] = userId
	}

	if userNanoId != "" {
		apiTokenFilter["created_by_nid"] = userNanoId
	}

	if descriptionFilter != "" {
		apiTokenFilter["description"] = primitive.Regex{
			Pattern: fmt.Sprintf(MongoRegexStringFormat, descriptionFilter),
			Options: "i",
		}
	}

	if statusFilter != "" {
		apiTokenFilter["status"] = primitive.Regex{
			Pattern: fmt.Sprintf(MongoRegexStringFormat, statusFilter),
			Options: "i",
		}
	}

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

	// NICE_TO_HAVE: Wrap context with observability platform transaction

	client, err := r.InitialiseClient(ctx)
	if err != nil {
		return nil, err
	}

	collection := r.GetApiTokenCollection(client)

	apiToken.SetUpdatedAtTimeToNow()

	err = ExecuteUpdateOneCommand(ctx, collection, bson.M{"_id": apiToken.ID}, bson.M{"$set": apiToken}, "api-token")
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
func (r MongoDbRepository) GetAPITokens(ctx context.Context, req *apitoken.GetAPITokensRequest) ([]apitoken.UserAPIToken, error) {
	var (
		result          []apitoken.UserAPIToken
		queryFilter     bson.D = bson.D{}
		requestFilter   bson.D = bson.D{}
		paginationLimit *int64 = GetPaginationLimit(int64(req.PerPage))
	)

	findOptions := options.Find()

	findOptions.Limit = paginationLimit
	findOptions.Skip = GetPaginationSkip(int64(req.Page), paginationLimit)

	// generate query filter from request
	if req.Description != "" {
		queryFilter = append(queryFilter, bson.E{Key: "description", Value: primitive.Regex{
			Pattern: fmt.Sprintf(MongoRegexStringFormat, req.Description),
			Options: "i",
		},
		})
	}

	if req.Status != "" {
		queryFilter = append(queryFilter, bson.E{Key: "status", Value: primitive.Regex{
			Pattern: fmt.Sprintf(MongoRegexStringFormat, req.Status),
			Options: "i",
		},
		})
	}

	if req.CreatedByID != "" {
		queryFilter = append(queryFilter, bson.E{Key: "created_by_id", Value: req.CreatedByID})
	}

	if req.CreatedByNanoId != "" {
		queryFilter = append(queryFilter, bson.E{Key: "created_by_nid", Value: req.CreatedByNanoId})
	}

	if req.OnlyEphemeral {
		queryFilter = append(queryFilter, bson.E{Key: "ttl_expires_at", Value: bson.M{"$exists": true}})
	}

	if req.OnlyPermanent {
		queryFilter = append(queryFilter, bson.E{Key: "ttl_expires_at", Value: bson.M{"$exists": false}})
	}

	// generate sort filter from request
	switch req.Order {
	case "created_at_asc":
		requestFilter = append(requestFilter, bson.E{Key: "created_at", Value: 1})
	case "created_at_desc":
		requestFilter = append(requestFilter, bson.E{Key: "created_at", Value: -1})

	case "last_used_at_asc":
		requestFilter = append(requestFilter, bson.E{Key: "last_used_at", Value: 1})
	case "last_used_at_desc":
		requestFilter = append(requestFilter, bson.E{Key: "last_used_at", Value: -1})

	case "updated_at_asc":
		requestFilter = append(requestFilter, bson.E{Key: "updated_at", Value: 1})
	case "updated_at_desc":
		requestFilter = append(requestFilter, bson.E{Key: "updated_at", Value: -1})

	default:
		requestFilter = append(requestFilter, bson.E{Key: "created_at", Value: -1})
	}

	// Sort by request field
	findOptions.SetSort(requestFilter)

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

// DeleteAPITokenFor removes apitoken with passed ID from apitoken collection.
// Also updates user collection to remove reference
func (r MongoDbRepository) DeleteAPITokenFor(ctx context.Context, userID string, apiTokenID string) error {

	// NICE_TO_HAVE: Wrap context with observability platform transaction

	err := r.DeleteAPITokenByID(ctx, apiTokenID)
	if err != nil {
		return err
	}

	return nil
}
