package user

import (
	"context"
	"errors"

	"github.com/ooaklee/ghatd/external/common"
	"github.com/ooaklee/ghatd/external/repository"
	"github.com/ooaklee/ghatd/external/toolbox"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const UserCollection = "users"

// MongoDbStore represents the datastore to hold resource data
type MongoDbStore interface {
	ExecuteCountDocuments(ctx context.Context, collection *mongo.Collection, filter interface{}, opts ...*options.CountOptions) (int64, error)
	ExecuteDeleteOneCommand(ctx context.Context, collection *mongo.Collection, filter interface{}, targetObjectName string) error
	ExecuteFindCommand(ctx context.Context, collection *mongo.Collection, filter interface{}, opts ...*options.FindOptions) (*mongo.Cursor, error)
	ExecuteInsertOneCommand(ctx context.Context, collection *mongo.Collection, document interface{}, resultObjectName string) (*mongo.InsertOneResult, error)
	ExecuteUpdateOneCommand(ctx context.Context, collection *mongo.Collection, filter interface{}, updateFilter interface{}, resultObjectName string) error
	ExecuteDeleteManyCommand(ctx context.Context, collection *mongo.Collection, filter interface{}, targetObjectName string) error
	ExecuteFindOneCommandDecodeResult(ctx context.Context, collection *mongo.Collection, filter interface{}, result interface{}, resultObjectName string, logError bool, onFailureErr error) error
	ExecuteAggregateCommand(ctx context.Context, collection *mongo.Collection, mongoPipeline []bson.D) (*mongo.Cursor, error)
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

// GetUserCollection returns collection used for users domain
func (r *Repository) GetUserCollection(ctx context.Context) (*mongo.Collection, error) {

	_, err := r.Store.InitialiseClient(ctx)
	if err != nil {
		return nil, err
	}

	db, err := r.Store.GetDatabase(ctx, "")
	if err != nil {
		return nil, err
	}
	collection := db.Collection(UserCollection)

	return collection, nil
}

// GetTotalUsers total users from DB that match passed arguments
func (r *Repository) GetTotalUsers(ctx context.Context, firstNameFilter, lastNameFilter, emailFilter, status string, onlyAdmin bool) (int64, error) {

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

	collection, err := r.GetUserCollection(ctx)
	if err != nil {
		return 0, err
	}

	return r.Store.ExecuteCountDocuments(ctx, collection, userFilter)
}

// UpdateUser updates user passed in the DB
func (r *Repository) UpdateUser(ctx context.Context, user *User) (*User, error) {

	collection, err := r.GetUserCollection(ctx)
	if err != nil {
		return nil, err
	}

	user.SetUpdatedAtTimeToNow()

	err = r.Store.ExecuteUpdateOneCommand(ctx, collection, bson.M{"_id": user.ID}, bson.M{"$set": user}, "user")
	if err != nil {
		return nil, err
	}

	return user, nil

}

// DeleteUserByID deletes user from DB
func (r *Repository) DeleteUserByID(ctx context.Context, id string) error {

	deleteFilter := bson.M{"_id": id}

	collection, err := r.GetUserCollection(ctx)
	if err != nil {
		return err
	}

	return r.Store.ExecuteDeleteOneCommand(ctx, collection, deleteFilter, "User")
}

// GetUserByNanoId is returning a user with matching nano Id
func (r *Repository) GetUserByNanoId(ctx context.Context, nanoId string) (*User, error) {
	var result User

	collection, err := r.GetUserCollection(ctx)
	if err != nil {
		return nil, err
	}

	err = r.Store.ExecuteFindOneCommandDecodeResult(ctx, collection, bson.M{"_nano_id": nanoId}, &result, "User", true, errors.New(ErrKeyResourceNotFound))
	if err != nil {
		return nil, err
	}

	return &result, nil
}

// GetUserByID returns the users with matching id
func (r *Repository) GetUserByID(ctx context.Context, id string) (*User, error) {
	var result User

	collection, err := r.GetUserCollection(ctx)
	if err != nil {
		return nil, err
	}

	err = r.Store.ExecuteFindOneCommandDecodeResult(ctx, collection, bson.M{"_id": id}, &result, "User", true, errors.New(ErrKeyResourceNotFound))
	if err != nil {
		return nil, err
	}

	return &result, nil

}

// GetUsers returns users matching filters from the DB
func (r *Repository) GetUsers(ctx context.Context, req *GetUsersRequest) ([]User, error) {
	var (
		result          []User
		queryFilter     bson.D = bson.D{}
		requestFilter   bson.D = bson.D{}
		paginationLimit *int64 = repository.GetPaginationLimit(int64(req.PerPage))
	)

	findOptions := options.Find()

	findOptions.Limit = paginationLimit
	findOptions.Skip = repository.GetPaginationSkip(int64(req.Page), paginationLimit)

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

	collection, err := r.GetUserCollection(ctx)
	if err != nil {
		return nil, err
	}

	c, err := r.Store.ExecuteFindCommand(ctx, collection, queryFilter, findOptions)
	if err != nil {
		return nil, err
	}

	if err = r.Store.MapAllInCursorToResult(ctx, c, &result, "users"); err != nil {
		return nil, err
	}

	return result, nil
}

// GetSampleUser returns a random user from the DB
func (r *Repository) GetSampleUser(ctx context.Context) ([]User, error) {
	var result []User

	sampleStage := generateSampleFilter([]bson.D{}, 1)

	collection, err := r.GetUserCollection(ctx)
	if err != nil {
		return nil, err
	}

	c, err := r.Store.ExecuteAggregateCommand(ctx, collection, sampleStage)
	if err != nil {
		return nil, err
	}

	if err = r.Store.MapAllInCursorToResult(ctx, c, &result, "users"); err != nil {
		return nil, err
	}

	return result, nil
}

// CreateUser creates an user in the DB
func (r *Repository) CreateUser(ctx context.Context, newUser *User) (*User, error) {

	collection, err := r.GetUserCollection(ctx)
	if err != nil {
		return nil, err
	}

	// Check user's email is new
	_, err = r.GetUserByEmail(ctx, newUser.Email, false)
	if err == nil {
		return nil, errors.New(ErrKeyResourceConflict)
	} else if err.Error() != ErrKeyResourceNotFound {
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
func (r *Repository) GetUserByEmail(ctx context.Context, email string, logError bool) (*User, error) {
	var result User

	collection, err := r.GetUserCollection(ctx)
	if err != nil {
		return nil, err
	}

	err = r.Store.ExecuteFindOneCommandDecodeResult(ctx, collection, bson.M{"email": email}, &result, "User", logError, errors.New(ErrKeyResourceNotFound))
	if err != nil {
		return nil, err
	}

	return &result, nil

}

// generateSampleFilter returns the filter used to pull 1 sample document from collection. Without a query filter,
// sample uses entire DB.
func generateSampleFilter(queryFilter []bson.D, sampleSize int) []bson.D {

	finalisedFilter := []bson.D{}

	sampleAggregation := bson.E{Key: "$sample", Value: bson.D{bson.E{Key: "size", Value: sampleSize}}}

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
