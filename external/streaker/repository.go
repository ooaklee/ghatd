package streaker

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

const defaultCollectionInitMaxAttemptsLimit = 3

// MongoDbStore represents the datastore methods needed by streaker.
type MongoDbStore interface {
	// ExecuteCountDocuments counts documents in the given MongoDB collection
	// matching the filter, applying any count options, and returns the count.
	ExecuteCountDocuments(ctx context.Context, collection *mongo.Collection, filter interface{}, opts ...options.Lister[options.CountOptions]) (int64, error)
	// ExecuteFindCommand runs a find query on the given collection with the
	// supplied filter and options, returning a cursor over matching documents.
	ExecuteFindCommand(ctx context.Context, collection *mongo.Collection, filter interface{}, opts ...options.Lister[options.FindOptions]) (*mongo.Cursor, error)
	// ExecuteInsertOneCommand inserts the document into the given collection, using
	// resultObjectName for error reporting, and returns the insert result.
	ExecuteInsertOneCommand(ctx context.Context, collection *mongo.Collection, document interface{}, resultObjectName string) (*mongo.InsertOneResult, error)
	// ExecuteFindOneCommandDecodeResult finds one document matching the filter and
	// decodes it into result, optionally logging errors and returning onFailureErr
	// on failure.
	ExecuteFindOneCommandDecodeResult(ctx context.Context, collection *mongo.Collection, filter interface{}, result interface{}, resultObjectName string, logError bool, onFailureErr error) error

	// GetDatabase resolves the named database handle from the underlying client for
	// datastore access.
	GetDatabase(ctx context.Context, dbName string) (*mongo.Database, error)
	// InitialiseClient establishes the underlying MongoDB client connection and
	// returns it for subsequent operations.
	InitialiseClient(ctx context.Context) (*mongo.Client, error)
	// MapAllInCursorToResult iterates the cursor and decodes every document into
	// result, using resultObjectName for error reporting.
	MapAllInCursorToResult(ctx context.Context, cursor *mongo.Cursor, result interface{}, resultObjectName string) error
	// MapOneInCursorToResult decodes the single document held by the cursor into
	// result, using resultObjectName for error reporting.
	MapOneInCursorToResult(ctx context.Context, cursor *mongo.Cursor, result interface{}, resultObjectName string) error
}

// Repository represents the datastore to hold streak data.
type Repository struct {
	Store                          MongoDbStore
	collectionInitMaxAttemptsLimit int

	collection      *mongo.Collection
	collectionMutex sync.Mutex
}

// NewRepository initiates a new instance of repository.
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

// GetStreakCollection returns collection used for streaker domain.
func (r *Repository) GetStreakCollection(ctx context.Context) (*mongo.Collection, error) {
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

		r.collection = db.Collection(StreakCollection)
		return r.collection, nil
	}

	return nil, fmt.Errorf("%w: unable to initialise %s collection after %d attempts: %w", ErrDatabaseError, StreakCollection, collectionInitMaxAttemptsLimit, lastErr)
}

// insertStreak resolves the streak collection, stamps CreatedAt with the
// current time when unset, inserts the entry, and returns the streak as
// persisted or the insert error.
func (r *Repository) insertStreak(ctx context.Context, streak *Streak) (*Streak, error) {
	collection, err := r.GetStreakCollection(ctx)
	if err != nil {
		return nil, err
	}

	if streak.CreatedAt == "" {
		streak.SetCreatedAtTimeToNow()
	}

	_, err = r.Store.ExecuteInsertOneCommand(ctx, collection, streak, "streak")
	if err != nil {
		return nil, err
	}

	return streak, nil
}

// CreateRawStreak creates a precomputed streak entry.
func (r *Repository) CreateRawStreak(ctx context.Context, streak *Streak) (*Streak, error) {
	return r.insertStreak(ctx, streak)
}

// CreateStreak creates a streak entry.
func (r *Repository) CreateStreak(ctx context.Context, streak *Streak) (*Streak, error) {
	if streak.Id == "" {
		streak.GenerateId()
	}

	if streak.NanoId == "" {
		streak.GenerateNanoId()
	}

	return r.insertStreak(ctx, streak)
}

// GetStreakByScopeAndPeriod retrieves a streak entry by its counter scope and period key.
func (r *Repository) GetStreakByScopeAndPeriod(ctx context.Context, req *GetLatestStreakRequest) (*Streak, error) {
	collection, err := r.GetStreakCollection(ctx)
	if err != nil {
		return nil, err
	}

	queryFilter := buildStreakQueryFilter(&req.StreakStatsRequest)
	queryFilter["period_key"] = req.PeriodKey

	var result Streak
	err = r.Store.ExecuteFindOneCommandDecodeResult(ctx, collection, queryFilter, &result, "streak", false, ErrResourceNotFound)
	if err != nil {
		return nil, err
	}

	return &result, nil
}

// GetLatestStreak retrieves the latest streak entry matching the provided filters.
func (r *Repository) GetLatestStreak(ctx context.Context, req *GetLatestStreakRequest) (*Streak, error) {
	collection, err := r.GetStreakCollection(ctx)
	if err != nil {
		return nil, err
	}

	queryFilter := buildStreakQueryFilter(&req.StreakStatsRequest)
	findOptions := options.Find().
		SetSort(bson.D{
			{Key: "occurred_at", Value: -1},
			{Key: "created_at", Value: -1},
		}).
		SetLimit(1)

	cursor, err := r.Store.ExecuteFindCommand(ctx, collection, queryFilter, findOptions)
	if err != nil {
		return nil, err
	}

	var streak Streak
	if err = r.Store.MapOneInCursorToResult(ctx, cursor, &streak, "streak"); err != nil {
		if strings.Contains(err.Error(), "not found") || strings.Contains(err.Error(), "no-documents-found") {
			return nil, ErrResourceNotFound
		}
		return nil, err
	}

	return &streak, nil
}

