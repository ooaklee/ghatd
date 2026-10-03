package apitoken

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ooaklee/ghatd/external/repository"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

const (
	// MongoRegexStringFormat holds format string for case insensitive regex mapping
	// in mongo queries
	MongoRegexStringFormat = ".*%s.*"
)

// ApiTokenCollection collection name for api tokens
const ApiTokenCollection string = "apitokens"

const defaultCollectionInitMaxAttemptsLimit = 3

// GetAPITokenByDigest performs an exact owner/digest lookup rather than scanning
// token pages. It intentionally returns no secret and never logs the filter.
// The service separately checks identity, status and expiry before acceptance.
func (r *Repository) GetAPITokenByDigest(ctx context.Context, nanoID string, digest []byte) (*UserAPIToken, error) {
	if nanoID == "" || len(digest) != 32 {
		return nil, ErrUnableToValidateUserAPIToken
	}
	collection, err := r.GetApiTokenCollection(ctx)
	if err != nil {
		return nil, err
	}
	var result UserAPIToken
	if err := r.Store.ExecuteFindOneCommandDecodeResult(ctx, collection, bson.M{"created_by_nid": nanoID, "value_sha": digest}, &result, "ApiToken", false, ErrUnableToValidateUserAPIToken); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ErrUnableToValidateUserAPIToken
		}
		return nil, err
	}
	result.Value = ""
	return &result, nil
}

// TouchAPIToken atomically updates only usage telemetry for an active, exactly
// matched credential. A deleted, replaced or revoked token is never recreated.
// Shared repository telemetry never logs the digest filter or raw errors.
func (r *Repository) TouchAPIToken(ctx context.Context, tokenID, ownerID string, digest []byte, at time.Time) error {
	if tokenID == "" || ownerID == "" || len(digest) != 32 || at.IsZero() {
		return ErrNoMatchingUserAPITokenFound
	}
	collection, err := r.GetApiTokenCollection(ctx)
	if err != nil {
		return err
	}
	result, err := r.Store.ExecuteUpdateOneCommandResult(ctx, collection,
		bson.M{"_id": tokenID, "created_by_id": ownerID, "value_sha": digest, "status": UserTokenStatusKeyActive},
		bson.M{"$set": bson.M{"last_used_at": at.UTC().Format(time.RFC3339Nano)}},
	)
	if err != nil {
		return err
	}
	if result == nil {
		return ErrServiceUnavailable
	}
	if result.MatchedCount != 1 {
		return ErrNoMatchingUserAPITokenFound
	}
	return nil
}

// MongoDbStore represents the datastore to hold resource data
type MongoDbStore interface {
	// Result-bearing writes preserve matched/deleted counts for ownership checks.
	ExecuteUpdateOneCommandResult(context.Context, *mongo.Collection, any, any, ...options.Lister[options.UpdateOneOptions]) (*mongo.UpdateResult, error)
	ExecuteDeleteOneCommandResult(context.Context, *mongo.Collection, any, ...options.Lister[options.DeleteOneOptions]) (*mongo.DeleteResult, error)
	ExecuteCountDocuments(ctx context.Context, collection *mongo.Collection, filter interface{}, opts ...options.Lister[options.CountOptions]) (int64, error)
	ExecuteDeleteOneCommand(ctx context.Context, collection *mongo.Collection, filter interface{}, targetObjectName string) error
	ExecuteFindCommand(ctx context.Context, collection *mongo.Collection, filter interface{}, opts ...options.Lister[options.FindOptions]) (*mongo.Cursor, error)
	ExecuteInsertOneCommand(ctx context.Context, collection *mongo.Collection, document interface{}, resultObjectName string) (*mongo.InsertOneResult, error)
	ExecuteUpdateOneCommand(ctx context.Context, collection *mongo.Collection, filter interface{}, updateFilter interface{}, resultObjectName string) error
	ExecuteDeleteManyCommand(ctx context.Context, collection *mongo.Collection, filter interface{}, targetObjectName string) error
	ExecuteFindOneCommandDecodeResult(ctx context.Context, collection *mongo.Collection, filter interface{}, result interface{}, resultObjectName string, logError bool, onFailureErr error) error

	GetDatabase(ctx context.Context, dbName string) (*mongo.Database, error)
	InitialiseClient(ctx context.Context) (*mongo.Client, error)
	MapAllInCursorToResult(ctx context.Context, cursor *mongo.Cursor, result interface{}, resultObjectName string) error
	MapOneInCursorToResult(ctx context.Context, cursor *mongo.Cursor, result interface{}, resultObjectName string) error
}

