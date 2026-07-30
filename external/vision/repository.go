package vision

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"sync"

	"github.com/ooaklee/ghatd/external/logger"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.uber.org/zap"
)

const defaultCollectionInitMaxAttemptsLimit = 3

// MongoDbStore represents the datastore methods needed by vision.
type MongoDbStore interface {
	ExecuteCountDocuments(ctx context.Context, collection *mongo.Collection, filter interface{}, opts ...options.Lister[options.CountOptions]) (int64, error)
	ExecuteDeleteOneCommand(ctx context.Context, collection *mongo.Collection, filter interface{}, targetObjectName string) error
	ExecuteFindCommand(ctx context.Context, collection *mongo.Collection, filter interface{}, opts ...options.Lister[options.FindOptions]) (*mongo.Cursor, error)
	ExecuteFindOneCommandDecodeResult(ctx context.Context, collection *mongo.Collection, filter interface{}, result interface{}, resultObjectName string, logError bool, onFailureErr error) error
	ExecuteInsertOneCommand(ctx context.Context, collection *mongo.Collection, document interface{}, resultObjectName string) (*mongo.InsertOneResult, error)
	ExecuteUpdateOneCommand(ctx context.Context, collection *mongo.Collection, filter interface{}, updateFilter interface{}, resultObjectName string) error

	GetDatabase(ctx context.Context, dbName string) (*mongo.Database, error)
	InitialiseClient(ctx context.Context) (*mongo.Client, error)
	MapAllInCursorToResult(ctx context.Context, cursor *mongo.Cursor, result interface{}, resultObjectName string) error
}

// Repository handles vision data persistence.
type Repository struct {
	Store                          MongoDbStore
	collectionInitMaxAttemptsLimit int

	collection      *mongo.Collection
	collectionMutex sync.Mutex
}

// NewRepository creates a new vision repository.
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

// GetVisionCollection returns the collection used for vision records.
func (r *Repository) GetVisionCollection(ctx context.Context) (*mongo.Collection, error) {
	logger := logger.AcquireOperationFrom(ctx, "internal/vision", "get-vision-collection")
	r.collectionMutex.Lock()
	defer r.collectionMutex.Unlock()

	if r.collection != nil {
		logger.Debug("vision-collection-cache-hit")
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
			logger.Warn("vision-collection-client-init-failed", zap.Int("attempt", attempt), zap.Error(err))
			continue
		}

		db, err := r.Store.GetDatabase(ctx, "")
		if err != nil {
			lastErr = err
			logger.Warn("vision-collection-database-resolve-failed", zap.Int("attempt", attempt), zap.Error(err))
			continue
		}

		r.collection = db.Collection(VisionCollection)
		logger.Info("vision-collection-ready", zap.Int("attempt", attempt), zap.String("collection", VisionCollection))
		return r.collection, nil
	}

	logger.Error("vision-collection-initialisation-failed", zap.Int("max-attempts", collectionInitMaxAttemptsLimit), zap.Error(lastErr))
	return nil, fmt.Errorf("%w: unable to initialise %s collection after %d attempts: %w", ErrVisionDatabaseError, VisionCollection, collectionInitMaxAttemptsLimit, lastErr)
}

// CreateVision creates a new vision in the repository.
func (r *Repository) CreateVision(ctx context.Context, vision *Vision) (*Vision, error) {
	logger := logger.AcquireOperationFrom(ctx, "internal/vision", "create-vision")
	collection, err := r.GetVisionCollection(ctx)
	if err != nil {
		logger.Error("vision-repository-create-collection-unavailable", zap.Error(err))
		return nil, err
	}

	_, err = r.Store.ExecuteInsertOneCommand(ctx, collection, vision, "vision")
	if err != nil {
		logger.Error("vision-repository-create-failed", zap.String("vision-id", vision.ID), zap.Error(err))
		return nil, err
	}

	logger.Debug("vision-repository-create-completed", zap.String("vision-id", vision.ID))
	return vision, nil
}

// GetVisionByID retrieves a vision by ID.
func (r *Repository) GetVisionByID(ctx context.Context, id string) (*Vision, error) {
	logger := logger.AcquireOperationFrom(ctx, "internal/vision", "get-vision-by-id")
	collection, err := r.GetVisionCollection(ctx)
	if err != nil {
		logger.Error("vision-repository-get-by-id-collection-unavailable", zap.String("vision-id", id), zap.Error(err))
		return nil, err
	}

	var result Vision
	err = r.Store.ExecuteFindOneCommandDecodeResult(ctx, collection, bson.M{"_id": id}, &result, "vision", true, ErrVisionResourceNotFound)
	if err != nil {
		logger.Error("vision-repository-get-by-id-failed", zap.String("vision-id", id), zap.Error(err))
		return nil, err
	}

	logger.Debug("vision-repository-get-by-id-completed", zap.String("vision-id", result.ID))
	return &result, nil
}

