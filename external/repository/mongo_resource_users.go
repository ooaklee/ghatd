package repository

import (
	"context"
	"errors"

	"github.com/ooaklee/ghatd/external/common"
	"github.com/ooaklee/ghatd/external/toolbox"
	"github.com/ooaklee/ghatd/external/user"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// GetTotalUsers total users from DB that match passed arguments
func (r MongoDbRepository) GetTotalUsers(ctx context.Context, firstNameFilter, lastNameFilter, emailFilter, status string, onlyAdmin bool) (int64, error) {

	// Example mongo query
	// /// get user total
	// db.getCollection("users").countDocuments({_id : { $ne : null }})
	// /// get user with first name x total
	// db.getCollection("users").countDocuments({_id : { $ne : null }, first_name: "x"})
	// /// get active admin user total
	// db.getCollection("users").countDocuments({_id : { $ne : null }, status: { $in : ["ACTIVE"] }, roles: { $in : ["ADMIN"] }})

	userFilter := bson.M{"_id": bson.M{"$exists": true}}

	if firstNameFilter != "" {
		userFilter["first_name"] = bson.M{"$in": []string{toolbox.StringConvertToTitleCase(firstNameFilter)}}
	}

	if lastNameFilter != "" {
		userFilter["last_name"] = toolbox.StringConvertToTitleCase(lastNameFilter)
	}

	if emailFilter != "" {
		userFilter["email"] = bson.M{"$in": []string{toolbox.StringStandardisedToLower(emailFilter)}}
	}

	if status != "" {
		userFilter["status"] = bson.M{"$in": []string{toolbox.StringStandardisedToUpper(status)}}
	}

	if onlyAdmin {
		userFilter["roles"] = bson.M{"$in": []string{string(common.UserRoleAdmin)}}
	}

	client, err := r.InitialiseClient(ctx)
	if err != nil {
		return 0, err
	}

	collection := r.GetUserCollection(client)

	return ExecuteCountDocuments(ctx, collection, userFilter)
}

// UpdateUser updates user passed in the DB
func (r MongoDbRepository) UpdateUser(ctx context.Context, user *user.User) (*user.User, error) {

	// NICE_TO_HAVE: Wrap context with observability platform transaction

	client, err := r.InitialiseClient(ctx)
	if err != nil {
		return nil, err
	}

	collection := r.GetUserCollection(client)

	user.SetUpdatedAtTimeToNow()

	err = ExecuteUpdateOneCommand(ctx, collection, bson.M{"_id": user.ID}, bson.M{"$set": user}, "user")
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
func (r MongoDbRepository) GetUsers(ctx context.Context, req *user.GetUsersRequest) ([]user.User, error) {
	var (
		result          []user.User
		queryFilter     bson.D = bson.D{}
		requestFilter   bson.D = bson.D{}
		paginationLimit *int64 = GetPaginationLimit(int64(req.PerPage))
	)

	findOptions := options.Find()

	findOptions.Limit = paginationLimit
	findOptions.Skip = GetPaginationSkip(int64(req.Page), paginationLimit)

	// generate query filter from request
	if req.FirstName != "" {
		queryFilter = append(queryFilter, bson.E{Key: "first_name", Value: bson.M{"$in": []string{toolbox.StringConvertToTitleCase(req.FirstName)}}})
	}

	if req.LastName != "" {
		queryFilter = append(queryFilter, bson.E{Key: "last_name", Value: toolbox.StringConvertToTitleCase(req.LastName)})
	}

	if req.Status != "" {
		queryFilter = append(queryFilter, bson.E{Key: "status", Value: bson.M{"$in": []string{toolbox.StringStandardisedToUpper(req.Status)}}})
	}

	if req.IsAdmin {
		queryFilter = append(queryFilter, bson.E{Key: "roles", Value: bson.M{"$in": []string{string(common.UserRoleAdmin)}}})
	}

	if req.Email != "" {
		queryFilter = append(queryFilter, bson.E{Key: "email", Value: toolbox.StringStandardisedToLower(req.Email)})
	}

	// generate sort filter from request
	switch req.Order {
	case "created_at_asc":
		requestFilter = append(requestFilter, bson.E{Key: "meta.created_at", Value: 1})
	case "created_at_desc":
		requestFilter = append(requestFilter, bson.E{Key: "meta.created_at", Value: -1})

	case "last_login_at_asc":
		requestFilter = append(requestFilter, bson.E{Key: "meta.last_login_at", Value: 1})
	case "last_login_at_desc":
		requestFilter = append(requestFilter, bson.E{Key: "meta.last_login_at", Value: -1})

	case "activated_at_asc":
		requestFilter = append(requestFilter, bson.E{Key: "meta.activated_at", Value: 1})
	case "activated_at_desc":
		requestFilter = append(requestFilter, bson.E{Key: "meta.activated_at", Value: -1})

	case "status_changed_at_asc":
		requestFilter = append(requestFilter, bson.E{Key: "meta.status_changed_at", Value: 1})
	case "status_changed_at_desc":
		requestFilter = append(requestFilter, bson.E{Key: "meta.status_changed_at", Value: -1})

	case "last_fresh_login_at_asc":
		requestFilter = append(requestFilter, bson.E{Key: "meta.last_fresh_login_at", Value: 1})
	case "last_fresh_login_at_desc":
		requestFilter = append(requestFilter, bson.E{Key: "meta.last_fresh_login_at", Value: -1})

	case "email_verified_at_asc":
		requestFilter = append(requestFilter, bson.E{Key: "meta.last_fresh_login_at", Value: 1})
	case "email_verified_at_desc":
		requestFilter = append(requestFilter, bson.E{Key: "meta.last_fresh_login_at", Value: -1})

	default:
		requestFilter = append(requestFilter, bson.E{Key: "meta.created_at", Value: -1})
	}

	// Sort by request field
	findOptions.SetSort(requestFilter)

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