// Repository stores credentials through the shared Mongo helpers. It caches the
// collection safely; optional transactional inventory preparation is explicit.
type Repository struct {
	// Store owns the managed client and operation logging; configure before use.
	Store                          MongoDbStore
	collectionInitMaxAttemptsLimit int

	collection      *mongo.Collection
	collectionMutex sync.Mutex
	// inventoryReady is set only after explicit transactional startup setup.
	inventoryReady atomic.Bool
}

// NewRepository initiates new instance of repository
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

// GetApiTokenCollection returns collection used for api token domain
func (r *Repository) GetApiTokenCollection(ctx context.Context) (*mongo.Collection, error) {
	if r == nil || r.Store == nil || ctx == nil {
		return nil, ErrServiceUnavailable
	}
	if err := ctx.Err(); err != nil {
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

	var lastErr error
	collectionInitMaxAttemptsLimit := r.collectionInitMaxAttemptsLimit
	if collectionInitMaxAttemptsLimit <= 0 {
		collectionInitMaxAttemptsLimit = defaultCollectionInitMaxAttemptsLimit
	}
	for attempt := 1; attempt <= collectionInitMaxAttemptsLimit; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
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

		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if db == nil {
			return nil, ErrServiceUnavailable
		}
		r.collection = db.Collection(ApiTokenCollection)
		return r.collection, nil
	}

	return nil, fmt.Errorf("unable to initialise %s collection after %d attempts: %w", ApiTokenCollection, collectionInitMaxAttemptsLimit, lastErr)
}

// DeleteResourcesByOwnerId deletes all token resources that belongs to the specified user id
func (r *Repository) DeleteResourcesByOwnerId(ctx context.Context, ownerId string) error {
	if ownerId == "" {
		return ErrRequiredUserIDMissing
	}

	var filter bson.M

	filter = bson.M{"created_by_id": ownerId}

	collection, err := r.GetApiTokenCollection(ctx)
	if err != nil {
		return err
	}

	err = r.Store.ExecuteDeleteManyCommand(ctx, collection, filter, "ApiTokens")
	if err != nil {
		return err
	}

	return nil
}

// GetTotalApiTokens total api token from DB that match passed arguments
func (r *Repository) GetTotalApiTokens(ctx context.Context, userId, userNanoId, descriptionFilter, statusFilter, to, from string, onlyEphemeral bool, onlyPermanent bool) (int64, error) {
	if onlyEphemeral && onlyPermanent {
		return 0, ErrInvalidTokenQuery
	}

	apiTokenFilter := bson.M{"_id": bson.M{"$exists": true}}

	if userId != "" {
		apiTokenFilter["created_by_id"] = userId
	}

	if userNanoId != "" {
		apiTokenFilter["created_by_nid"] = userNanoId
	}

	if descriptionFilter != "" {
		apiTokenFilter["description"] = bson.Regex{
			Pattern: fmt.Sprintf(MongoRegexStringFormat, regexp.QuoteMeta(descriptionFilter)),
			Options: "i",
		}
	}

	if statusFilter != "" {
		apiTokenFilter["status"] = bson.Regex{
			Pattern: fmt.Sprintf(MongoRegexStringFormat, regexp.QuoteMeta(statusFilter)),
			Options: "i",
		}
	}

	if onlyEphemeral {
		apiTokenFilter["ttl_expires_at"] = bson.M{"$nin": bson.A{nil, ""}}
	}

	if onlyPermanent {
		apiTokenFilter["ttl_expires_at"] = bson.M{"$in": bson.A{nil, ""}}
	}

	if to != "" || from != "" {

		timeRangeFilter := bson.M{}

		if from != "" {
			timeRangeFilter["$gt"] = from
		}

		if to != "" {
			timeRangeFilter["$lt"] = to
		}

		apiTokenFilter["created_at"] = timeRangeFilter
	}

	collection, err := r.GetApiTokenCollection(ctx)
	if err != nil {
		return 0, err
	}

	return r.Store.ExecuteCountDocuments(ctx, collection, apiTokenFilter)
}

