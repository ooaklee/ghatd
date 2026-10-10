package voter

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/ooaklee/ghatd/external/logger"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/mongo/writeconcern"
)

// Collection is the shared vote collection; domains remain separated by Target.
const Collection = "votes"

// MongoDbStore is the shared-helper adapter port. Only the repository knows
// Mongo types; helpers preserve native failures and metadata-only telemetry.
type MongoDbStore interface {
	// InitialiseClient creates the underlying MongoDB client for the MongoDbStore
	// adapter port; it returns the client or an error, and only the repository
	// knows Mongo types.
	InitialiseClient(context.Context) (*mongo.Client, error)
	// GetDatabase returns the named mongo.Database handle for the MongoDbStore
	// adapter port, or an error; helpers preserve native failures per the owning
	// contract.
	GetDatabase(context.Context, string) (*mongo.Database, error)
	// ExecuteUpdateOneCommandResult applies a single-document update on the given
	// collection for the MongoDbStore adapter port, returning the
	// mongo.UpdateResult receipt with acknowledgement and counts, or an error.
	ExecuteUpdateOneCommandResult(context.Context, *mongo.Collection, any, any, ...options.Lister[options.UpdateOneOptions]) (*mongo.UpdateResult, error)
	// ExecuteDeleteOneCommandResult applies a single-document delete on the given
	// collection for the MongoDbStore adapter port, returning the
	// mongo.DeleteResult receipt with acknowledgement and deleted count, or an
	// error.
	ExecuteDeleteOneCommandResult(context.Context, *mongo.Collection, any, ...options.Lister[options.DeleteOneOptions]) (*mongo.DeleteResult, error)
	// ExecuteAggregateCommand runs the given aggregation pipeline stages on the
	// collection for the MongoDbStore adapter port and returns a cursor over the
	// results, or an error.
	ExecuteAggregateCommand(context.Context, *mongo.Collection, []bson.D) (*mongo.Cursor, error)
}

// Repository stores one mutable vote per actor/target outside parent records.
// It owns neither the client nor target authorization and never retries writes.
type Repository struct {
	// Store supplies the managed Mongo client and safe command helpers.
	Store MongoDbStore
	// mu serializes collection initialization; dependencies are immutable after wiring.
	mu sync.Mutex
	// collection caches the majority-write collection after successful setup.
	collection *mongo.Collection
}

// NewRepository composes a caller-owned managed store without connecting or
// creating indexes. Use EnsureIndexes during an explicit startup migration.
func NewRepository(store MongoDbStore) *Repository { return &Repository{Store: store} }