// GetLongestStreak retrieves the streak entry with the highest current count.
func (r *Repository) GetLongestStreak(ctx context.Context, req *GetLongestStreakRequest) (*Streak, error) {
	collection, err := r.GetStreakCollection(ctx)
	if err != nil {
		return nil, err
	}

	queryFilter := buildStreakQueryFilter(&req.StreakStatsRequest)
	findOptions := options.Find().
		SetSort(bson.D{
			{Key: "current_count", Value: -1},
			{Key: "occurred_at", Value: -1},
			{Key: "created_at", Value: -1},
		}).
		SetLimit(1)

	cursor, err := r.Store.ExecuteFindCommand(ctx, collection, queryFilter, findOptions)
	if err != nil {
		return nil, err
	}

	var streak Streak
	if err = r.Store.MapOneInCursorToResult(ctx, cursor, &streak, "streak"); err != nil {
		if strings.Contains(err.Error(), "not found") || strings.Contains(err.Error(), "no-documents-found") {
			return nil, ErrResourceNotFound
		}
		return nil, err
	}

	return &streak, nil
}

// GetTotalStreaks counts the streak entries matching the provided filters.
func (r *Repository) GetTotalStreaks(ctx context.Context, req *GetNumberOfStreaksRequest) (int64, error) {
	collection, err := r.GetStreakCollection(ctx)
	if err != nil {
		return 0, err
	}

	queryFilter := buildStreakQueryFilter(&req.StreakStatsRequest)
	return r.Store.ExecuteCountDocuments(ctx, collection, queryFilter)
}

// ListStreaks retrieves streak entries matching the provided filters.
func (r *Repository) ListStreaks(ctx context.Context, req *ListStreaksRequest) ([]*Streak, error) {
	collection, err := r.GetStreakCollection(ctx)
	if err != nil {
		return nil, err
	}

	queryFilter := buildStreakQueryFilter(&req.StreakStatsRequest)
	addStreakListFilter(queryFilter, req)

	sortDirection := -1
	if strings.EqualFold(req.Sort, "asc") || strings.EqualFold(req.Sort, "ascending") {
		sortDirection = 1
	}

	page := req.Page
	if page <= 0 {
		page = 1
	}
	perPage := req.PerPage
	if perPage <= 0 {
		perPage = 100
	}

	findOptions := options.Find().
		SetSort(bson.D{
			{Key: "occurred_at", Value: sortDirection},
			{Key: "created_at", Value: sortDirection},
		}).
		SetSkip(int64((page - 1) * perPage)).
		SetLimit(int64(perPage))

	cursor, err := r.Store.ExecuteFindCommand(ctx, collection, queryFilter, findOptions)
	if err != nil {
		return nil, err
	}

	var streaks []*Streak
	if err = r.Store.MapAllInCursorToResult(ctx, cursor, &streaks, "streaks"); err != nil {
		if strings.Contains(err.Error(), "not found") || strings.Contains(err.Error(), "no-documents-found") {
			return []*Streak{}, nil
		}
		return nil, err
	}

	if streaks == nil {
		streaks = []*Streak{}
	}

	return streaks, nil
}

// buildStreakQueryFilter converts a stats request into a MongoDB filter, adding
// exact matches only for non-empty scope and period-type fields; a nil request
// yields a filter matching any document with an _id.
func buildStreakQueryFilter(req *StreakStatsRequest) bson.M {
	queryFilter := bson.M{"_id": bson.M{"$exists": true}}

	if req == nil {
		return queryFilter
	}

	if req.StreakType != "" {
		queryFilter["streak_type"] = req.StreakType
	}

	if req.OwnerId != "" {
		queryFilter["owner_id"] = req.OwnerId
	}

	if req.TargetType != "" {
		queryFilter["target_type"] = req.TargetType
	}

	if req.TargetId != "" {
		queryFilter["target_id"] = req.TargetId
	}

	if req.PeriodType != "" {
		queryFilter["period_type"] = req.PeriodType
	}

	return queryFilter
}

// addStreakListFilter adds list-specific period constraints to a query filter
// in place. An exact PeriodKey overrides any range; otherwise PeriodKeyFrom/To
// and OccurredAtFrom/To become inclusive bounds. A nil request leaves the
// filter unchanged.
func addStreakListFilter(queryFilter bson.M, req *ListStreaksRequest) {
	if req == nil {
		return
	}

	if req.PeriodKey != "" {
		queryFilter["period_key"] = req.PeriodKey
		return
	}

	periodKeyRange := bson.M{}
	if req.PeriodKeyFrom != "" {
		periodKeyRange["$gte"] = req.PeriodKeyFrom
	}
	if req.PeriodKeyTo != "" {
		periodKeyRange["$lte"] = req.PeriodKeyTo
	}
	if len(periodKeyRange) > 0 {
		queryFilter["period_key"] = periodKeyRange
	}

	occurredAtRange := bson.M{}
	if req.OccurredAtFrom != "" {
		occurredAtRange["$gte"] = req.OccurredAtFrom
	}
	if req.OccurredAtTo != "" {
		occurredAtRange["$lte"] = req.OccurredAtTo
	}
	if len(occurredAtRange) > 0 {
		queryFilter["occurred_at"] = occurredAtRange
	}
}