// CreateUserAPIToken creates an user apitoken in the DB
func (r *Repository) CreateUserAPIToken(ctx context.Context, apiToken *UserAPIToken) (*UserAPIToken, error) {
	if apiToken == nil || apiToken.CreatedByID == "" {
		return nil, ErrRequiredUserIDMissing
	}

	collection, err := r.GetApiTokenCollection(ctx)
	if err != nil {
		return nil, err
	}

	apiToken.Generate().GenerateNewUUID()

	result, err := r.Store.ExecuteInsertOneCommand(ctx, collection, apiToken, "api-token")
	if err != nil {
		return nil, err
	}
	if result == nil {
		return nil, ErrServiceUnavailable
	}
	insertedID, ok := result.InsertedID.(string)
	if !ok || insertedID != apiToken.ID {
		return nil, ErrServiceUnavailable
	}

	return apiToken, nil
}

// UpdateAPIToken is a trusted legacy whole-record update, not an owner-checked
// management operation. Callers must prevent stale authority overwrites.
// Deprecated: use SetAPITokenStatusFor or TouchAPIToken for field-only mutations.
func (r *Repository) UpdateAPIToken(ctx context.Context, apiToken *UserAPIToken) (*UserAPIToken, error) {
	if apiToken == nil || apiToken.ID == "" {
		return nil, ErrResourceNotFound
	}

	collection, err := r.GetApiTokenCollection(ctx)
	if err != nil {
		return nil, err
	}

	apiToken.SetUpdatedAtTimeToNow()

	err = r.Store.ExecuteUpdateOneCommand(ctx, collection, bson.M{"_id": apiToken.ID}, bson.M{"$set": apiToken}, "api-token")
	if err != nil {
		return nil, err
	}

	return apiToken, nil

}

// DeleteAPITokenByID is a trusted administrative deletion without an owner check.
// Deprecated: use DeleteAPITokenFor for owner-bound credential management.
func (r *Repository) DeleteAPITokenByID(ctx context.Context, apiTokenID string) error {
	if apiTokenID == "" {
		return ErrResourceNotFound
	}
	deleteFilter := bson.M{"_id": apiTokenID}

	collection, err := r.GetApiTokenCollection(ctx)
	if err != nil {
		return err
	}

	return r.Store.ExecuteDeleteOneCommand(ctx, collection, deleteFilter, "ApiToken")

}

// GetAPITokenByID returns the apitoken with matching id
func (r *Repository) GetAPITokenByID(ctx context.Context, apiTokenID string) (*UserAPIToken, error) {
	var result UserAPIToken

	collection, err := r.GetApiTokenCollection(ctx)
	if err != nil {
		return nil, err
	}

	err = r.Store.ExecuteFindOneCommandDecodeResult(ctx, collection, bson.M{"_id": apiTokenID}, &result, "ApiToken", true, ErrResourceNotFound)
	if err != nil {
		return nil, err
	}

	return &result, nil

}

