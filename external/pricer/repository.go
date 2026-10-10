package pricer

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/ooaklee/ghatd/external/repository"
	"github.com/ooaklee/ghatd/external/toolbox"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

const (
	// PricePlansCollection collection name for price plans.
	PricePlansCollection string = "pricing_plans"

	// PriceFeaturesCollection collection name for price features.
	PriceFeaturesCollection string = "pricing_features"

	defaultCollectionInitMaxAttemptsLimit = 3
)

// TODO: Add unique index for pricing_plans.slug in migrations/bootstrap.
// TODO: Add unique index for pricing_features.slug in migrations/bootstrap.

// MongoDbStore represents the datastore to hold pricer data.
type MongoDbStore interface {
	// ExecuteCountDocuments returns the number of documents in the given collection
	// matching the filter, accepting Mongo count options for the pricer datastore.
	ExecuteCountDocuments(ctx context.Context, collection *mongo.Collection, filter interface{}, opts ...options.Lister[options.CountOptions]) (int64, error)
	// ExecuteDeleteOneCommand deletes a single document matching the filter from
	// the collection; targetObjectName identifies the resource in store
	// diagnostics.
	ExecuteDeleteOneCommand(ctx context.Context, collection *mongo.Collection, filter interface{}, targetObjectName string) error
	// ExecuteFindCommand runs a find query on the collection with the filter and
	// find options, returning a cursor over matching documents.
	ExecuteFindCommand(ctx context.Context, collection *mongo.Collection, filter interface{}, opts ...options.Lister[options.FindOptions]) (*mongo.Cursor, error)
	// ExecuteInsertOneCommand inserts the document into the collection and returns
	// the insert result; resultObjectName identifies the resource in store
	// diagnostics.
	ExecuteInsertOneCommand(ctx context.Context, collection *mongo.Collection, document interface{}, resultObjectName string) (*mongo.InsertOneResult, error)
	// ExecuteUpdateOneCommand applies the update filter to a single document
	// matching the given filter in the collection; resultObjectName identifies the
	// resource in store diagnostics.
	ExecuteUpdateOneCommand(ctx context.Context, collection *mongo.Collection, filter interface{}, updateFilter interface{}, resultObjectName string) error
	// ExecuteDeleteManyCommand deletes all documents matching the filter from the
	// collection; targetObjectName identifies the resource in store diagnostics.
	ExecuteDeleteManyCommand(ctx context.Context, collection *mongo.Collection, filter interface{}, targetObjectName string) error
	// ExecuteFindOneCommandDecodeResult finds a single matching document and
	// decodes it into result; logError and onFailureErr control error logging and
	// the returned failure error.
	ExecuteFindOneCommandDecodeResult(ctx context.Context, collection *mongo.Collection, filter interface{}, result interface{}, resultObjectName string, logError bool, onFailureErr error) error
	// ExecuteAggregateCommand runs the aggregation pipeline against the collection
	// and returns a cursor over the aggregated results.
	ExecuteAggregateCommand(ctx context.Context, collection *mongo.Collection, mongoPipeline []bson.D) (*mongo.Cursor, error)

	// GetDatabase returns the Mongo database handle for the given database name
	// from the pricer datastore.
	GetDatabase(ctx context.Context, dbName string) (*mongo.Database, error)
	// InitialiseClient establishes the datastore connection and returns the
	// initialised Mongo client.
	InitialiseClient(ctx context.Context) (*mongo.Client, error)
	// MapAllInCursorToResult decodes every document in the cursor into result;
	// resultObjectName identifies the resource in store diagnostics.
	MapAllInCursorToResult(ctx context.Context, cursor *mongo.Cursor, result interface{}, resultObjectName string) error
	// MapOneInCursorToResult decodes a single document from the cursor into result;
	// resultObjectName identifies the resource in store diagnostics.
	MapOneInCursorToResult(ctx context.Context, cursor *mongo.Cursor, result interface{}, resultObjectName string) error
}

