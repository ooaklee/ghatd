package repository

import (
	"context"
	"errors"

	"github.com/ooaklee/ghatd/external/toolbox"
	"github.com/ooaklee/ghatd/external/user"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// userPurgeAPITokenByID attempts to pull api token from the passed user's embedded collection
// logs errors if any to not disrupt deletion
func (r MongoDbRepository) userPurgeAPITokenByID(ctx context.Context, userID string, apiTokenID string) {

	matchFilter := bson.M{"_id": userID}

	updateFilter := bson.M{"$pull": bson.M{"api_tokens": apiTokenID}, "$set": bson.M{"meta.updated_at": toolbox.TimeNowUTC()}}

	// NICE_TO_HAVE: Wrap context with observability platform transaction

	client, err := r.InitialiseClient(ctx)
	if err != nil {
		RepositoryLogEntry(ctx, logError, "failed-user-api-token-purge", err)
	}

	collection := r.GetUserCollection(client)

	err = ExecuteUpdateOneCommand(ctx, collection, matchFilter, updateFilter, "ApiToken")
	if err != nil {
		RepositoryLogEntry(ctx, logError, "failed-user-api-token-purge", err)
	}

}

// UpdateUser updates user passed in the DB
func (r MongoDbRepository) UpdateUser(ctx context.Context, user *user.User) (*user.User, error) {

	updateFilter := bson.M{"_id": user.ID}

	user.SetUpdatedAtTimeToNow()

	// NICE_TO_HAVE: Wrap context with observability platform transaction

	client, err := r.InitialiseClient(ctx)
	if err != nil {
		return nil, err
	}

	collection := r.GetUserCollection(client)

	err = ExecuteReplaceOneCommand(ctx, collection, updateFilter, user, "User")
	if err != nil {
		return nil, err
	}

	return user, nil

}

// DeleteUserByID deletes user from DB
func (r MongoDbRepository) DeleteUserByID(ctx context.Context, id string) error {

	deleteFilter := bson.M{"_id": id}

	// NICE_TO_HAVE: Wrap context with observability platform transaction

	client, err := r.InitialiseClient(ctx)
	if err != nil {
		return err
	}

	collection := r.GetUserCollection(client)

	return ExecuteDeleteOneCommand(ctx, collection, deleteFilter, "User")
}

// GetUserByNanoId is returning a user with matching nano Id
func (r MongoDbRepository) GetUserByNanoId(ctx context.Context, nanoId string) (*user.User, error) {
	var result user.User

	// NICE_TO_HAVE: Wrap context with observability platform transaction

	client, err := r.InitialiseClient(ctx)
	if err != nil {
		return nil, err
	}

	collection := r.GetUserCollection(client)

	err = ExecuteFindOneCommandDecodeResult(ctx, collection, bson.M{"_nano_id": nanoId}, &result, "User", true, user.ErrKeyResourceNotFound)
	if err != nil {
		return nil, err
	}

	return &result, nil
}

// GetUserByID returns the users with matching id
func (r MongoDbRepository) GetUserByID(ctx context.Context, id string) (*user.User, error) {
	var result user.User

	// NICE_TO_HAVE: Wrap context with observability platform transaction

	client, err := r.InitialiseClient(ctx)
	if err != nil {
		return nil, err
	}

	collection := r.GetUserCollection(client)

	err = ExecuteFindOneCommandDecodeResult(ctx, collection, bson.M{"_id": id}, &result, "User", true, user.ErrKeyResourceNotFound)
	if err != nil {
		return nil, err
	}

	return &result, nil

}

// GetUsers returns users matching filters from the DB
func (r MongoDbRepository) GetUsers(ctx context.Context, queryFilter bson.D, requestFilter *bson.D) ([]user.User, error) {
	var result []user.User
	findOptions := options.Find()

	// Sort by request field
	findOptions.SetSort(*requestFilter)

	// NICE_TO_HAVE: Wrap context with observability platform transaction

	client, err := r.InitialiseClient(ctx)
	if err != nil {
		return nil, err
	}

	collection := r.GetUserCollection(client)

	c, err := ExecuteFindCommand(ctx, collection, queryFilter, findOptions)
	if err != nil {
		return nil, err
	}

	if err = MapAllInCursorToResult(ctx, c, &result, "users"); err != nil {
		return nil, err
	}

	return result, nil
}

// GetSampleUser returns a random user from the DB
func (r MongoDbRepository) GetSampleUser(ctx context.Context) ([]user.User, error) {
	var result []user.User

	sampleStage := GenerateSampleFilter([]bson.D{}, 1)

	// NICE_TO_HAVE: Wrap context with observability platform transaction

	client, err := r.InitialiseClient(ctx)
	if err != nil {
		return nil, err
	}

	collection := r.GetUserCollection(client)

	c, err := ExecuteAggregateCommand(ctx, collection, sampleStage)
	if err != nil {
		return nil, err
	}

	if err = MapAllInCursorToResult(ctx, c, &result, "sample user(s)"); err != nil {
		return nil, err
	}

	return result, nil
}

// CreateUser creates an user in the DB
func (r MongoDbRepository) CreateUser(ctx context.Context, newUser *user.User) (*user.User, error) {

	// NICE_TO_HAVE: Wrap context with observability platform transaction

	client, err := r.InitialiseClient(ctx)
	if err != nil {
		return nil, err
	}

	collection := r.GetUserCollection(client)

	// Check user's email is new
	_, err = r.GetUserByEmail(ctx, newUser.Email, false)
	if err == nil {
		return nil, errors.New(user.ErrKeyResourceConflict)
	} else if err.Error() != user.ErrKeyResourceNotFound {
		return nil, err
	}

	newUser.GenerateNewUUID().GenerateNewNanoId().SetCreatedAtTimeToNow().SetInitialState()

	_, err = collection.InsertOne(ctx, newUser)
	if err != nil {
		return nil, err
	}

	return newUser, nil
}

// GetUserByEmail returns the user with matching email address
func (r MongoDbRepository) GetUserByEmail(ctx context.Context, email string, logError bool) (*user.User, error) {
	var result user.User

	// NICE_TO_HAVE: Wrap context with observability platform transaction

	client, err := r.InitialiseClient(ctx)
	if err != nil {
		return nil, err
	}

	collection := r.GetUserCollection(client)

	err = ExecuteFindOneCommandDecodeResult(ctx, collection, bson.M{"email": email}, &result, "User", logError, user.ErrKeyResourceNotFound)
	if err != nil {
		return nil, err
	}

	return &result, nil

}
