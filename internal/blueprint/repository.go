package blueprint

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"sync"

	"github.com/ooaklee/ghatd/external/logger"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.uber.org/zap"
)

const defaultCollectionInitMaxAttemptsLimit = 3

// MongoDbStore represents the datastore methods needed by blueprint.
type MongoDbStore interface {
	ExecuteCountDocuments(ctx context.Context, collection *mongo.Collection, filter interface{}, opts ...*options.CountOptions) (int64, error)
	ExecuteDeleteOneCommand(ctx context.Context, collection *mongo.Collection, filter interface{}, targetObjectName string) error
	ExecuteFindCommand(ctx context.Context, collection *mongo.Collection, filter interface{}, opts ...*options.FindOptions) (*mongo.Cursor, error)
	ExecuteFindOneCommandDecodeResult(ctx context.Context, collection *mongo.Collection, filter interface{}, result interface{}, resultObjectName string, logError bool, onFailureErr error) error
	ExecuteInsertOneCommand(ctx context.Context, collection *mongo.Collection, document interface{}, resultObjectName string) (*mongo.InsertOneResult, error)
	ExecuteUpdateOneCommand(ctx context.Context, collection *mongo.Collection, filter interface{}, updateFilter interface{}, resultObjectName string) error

	GetDatabase(ctx context.Context, dbName string) (*mongo.Database, error)
	InitialiseClient(ctx context.Context) (*mongo.Client, error)
	MapAllInCursorToResult(ctx context.Context, cursor *mongo.Cursor, result interface{}, resultObjectName string) error
}

// Repository handles blueprint data persistence.
type Repository struct {
	Store                          MongoDbStore
	collectionInitMaxAttemptsLimit int

	collection      *mongo.Collection
	collectionMutex sync.Mutex
}

// NewRepository creates a new blueprint repository.
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

// GetBlueprintCollection returns the collection used for blueprint records.
func (r *Repository) GetBlueprintCollection(ctx context.Context) (*mongo.Collection, error) {
	logger := logger.AcquireOperationFrom(ctx, "internal/blueprint", "get-blueprint-collection")
	r.collectionMutex.Lock()
	defer r.collectionMutex.Unlock()

	if r.collection != nil {
		logger.Debug("blueprint-collection-cache-hit")
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
			logger.Warn("blueprint-collection-client-init-failed", zap.Int("attempt", attempt), zap.Error(err))
			continue
		}

		db, err := r.Store.GetDatabase(ctx, "")
		if err != nil {
			lastErr = err
			logger.Warn("blueprint-collection-database-resolve-failed", zap.Int("attempt", attempt), zap.Error(err))
			continue
		}

		r.collection = db.Collection(BlueprintCollection)
		logger.Info("blueprint-collection-ready", zap.Int("attempt", attempt), zap.String("collection", BlueprintCollection))
		return r.collection, nil
	}

	logger.Error("blueprint-collection-initialisation-failed", zap.Int("max-attempts", collectionInitMaxAttemptsLimit), zap.Error(lastErr))
	return nil, fmt.Errorf("%w: unable to initialise %s collection after %d attempts: %w", ErrBlueprintDatabaseError, BlueprintCollection, collectionInitMaxAttemptsLimit, lastErr)
}

// CreateBlueprint creates a new blueprint in the repository.
func (r *Repository) CreateBlueprint(ctx context.Context, blueprint *Blueprint) (*Blueprint, error) {
	logger := logger.AcquireOperationFrom(ctx, "internal/blueprint", "create-blueprint")
	collection, err := r.GetBlueprintCollection(ctx)
	if err != nil {
		logger.Error("blueprint-repository-create-collection-unavailable", zap.Error(err))
		return nil, err
	}

	_, err = r.Store.ExecuteInsertOneCommand(ctx, collection, blueprint, "blueprint")
	if err != nil {
		logger.Error("blueprint-repository-create-failed", zap.String("blueprint-id", blueprint.ID), zap.Error(err))
		return nil, err
	}

	logger.Debug("blueprint-repository-create-completed", zap.String("blueprint-id", blueprint.ID))
	return blueprint, nil
}

// GetBlueprintByID retrieves a blueprint by ID.
func (r *Repository) GetBlueprintByID(ctx context.Context, id string) (*Blueprint, error) {
	logger := logger.AcquireOperationFrom(ctx, "internal/blueprint", "get-blueprint-by-id")
	collection, err := r.GetBlueprintCollection(ctx)
	if err != nil {
		logger.Error("blueprint-repository-get-by-id-collection-unavailable", zap.String("blueprint-id", id), zap.Error(err))
		return nil, err
	}

	var result Blueprint
	err = r.Store.ExecuteFindOneCommandDecodeResult(ctx, collection, bson.M{"_id": id}, &result, "blueprint", true, ErrBlueprintResourceNotFound)
	if err != nil {
		logger.Error("blueprint-repository-get-by-id-failed", zap.String("blueprint-id", id), zap.Error(err))
		return nil, err
	}

	logger.Debug("blueprint-repository-get-by-id-completed", zap.String("blueprint-id", result.ID))
	return &result, nil
}