// PricePlanRepository describes price plan persistence operations.
type PricePlanRepository interface {
	// CreatePricePlan persists the supplied price plan and returns the stored plan,
	// part of the price plan persistence contract.
	CreatePricePlan(ctx context.Context, pricePlan *PricePlan) (*PricePlan, error)
	// UpdatePricePlan persists changes to the supplied price plan identified by its
	// ID and returns the updated plan as part of the price plan persistence
	// contract.
	UpdatePricePlan(ctx context.Context, pricePlan *PricePlan) (*PricePlan, error)
	// GetPricePlanByID retrieves the stored price plan with the given ID; the
	// request controls inclusion of features, costs, and providers in the
	// projection.
	GetPricePlanByID(ctx context.Context, id string, req *GetPricePlanByIDRequest) (*PricePlan, error)
	// GetPricePlanBySlug retrieves the stored price plan matching the normalized
	// slug; the request controls inclusion of features, costs, and providers in the
	// projection.
	GetPricePlanBySlug(ctx context.Context, slug string, req *GetPricePlanBySlugRequest) (*PricePlan, error)
	// GetPricePlans retrieves price plans matching the request's filters, sort,
	// pagination, and inclusion options from the price plan persistence contract.
	GetPricePlans(ctx context.Context, req *GetPricePlansRequest) ([]PricePlan, error)
	// GetTotalPricePlans returns the count of price plans matching the request's
	// query filters, used for pagination totals.
	GetTotalPricePlans(ctx context.Context, req *GetPricePlansRequest) (int64, error)
	// PublishPricePlan sets the plan's status to published and records publication
	// metadata; id selects the plan, publishedByID and publishedAt attribute the
	// publication, defaulting the timestamp when empty.
	PublishPricePlan(ctx context.Context, id, publishedByID, publishedAt string) error
	// ArchivePricePlan sets the plan's status to archived and records update
	// attribution; id selects the plan, updatedByID and updatedAt attribute the
	// change, defaulting the timestamp when empty.
	ArchivePricePlan(ctx context.Context, id, updatedByID, updatedAt string) error
	// SoftDeletePricePlan marks the plan identified by id as deleted by setting
	// archived status with deletion attribution; deletedByID and deletedAt
	// attribute the deletion, defaulting the timestamp when empty.
	SoftDeletePricePlan(ctx context.Context, id, deletedByID, deletedAt string) error
}

// PriceFeatureRepository describes feature catalog persistence operations.
type PriceFeatureRepository interface {
	// CreateFeature persists a new feature catalog item via PriceFeatureRepository,
	// filling missing ID, NanoID, CreatedAt and normalising the slug before insert,
	// returning the stored feature.
	CreateFeature(ctx context.Context, feature *PriceFeature) (*PriceFeature, error)
	// UpdateFeature replaces the stored feature catalog item matching feature.ID
	// via PriceFeatureRepository, stamping UpdatedAt, normalising the slug and
	// returning the updated feature.
	UpdateFeature(ctx context.Context, feature *PriceFeature) (*PriceFeature, error)
	// GetFeatureByID fetches a single feature catalog item by its ID via
	// PriceFeatureRepository, returning the found feature or
	// ErrPriceFeatureNotFound when no document matches.
	GetFeatureByID(ctx context.Context, id string) (*PriceFeature, error)
	// GetFeatures queries feature catalog items via PriceFeatureRepository using
	// the request's filters, ordering and pagination, returning the matching page
	// of features.
	GetFeatures(ctx context.Context, req *GetFeaturesRequest) ([]PriceFeature, error)
	// GetTotalFeatures counts feature catalog items matching the request's query
	// filters via PriceFeatureRepository, returning the total for pagination.
	GetTotalFeatures(ctx context.Context, req *GetFeaturesRequest) (int64, error)
	// SoftDeleteFeature marks the feature catalog item with the given ID deleted by
	// setting deleted_at, deleted_by_id and update audit fields, defaulting
	// deletedAt to the current UTC time when empty.
	SoftDeleteFeature(ctx context.Context, id, deletedByID, deletedAt string) error
}

// Repository represents the datastore to hold pricer data.
type Repository struct {
	Store                          MongoDbStore
	collectionInitMaxAttemptsLimit int

	pricePlansCollection         *mongo.Collection
	pricePlansCollectionMutex    sync.Mutex
	priceFeaturesCollection      *mongo.Collection
	priceFeaturesCollectionMutex sync.Mutex
}

// NewRepository initiates new instance of repository.
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

