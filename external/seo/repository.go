package seo

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const defaultCollectionInitMaxAttemptsLimit = 3

// MongoDbStore represents the datastore methods needed by seo.
type MongoDbStore interface {
	ExecuteCountDocuments(ctx context.Context, collection *mongo.Collection, filter interface{}, opts ...*options.CountOptions) (int64, error)
	ExecuteDeleteManyCommand(ctx context.Context, collection *mongo.Collection, filter interface{}, targetObjectName string) error
	ExecuteFindCommand(ctx context.Context, collection *mongo.Collection, filter interface{}, opts ...*options.FindOptions) (*mongo.Cursor, error)
	ExecuteFindOneCommandDecodeResult(ctx context.Context, collection *mongo.Collection, filter interface{}, result interface{}, resultObjectName string, logError bool, onFailureErr error) error
	ExecuteInsertOneCommand(ctx context.Context, collection *mongo.Collection, document interface{}, resultObjectName string) (*mongo.InsertOneResult, error)
	ExecuteUpdateOneCommand(ctx context.Context, collection *mongo.Collection, filter interface{}, updateFilter interface{}, resultObjectName string) error

	GetDatabase(ctx context.Context, dbName string) (*mongo.Database, error)
	InitialiseClient(ctx context.Context) (*mongo.Client, error)
	MapAllInCursorToResult(ctx context.Context, cursor *mongo.Cursor, result interface{}, resultObjectName string) error
}

// Repository handles sitemap item data persistence.
type Repository struct {
	Store                          MongoDbStore
	collectionInitMaxAttemptsLimit int

	collection      *mongo.Collection
	collectionMutex sync.Mutex
}

// NewRepository creates a new sitemap repository.
func NewRepository(store MongoDbStore) *Repository {
	return &Repository{
		Store:                          store,
		collectionInitMaxAttemptsLimit: defaultCollectionInitMaxAttemptsLimit,
	}
}

// WithCollectionInitMaxAttemptsLimit overrides collection initialisation attempts.
func (r *Repository) WithCollectionInitMaxAttemptsLimit(limit int) *Repository {
	if limit > 0 {
		r.collectionInitMaxAttemptsLimit = limit
	}
	return r
}

// GetSitemapItemsCollection returns the collection used for sitemap item records.
func (r *Repository) GetSitemapItemsCollection(ctx context.Context) (*mongo.Collection, error) {
	r.collectionMutex.Lock()
	defer r.collectionMutex.Unlock()

	if r.collection != nil {
		return r.collection, nil
	}

	var lastErr error
	limit := r.collectionInitMaxAttemptsLimit
	if limit <= 0 {
		limit = defaultCollectionInitMaxAttemptsLimit
	}

	for attempt := 1; attempt <= limit; attempt++ {
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

		r.collection = db.Collection(SitemapItemsCollection)
		return r.collection, nil
	}

	return nil, fmt.Errorf("%w: unable to initialise %s collection after %d attempts: %w", ErrSitemapItemDatabaseError, SitemapItemsCollection, limit, lastErr)
}

// CreateSitemapItem creates a new sitemap item.
func (r *Repository) CreateSitemapItem(ctx context.Context, item *SitemapItem) (*SitemapItem, error) {
	collection, err := r.GetSitemapItemsCollection(ctx)
	if err != nil {
		return nil, err
	}

	if _, err = r.Store.ExecuteInsertOneCommand(ctx, collection, item, "sitemap item"); err != nil {
		return nil, err
	}

	return item, nil
}

// GetSitemapItemByURI retrieves a sitemap item by URI.
func (r *Repository) GetSitemapItemByURI(ctx context.Context, uri string) (*SitemapItem, error) {
	collection, err := r.GetSitemapItemsCollection(ctx)
	if err != nil {
		return nil, err
	}

	var result SitemapItem
	err = r.Store.ExecuteFindOneCommandDecodeResult(ctx, collection, bson.M{"uri": normaliseSitemapURI(uri)}, &result, "sitemap item", true, ErrSitemapItemResourceNotFound)
	if err != nil {
		return nil, err
	}

	return &result, nil
}

// GetLatestSitemapItemByURIRegex retrieves the newest sitemap item whose URI matches the given regex.
func (r *Repository) GetLatestSitemapItemByURIRegex(ctx context.Context, uriRegex string) (*SitemapItem, error) {
	collection, err := r.GetSitemapItemsCollection(ctx)
	if err != nil {
		return nil, err
	}

	findOptions := options.FindOne().SetSort(bson.D{
		{Key: "last_mod", Value: -1},
		{Key: "created_at", Value: -1},
	})

	var result SitemapItem
	err = collection.FindOne(ctx, bson.M{"uri": bson.M{"$regex": uriRegex}}, findOptions).Decode(&result)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ErrSitemapItemResourceNotFound
		}
		return nil, err
	}

	return &result, nil
}

