package user

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/ooaklee/ghatd/external/toolbox"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// UserCollection collection name for users
const UserCollection string = "users"

const defaultCollectionInitMaxAttemptsLimit = 3

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
	Store                          MongoDbStore
	collectionInitMaxAttemptsLimit int

	collection      *mongo.Collection
	collectionMutex sync.Mutex
}

// NewRepository creates a new user repository
func NewRepository(store MongoDbStore) *Repository {
	return &Repository{
		Store:                          store,
		collectionInitMaxAttemptsLimit: defaultCollectionInitMaxAttemptsLimit,
	}
}

// WithCollectionInitMaxAttemptsLimit overrides the number of collection initialisation attempts.
func (r *Repository) WithCollectionInitMaxAttemptsLimit(limit int) *Repository {
	if limit > 0 {
		r.collectionInitMaxAttemptsLimit = limit
	}

	return r
}

// GetUserCollection returns collection used for users domain
func (r *Repository) GetUserCollection(ctx context.Context) (*mongo.Collection, error) {
	r.collectionMutex.Lock()
	defer r.collectionMutex.Unlock()

	if r.collection != nil {
		return r.collection, nil
	}

	var lastErr error
	collectionInitMaxAttemptsLimit := r.collectionInitMaxAttemptsLimit
	if collectionInitMaxAttemptsLimit <= 0 {
		collectionInitMaxAttemptsLimit = defaultCollectionInitMaxAttemptsLimit
	}
	for attempt := 1; attempt <= collectionInitMaxAttemptsLimit; attempt++ {
		_, err := r.Store.InitialiseClient(ctx)
		if err != nil {
			lastErr = err
			continue
		}

		db, err := r.Store.GetDatabase(ctx, "")
		if err != nil {
			lastErr = err
			continue
		}

		r.collection = db.Collection(UserCollection)
		return r.collection, nil
	}

	return nil, fmt.Errorf("%w: unable to initialise %s collection after %d attempts: %w", ErrDatabaseError, UserCollection, collectionInitMaxAttemptsLimit, lastErr)
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
	err = r.Store.ExecuteFindOneCommandDecodeResult(ctx, collection, queryFilter, &result, "user", true, ErrUserNotFound)
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
	err = r.Store.ExecuteFindOneCommandDecodeResult(ctx, collection, queryFilter, &result, "user", true, ErrUserNotFound)
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
	err = r.Store.ExecuteFindOneCommandDecodeResult(ctx, collection, queryFilter, &result, "user", logError, ErrUserNotFound)
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
	queryFilter := r.buildUserQueryFilter(req.EmailFilter, "", req.FirstNameFilter, req.LastNameFilter, req.StatusFilter, req.RoleFilter, req.IDsFilter, req.RolesFilter, req.OnlyAdmin, req.EmailVerified, req.PhoneVerified, req.ExtensionKey, req.ExtensionValue)

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

	queryFilter := r.buildUserQueryFilter(req.EmailFilter, req.EmailRegex, req.FirstNameFilter, req.LastNameFilter, req.StatusFilter, req.RoleFilter, req.IDsFilter, req.RolesFilter, req.OnlyAdmin, req.EmailVerified, req.PhoneVerified, req.ExtensionKey, req.ExtensionValue)

	count, err := r.Store.ExecuteCountDocuments(ctx, collection, queryFilter)
	if err != nil {
		return 0, err
	}

	return count, nil
}

// GetUserStatsCounts retrieves per-status user counts in a single round-trip using
// a $facet aggregation, giving a consistent point-in-time snapshot.
func (r *Repository) GetUserStatsCounts(ctx context.Context, req *GetUserStatsRequest) (*UserStats, error) {
	collection, err := r.GetUserCollection(ctx)
	if err != nil {
		return nil, err
	}

	// Build the base match stage from the email regex filter (if provided).
	matchStage := bson.D{}
	if req.WithEmailRegex != "" {
		matchStage = bson.D{{Key: "email", Value: bson.M{"$regex": req.WithEmailRegex}}}
	}

	statusCountPipeline := func(status string) bson.A {
		return bson.A{
			bson.D{{Key: "$match", Value: bson.M{"status": status}}},
			bson.D{{Key: "$count", Value: "count"}},
		}
	}

	pipeline := []bson.D{
		{{Key: "$match", Value: matchStage}},
		{{Key: "$facet", Value: bson.D{
			{Key: AccountStatusKeyProvisioned, Value: statusCountPipeline(AccountStatusKeyProvisioned)},
			{Key: AccountStatusKeyActive, Value: statusCountPipeline(AccountStatusKeyActive)},
			{Key: AccountStatusKeyDeactivated, Value: statusCountPipeline(AccountStatusKeyDeactivated)},
			{Key: AccountStatusKeyLockedOut, Value: statusCountPipeline(AccountStatusKeyLockedOut)},
			{Key: AccountStatusKeyRecovery, Value: statusCountPipeline(AccountStatusKeyRecovery)},
			{Key: AccountStatusKeySuspended, Value: statusCountPipeline(AccountStatusKeySuspended)},
		}}},
	}

	cursor, err := r.Store.ExecuteAggregateCommand(ctx, collection, pipeline)
	if err != nil {
		return nil, err
	}

	// $facet always returns exactly one document.
	type countDoc struct {
		Count int64 `bson:"count"`
	}
	type facetResult struct {
		Provisioned []countDoc `bson:"PROVISIONED"`
		Active      []countDoc `bson:"ACTIVE"`
		Deactivated []countDoc `bson:"DEACTIVATED"`
		LockedOut   []countDoc `bson:"LOCKED_OUT"`
		Recovery    []countDoc `bson:"RECOVERY"`
		Suspended   []countDoc `bson:"SUSPENDED"`
	}

	var result facetResult
	if err := r.Store.MapOneInCursorToResult(ctx, cursor, &result, "user-stats-counts"); err != nil {
		return nil, err
	}

	extractCount := func(docs []countDoc) int64 {
		if len(docs) == 0 {
			return 0
		}
		return docs[0].Count
	}

	byStatus := &UserStatusStats{
		Provisioned: extractCount(result.Provisioned),
		Active:      extractCount(result.Active),
		Deactivated: extractCount(result.Deactivated),
		LockedOut:   extractCount(result.LockedOut),
		Recovery:    extractCount(result.Recovery),
		Suspended:   extractCount(result.Suspended),
	}

	return &UserStats{
		Total:    byStatus.CalculateTotal(),
		ByStatus: *byStatus,
	}, nil
}

// Helper methods

// buildUserQueryFilter builds a query filter for user searches
func (r *Repository) buildUserQueryFilter(emailFilter, emailRegex, firstNameFilter, lastNameFilter, statusFilter, roleFilter string, idsFilter, rolesFilter []string, onlyAdmin bool, emailVerified, phoneVerified *bool, extensionKey string, extensionValue interface{}) bson.M {
	queryFilter := bson.M{}

	if emailRegex != "" {
		queryFilter["email"] = bson.M{
			"$regex": emailRegex,
		}
	} else if emailFilter != "" {
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
		queryFilter["status"] = toolbox.StringStandardisedToUpper(statusFilter)
	}

	if roleFilter != "" {
		queryFilter["roles"] = roleFilter
	}

	if len(idsFilter) > 0 {
		queryFilter["_id"] = bson.M{"$in": idsFilter}
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
		} else {
			queryFilter["extensions."+extensionKey] = bson.M{"$exists": true}
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