// GetPricePlansCollection returns collection used for pricing plans.
func (r *Repository) GetPricePlansCollection(ctx context.Context) (*mongo.Collection, error) {
	r.pricePlansCollectionMutex.Lock()
	defer r.pricePlansCollectionMutex.Unlock()

	if r.pricePlansCollection != nil {
		return r.pricePlansCollection, nil
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

		r.pricePlansCollection = db.Collection(PricePlansCollection)
		return r.pricePlansCollection, nil
	}

	return nil, fmt.Errorf("%w: unable to initialise %s collection after %d attempts: %w", ErrDatabaseError, PricePlansCollection, collectionInitMaxAttemptsLimit, lastErr)
}

// GetPriceFeaturesCollection returns collection used for pricing features.
func (r *Repository) GetPriceFeaturesCollection(ctx context.Context) (*mongo.Collection, error) {
	r.priceFeaturesCollectionMutex.Lock()
	defer r.priceFeaturesCollectionMutex.Unlock()

	if r.priceFeaturesCollection != nil {
		return r.priceFeaturesCollection, nil
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

		r.priceFeaturesCollection = db.Collection(PriceFeaturesCollection)
		return r.priceFeaturesCollection, nil
	}

	return nil, fmt.Errorf("%w: unable to initialise %s collection after %d attempts: %w", ErrDatabaseError, PriceFeaturesCollection, collectionInitMaxAttemptsLimit, lastErr)
}

// CreatePricePlan creates a price plan in repository.
func (r *Repository) CreatePricePlan(ctx context.Context, pricePlan *PricePlan) (*PricePlan, error) {
	collection, err := r.GetPricePlansCollection(ctx)
	if err != nil {
		return nil, err
	}

	if pricePlan.ID == "" {
		pricePlan.ID = toolbox.GenerateUuidV4()
	}
	if pricePlan.NanoID == "" {
		pricePlan.NanoID = toolbox.GenerateNanoId()
	}
	if pricePlan.CreatedAt == "" {
		pricePlan.CreatedAt = toolbox.TimeNowUTC()
	}
	if pricePlan.Status == "" {
		pricePlan.Status = PricePlanStatusDraft
	}
	if pricePlan.Slug != "" {
		pricePlan.NormaliseSlug()
	}

	_, err = r.Store.ExecuteInsertOneCommand(ctx, collection, pricePlan, "price_plan")
	if err != nil {
		return nil, err
	}

	return pricePlan, nil
}

// UpdatePricePlan updates a price plan in repository.
func (r *Repository) UpdatePricePlan(ctx context.Context, pricePlan *PricePlan) (*PricePlan, error) {
	collection, err := r.GetPricePlansCollection(ctx)
	if err != nil {
		return nil, err
	}

	pricePlan.UpdatedAt = toolbox.TimeNowUTC()
	if pricePlan.Slug != "" {
		pricePlan.NormaliseSlug()
	}

	err = r.Store.ExecuteUpdateOneCommand(ctx, collection, bson.M{"_id": pricePlan.ID}, buildPricePlanUpdate(pricePlan), "price_plan")
	if err != nil {
		return nil, err
	}

	return pricePlan, nil
}

// buildPricePlanUpdate creates an explicit update document so clearing a
// mutable optional field is persisted instead of being hidden by BSON
// omitempty tags. Lifecycle audit fields are only set when supplied and are
// never removed by a general plan update.
func buildPricePlanUpdate(pricePlan *PricePlan) bson.M {
	setFields := bson.M{
		"slug":          pricePlan.Slug,
		"name":          pricePlan.Name,
		"status":        pricePlan.Status,
		"updated_at":    pricePlan.UpdatedAt,
		"updated_by_id": pricePlan.UpdatedByID,
	}
	unsetFields := bson.M{}

	setOrUnsetPricePlanField(setFields, unsetFields, "description", pricePlan.Description, pricePlan.Description == "")
	setOrUnsetPricePlanField(setFields, unsetFields, "features", pricePlan.Features, len(pricePlan.Features) == 0)
	setOrUnsetPricePlanField(setFields, unsetFields, "costs", pricePlan.Costs, len(pricePlan.Costs) == 0)
	setOrUnsetPricePlanField(setFields, unsetFields, "discounts", pricePlan.Discounts, len(pricePlan.Discounts) == 0)
	setOrUnsetPricePlanField(setFields, unsetFields, "payment_terms", pricePlan.PaymentTerms, pricePlan.PaymentTerms == nil)
	setOrUnsetPricePlanField(setFields, unsetFields, "provider_refs", pricePlan.ProviderRefs, len(pricePlan.ProviderRefs) == 0)
	setOrUnsetPricePlanField(setFields, unsetFields, "metadata", pricePlan.Metadata, len(pricePlan.Metadata) == 0)
	setOrUnsetPricePlanField(setFields, unsetFields, "display_order", pricePlan.DisplayOrder, pricePlan.DisplayOrder == nil)

	setPricePlanAuditField(setFields, "published_at", pricePlan.PublishedAt)
	setPricePlanAuditField(setFields, "published_by_id", pricePlan.PublishedByID)
	setPricePlanAuditField(setFields, "deleted_at", pricePlan.DeletedAt)
	setPricePlanAuditField(setFields, "deleted_by_id", pricePlan.DeletedByID)

	update := bson.M{"$set": setFields}
	if len(unsetFields) > 0 {
		update["$unset"] = unsetFields
	}

	return update
}