// GetAPITokens returns apitokens matching filters from the DB
func (r *Repository) GetAPITokens(ctx context.Context, req *GetAPITokensRequest) ([]UserAPIToken, error) {
	if req == nil || (req.OnlyEphemeral && req.OnlyPermanent) {
		return nil, ErrInvalidTokenQuery
	}
	var (
		result          []UserAPIToken
		queryFilter     bson.D = bson.D{}
		requestFilter   bson.D = bson.D{}
		paginationLimit *int64 = repository.GetPaginationLimit(int64(req.PerPage))
	)

	findOptions := options.Find()

	findOptions.SetLimit(*paginationLimit)
	findOptions.SetSkip(*repository.GetPaginationSkip(int64(req.Page), paginationLimit))

	// generate query filter from request
	if req.Description != "" {
		queryFilter = append(queryFilter, bson.E{Key: "description", Value: bson.Regex{
			Pattern: fmt.Sprintf(MongoRegexStringFormat, regexp.QuoteMeta(req.Description)),
			Options: "i",
		},
		})
	}

	if req.Status != "" {
		queryFilter = append(queryFilter, bson.E{Key: "status", Value: bson.Regex{
			Pattern: fmt.Sprintf(MongoRegexStringFormat, regexp.QuoteMeta(req.Status)),
			Options: "i",
		},
		})
	}

	if req.CreatedByID != "" {
		queryFilter = append(queryFilter, bson.E{Key: "created_by_id", Value: req.CreatedByID})
	}

	if req.CreatedByNanoId != "" {
		queryFilter = append(queryFilter, bson.E{Key: "created_by_nid", Value: req.CreatedByNanoId})
	}

	if req.OnlyEphemeral {
		queryFilter = append(queryFilter, bson.E{Key: "ttl_expires_at", Value: bson.M{"$nin": bson.A{nil, ""}}})
	}

	if req.OnlyPermanent {
		queryFilter = append(queryFilter, bson.E{Key: "ttl_expires_at", Value: bson.M{"$in": bson.A{nil, ""}}})
	}

	// generate sort filter from request
	switch req.Order {
	case "created_at_asc":
		requestFilter = append(requestFilter, bson.E{Key: "created_at", Value: 1})
	case "created_at_desc":
		requestFilter = append(requestFilter, bson.E{Key: "created_at", Value: -1})

	case "last_used_at_asc":
		requestFilter = append(requestFilter, bson.E{Key: "last_used_at", Value: 1})
	case "last_used_at_desc":
		requestFilter = append(requestFilter, bson.E{Key: "last_used_at", Value: -1})

	case "updated_at_asc":
		requestFilter = append(requestFilter, bson.E{Key: "updated_at", Value: 1})
	case "updated_at_desc":
		requestFilter = append(requestFilter, bson.E{Key: "updated_at", Value: -1})

	default:
		requestFilter = append(requestFilter, bson.E{Key: "created_at", Value: -1})
	}

	// Sort by request field
	requestFilter = append(requestFilter, bson.E{Key: "_id", Value: 1})
	findOptions.SetSort(requestFilter)

	collection, err := r.GetApiTokenCollection(ctx)
	if err != nil {
		return nil, err
	}

	c, err := r.Store.ExecuteFindCommand(ctx, collection, queryFilter, findOptions)
	if err != nil {
		return nil, err
	}

	if err = r.Store.MapAllInCursorToResult(ctx, c, &result, "apitoken"); err != nil {
		return nil, err
	}

	return result, nil
}

// DeleteAPITokenFor deletes only the exact owner/credential pair, regardless of
// expiry or status. An absent or differently owned record is indistinguishable.
func (r *Repository) DeleteAPITokenFor(ctx context.Context, userID string, apiTokenID string) error {
	if ctx == nil || userID == "" || apiTokenID == "" {
		return ErrResourceNotFound
	}
	collection, err := r.GetApiTokenCollection(ctx)
	if err != nil {
		return err
	}
	result, err := r.Store.ExecuteDeleteOneCommandResult(ctx, collection, bson.M{"_id": apiTokenID, "created_by_id": userID})
	if err != nil {
		return err
	}
	if result == nil {
		return ErrServiceUnavailable
	}
	if result.DeletedCount != 1 {
		return ErrResourceNotFound
	}
	return nil
}

// SetAPITokenStatusFor updates only status and its change time for the exact
// owner. It never upserts, rewrites secrets/expiry or uses a stale read/replace.
// Repeating the same desired state succeeds if the owner/record still matches.
func (r *Repository) SetAPITokenStatusFor(ctx context.Context, ownerID, tokenID, status string) error {
	if ctx == nil || ownerID == "" || tokenID == "" {
		return ErrResourceNotFound
	}
	if status != UserTokenStatusKeyActive && status != UserTokenStatusKeyRevoked {
		return ErrTokenStatusInvalid
	}
	collection, err := r.GetApiTokenCollection(ctx)
	if err != nil {
		return err
	}
	result, err := r.Store.ExecuteUpdateOneCommandResult(ctx, collection, bson.M{"_id": tokenID, "created_by_id": ownerID}, bson.M{"$set": bson.M{"status": status, "updated_at": time.Now().UTC().Format(time.RFC3339Nano)}})
	if err != nil {
		return err
	}
	if result == nil {
		return ErrServiceUnavailable
	}
	if result.MatchedCount != 1 {
		return ErrResourceNotFound
	}
	return nil
}