// GetVoteCollection caches the managed collection. Setup retries are bounded
// to three attempts; cancellation stops retries. Mutations have no retry loop.
func (r *Repository) GetVoteCollection(ctx context.Context) (*mongo.Collection, error) {
	if ctx == nil {
		return nil, ErrInvalidRequest
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if r == nil || nilDependency(r.Store) {
		return nil, ErrUnavailable
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if r.collection != nil {
		return r.collection, nil
	}
	log := logger.AcquireOperationFrom(ctx, "external/voter", "collection")
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		client, err := r.Store.InitialiseClient(ctx)
		if err != nil {
			lastErr = err
			log.Warn("voter-client-setup-failed")
			continue
		}
		if client == nil {
			return nil, ErrUnavailable
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		db, err := r.Store.GetDatabase(ctx, "")
		if err != nil {
			lastErr = err
			log.Warn("voter-database-setup-failed")
			continue
		}
		if db == nil {
			return nil, ErrUnavailable
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		r.collection = db.Collection(Collection, options.Collection().SetWriteConcern(writeconcern.Majority()))
		return r.collection, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("voter collection setup: %w", lastErr)
}

// voteID derives storage identity from a length-safe tuple, never concatenated
// delimiters. This is not a credential or anonymization of the actor identity.
func voteID(actor string, target Target) string {
	value, _ := json.Marshal([]string{target.Scope, target.Domain, target.ResourceID, target.ChildID, actor})
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}

// targetFilter includes empty scope/child values explicitly to prevent aliasing.
func targetFilter(target Target) bson.M {
	return bson.M{"scope": target.Scope, "domain": target.Domain, "resource_id": target.ResourceID, "child_id": target.ChildID}
}

// SetVote upserts by deterministic identity. A matched no-op or one confirmed
// insertion succeeds. Inconsistent/unacknowledged receipts are unavailable.
func (r *Repository) SetVote(ctx context.Context, actor string, target Target, value Value) error {
	if ctx == nil {
		return ErrInvalidRequest
	}
	if err := validateActor(ctx, actor, false); err != nil {
		return err
	}
	if !validTarget(target) || !value.Valid() {
		return ErrInvalidRequest
	}
	collection, err := r.GetVoteCollection(ctx)
	if err != nil {
		return err
	}
	id := voteID(actor, target)
	insert := targetFilter(target)
	insert["actor_id"] = actor
	receipt, err := r.Store.ExecuteUpdateOneCommandResult(ctx, collection, bson.M{"_id": id}, bson.M{
		"$set":         bson.M{"vote": value, "updated_at": time.Now().UTC()},
		"$setOnInsert": insert,
	}, options.UpdateOne().SetUpsert(true))
	if err != nil {
		return err
	}
	if !validSetReceipt(receipt, id) {
		return ErrUnavailable
	}
	return nil
}

// validSetReceipt distinguishes insertion, matched assignment and uncertainty.
func validSetReceipt(r *mongo.UpdateResult, id string) bool {
	if r == nil || !r.Acknowledged {
		return false
	}
	if r.UpsertedCount == 1 {
		return r.UpsertedID == id && r.MatchedCount == 0 && r.ModifiedCount == 0
	}
	return r.UpsertedCount == 0 && r.UpsertedID == nil && r.MatchedCount == 1 && (r.ModifiedCount == 0 || r.ModifiedCount == 1)
}

// RemoveVote acknowledges zero deletions as an already absent vote. It never
// deletes other actors or targets and never retries an uncertain result.
func (r *Repository) RemoveVote(ctx context.Context, actor string, target Target) error {
	if ctx == nil {
		return ErrInvalidRequest
	}
	if err := validateActor(ctx, actor, false); err != nil {
		return err
	}
	if !validTarget(target) {
		return ErrInvalidRequest
	}
	collection, err := r.GetVoteCollection(ctx)
	if err != nil {
		return err
	}
	receipt, err := r.Store.ExecuteDeleteOneCommandResult(ctx, collection, bson.M{"_id": voteID(actor, target)})
	if err != nil {
		return err
	}
	if receipt == nil || !receipt.Acknowledged || receipt.DeletedCount < 0 || receipt.DeletedCount > 1 {
		return ErrUnavailable
	}
	return nil
}

// GetSummaries aggregates totals and the viewer's vote in one query; it does
// not materialize voter lists. Counts are observations, not a transaction with
// preceding writes. Stored malformed values fail closed, never become downvotes.
func (r *Repository) GetSummaries(ctx context.Context, actor string, targets []Target) (map[Target]Summary, error) {
	if ctx == nil {
		return nil, ErrInvalidRequest
	}
	if err := validateActor(ctx, actor, true); err != nil {
		return nil, err
	}
	if err := validateTargets(targets); err != nil {
		return nil, err
	}
	targets = append([]Target(nil), targets...)
	collection, err := r.GetVoteCollection(ctx)
	if err != nil {
		return nil, err
	}
	filters := make(bson.A, 0, len(targets))
	result := make(map[Target]Summary, len(targets))
	for _, target := range targets {
		filters = append(filters, targetFilter(target))
		result[target] = Summary{}
	}
	countIf := func(predicate any) bson.M { return bson.M{"$sum": bson.M{"$cond": bson.A{predicate, 1, 0}}} }
	up := bson.M{"$eq": bson.A{"$vote", Up}}
	down := bson.M{"$eq": bson.A{"$vote", Down}}
	// Opaque IDs beginning with '$' must remain values, not aggregation expressions.
	literalActor := bson.M{"$literal": actor}
	own := bson.M{"$and": bson.A{bson.M{"$ne": bson.A{literalActor, ""}}, bson.M{"$eq": bson.A{"$actor_id", literalActor}}}}
	valid := bson.M{"$and": bson.A{
		bson.M{"$in": bson.A{bson.M{"$type": "$vote"}, bson.A{"int", "long"}}},
		bson.M{"$in": bson.A{"$vote", bson.A{Down, Up}}},
		bson.M{"$eq": bson.A{bson.M{"$type": "$actor_id"}, "string"}},
		bson.M{"$ne": bson.A{"$actor_id", ""}},
	}}
	pipeline := []bson.D{
		{{Key: "$match", Value: bson.M{"$or": filters}}},
		{{Key: "$group", Value: bson.M{
			"_id": bson.M{"scope": "$scope", "domain": "$domain", "resource_id": "$resource_id", "child_id": "$child_id"},
			"up":  countIf(up), "down": countIf(down),
			"invalid":  countIf(bson.M{"$not": bson.A{valid}}),
			"own_up":   countIf(bson.M{"$and": bson.A{own, up}}),
			"own_down": countIf(bson.M{"$and": bson.A{own, down}}),
		}}},
	}
	cursor, err := r.Store.ExecuteAggregateCommand(ctx, collection, pipeline)
	if err != nil {
		return nil, err
	}
	if cursor == nil {
		return nil, ErrUnavailable
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
		defer cancel()
		_ = cursor.Close(cleanup)
	}()
	var rows []struct {
		Target  Target `bson:"_id"`
		Up      int    `bson:"up"`
		Down    int    `bson:"down"`
		Invalid int    `bson:"invalid"`
		OwnUp   int    `bson:"own_up"`
		OwnDown int    `bson:"own_down"`
	}
	if err := cursor.All(ctx, &rows); err != nil {
		return nil, err
	}
	seen := make(map[Target]bool, len(rows))
	for _, row := range rows {
		_, ok := result[row.Target]
		if !ok || seen[row.Target] || row.Invalid != 0 || row.Up < 0 || row.Down < 0 || row.OwnUp < 0 || row.OwnDown < 0 || row.OwnUp+row.OwnDown > 1 || row.OwnUp > row.Up || row.OwnDown > row.Down {
			return nil, ErrUnavailable
		}
		seen[row.Target] = true
		summary := Summary{Up: row.Up, Down: row.Down}
		if row.OwnUp == 1 {
			value := Up
			summary.ViewerVote = &value
		}
		if row.OwnDown == 1 {
			value := Down
			summary.ViewerVote = &value
		}
		if actor == "" && summary.ViewerVote != nil {
			return nil, ErrUnavailable
		}
		result[row.Target] = summary
	}
	return result, nil
}
