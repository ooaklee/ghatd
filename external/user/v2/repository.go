package user

import (
	"context"
	"errors"
	"strings"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// UserCollection collection name for users
const UserCollection string = "users"

// MongoDbStore represents the datastore to hold user data
type MongoDbStore interface {
	ExecuteCountDocuments(ctx context.Context, collection *mongo.Collection, filter interface{}, opts ...*options.CountOptions) (int64, error)
	ExecuteDeleteOneCommand(ctx context.Context, collection *mongo.Collection, filter interface{}, targetObjectName string) error
	ExecuteFindCommand(ctx context.Context, collection *mongo.Collection, filter interface{}, opts ...*options.FindOptions) (*mongo.Cursor, error)
	ExecuteInsertOneCommand(ctx context.Context, collection *mongo.Collection, document interface{}, resultObjectName string) (*mongo.InsertOneResult, error)
	ExecuteUpdateOneCommand(ctx context.Context, collection *mongo.Collection, filter interface{}, updateFilter interface{}, resultObjectName string) error
	ExecuteDeleteManyCommand(ctx context.Context, collection *mongo.Collection, filter interface{}, targetObjectName string) error
	ExecuteFindOneCommandDecodeResult(ctx context.Context, collection *mongo.Collection, filter interface{}, result interface{}, resultObjectName string, logError bool, onFailureErr error) error
	ExecuteAggregateCommand(ctx context.Context, collection *mongo.Collection, mongoPipeline []bson.D) (*mongo.Cursor, error)
	ExecuteReplaceOneCommand(ctx context.Context, collection *mongo.Collection, filter interface{}, replacementObject interface{}, resultObjectName string) error
	ExecuteUpdateManyCommand(ctx context.Context, collection *mongo.Collection, filter interface{}, updateFilter interface{}, resultObjectName string) error
	ExecuteInsertManyCommand(ctx context.Context, collection *mongo.Collection, documents []interface{}, resultObjectName string) (*mongo.InsertManyResult, error)

	GetDatabase(ctx context.Context, dbName string) (*mongo.Database, error)
	InitialiseClient(ctx context.Context) (*mongo.Client, error)
	MapAllInCursorToResult(ctx context.Context, cursor *mongo.Cursor, result interface{}, resultObjectName string) error
	MapOneInCursorToResult(ctx context.Context, cursor *mongo.Cursor, result interface{}, resultObjectName string) error
}

// Repository handles user data persistence
type Repository struct {
	Store MongoDbStore
}

// NewRepository creates a new user repository
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

// CreateUser creates a new user in the repository
func (r *Repository) CreateUser(ctx context.Context, user *UniversalUser) (*UniversalUser, error) {
	collection, err := r.GetUserCollection(ctx)
	if err != nil {
		return nil, err
	}

	_, err = r.Store.ExecuteInsertOneCommand(ctx, collection, user, "user")
	if err != nil {
		return nil, err
	}

	return user, nil
}

// GetUserByID retrieves a user by ID
func (r *Repository) GetUserByID(ctx context.Context, id string) (*UniversalUser, error) {
	collection, err := r.GetUserCollection(ctx)
	if err != nil {
		return nil, err
	}

	queryFilter := bson.M{
		"_id": id,
	}

	var result UniversalUser
	err = r.Store.ExecuteFindOneCommandDecodeResult(ctx, collection, queryFilter, &result, "user", true, errors.New(ErrKeyUserNotFound))
	if err != nil {
		return nil, err
	}

	return &result, nil
}

// GetUserByNanoID retrieves a user by nano ID
func (r *Repository) GetUserByNanoID(ctx context.Context, nanoID string) (*UniversalUser, error) {
	collection, err := r.GetUserCollection(ctx)
	if err != nil {
		return nil, err
	}

	queryFilter := bson.M{
		"_nano_id": nanoID,
	}

	var result UniversalUser
	err = r.Store.ExecuteFindOneCommandDecodeResult(ctx, collection, queryFilter, &result, "user", true, errors.New(ErrKeyUserNotFound))
	if err != nil {
		return nil, err
	}

	return &result, nil
}

// GetUserByEmail retrieves a user by email
func (r *Repository) GetUserByEmail(ctx context.Context, email string, logError bool) (*UniversalUser, error) {
	collection, err := r.GetUserCollection(ctx)
	if err != nil {
		return nil, err
	}

	queryFilter := bson.M{
		"email": normaliseUserEmail(email),
	}

	var result UniversalUser
	err = r.Store.ExecuteFindOneCommandDecodeResult(ctx, collection, queryFilter, &result, "user", logError, errors.New(ErrKeyUserNotFound))
	if err != nil {
		return nil, err
	}

	return &result, nil
}

// UpdateUser updates an existing user
func (r *Repository) UpdateUser(ctx context.Context, user *UniversalUser) (*UniversalUser, error) {
	collection, err := r.GetUserCollection(ctx)
	if err != nil {
		return nil, err
	}

	queryFilter := bson.M{
		"_id": user.ID,
	}

	update := bson.M{
		"$set": user,
	}

	err = r.Store.ExecuteUpdateOneCommand(ctx, collection, queryFilter, update, "user")
	if err != nil {
		return nil, err
	}

	return user, nil
}

// DeleteUserByID deletes a user by ID
func (r *Repository) DeleteUserByID(ctx context.Context, id string) error {
	collection, err := r.GetUserCollection(ctx)
	if err != nil {
		return err
	}

	queryFilter := bson.M{
		"_id": id,
	}

	err = r.Store.ExecuteDeleteOneCommand(ctx, collection, queryFilter, "user")
	return err
}

// GetUsers retrieves users with filters and pagination
func (r *Repository) GetUsers(ctx context.Context, req *GetUsersRequest) ([]UniversalUser, error) {
	collection, err := r.GetUserCollection(ctx)
	if err != nil {
		return nil, err
	}

	// Build query filter
	queryFilter := r.buildUserQueryFilter(req.EmailFilter, req.FirstNameFilter, req.LastNameFilter, req.StatusFilter, req.RoleFilter, req.RolesFilter, req.OnlyAdmin, req.EmailVerified, req.PhoneVerified, req.ExtensionKey, req.ExtensionValue)

	// Build sort options
	sortOptions := r.buildSortOptions(req.Order)

	// Calculate skip
	skip := int64((req.Page - 1) * req.PerPage)
	limit := int64(req.PerPage)

	options := options.Find().
		SetSort(sortOptions).
		SetSkip(skip).
		SetLimit(limit)

	cursor, err := r.Store.ExecuteFindCommand(ctx, collection, queryFilter, options)
	if err != nil {
		return nil, err
	}

	var results []UniversalUser
	err = r.Store.MapAllInCursorToResult(ctx, cursor, &results, "user")
	if err != nil {
		return nil, err
	}

	return results, nil
}

// GetTotalUsers retrieves the total count of users matching filters
func (r *Repository) GetTotalUsers(ctx context.Context, req *GetTotalUsersRequest) (int64, error) {
	collection, err := r.GetUserCollection(ctx)
	if err != nil {
		return 0, err
	}

	queryFilter := r.buildUserQueryFilter(req.EmailFilter, req.FirstNameFilter, req.LastNameFilter, req.StatusFilter, req.RoleFilter, req.RolesFilter, req.OnlyAdmin, req.EmailVerified, req.PhoneVerified, req.ExtensionKey, req.ExtensionValue)

	count, err := r.Store.ExecuteCountDocuments(ctx, collection, queryFilter)
	if err != nil {
		return 0, err
	}

	return count, nil
}

// GetUsersByRoles retrieves users by roles with pagination
func (r *Repository) GetUsersByRoles(ctx context.Context, roles []string, page, perPage int, order string) ([]UniversalUser, error) {
	collection, err := r.GetUserCollection(ctx)
	if err != nil {
		return nil, err
	}

	queryFilter := bson.M{
		"roles": bson.M{"$in": roles},
	}

	sortOptions := r.buildSortOptions(order)
	skip := int64((page - 1) * perPage)
	limit := int64(perPage)

	options := options.Find().
		SetSort(sortOptions).
		SetSkip(skip).
		SetLimit(limit)

	cursor, err := r.Store.ExecuteFindCommand(ctx, collection, queryFilter, options)
	if err != nil {
		return nil, err
	}

	var results []UniversalUser
	err = r.Store.MapAllInCursorToResult(ctx, cursor, &results, "user")
	if err != nil {
		return nil, err
	}

	return results, nil
}

// GetUsersByStatus retrieves users by status with pagination
func (r *Repository) GetUsersByStatus(ctx context.Context, status string, page, perPage int, order string) ([]UniversalUser, error) {
	collection, err := r.GetUserCollection(ctx)
	if err != nil {
		return nil, err
	}

	queryFilter := bson.M{
		"status": status,
	}

	sortOptions := r.buildSortOptions(order)
	skip := int64((page - 1) * perPage)
	limit := int64(perPage)

	options := options.Find().
		SetSort(sortOptions).
		SetSkip(skip).
		SetLimit(limit)

	cursor, err := r.Store.ExecuteFindCommand(ctx, collection, queryFilter, options)
	if err != nil {
		return nil, err
	}

	var results []UniversalUser
	err = r.Store.MapAllInCursorToResult(ctx, cursor, &results, "user")
	if err != nil {
		return nil, err
	}

	return results, nil
}

// SearchUsersByExtension searches users by extension field
func (r *Repository) SearchUsersByExtension(ctx context.Context, key string, value interface{}, page, perPage int) ([]UniversalUser, error) {
	collection, err := r.GetUserCollection(ctx)
	if err != nil {
		return nil, err
	}

	queryFilter := bson.M{
		"extensions." + key: value,
	}

	skip := int64((page - 1) * perPage)
	limit := int64(perPage)

	options := options.Find().
		SetSkip(skip).
		SetLimit(limit)

	cursor, err := r.Store.ExecuteFindCommand(ctx, collection, queryFilter, options)
	if err != nil {
		return nil, err
	}

	var results []UniversalUser
	err = r.Store.MapAllInCursorToResult(ctx, cursor, &results, "user")
	if err != nil {
		return nil, err
	}

	return results, nil
}

// Helper methods

// buildUserQueryFilter builds a query filter for user searches
func (r *Repository) buildUserQueryFilter(emailFilter, firstNameFilter, lastNameFilter, statusFilter, roleFilter string, rolesFilter []string, onlyAdmin bool, emailVerified, phoneVerified *bool, extensionKey string, extensionValue interface{}) bson.M {
	queryFilter := bson.M{}

	if emailFilter != "" {
		queryFilter["email"] = bson.M{
			"$regex":   emailFilter,
			"$options": "i",
		}
	}

	if firstNameFilter != "" {
		queryFilter["personal_info.first_name"] = bson.M{
			"$regex":   firstNameFilter,
			"$options": "i",
		}
	}

	if lastNameFilter != "" {
		queryFilter["personal_info.last_name"] = bson.M{
			"$regex":   lastNameFilter,
			"$options": "i",
		}
	}

	if statusFilter != "" {
		queryFilter["status"] = statusFilter
	}

	if roleFilter != "" {
		queryFilter["roles"] = roleFilter
	}

	if len(rolesFilter) > 0 {
		queryFilter["roles"] = bson.M{"$in": rolesFilter}
	}

	if onlyAdmin {
		queryFilter["roles"] = UserRoleAdmin
	}

	if emailVerified != nil {
		queryFilter["verification.email_verified"] = *emailVerified
	}

	if phoneVerified != nil {
		queryFilter["verification.phone_verified"] = *phoneVerified
	}

	if extensionKey != "" {
		if extensionValue != nil {
			queryFilter["extensions."+extensionKey] = extensionValue
		}
	}

	return queryFilter
}

// buildSortOptions builds sort options based on order string
func (r *Repository) buildSortOptions(order string) bson.D {
	switch order {
	case GetUserOrderCreatedAtAsc:
		return bson.D{{Key: "metadata.created_at", Value: 1}}
	case GetUserOrderCreatedAtDesc:
		return bson.D{{Key: "metadata.created_at", Value: -1}}
	case GetUserOrderUpdatedAtAsc:
		return bson.D{{Key: "metadata.updated_at", Value: 1}}
	case GetUserOrderUpdatedAtDesc:
		return bson.D{{Key: "metadata.updated_at", Value: -1}}
	case GetUserOrderLastLoginAtAsc:
		return bson.D{{Key: "metadata.last_login_at", Value: 1}}
	case GetUserOrderLastLoginAtDesc:
		return bson.D{{Key: "metadata.last_login_at", Value: -1}}
	case GetUserOrderActivatedAtAsc:
		return bson.D{{Key: "metadata.activated_at", Value: 1}}
	case GetUserOrderActivatedAtDesc:
		return bson.D{{Key: "metadata.activated_at", Value: -1}}
	case GetUserOrderStatusChangedAtAsc:
		return bson.D{{Key: "metadata.status_changed_at", Value: 1}}
	case GetUserOrderStatusChangedAtDesc:
		return bson.D{{Key: "metadata.status_changed_at", Value: -1}}
	case GetUserOrderEmailVerifiedAtAsc:
		return bson.D{{Key: "verification.email_verified_at", Value: 1}}
	case GetUserOrderEmailVerifiedAtDesc:
		return bson.D{{Key: "verification.email_verified_at", Value: -1}}
	default:
		return bson.D{{Key: "metadata.created_at", Value: -1}}
	}
}

// normaliseUserEmail standardises email to lowercase
func normaliseUserEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}
