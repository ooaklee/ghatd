package blueprint

import (
	"context"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/ooaklee/ghatd/external/logger"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.uber.org/zap"
)

const defaultCollectionInitMaxAttemptsLimit = 3

// MongoDbStore is the shared-helper persistence port. Update/delete receipts
// must retain acknowledgement and counts; an error-only adapter cannot prove
// the selected record existed. Implementations preserve native dependency errors.
type MongoDbStore interface {
	// ExecuteCountDocuments counts documents matching the filter on the given
	// collection through the MongoDbStore persistence port, applying optional count
	// options, and returns the count or an error.
	ExecuteCountDocuments(ctx context.Context, collection *mongo.Collection, filter interface{}, opts ...options.Lister[options.CountOptions]) (int64, error)
	// ExecuteDeleteOneCommandResult deletes at most one matching document through
	// the MongoDbStore persistence port; the returned mongo.DeleteResult must
	// retain acknowledgement and counts so the selected record's existence is
	// provable.
	ExecuteDeleteOneCommandResult(ctx context.Context, collection *mongo.Collection, filter any, opts ...options.Lister[options.DeleteOneOptions]) (*mongo.DeleteResult, error)
	// ExecuteFindCommand runs a query on the given collection with optional find
	// options through the MongoDbStore persistence port and returns a cursor over
	// matching documents, or an error.
	ExecuteFindCommand(ctx context.Context, collection *mongo.Collection, filter interface{}, opts ...options.Lister[options.FindOptions]) (*mongo.Cursor, error)
	// ExecuteFindOneCommandDecodeResult fetches one matching document and decodes
	// it into result through the MongoDbStore persistence port; logError controls
	// error logging and onFailureErr is returned when no document matches.
	ExecuteFindOneCommandDecodeResult(ctx context.Context, collection *mongo.Collection, filter interface{}, result interface{}, resultObjectName string, logError bool, onFailureErr error) error
	// ExecuteInsertOneCommand inserts one document into the given collection
	// through the MongoDbStore persistence port, using resultObjectName for
	// telemetry, and returns the insertion receipt or an error.
	ExecuteInsertOneCommand(ctx context.Context, collection *mongo.Collection, document interface{}, resultObjectName string) (*mongo.InsertOneResult, error)
	// ExecuteUpdateOneCommandResult applies a single-document update through the
	// MongoDbStore persistence port; the returned mongo.UpdateResult must retain
	// acknowledgement and counts, and native dependency errors are preserved.
	ExecuteUpdateOneCommandResult(ctx context.Context, collection *mongo.Collection, filter, update any, opts ...options.Lister[options.UpdateOneOptions]) (*mongo.UpdateResult, error)
	// GetDatabase returns the mongo.Database handle for the given database name
	// through the MongoDbStore persistence port, or an error.
	GetDatabase(ctx context.Context, dbName string) (*mongo.Database, error)
	// InitialiseClient creates the underlying MongoDB client for the MongoDbStore
	// persistence port and returns it, or an error.
	InitialiseClient(ctx context.Context) (*mongo.Client, error)
	// MapAllInCursorToResult decodes all remaining cursor documents into result
	// through the MongoDbStore persistence port, using resultObjectName for
	// telemetry, and reports decoding failures as an error.
	MapAllInCursorToResult(ctx context.Context, cursor *mongo.Cursor, result interface{}, resultObjectName string) error
}

// Repository uses managed Mongo helpers while owning domain result semantics.
// Configure Store before concurrent use; the repository does not own its client.
type Repository struct {
	// Store supplies driver operations and safe shared-helper telemetry.
	Store MongoDbStore
	// collectionInitMaxAttemptsLimit bounds setup retries, never mutation retries.
	collectionInitMaxAttemptsLimit int
	// collection is initialized once under collectionMutex and reused.
	collection *mongo.Collection
	// collectionMutex serializes setup and retry-limit configuration.
	collectionMutex sync.Mutex
}