// setOrUnsetPricePlanField records value under key in setFields, or marks the
// key for removal in unsetFields when unset is true.
func setOrUnsetPricePlanField(setFields, unsetFields bson.M, key string, value interface{}, unset bool) {
	if unset {
		unsetFields[key] = ""
		return
	}
	setFields[key] = value
}

// setPricePlanAuditField writes a lifecycle audit field only when its value is
// non-empty, so general updates never blank stored audit history.
func setPricePlanAuditField(setFields bson.M, key, value string) {
	if value != "" {
		setFields[key] = value
	}
}

// GetPricePlanByID retrieves a price plan by ID.
func (r *Repository) GetPricePlanByID(ctx context.Context, id string, req *GetPricePlanByIDRequest) (*PricePlan, error) {
	collection, err := r.GetPricePlansCollection(ctx)
	if err != nil {
		return nil, err
	}

	var result PricePlan
	findOptions := options.Find().SetLimit(1)
	applyPricePlanFindProjection(findOptions, pricePlanIncludeOptions{
		includeFeatures:  req != nil && req.IncludeFeatures,
		includeCosts:     req != nil && req.IncludeCosts,
		includeProviders: req != nil && req.IncludeProviders,
	})

	cursor, err := r.Store.ExecuteFindCommand(ctx, collection, bson.M{"_id": id}, findOptions)
	if err != nil {
		return nil, err
	}

	err = r.Store.MapOneInCursorToResult(ctx, cursor, &result, "price_plan")
	if err != nil {
		if errors.Is(err, repository.NewRepositoryError(repository.ErrResourceNotFound, "")) {
			return nil, ErrPricePlanNotFound
		}
		return nil, err
	}

	return &result, nil
}

// GetPricePlanBySlug retrieves a price plan by slug.
func (r *Repository) GetPricePlanBySlug(ctx context.Context, slug string, req *GetPricePlanBySlugRequest) (*PricePlan, error) {
	collection, err := r.GetPricePlansCollection(ctx)
	if err != nil {
		return nil, err
	}

	var result PricePlan
	findOptions := options.Find().SetLimit(1)
	applyPricePlanFindProjection(findOptions, pricePlanIncludeOptions{
		includeFeatures:  req != nil && req.IncludeFeatures,
		includeCosts:     req != nil && req.IncludeCosts,
		includeProviders: req != nil && req.IncludeProviders,
	})

	cursor, err := r.Store.ExecuteFindCommand(ctx, collection, bson.M{"slug": NormalisePriceSlug(slug)}, findOptions)
	if err != nil {
		return nil, err
	}

	err = r.Store.MapOneInCursorToResult(ctx, cursor, &result, "price_plan")
	if err != nil {
		if errors.Is(err, repository.NewRepositoryError(repository.ErrResourceNotFound, "")) {
			return nil, ErrPricePlanNotFound
		}
		return nil, err
	}

	return &result, nil
}