// GetVisionByNameAndKind retrieves a vision by its natural key.
func (r *Repository) GetVisionByNameAndKind(ctx context.Context, name, kind string) (*Vision, error) {
	logger := logger.AcquireOperationFrom(ctx, "internal/vision", "get-vision-by-name-and-kind")
	collection, err := r.GetVisionCollection(ctx)
	if err != nil {
		logger.Error("vision-repository-get-by-name-collection-unavailable", zap.Error(err))
		return nil, err
	}

	var result Vision
	err = r.Store.ExecuteFindOneCommandDecodeResult(ctx, collection, bson.M{
		"name": normaliseVisionName(name),
		"kind": normaliseVisionKind(kind),
	}, &result, "vision", true, ErrVisionResourceNotFound)
	if err != nil {
		logger.Error("vision-repository-get-by-name-failed", zap.String("name", normaliseVisionName(name)), zap.String("kind", normaliseVisionKind(kind)), zap.Error(err))
		return nil, err
	}

	logger.Debug("vision-repository-get-by-name-completed", zap.String("vision-id", result.ID))
	return &result, nil
}

// GetVisions retrieves visions by filter.
func (r *Repository) GetVisions(ctx context.Context, req *GetVisionsRequest) ([]Vision, error) {
	logger := logger.AcquireOperationFrom(ctx, "internal/vision", "get-visions")
	collection, err := r.GetVisionCollection(ctx)
	if err != nil {
		logger.Error("vision-repository-list-collection-unavailable", zap.Error(err))
		return nil, err
	}

	filter := buildVisionListFilter(req)
	findOptions := options.Find().
		SetSort(bson.D{{Key: "created_at", Value: -1}, {Key: "name", Value: 1}})

	page, pageSize := normalisePagination(req)
	if pageSize > 0 {
		findOptions.SetLimit(pageSize)
		findOptions.SetSkip((page - 1) * pageSize)
	}

	cursor, err := r.Store.ExecuteFindCommand(ctx, collection, filter, findOptions)
	if err != nil {
		logger.Error("vision-repository-list-cursor-failed", zap.Error(err))
		return nil, err
	}

	var result []Vision
	if err = r.Store.MapAllInCursorToResult(ctx, cursor, &result, "visions"); err != nil {
		logger.Error("vision-repository-list-decode-failed", zap.Error(err))
		return nil, err
	}

	logger.Debug("vision-repository-list-completed", zap.Int("returned", len(result)))
	return result, nil
}

// GetTotalVisions counts visions by filter.
func (r *Repository) GetTotalVisions(ctx context.Context, req *GetVisionsRequest) (int64, error) {
	logger := logger.AcquireOperationFrom(ctx, "internal/vision", "get-total-visions")
	collection, err := r.GetVisionCollection(ctx)
	if err != nil {
		logger.Error("vision-repository-count-collection-unavailable", zap.Error(err))
		return 0, err
	}

	total, err := r.Store.ExecuteCountDocuments(ctx, collection, buildVisionListFilter(req))
	if err != nil {
		logger.Error("vision-repository-count-failed", zap.Error(err))
		return 0, err
	}
	logger.Debug("vision-repository-count-completed", zap.Int64("total", total))
	return total, nil
}

// UpdateVision updates an existing vision.
func (r *Repository) UpdateVision(ctx context.Context, vision *Vision) (*Vision, error) {
	logger := logger.AcquireOperationFrom(ctx, "internal/vision", "update-vision")
	collection, err := r.GetVisionCollection(ctx)
	if err != nil {
		logger.Error("vision-repository-update-collection-unavailable", zap.String("vision-id", vision.ID), zap.Error(err))
		return nil, err
	}

	err = r.Store.ExecuteUpdateOneCommand(ctx, collection, bson.M{"_id": vision.ID}, bson.M{"$set": vision}, "vision")
	if err != nil {
		logger.Error("vision-repository-update-failed", zap.String("vision-id", vision.ID), zap.Error(err))
		return nil, err
	}

	logger.Debug("vision-repository-update-completed", zap.String("vision-id", vision.ID))
	return vision, nil
}

// DeleteVisionByID deletes a vision by ID.
func (r *Repository) DeleteVisionByID(ctx context.Context, id string) error {
	logger := logger.AcquireOperationFrom(ctx, "internal/vision", "delete-vision-by-id")
	collection, err := r.GetVisionCollection(ctx)
	if err != nil {
		logger.Error("vision-repository-delete-collection-unavailable", zap.String("vision-id", id), zap.Error(err))
		return err
	}

	if err := r.Store.ExecuteDeleteOneCommand(ctx, collection, bson.M{"_id": id}, "vision"); err != nil {
		logger.Error("vision-repository-delete-failed", zap.String("vision-id", id), zap.Error(err))
		return err
	}

	logger.Debug("vision-repository-delete-completed", zap.String("vision-id", id))
	return nil
}

func buildVisionListFilter(req *GetVisionsRequest) bson.M {
	filter := bson.M{"_id": bson.M{"$exists": true}}
	if req == nil {
		return filter
	}

	if kind := normaliseVisionKind(req.Kind); kind != "" {
		filter["kind"] = kind
	}
	if status := normaliseVisionStatus(req.Status); status != "" {
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

func normalisePagination(req *GetVisionsRequest) (int64, int64) {
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
