package blueprint

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"sync"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
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

		r.collection = db.Collection(BlueprintCollection)
		return r.collection, nil
	}

	return nil, fmt.Errorf("%w: unable to initialise %s collection after %d attempts: %w", ErrBlueprintDatabaseError, BlueprintCollection, collectionInitMaxAttemptsLimit, lastErr)
}

// CreateBlueprint creates a new blueprint in the repository.
func (r *Repository) CreateBlueprint(ctx context.Context, blueprint *Blueprint) (*Blueprint, error) {
	collection, err := r.GetBlueprintCollection(ctx)
	if err != nil {
		return nil, err
	}

	_, err = r.Store.ExecuteInsertOneCommand(ctx, collection, blueprint, "blueprint")
	if err != nil {
		return nil, err
	}

	return blueprint, nil
}

// GetBlueprintByID retrieves a blueprint by ID.
func (r *Repository) GetBlueprintByID(ctx context.Context, id string) (*Blueprint, error) {
	collection, err := r.GetBlueprintCollection(ctx)
	if err != nil {
		return nil, err
	}

	var result Blueprint
	err = r.Store.ExecuteFindOneCommandDecodeResult(ctx, collection, bson.M{"_id": id}, &result, "blueprint", true, ErrBlueprintResourceNotFound)
	if err != nil {
		return nil, err
	}

	return &result, nil
}

// GetBlueprintByNameAndKind retrieves a blueprint by its natural key.
func (r *Repository) GetBlueprintByNameAndKind(ctx context.Context, name, kind string) (*Blueprint, error) {
	collection, err := r.GetBlueprintCollection(ctx)
	if err != nil {
		return nil, err
	}

	var result Blueprint
	err = r.Store.ExecuteFindOneCommandDecodeResult(ctx, collection, bson.M{
		"name": normaliseBlueprintName(name),
		"kind": normaliseBlueprintKind(kind),
	}, &result, "blueprint", true, ErrBlueprintResourceNotFound)
	if err != nil {
		return nil, err
	}

	return &result, nil
}

// GetBlueprints retrieves blueprints by filter.
func (r *Repository) GetBlueprints(ctx context.Context, req *GetBlueprintsRequest) ([]Blueprint, error) {
	collection, err := r.GetBlueprintCollection(ctx)
	if err != nil {
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
		return nil, err
	}

	var result []Blueprint
	if err = r.Store.MapAllInCursorToResult(ctx, cursor, &result, "blueprints"); err != nil {
		return nil, err
	}

	return result, nil
}

// GetTotalBlueprints counts blueprints by filter.
func (r *Repository) GetTotalBlueprints(ctx context.Context, req *GetBlueprintsRequest) (int64, error) {
	collection, err := r.GetBlueprintCollection(ctx)
	if err != nil {
		return 0, err
	}

	return r.Store.ExecuteCountDocuments(ctx, collection, buildBlueprintListFilter(req))
}

// UpdateBlueprint updates an existing blueprint.
func (r *Repository) UpdateBlueprint(ctx context.Context, blueprint *Blueprint) (*Blueprint, error) {
	collection, err := r.GetBlueprintCollection(ctx)
	if err != nil {
		return nil, err
	}

	err = r.Store.ExecuteUpdateOneCommand(ctx, collection, bson.M{"_id": blueprint.ID}, bson.M{"$set": blueprint}, "blueprint")
	if err != nil {
		return nil, err
	}

	return blueprint, nil
}

// DeleteBlueprintByID deletes a blueprint by ID.
func (r *Repository) DeleteBlueprintByID(ctx context.Context, id string) error {
	collection, err := r.GetBlueprintCollection(ctx)
	if err != nil {
		return err
	}

	return r.Store.ExecuteDeleteOneCommand(ctx, collection, bson.M{"_id": id}, "blueprint")
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