// GetBlueprintByNameAndKind retrieves a blueprint by its natural key.
func (r *Repository) GetBlueprintByNameAndKind(ctx context.Context, name, kind string) (*Blueprint, error) {
	logger := logger.AcquireOperationFrom(ctx, "internal/blueprint", "get-blueprint-by-name-and-kind")
	collection, err := r.GetBlueprintCollection(ctx)
	if err != nil {
		logger.Error("blueprint-repository-get-by-name-collection-unavailable", zap.Error(err))
		return nil, err
	}

	var result Blueprint
	err = r.Store.ExecuteFindOneCommandDecodeResult(ctx, collection, bson.M{
		"name": normaliseBlueprintName(name),
		"kind": normaliseBlueprintKind(kind),
	}, &result, "blueprint", true, ErrBlueprintResourceNotFound)
	if err != nil {
		logger.Error("blueprint-repository-get-by-name-failed", zap.String("name", normaliseBlueprintName(name)), zap.String("kind", normaliseBlueprintKind(kind)), zap.Error(err))
		return nil, err
	}

	logger.Debug("blueprint-repository-get-by-name-completed", zap.String("blueprint-id", result.ID))
	return &result, nil
}

// GetBlueprints retrieves blueprints by filter.
func (r *Repository) GetBlueprints(ctx context.Context, req *GetBlueprintsRequest) ([]Blueprint, error) {
	logger := logger.AcquireOperationFrom(ctx, "internal/blueprint", "get-blueprints")
	collection, err := r.GetBlueprintCollection(ctx)
	if err != nil {
		logger.Error("blueprint-repository-list-collection-unavailable", zap.Error(err))
		return nil, err
	}

	filter := buildBlueprintListFilter(req)
	findOptions := options.Find().
		SetSort(bson.D{{Key: "created_at", Value: -1}, {Key: "name", Value: 1}})

	page, pageSize := normalisePagination(req)
	if pageSize > 0 {
		findOptions.SetLimit(pageSize)
		findOptions.SetSkip((page - 1) * pageSize)
	}

	cursor, err := r.Store.ExecuteFindCommand(ctx, collection, filter, findOptions)
	if err != nil {
		logger.Error("blueprint-repository-list-cursor-failed", zap.Error(err))
		return nil, err
	}

	var result []Blueprint
	if err = r.Store.MapAllInCursorToResult(ctx, cursor, &result, "blueprints"); err != nil {
		logger.Error("blueprint-repository-list-decode-failed", zap.Error(err))
		return nil, err
	}

	logger.Debug("blueprint-repository-list-completed", zap.Int("returned", len(result)))
	return result, nil
}

// GetTotalBlueprints counts blueprints by filter.
func (r *Repository) GetTotalBlueprints(ctx context.Context, req *GetBlueprintsRequest) (int64, error) {
	logger := logger.AcquireOperationFrom(ctx, "internal/blueprint", "get-total-blueprints")
	collection, err := r.GetBlueprintCollection(ctx)
	if err != nil {
		logger.Error("blueprint-repository-count-collection-unavailable", zap.Error(err))
		return 0, err
	}

	total, err := r.Store.ExecuteCountDocuments(ctx, collection, buildBlueprintListFilter(req))
	if err != nil {
		logger.Error("blueprint-repository-count-failed", zap.Error(err))
		return 0, err
	}
	logger.Debug("blueprint-repository-count-completed", zap.Int64("total", total))
	return total, nil
}

// UpdateBlueprint updates an existing blueprint.
func (r *Repository) UpdateBlueprint(ctx context.Context, blueprint *Blueprint) (*Blueprint, error) {
	logger := logger.AcquireOperationFrom(ctx, "internal/blueprint", "update-blueprint")
	collection, err := r.GetBlueprintCollection(ctx)
	if err != nil {
		logger.Error("blueprint-repository-update-collection-unavailable", zap.String("blueprint-id", blueprint.ID), zap.Error(err))
		return nil, err
	}

	err = r.Store.ExecuteUpdateOneCommand(ctx, collection, bson.M{"_id": blueprint.ID}, bson.M{"$set": blueprint}, "blueprint")
	if err != nil {
		logger.Error("blueprint-repository-update-failed", zap.String("blueprint-id", blueprint.ID), zap.Error(err))
		return nil, err
	}

	logger.Debug("blueprint-repository-update-completed", zap.String("blueprint-id", blueprint.ID))
	return blueprint, nil
}

// DeleteBlueprintByID deletes a blueprint by ID.
func (r *Repository) DeleteBlueprintByID(ctx context.Context, id string) error {
	logger := logger.AcquireOperationFrom(ctx, "internal/blueprint", "delete-blueprint-by-id")
	collection, err := r.GetBlueprintCollection(ctx)
	if err != nil {
		logger.Error("blueprint-repository-delete-collection-unavailable", zap.String("blueprint-id", id), zap.Error(err))
		return err
	}

	if err := r.Store.ExecuteDeleteOneCommand(ctx, collection, bson.M{"_id": id}, "blueprint"); err != nil {
		logger.Error("blueprint-repository-delete-failed", zap.String("blueprint-id", id), zap.Error(err))
		return err
	}

	logger.Debug("blueprint-repository-delete-completed", zap.String("blueprint-id", id))
	return nil
}

func buildBlueprintListFilter(req *GetBlueprintsRequest) bson.M {
	filter := bson.M{"_id": bson.M{"$exists": true}}
	if req == nil {
		return filter
	}

	if kind := normaliseBlueprintKind(req.Kind); kind != "" {
		filter["kind"] = kind
	}
	if status := normaliseBlueprintStatus(req.Status); status != "" {
		filter["status"] = status
	}
	if query := strings.TrimSpace(req.Query); query != "" {
		regex := bson.M{"$regex": regexp.QuoteMeta(query), "$options": "i"}
		filter["$or"] = []bson.M{
			{"name": regex},
			{"kind": regex},
			{"description": regex},
		}
	}

	return filter
}

func normalisePagination(req *GetBlueprintsRequest) (int64, int64) {
	if req == nil {
		return 1, 0
	}

	page := req.Page
	if page <= 0 {
		page = 1
	}

	pageSize := req.PageSize
	if pageSize < 0 {
		pageSize = 0
	}

	return page, pageSize
}
