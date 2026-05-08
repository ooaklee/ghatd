package streaker

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const collectionInitMaxAttempts = 3

// MongoDbStore represents the datastore methods needed by streaker.
type MongoDbStore interface {
	ExecuteCountDocuments(ctx context.Context, collection *mongo.Collection, filter interface{}, opts ...*options.CountOptions) (int64, error)
	ExecuteFindCommand(ctx context.Context, collection *mongo.Collection, filter interface{}, opts ...*options.FindOptions) (*mongo.Cursor, error)
	ExecuteInsertOneCommand(ctx context.Context, collection *mongo.Collection, document interface{}, resultObjectName string) (*mongo.InsertOneResult, error)
	ExecuteFindOneCommandDecodeResult(ctx context.Context, collection *mongo.Collection, filter interface{}, result interface{}, resultObjectName string, logError bool, onFailureErr error) error

	GetDatabase(ctx context.Context, dbName string) (*mongo.Database, error)
	InitialiseClient(ctx context.Context) (*mongo.Client, error)
	MapAllInCursorToResult(ctx context.Context, cursor *mongo.Cursor, result interface{}, resultObjectName string) error
	MapOneInCursorToResult(ctx context.Context, cursor *mongo.Cursor, result interface{}, resultObjectName string) error
}

// Repository represents the datastore to hold streak data.
type Repository struct {
	Store MongoDbStore

	collection      *mongo.Collection
	collectionMutex sync.Mutex
}

// NewRepository initiates a new instance of repository.
func NewRepository(store MongoDbStore) *Repository {
	return &Repository{
		Store: store,
	}
}

// GetStreakCollection returns collection used for streaker domain.
func (r *Repository) GetStreakCollection(ctx context.Context) (*mongo.Collection, error) {
	if r.collection != nil {
		return r.collection, nil
	}

	r.collectionMutex.Lock()
	defer r.collectionMutex.Unlock()

	if r.collection != nil {
		return r.collection, nil
	}

	var lastErr error
	for attempt := 1; attempt <= collectionInitMaxAttempts; attempt++ {
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

	return nil, fmt.Errorf("%s: unable to initialise streak collection after %d attempts: %w", ErrKeyDatabaseError, collectionInitMaxAttempts, lastErr)
}

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
	err = r.Store.ExecuteFindOneCommandDecodeResult(ctx, collection, queryFilter, &result, "streak", false, errors.New(ErrKeyResourceNotFound))
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
			return nil, errors.New(ErrKeyResourceNotFound)
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
			return nil, errors.New(ErrKeyResourceNotFound)
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
