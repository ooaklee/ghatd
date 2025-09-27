package apitoken

import (
	"context"
	"errors"
	"fmt"

	"github.com/ooaklee/ghatd/external/repository"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const (
	// MongoRegexStringFormat holds format string for case insensitive regex mapping
	// in mongo queries
	MongoRegexStringFormat = ".*%s.*"
)

// ApiTokenCollection collection name for api tokens
const ApiTokenCollection string = "apitokens"

// MongoDbStore represents the datastore to hold resource data
type MongoDbStore interface {
	ExecuteCountDocuments(ctx context.Context, collection *mongo.Collection, filter interface{}, opts ...*options.CountOptions) (int64, error)
	ExecuteDeleteOneCommand(ctx context.Context, collection *mongo.Collection, filter interface{}, targetObjectName string) error
	ExecuteFindCommand(ctx context.Context, collection *mongo.Collection, filter interface{}, opts ...*options.FindOptions) (*mongo.Cursor, error)
	ExecuteInsertOneCommand(ctx context.Context, collection *mongo.Collection, document interface{}, resultObjectName string) (*mongo.InsertOneResult, error)
	ExecuteUpdateOneCommand(ctx context.Context, collection *mongo.Collection, filter interface{}, updateFilter interface{}, resultObjectName string) error
	ExecuteDeleteManyCommand(ctx context.Context, collection *mongo.Collection, filter interface{}, targetObjectName string) error
	ExecuteFindOneCommandDecodeResult(ctx context.Context, collection *mongo.Collection, filter interface{}, result interface{}, resultObjectName string, logError bool, onFailureErr error) error
	// ExecuteAggregateCommand(ctx context.Context, collection *mongo.Collection, mongoPipeline []bson.D) (*mongo.Cursor, error)
	// ExecuteReplaceOneCommand(ctx context.Context, collection *mongo.Collection, filter interface{}, replacementObject interface{}, resultObjectName string) error
	// ExecuteUpdateManyCommand(ctx context.Context, collection *mongo.Collection, filter interface{}, updateFilter interface{}, resultObjectName string) error
	// ExecuteInsertManyCommand(ctx context.Context, collection *mongo.Collection, documents []interface{}, resultObjectName string) (*mongo.InsertManyResult, error)

	GetDatabase(ctx context.Context, dbName string) (*mongo.Database, error)
	InitialiseClient(ctx context.Context) (*mongo.Client, error)
	MapAllInCursorToResult(ctx context.Context, cursor *mongo.Cursor, result interface{}, resultObjectName string) error
	MapOneInCursorToResult(ctx context.Context, cursor *mongo.Cursor, result interface{}, resultObjectName string) error
}

// Repository represents the datastore to hold resource data
type Repository struct {
	Store MongoDbStore
}

// NewRepository initiates new instance of repository
func NewRepository(store MongoDbStore) *Repository {
	return &Repository{
		Store: store,
	}
}

// GetApiTokenCollection returns collection used for api token domain
func (r *Repository) GetApiTokenCollection(ctx context.Context) (*mongo.Collection, error) {

	_, err := r.Store.InitialiseClient(ctx)
	if err != nil {
		return nil, err
	}

	db, err := r.Store.GetDatabase(ctx, "")
	if err != nil {
		return nil, err
	}
	collection := db.Collection(ApiTokenCollection)

	return collection, nil
}

// DeleteResourcesByOwnerId deletes all token resources that belongs to the specified user id
func (r *Repository) DeleteResourcesByOwnerId(ctx context.Context, ownerId string) error {

	var filter bson.M

	filter = bson.M{"created_by_id": ownerId}

	collection, err := r.GetApiTokenCollection(ctx)
	if err != nil {
		return err
	}

	err = r.Store.ExecuteDeleteManyCommand(ctx, collection, filter, "ApiTokens")
	if err != nil {
		return err
	}

	return nil
}

// GetTotalApiTokens total api token from DB that match passed arguments
func (r *Repository) GetTotalApiTokens(ctx context.Context, userId, userNanoId, descriptionFilter, statusFilter, to, from string, onlyEphemeral bool, onlyPermanent bool) (int64, error) {

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

	collection, err := r.GetApiTokenCollection(ctx)
	if err != nil {
		return 0, err
	}

	return r.Store.ExecuteCountDocuments(ctx, collection, apiTokenFilter)
}

// CreateUserAPIToken creates an user apitoken in the DB
func (r *Repository) CreateUserAPIToken(ctx context.Context, apiToken *UserAPIToken) (*UserAPIToken, error) {

	collection, err := r.GetApiTokenCollection(ctx)
	if err != nil {
		return nil, err
	}

	apiToken.Generate().GenerateNewCodename().GenerateNewUUID()

	_, err = r.Store.ExecuteInsertOneCommand(ctx, collection, apiToken, "api-token")
	if err != nil {
		return nil, err
	}

	return apiToken, nil
}

// UpdateAPIToken updates apitoken passed in the DB
func (r *Repository) UpdateAPIToken(ctx context.Context, apiToken *UserAPIToken) (*UserAPIToken, error) {

	collection, err := r.GetApiTokenCollection(ctx)
	if err != nil {
		return nil, err
	}

	apiToken.SetUpdatedAtTimeToNow()

	err = r.Store.ExecuteUpdateOneCommand(ctx, collection, bson.M{"_id": apiToken.ID}, bson.M{"$set": apiToken}, "api-token")
	if err != nil {
		return nil, err
	}

	return apiToken, nil

}

// DeleteAPITokenByID removes passed api token ID from DB
func (r *Repository) DeleteAPITokenByID(ctx context.Context, apiTokenID string) error {
	deleteFilter := bson.M{"_id": apiTokenID}

	collection, err := r.GetApiTokenCollection(ctx)
	if err != nil {
		return err
	}

	return r.Store.ExecuteDeleteOneCommand(ctx, collection, deleteFilter, "ApiToken")

}

// GetAPITokenByID returns the apitoken with matching id
func (r *Repository) GetAPITokenByID(ctx context.Context, apiTokenID string) (*UserAPIToken, error) {
	var result UserAPIToken

	collection, err := r.GetApiTokenCollection(ctx)
	if err != nil {
		return nil, err
	}

	err = r.Store.ExecuteFindOneCommandDecodeResult(ctx, collection, bson.M{"_id": apiTokenID}, &result, "ApiToken", true, errors.New(ErrKeyResourceNotFound))
	if err != nil {
		return nil, err
	}

	return &result, nil

}

// GetAPITokens returns apitokens matching filters from the DB
func (r *Repository) GetAPITokens(ctx context.Context, req *GetAPITokensRequest) ([]UserAPIToken, error) {
	var (
		result          []UserAPIToken
		queryFilter     bson.D = bson.D{}
		requestFilter   bson.D = bson.D{}
		paginationLimit *int64 = repository.GetPaginationLimit(int64(req.PerPage))
	)

	findOptions := options.Find()

	findOptions.Limit = paginationLimit
	findOptions.Skip = repository.GetPaginationSkip(int64(req.Page), paginationLimit)

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

	collection, err := r.GetApiTokenCollection(ctx)
	if err != nil {
		return nil, err
	}

	c, err := r.Store.ExecuteFindCommand(ctx, collection, queryFilter, findOptions)
	if err != nil {
		return nil, err
	}

	if err = r.Store.MapAllInCursorToResult(ctx, c, &result, "apitoken"); err != nil {
		return nil, err
	}

	return result, nil
}

// DeleteAPITokenFor removes apitoken with passed ID from apitoken collection.
// Also updates user collection to remove reference
func (r *Repository) DeleteAPITokenFor(ctx context.Context, userID string, apiTokenID string) error {

	err := r.DeleteAPITokenByID(ctx, apiTokenID)
	if err != nil {
		return err
	}

	return nil
}