// GetPricePlans retrieves price plans with filters and pagination.
func (r *Repository) GetPricePlans(ctx context.Context, req *GetPricePlansRequest) ([]PricePlan, error) {
	collection, err := r.GetPricePlansCollection(ctx)
	if err != nil {
		return nil, err
	}

	if req == nil {
		req = &GetPricePlansRequest{}
	}

	var results []PricePlan
	findOptions := options.Find()
	paginationLimit := repository.GetPaginationLimit(int64(req.PerPage))
	findOptions.SetLimit(*paginationLimit)
	findOptions.SetSkip(*repository.GetPaginationSkip(int64(req.Page), paginationLimit))
	findOptions.SetSort(buildPricerSortOptions(req.Order))
	applyPricePlanFindProjection(findOptions, pricePlanIncludeOptions{
		includeFeatures:  req.IncludeFeatures,
		includeCosts:     req.IncludeCosts,
		includeProviders: req.IncludeProviders,
	})

	cursor, err := r.Store.ExecuteFindCommand(ctx, collection, buildPricePlanQueryFilter(req), findOptions)
	if err != nil {
		return nil, err
	}

	err = r.Store.MapAllInCursorToResult(ctx, cursor, &results, "price_plans")
	if err != nil {
		return nil, err
	}

	return results, nil
}

// GetTotalPricePlans retrieves the total count of price plans matching filters.
func (r *Repository) GetTotalPricePlans(ctx context.Context, req *GetPricePlansRequest) (int64, error) {
	collection, err := r.GetPricePlansCollection(ctx)
	if err != nil {
		return 0, err
	}

	if req == nil {
		req = &GetPricePlansRequest{}
	}

	return r.Store.ExecuteCountDocuments(ctx, collection, buildPricePlanQueryFilter(req))
}

// PublishPricePlan updates a price plan status and publication metadata.
func (r *Repository) PublishPricePlan(ctx context.Context, id, publishedByID, publishedAt string) error {
	collection, err := r.GetPricePlansCollection(ctx)
	if err != nil {
		return err
	}

	if publishedAt == "" {
		publishedAt = toolbox.TimeNowUTC()
	}

	update := bson.M{
		"$set": bson.M{
			"status":          PricePlanStatusPublished,
			"published_at":    publishedAt,
			"published_by_id": publishedByID,
			"updated_at":      toolbox.TimeNowUTC(),
			"updated_by_id":   publishedByID,
		},
	}

	return r.Store.ExecuteUpdateOneCommand(ctx, collection, bson.M{"_id": id}, update, "price_plan")
}

// ArchivePricePlan updates a price plan status and update metadata.
func (r *Repository) ArchivePricePlan(ctx context.Context, id, updatedByID, updatedAt string) error {
	collection, err := r.GetPricePlansCollection(ctx)
	if err != nil {
		return err
	}

	if updatedAt == "" {
		updatedAt = toolbox.TimeNowUTC()
	}

	update := bson.M{
		"$set": bson.M{
			"status":        PricePlanStatusArchived,
			"updated_at":    updatedAt,
			"updated_by_id": updatedByID,
		},
	}

	return r.Store.ExecuteUpdateOneCommand(ctx, collection, bson.M{"_id": id}, update, "price_plan")
}

// SoftDeletePricePlan soft-deletes a price plan.
func (r *Repository) SoftDeletePricePlan(ctx context.Context, id, deletedByID, deletedAt string) error {
	collection, err := r.GetPricePlansCollection(ctx)
	if err != nil {
		return err
	}

	if deletedAt == "" {
		deletedAt = toolbox.TimeNowUTC()
	}

	update := bson.M{
		"$set": bson.M{
			"status":        PricePlanStatusArchived,
			"deleted_at":    deletedAt,
			"deleted_by_id": deletedByID,
			"updated_at":    toolbox.TimeNowUTC(),
			"updated_by_id": deletedByID,
		},
	}

	return r.Store.ExecuteUpdateOneCommand(ctx, collection, bson.M{"_id": id}, update, "price_plan")
}

// CreateFeature creates a feature catalog item in repository.
func (r *Repository) CreateFeature(ctx context.Context, feature *PriceFeature) (*PriceFeature, error) {
	collection, err := r.GetPriceFeaturesCollection(ctx)
	if err != nil {
		return nil, err
	}

	if feature.ID == "" {
		feature.ID = toolbox.GenerateUuidV4()
	}
	if feature.NanoID == "" {
		feature.NanoID = toolbox.GenerateNanoId()
	}
	if feature.CreatedAt == "" {
		feature.CreatedAt = toolbox.TimeNowUTC()
	}
	if feature.Slug != "" {
		feature.NormaliseSlug()
	}

	_, err = r.Store.ExecuteInsertOneCommand(ctx, collection, feature, "price_feature")
	if err != nil {
		return nil, err
	}

	return feature, nil
}

