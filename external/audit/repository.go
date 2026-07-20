package audit

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// AuditCollection collection name for audit events
const AuditCollection string = "audit"

const defaultCollectionInitMaxAttemptsLimit = 3

// MongoDbStore represents the datastore to hold resource data
type MongoDbStore interface {
	ExecuteInsertOneCommand(ctx context.Context, collection *mongo.Collection, document interface{}, resultObjectName string) (*mongo.InsertOneResult, error)
	ExecuteCountDocuments(ctx context.Context, collection *mongo.Collection, filter interface{}, opts ...options.Lister[options.CountOptions]) (int64, error)
	// ExecuteDeleteOneCommand(ctx context.Context, collection *mongo.Collection, filter interface{}, targetObjectName string) error
	// ExecuteFindCommand(ctx context.Context, collection *mongo.Collection, filter interface{}, opts ...options.Lister[options.FindOptions]) (*mongo.Cursor, error)
	// ExecuteUpdateOneCommand(ctx context.Context, collection *mongo.Collection, filter interface{}, updateFilter interface{}, resultObjectName string) error
	// ExecuteDeleteManyCommand(ctx context.Context, collection *mongo.Collection, filter interface{}, targetObjectName string) error
	// ExecuteFindOneCommandDecodeResult(ctx context.Context, collection *mongo.Collection, filter interface{}, result interface{}, resultObjectName string, logError bool, onFailureErr error) error
	// ExecuteAggregateCommand(ctx context.Context, collection *mongo.Collection, mongoPipeline []bson.D) (*mongo.Cursor, error)
	// ExecuteReplaceOneCommand(ctx context.Context, collection *mongo.Collection, filter interface{}, replacementObject interface{}, resultObjectName string) error
	// ExecuteUpdateManyCommand(ctx context.Context, collection *mongo.Collection, filter interface{}, updateFilter interface{}, resultObjectName string) error
	// ExecuteInsertManyCommand(ctx context.Context, collection *mongo.Collection, documents []interface{}, resultObjectName string) (*mongo.InsertManyResult, error)

	GetDatabase(ctx context.Context, dbName string) (*mongo.Database, error)
	InitialiseClient(ctx context.Context) (*mongo.Client, error)
	MapAllInCursorToResult(ctx context.Context, cursor *mongo.Cursor, result interface{}, resultObjectName string) error
	MapOneInCursorToResult(ctx context.Context, cursor *mongo.Cursor, result interface{}, resultObjectName string) error
}

// Repository represents the datastore to hold resource data
type Repository struct {
	Store                          MongoDbStore
	collectionInitMaxAttemptsLimit int

	collection      *mongo.Collection
	collectionMutex sync.Mutex
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

// GetAuditCollection returns collection used for audit domain
func (r *Repository) GetAuditCollection(ctx context.Context) (*mongo.Collection, error) {
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

		r.collection = db.Collection(AuditCollection)
		return r.collection, nil
	}

	return nil, fmt.Errorf("unable to initialise %s collection after %d attempts: %w", AuditCollection, collectionInitMaxAttemptsLimit, lastErr)
}

// GetTotalAuditLogEvents total log entries token from DB that match passed arguments
func (r *Repository) GetTotalAuditLogEvents(ctx context.Context, userId string, to string, from string, domains string, actions []AuditAction, targetId string, targetTypes []TargetType) (int64, error) {

	// Example mongo query
	// /// get log events total
	// db.getCollection("audit").countDocuments({actor_id: "cdd23f83-23ab-4019-8cce-038e51cbab0b" })
	// /// get standard viewing total
	// db.getCollection("audit").countDocuments({actor_id: "cdd23f83-23ab-4019-8cce-038e51cbab0b", action: {"$in": ["VIEWING_STANDARD"]} , action_at: {
	// 	$gt: '2023-12-10T00:00:00.000Z',
	// 	$lt: '2023-12-10T01:31:00.000Z'
	//   } })
	// /// get advance viewing total
	// db.getCollection("audit").countDocuments({actor_id: "cdd23f83-23ab-4019-8cce-038e51cbab0b", action: {"$in": ["VIEWING_ADVANCE"]} , action_at: {
	// 	$gt: '2023-12-10T00:00:00.000Z',
	// 	$lt: '2023-12-10T01:31:00.000Z'
	//   } })
	// /// get domain total
	// db.getCollection("audit").countDocuments({actor_id: "cdd23f83-23ab-4019-8cce-038e51cbab0b",domain: {"$in": ["contentmanager"]} , action_at: {
	// 	$gt: '2023-12-10T00:00:00.000Z',
	// 	$lt: '2023-12-10T01:31:00.000Z'
	//   } })

	auditFilter := bson.M{"actor_id": userId}

	if domains != "" {
		auditFilter["domain"] = bson.M{"$in": strings.Split(domains, ",")}
	}

	if len(actions) > 0 || actions != nil {
		auditFilter["action"] = bson.M{"$in": actions}
	}

	if targetId != "" {
		auditFilter["target_id"] = targetId
	}

	if len(targetTypes) > 0 || targetTypes != nil {
		auditFilter["target_types"] = bson.M{"$in": targetTypes}
	}

	if to != "" || from != "" {

		timeRangeFilter := bson.M{}

		if from != "" {
			timeRangeFilter["$gt"] = from
		}

		if to != "" {
			timeRangeFilter["$lt"] = to
		}

		auditFilter["action_at"] = timeRangeFilter
	}

	collection, err := r.GetAuditCollection(ctx)
	if err != nil {
		return 0, err
	}

	return r.Store.ExecuteCountDocuments(ctx, collection, auditFilter)
}

// CreateAuditLogEvent creates an log event in the DB
func (r *Repository) CreateAuditLogEvent(ctx context.Context, event *AuditLogEntry) error {

	collection, err := r.GetAuditCollection(ctx)
	if err != nil {
		return err
	}

	event.SetActionAtTimeToNow().GenerateNewUuid()

	_, err = r.Store.ExecuteInsertOneCommand(ctx, collection, event, "Audit-Log")
	if err != nil {
		return err
	}

	return nil
}