// NewRepository reuses the caller-owned store and defaults to three setup attempts.
func NewRepository(store MongoDbStore) *Repository {
	return &Repository{Store: store, collectionInitMaxAttemptsLimit: defaultCollectionInitMaxAttemptsLimit}
}

// WithCollectionInitMaxAttemptsLimit changes the setup retry budget. Nonpositive
// values retain the current setting; a nil receiver remains nil.
func (r *Repository) WithCollectionInitMaxAttemptsLimit(limit int) *Repository {
	if r != nil && limit > 0 {
		r.collectionMutex.Lock()
		r.collectionInitMaxAttemptsLimit = limit
		r.collectionMutex.Unlock()
	}
	return r
}

// validateStorageEntry rejects invalid wiring and cancelled contexts before logs or I/O.
func (r *Repository) validateStorageEntry(ctx context.Context) error {
	if ctx == nil {
		return ErrBlueprintInvalidPayload
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if r == nil || nilBlueprintDependency(r.Store) {
		return ErrBlueprintUnavailable
	}
	return nil
}

// GetBlueprintCollection caches the managed collection after bounded setup
// attempts. Cancellation is checked on both sides of the initialization lock
// and between dependencies. Final setup errors wrap only the original cause;
// missing/nil wiring is unavailable, not evidence of a missing record.
func (r *Repository) GetBlueprintCollection(ctx context.Context) (*mongo.Collection, error) {
	if err := r.validateStorageEntry(ctx); err != nil {
		return nil, err
	}
	r.collectionMutex.Lock()
	defer r.collectionMutex.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if r.collection != nil {
		return r.collection, nil
	}
	log := logger.AcquireOperationFrom(ctx, "internal/blueprint", "get-blueprint-collection")
	limit := r.collectionInitMaxAttemptsLimit
	if limit <= 0 {
		limit = defaultCollectionInitMaxAttemptsLimit
	}
	var lastErr error
	for attempt := 1; attempt <= limit; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		client, err := r.Store.InitialiseClient(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return nil, err
			}
			lastErr = err
			log.Warn("blueprint-collection-client-init-failed", zap.Int("attempt", attempt))
			continue
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if client == nil {
			return nil, ErrBlueprintUnavailable
		}
		db, err := r.Store.GetDatabase(ctx, "")
		if err != nil {
			if ctx.Err() != nil {
				return nil, err
			}
			lastErr = err
			log.Warn("blueprint-collection-database-resolve-failed", zap.Int("attempt", attempt))
			continue
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if db == nil {
			return nil, ErrBlueprintUnavailable
		}
		r.collection = db.Collection(BlueprintCollection)
		log.Debug("blueprint-collection-ready", zap.Int("attempt", attempt))
		return r.collection, nil
	}
	log.Error("blueprint-collection-initialisation-failed", zap.Int("max-attempts", limit))
	return nil, fmt.Errorf("blueprint collection initialization after %d attempts: %w", limit, lastErr)
}

// CreateBlueprint inserts a scalar snapshot and requires an acknowledged receipt
// for its pinned string ID. Uncertain outcomes do not authorize retries. Nested
// Metadata remains read-only by convention rather than deeply copied.
func (r *Repository) CreateBlueprint(ctx context.Context, value *Blueprint) (*Blueprint, error) {
	if err := r.validateStorageEntry(ctx); err != nil {
		return nil, err
	}
	if value == nil {
		return nil, ErrBlueprintInvalidPayload
	}
	document := *value
	id := document.ID
	if strings.TrimSpace(id) == "" {
		return nil, ErrBlueprintIDIsRequired
	}
	collection, err := r.GetBlueprintCollection(ctx)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	result, err := r.Store.ExecuteInsertOneCommand(ctx, collection, &document, "blueprint")
	if err != nil {
		return nil, err
	}
	if result == nil || !result.Acknowledged || document.ID != id {
		return nil, ErrBlueprintUnavailable
	}
	insertedID, ok := result.InsertedID.(string)
	if !ok || insertedID != id {
		return nil, ErrBlueprintUnavailable
	}
	return &document, nil
}

// blueprintAbsent recognizes only mongo.ErrNoDocuments or the domain sentinel
// through a bounded single-cause unwrap chain. Custom Is aliases and joined
// causes cannot turn an operational failure into authoritative absence.
func blueprintAbsent(err error) bool {
	for depth := 0; err != nil && depth < 64; depth++ {
		if err == mongo.ErrNoDocuments || err == ErrBlueprintResourceNotFound {
			return true
		}
		if nilBlueprintDependency(err) {
			return false
		}
		err = errors.Unwrap(err)
	}
	return false
}

// findBlueprint preserves decode/driver errors and maps only confirmed absence.
// The shared helper receives no replacement error, so mixed native causes survive.
func (r *Repository) findBlueprint(ctx context.Context, filter bson.M) (*Blueprint, error) {
	collection, err := r.GetBlueprintCollection(ctx)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var result Blueprint
	err = r.Store.ExecuteFindOneCommandDecodeResult(ctx, collection, filter, &result, "blueprint", true, nil)
	if err != nil {
		if blueprintAbsent(err) {
			return nil, ErrBlueprintResourceNotFound
		}
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(result.ID) == "" {
		return nil, ErrBlueprintUnavailable
	}
	return &result, nil
}

// GetBlueprintByID rejects absent identifiers and validates the selected identity.
func (r *Repository) GetBlueprintByID(ctx context.Context, id string) (*Blueprint, error) {
	if err := r.validateStorageEntry(ctx); err != nil {
		return nil, err
	}
	if strings.TrimSpace(id) == "" {
		return nil, ErrBlueprintIDIsRequired
	}
	result, err := r.findBlueprint(ctx, bson.M{"_id": id})
	if err != nil {
		return nil, err
	}
	if result.ID != id {
		return nil, ErrBlueprintUnavailable
	}
	return result, nil
}

// GetBlueprintByNameAndKind normalizes the natural key before querying and checks
// the decoded key. It does not establish uniqueness without a database index.
func (r *Repository) GetBlueprintByNameAndKind(ctx context.Context, name, kind string) (*Blueprint, error) {
	if err := r.validateStorageEntry(ctx); err != nil {
		return nil, err
	}
	name, kind = normaliseBlueprintName(name), normaliseBlueprintKind(kind)
	if name == "" {
		return nil, ErrBlueprintNameIsRequired
	}
	if kind == "" {
		return nil, ErrBlueprintKindIsRequired
	}
	result, err := r.findBlueprint(ctx, bson.M{"name": name, "kind": kind})
	if err != nil {
		return nil, err
	}
	if result.Name != name || result.Kind != kind {
		return nil, ErrBlueprintUnavailable
	}
	return result, nil
}

// closeBlueprintCursor bounds best-effort cleanup even after caller cancellation.
// Cursor.Close is idempotent when the shared mapper already consumed the cursor.
func closeBlueprintCursor(cursor *mongo.Cursor) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = cursor.Close(ctx)
}

// GetBlueprints lists literal-text search matches with independent scalar filter
// input. Legacy nonpositive page/page-size defaults are retained; overflowing
// offsets are rejected before setup or I/O. The repository owns cursor cleanup.
func (r *Repository) GetBlueprints(ctx context.Context, req *GetBlueprintsRequest) ([]Blueprint, error) {
	if err := r.validateStorageEntry(ctx); err != nil {
		return nil, err
	}
	input := GetBlueprintsRequest{}
	if req != nil {
		input = *req
	}
	page, size := normalisePagination(&input)
	if size > 0 && page-1 > math.MaxInt64/size {
		return nil, ErrBlueprintInvalidQueryParam
	}
	filter := buildBlueprintListFilter(&input)
	findOptions := options.Find().SetSort(bson.D{{Key: "created_at", Value: -1}, {Key: "name", Value: 1}})
	if size > 0 {
		findOptions.SetLimit(size).SetSkip((page - 1) * size)
	}
	collection, err := r.GetBlueprintCollection(ctx)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	cursor, err := r.Store.ExecuteFindCommand(ctx, collection, filter, findOptions)
	if cursor != nil {
		defer closeBlueprintCursor(cursor)
	}
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if cursor == nil {
		return nil, ErrBlueprintUnavailable
	}
	var result []Blueprint
	if err := r.Store.MapAllInCursorToResult(ctx, cursor, &result, "blueprints"); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

// GetTotalBlueprints preserves native count errors and rejects invalid negative
// counts. It is independent of listing, not a transactional list/count snapshot.
func (r *Repository) GetTotalBlueprints(ctx context.Context, req *GetBlueprintsRequest) (int64, error) {
	if err := r.validateStorageEntry(ctx); err != nil {
		return 0, err
	}
	input := GetBlueprintsRequest{}
	if req != nil {
		input = *req
	}
	filter := buildBlueprintListFilter(&input)
	collection, err := r.GetBlueprintCollection(ctx)
	if err != nil {
		return 0, err
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	total, err := r.Store.ExecuteCountDocuments(ctx, collection, filter)
	if err != nil {
		return 0, err
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if total < 0 {
		return 0, ErrBlueprintUnavailable
	}
	return total, nil
}

// UpdateBlueprint updates the selected record from a scalar snapshot without
// upsert. A matched no-op succeeds; zero match means absence. An acknowledged
// write remains acknowledged if cancellation occurs after the driver returns.
func (r *Repository) UpdateBlueprint(ctx context.Context, value *Blueprint) (*Blueprint, error) {
	if err := r.validateStorageEntry(ctx); err != nil {
		return nil, err
	}
	if value == nil {
		return nil, ErrBlueprintInvalidPayload
	}
	document := *value
	id := document.ID
	if strings.TrimSpace(id) == "" {
		return nil, ErrBlueprintIDIsRequired
	}
	collection, err := r.GetBlueprintCollection(ctx)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	result, err := r.Store.ExecuteUpdateOneCommandResult(ctx, collection, bson.M{"_id": id}, bson.M{"$set": &document})
	if err != nil {
		return nil, err
	}
	if result == nil || !result.Acknowledged || document.ID != id || result.UpsertedID != nil || result.UpsertedCount != 0 || result.MatchedCount < 0 || result.MatchedCount > 1 || result.ModifiedCount < 0 || result.ModifiedCount > result.MatchedCount {
		return nil, ErrBlueprintUnavailable
	}
	if result.MatchedCount == 0 {
		return nil, ErrBlueprintResourceNotFound
	}
	return &document, nil
}

// DeleteBlueprintByID requires one acknowledged deletion. Missing records return
// the domain absence error; uncertain or malformed receipts cannot report success.
func (r *Repository) DeleteBlueprintByID(ctx context.Context, id string) error {
	if err := r.validateStorageEntry(ctx); err != nil {
		return err
	}
	if strings.TrimSpace(id) == "" {
		return ErrBlueprintIDIsRequired
	}
	collection, err := r.GetBlueprintCollection(ctx)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	result, err := r.Store.ExecuteDeleteOneCommandResult(ctx, collection, bson.M{"_id": id})
	if err != nil {
		return err
	}
	if result == nil || !result.Acknowledged || result.DeletedCount < 0 || result.DeletedCount > 1 {
		return ErrBlueprintUnavailable
	}
	if result.DeletedCount == 0 {
		return ErrBlueprintResourceNotFound
	}
	return nil
}

// buildBlueprintListFilter preserves literal search text; it never accepts raw regex.
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

// normalisePagination retains the template's page-one and unbounded-size defaults.
// The query builder separately checks multiplication overflow before computing skip.
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