// UpdateFeature updates a feature catalog item in repository.
func (r *Repository) UpdateFeature(ctx context.Context, feature *PriceFeature) (*PriceFeature, error) {
	collection, err := r.GetPriceFeaturesCollection(ctx)
	if err != nil {
		return nil, err
	}

	feature.UpdatedAt = toolbox.TimeNowUTC()
	if feature.Slug != "" {
		feature.NormaliseSlug()
	}

	err = r.Store.ExecuteUpdateOneCommand(ctx, collection, bson.M{"_id": feature.ID}, bson.M{"$set": feature}, "price_feature")
	if err != nil {
		return nil, err
	}

	return feature, nil
}

// GetFeatureByID retrieves a feature catalog item by ID.
func (r *Repository) GetFeatureByID(ctx context.Context, id string) (*PriceFeature, error) {
	collection, err := r.GetPriceFeaturesCollection(ctx)
	if err != nil {
		return nil, err
	}

	var result PriceFeature
	cursor, err := r.Store.ExecuteFindCommand(ctx, collection, bson.M{"_id": id}, options.Find().SetLimit(1))
	if err != nil {
		return nil, err
	}

	err = r.Store.MapOneInCursorToResult(ctx, cursor, &result, "price_feature")
	if err != nil {
		if errors.Is(err, repository.NewRepositoryError(repository.ErrResourceNotFound, "")) {
			return nil, ErrPriceFeatureNotFound
		}
		return nil, err
	}

	return &result, nil
}

// GetFeatures retrieves feature catalog items with filters and pagination.
func (r *Repository) GetFeatures(ctx context.Context, req *GetFeaturesRequest) ([]PriceFeature, error) {
	collection, err := r.GetPriceFeaturesCollection(ctx)
	if err != nil {
		return nil, err
	}

	if req == nil {
		req = &GetFeaturesRequest{}
	}

	var results []PriceFeature
	findOptions := options.Find()
	paginationLimit := repository.GetPaginationLimit(int64(req.PerPage))
	findOptions.SetLimit(*paginationLimit)
	findOptions.SetSkip(*repository.GetPaginationSkip(int64(req.Page), paginationLimit))
	findOptions.SetSort(buildPricerSortOptions(req.Order))

	cursor, err := r.Store.ExecuteFindCommand(ctx, collection, buildPriceFeatureQueryFilter(req), findOptions)
	if err != nil {
		return nil, err
	}

	err = r.Store.MapAllInCursorToResult(ctx, cursor, &results, "price_features")
	if err != nil {
		return nil, err
	}

	return results, nil
}

// GetTotalFeatures retrieves the total count of feature catalog items matching filters.
func (r *Repository) GetTotalFeatures(ctx context.Context, req *GetFeaturesRequest) (int64, error) {
	collection, err := r.GetPriceFeaturesCollection(ctx)
	if err != nil {
		return 0, err
	}

	if req == nil {
		req = &GetFeaturesRequest{}
	}

	return r.Store.ExecuteCountDocuments(ctx, collection, buildPriceFeatureQueryFilter(req))
}

// SoftDeleteFeature soft-deletes a feature catalog item.
func (r *Repository) SoftDeleteFeature(ctx context.Context, id, deletedByID, deletedAt string) error {
	collection, err := r.GetPriceFeaturesCollection(ctx)
	if err != nil {
		return err
	}

	if deletedAt == "" {
		deletedAt = toolbox.TimeNowUTC()
	}

	update := bson.M{
		"$set": bson.M{
			"deleted_at":    deletedAt,
			"deleted_by_id": deletedByID,
			"updated_at":    toolbox.TimeNowUTC(),
			"updated_by_id": deletedByID,
		},
	}

	return r.Store.ExecuteUpdateOneCommand(ctx, collection, bson.M{"_id": id}, update, "price_feature")
}

// pricePlanIncludeOptions selects which heavy plan subdocuments (features,
// costs, provider refs) a find should project.
type pricePlanIncludeOptions struct {
	includeFeatures  bool
	includeCosts     bool
	includeProviders bool
}