// GetSitemapItems retrieves sitemap items by filter.
func (r *Repository) GetSitemapItems(ctx context.Context, req *GetSitemapItemsRequest) ([]SitemapItem, error) {
	collection, err := r.GetSitemapItemsCollection(ctx)
	if err != nil {
		return nil, err
	}

	findOptions := options.Find().SetSort(bson.D{{Key: "uri", Value: 1}})
	page, pageSize := normalisePagination(req)
	if pageSize > 0 {
		findOptions.SetLimit(pageSize)
		findOptions.SetSkip((page - 1) * pageSize)
	}

	cursor, err := r.Store.ExecuteFindCommand(ctx, collection, buildSitemapItemListFilter(req), findOptions)
	if err != nil {
		return nil, err
	}

	var result []SitemapItem
	if err = r.Store.MapAllInCursorToResult(ctx, cursor, &result, "sitemap items"); err != nil {
		return nil, err
	}

	return result, nil
}

// GetAllSitemapItems returns all sitemap items for XML generation.
func (r *Repository) GetAllSitemapItems(ctx context.Context) ([]SitemapItem, error) {
	return r.GetSitemapItems(ctx, &GetSitemapItemsRequest{})
}

// GetTotalSitemapItems counts sitemap items by filter.
func (r *Repository) GetTotalSitemapItems(ctx context.Context, req *GetSitemapItemsRequest) (int64, error) {
	collection, err := r.GetSitemapItemsCollection(ctx)
	if err != nil {
		return 0, err
	}

	return r.Store.ExecuteCountDocuments(ctx, collection, buildSitemapItemListFilter(req))
}

// UpdateSitemapItemByURI updates an existing sitemap item by URI.
func (r *Repository) UpdateSitemapItemByURI(ctx context.Context, item *SitemapItem) (*SitemapItem, error) {
	collection, err := r.GetSitemapItemsCollection(ctx)
	if err != nil {
		return nil, err
	}

	if err = r.Store.ExecuteUpdateOneCommand(ctx, collection, bson.M{"uri": item.URI}, bson.M{"$set": item}, "sitemap item"); err != nil {
		return nil, err
	}

	return item, nil
}

// UpsertSitemapItemByURI creates or replaces a sitemap item by URI.
func (r *Repository) UpsertSitemapItemByURI(ctx context.Context, item *SitemapItem) (*SitemapItem, bool, error) {
	collection, err := r.GetSitemapItemsCollection(ctx)
	if err != nil {
		return nil, false, err
	}

	update := bson.M{
		"$set": bson.M{
			"uri":              item.URI,
			"last_mod":         item.LastMod,
			"priority":         item.Priority,
			"change_frequency": item.ChangeFrequency,
			"created_by_id":    item.CreatedByID,
			"updated_at":       item.UpdatedAt,
			"updated_by_id":    item.UpdatedByID,
		},
		"$setOnInsert": bson.M{
			"_id":        item.ID,
			"created_at": item.CreatedAt,
		},
	}

	result, err := collection.UpdateOne(ctx, bson.M{"uri": item.URI}, update, options.Update().SetUpsert(true))
	if err != nil {
		return nil, false, err
	}

	created := result.UpsertedCount > 0
	return item, created, nil
}

// DeleteSitemapItemsByURIs deletes sitemap items matching exact URIs.
func (r *Repository) DeleteSitemapItemsByURIs(ctx context.Context, uris []string) error {
	if len(uris) == 0 {
		return nil
	}

	collection, err := r.GetSitemapItemsCollection(ctx)
	if err != nil {
		return err
	}

	return r.Store.ExecuteDeleteManyCommand(ctx, collection, bson.M{"uri": bson.M{"$in": uris}}, "sitemap items")
}

func buildSitemapItemListFilter(req *GetSitemapItemsRequest) bson.M {
	filter := bson.M{"_id": bson.M{"$exists": true}}
	if req == nil {
		return filter
	}

	if query := strings.TrimSpace(req.Query); query != "" {
		filter["uri"] = bson.M{"$regex": regexp.QuoteMeta(query), "$options": "i"}
	}

	return filter
}

func normalisePagination(req *GetSitemapItemsRequest) (int64, int64) {
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