// buildPricePlanQueryFilter translates a GetPricePlansRequest into a Mongo
// filter, applying comma-separated value, date-range, deletion and effective
// publication predicates; unset fields add no constraint.
func buildPricePlanQueryFilter(req *GetPricePlansRequest) bson.M {
	queryFilter := bson.M{"_id": bson.M{"$exists": true}}

	addCommaSeparatedFilter(queryFilter, "status", req.WithStatus)
	addCommaSeparatedFilter(queryFilter, "costs.billing_cadence", req.WithBillingCadence)
	addCommaSeparatedFilter(queryFilter, "costs.currency", req.WithCurrency)
	addCommaSeparatedFilter(queryFilter, "slug", req.Slugs)
	addCommaSeparatedFilter(queryFilter, "created_by_id", req.CreatedByIDs)
	addCommaSeparatedFilter(queryFilter, "published_by_id", req.PublishedByIDs)

	addDateRangeFilter(queryFilter, "created_at", req.CreatedAtFrom, req.CreatedAtTo)
	addDateRangeFilter(queryFilter, "published_at", req.PublishedAtFrom, req.PublishedAtTo)
	addDateRangeFilter(queryFilter, "deleted_at", req.DeletedAtFrom, req.DeletedAtTo)
	addDeletedFilter(queryFilter, req.IsDeleted, req.IsNotDeleted)
	addPricePlanPublishedFilter(queryFilter, req.IsPublished, req.IsNotPublished)

	return queryFilter
}

// addPricePlanPublishedFilter constrains the filter to plans whose status is
// published and whose published_at is present and not in the future, or the
// complement for isNotPublished, using the current UTC time.
func addPricePlanPublishedFilter(queryFilter bson.M, isPublished bool, isNotPublished bool) {
	currentTime := toolbox.TimeNowUTC()

	if isPublished {
		appendAndFilter(queryFilter, bson.M{"status": PricePlanStatusPublished})
		appendAndFilter(queryFilter, bson.M{
			"published_at": bson.M{"$exists": true, "$ne": "", "$lte": currentTime},
		})
	}
	if isNotPublished {
		appendAndFilter(queryFilter, bson.M{
			"$or": []bson.M{
				{"status": bson.M{"$ne": PricePlanStatusPublished}},
				{"published_at": bson.M{"$exists": false}},
				{"published_at": ""},
				{"published_at": bson.M{"$gt": currentTime}},
			},
		})
	}
}

// buildPriceFeatureQueryFilter translates a GetFeaturesRequest into a Mongo
// filter over type, unit, slug, creators, date ranges, deletion and publication
// state; unset fields add no constraint.
func buildPriceFeatureQueryFilter(req *GetFeaturesRequest) bson.M {
	queryFilter := bson.M{"_id": bson.M{"$exists": true}}

	addCommaSeparatedFilter(queryFilter, "type", req.WithTypes)
	addCommaSeparatedFilter(queryFilter, "unit", req.WithUnits)
	addCommaSeparatedFilter(queryFilter, "slug", req.Slugs)
	addCommaSeparatedFilter(queryFilter, "created_by_id", req.CreatedByIDs)
	addCommaSeparatedFilter(queryFilter, "published_by_id", req.PublishedByIDs)

	addDateRangeFilter(queryFilter, "created_at", req.CreatedAtFrom, req.CreatedAtTo)
	addDateRangeFilter(queryFilter, "published_at", req.PublishedAtFrom, req.PublishedAtTo)
	addDateRangeFilter(queryFilter, "deleted_at", req.DeletedAtFrom, req.DeletedAtTo)
	addDeletedFilter(queryFilter, req.IsDeleted, req.IsNotDeleted)
	addPublishedFilter(queryFilter, req.IsPublished, req.IsNotPublished)

	return queryFilter
}

// addCommaSeparatedFilter adds an $in filter for a comma-separated list,
// skipping empty values entirely.
func addCommaSeparatedFilter(queryFilter bson.M, field string, value string) {
	values := toolbox.SplitCommaSeparatedStringAndRemoveEmptyStrings(value)
	if len(values) > 0 {
		queryFilter[field] = bson.M{"$in": values}
	}
}

// addDateRangeFilter adds $gte/$lte string bounds on a field when the
// respective from or to value is non-empty.
func addDateRangeFilter(queryFilter bson.M, field string, from string, to string) {
	dateFilter := bson.M{}
	if from != "" {
		dateFilter["$gte"] = from
	}
	if to != "" {
		dateFilter["$lte"] = to
	}
	if len(dateFilter) > 0 {
		queryFilter[field] = dateFilter
	}
}

// addDeletedFilter matches documents with a non-empty deleted_at, or absent and
// empty deleted_at when isNotDeleted is set.
func addDeletedFilter(queryFilter bson.M, isDeleted bool, isNotDeleted bool) {
	if isDeleted {
		queryFilter["deleted_at"] = bson.M{"$exists": true, "$ne": ""}
	}
	if isNotDeleted {
		appendAndFilter(queryFilter, bson.M{
			"$or": []bson.M{
				{"deleted_at": bson.M{"$exists": false}},
				{"deleted_at": ""},
			},
		})
	}
}

// addPublishedFilter matches a present, non-empty published_at that is not in
// the future, or its complement when isNotPublished is set.
func addPublishedFilter(queryFilter bson.M, isPublished bool, isNotPublished bool) {
	currentTime := toolbox.TimeNowUTC()

	if isPublished {
		queryFilter["published_at"] = bson.M{"$exists": true, "$ne": "", "$lte": currentTime}
	}
	if isNotPublished {
		appendAndFilter(queryFilter, bson.M{
			"$or": []bson.M{
				{"published_at": bson.M{"$exists": false}},
				{"published_at": ""},
				{"published_at": bson.M{"$gt": currentTime}},
			},
		})
	}
}

// appendAndFilter appends filter to the query's $and array, creating the array
// when absent.
func appendAndFilter(queryFilter bson.M, filter bson.M) {
	if existing, ok := queryFilter["$and"].([]bson.M); ok {
		queryFilter["$and"] = append(existing, filter)
		return
	}

	queryFilter["$and"] = []bson.M{filter}
}

// buildPricerSortOptions maps a named sort order onto the corresponding single-
// key BSON sort, defaulting to created_at descending.
func buildPricerSortOptions(order string) bson.D {
	switch order {
	case "created_at_asc":
		return bson.D{{Key: "created_at", Value: 1}}
	case "created_at_desc":
		return bson.D{{Key: "created_at", Value: -1}}
	case "updated_at_asc":
		return bson.D{{Key: "updated_at", Value: 1}}
	case "updated_at_desc":
		return bson.D{{Key: "updated_at", Value: -1}}
	case "deleted_at_asc":
		return bson.D{{Key: "deleted_at", Value: 1}}
	case "deleted_at_desc":
		return bson.D{{Key: "deleted_at", Value: -1}}
	case "published_at_asc":
		return bson.D{{Key: "published_at", Value: 1}}
	case "published_at_desc":
		return bson.D{{Key: "published_at", Value: -1}}
	case "name_asc":
		return bson.D{{Key: "name", Value: 1}}
	case "name_desc":
		return bson.D{{Key: "name", Value: -1}}
	case "slug_asc":
		return bson.D{{Key: "slug", Value: 1}}
	case "slug_desc":
		return bson.D{{Key: "slug", Value: -1}}
	case "display_order_asc":
		return bson.D{{Key: "display_order", Value: 1}}
	case "display_order_desc":
		return bson.D{{Key: "display_order", Value: -1}}
	default:
		return bson.D{{Key: "created_at", Value: -1}}
	}
}

// applyPricePlanFindProjection sets an exclusion projection on the find options
// for any heavy plan fields not selected for inclusion.
func applyPricePlanFindProjection(findOptions *options.FindOptionsBuilder, includeOptions pricePlanIncludeOptions) {
	projection := buildPricePlanProjection(includeOptions)
	if len(projection) > 0 {
		findOptions.SetProjection(projection)
	}
}

// buildPricePlanProjection excludes features, costs or provider_refs when the
// corresponding include option is false; full inclusion yields an empty
// projection.
func buildPricePlanProjection(includeOptions pricePlanIncludeOptions) bson.M {
	projection := bson.M{}
	if !includeOptions.includeFeatures {
		projection["features"] = 0
	}
	if !includeOptions.includeCosts {
		projection["costs"] = 0
	}
	if !includeOptions.includeProviders {
		projection["provider_refs"] = 0
	}

	return projection
}
