package indexnow

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// AttemptCollection is the MongoDB collection name for attempt records.
const AttemptCollection = "indexnow_attempts"

const defaultCollectionInitMaxAttemptsLimit = 3
const repositoryListLimit = 1000

// MongoDbStore is the shared-helper persistence port (see
// internal/blueprint). Implementations must preserve native dependency
// errors; the repository owns domain result semantics only.
type MongoDbStore interface {
	// ExecuteFindCommand runs a query on the given collection with optional
	// find options through the MongoDbStore persistence port and returns a
	// cursor over matching documents, or an error.
	ExecuteFindCommand(ctx context.Context, collection *mongo.Collection, filter interface{}, opts ...options.Lister[options.FindOptions]) (*mongo.Cursor, error)
	// ExecuteFindOneCommandDecodeResult fetches one matching document and
	// decodes it into result through the MongoDbStore persistence port;
	// logError controls error logging and onFailureErr is returned when no
	// document matches.
	ExecuteFindOneCommandDecodeResult(ctx context.Context, collection *mongo.Collection, filter interface{}, result interface{}, resultObjectName string, logError bool, onFailureErr error) error
	// ExecuteInsertOneCommand inserts one document into the given collection
	// through the MongoDbStore persistence port, using resultObjectName for
	// telemetry, and returns the insertion receipt or an error.
	ExecuteInsertOneCommand(ctx context.Context, collection *mongo.Collection, document interface{}, resultObjectName string) (*mongo.InsertOneResult, error)
	// ExecuteUpdateOneCommandResult applies a single-document update through
	// the MongoDbStore persistence port; the returned mongo.UpdateResult must
	// retain acknowledgement and counts so the selected record's existence and
	// non-upsert behaviour are provable.
	ExecuteUpdateOneCommandResult(ctx context.Context, collection *mongo.Collection, filter, update any, opts ...options.Lister[options.UpdateOneOptions]) (*mongo.UpdateResult, error)
	// GetDatabase returns the mongo.Database handle for the given database name
	// through the MongoDbStore persistence port; an empty name selects the
	// configured default database.
	GetDatabase(ctx context.Context, dbName string) (*mongo.Database, error)
	// InitialiseClient creates the underlying MongoDB client for the
	// MongoDbStore persistence port and returns it, or an error.
	InitialiseClient(ctx context.Context) (*mongo.Client, error)
	// MapAllInCursorToResult decodes all remaining cursor documents into
	// result through the MongoDbStore persistence port, using
	// resultObjectName for telemetry, and reports decoding failures as an
	// error.
	MapAllInCursorToResult(ctx context.Context, cursor *mongo.Cursor, result interface{}, resultObjectName string) error
}

// Repository is the production MongoDB AttemptRepository adapter. It follows
// the blueprint pattern: cached collection initialisation with bounded setup
// retries, cancellation checks around setup, and strict receipt checking. The client
// stays owned by the shared store; configure the store before concurrent use.
//
// Records contain only audit fields (see Attempt); the API key, key location,
// request payload, response body, and raw transport diagnostics are never
// persisted. Native driver errors are returned unwrapped where the caller can
// treat them as operational failures; the repository never logs payloads.
type Repository struct {
	// Store supplies driver operations and shared-helper telemetry.
	Store MongoDbStore
	// collectionInitMaxAttemptsLimit bounds setup retries, never mutation
	// retries.
	collectionInitMaxAttemptsLimit int
	// collection is initialized once under collectionMutex and reused.
	collection *mongo.Collection
	// collectionMutex serializes setup and retry-limit configuration.
	collectionMutex sync.Mutex
}

// NewRepository reuses the caller-owned store and defaults to three setup
// attempts. A nil store is rejected lazily on first use, never with a panic.
func NewRepository(store MongoDbStore) *Repository {
	return &Repository{Store: store, collectionInitMaxAttemptsLimit: defaultCollectionInitMaxAttemptsLimit}
}

// WithCollectionInitMaxAttemptsLimit changes the setup retry budget.
// Nonpositive values retain the current setting; a nil receiver remains nil.
func (r *Repository) WithCollectionInitMaxAttemptsLimit(limit int) *Repository {
	if r != nil && limit > 0 {
		r.collectionMutex.Lock()
		r.collectionInitMaxAttemptsLimit = limit
		r.collectionMutex.Unlock()
	}
	return r
}

// validateStorageEntry rejects invalid wiring and cancelled contexts before
// logs or I/O.
func (r *Repository) validateStorageEntry(ctx context.Context) error {
	if ctx == nil {
		return ErrAttemptInvalidPayload
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if r == nil || nilDependency(r.Store) {
		return ErrAttemptUnavailable
	}
	return nil
}

// GetAttemptCollection caches the managed collection after bounded setup
// following the standard GHATD MongoDB pattern (lock, cache reuse, client
// init, database resolution, cache, bounded retry; cancellation stops further
// attempts). Nil stores/databases fail as unavailable with native causes.
func (r *Repository) GetAttemptCollection(ctx context.Context) (*mongo.Collection, error) {
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
		if cancelErr := ctx.Err(); cancelErr != nil {
			return nil, cancelErr
		}
		if err != nil {
			lastErr = err
		} else if client == nil {
			lastErr = ErrAttemptUnavailable
		} else {
			db, dbErr := r.Store.GetDatabase(ctx, "")
			if cancelErr := ctx.Err(); cancelErr != nil {
				return nil, cancelErr
			}
			if dbErr != nil {
				lastErr = dbErr
			} else if db != nil {
				r.collection = db.Collection(AttemptCollection)
				return r.collection, nil
			} else {
				lastErr = ErrAttemptUnavailable
			}
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
	}
	if lastErr == nil {
		lastErr = ErrAttemptUnavailable
	}
	return nil, lastErr
}

// validateAttemptRecord rejects nil records and records without identity or a
// start timestamp before any database interaction.
func validateAttemptRecord(attempt *Attempt) error {
	if attempt == nil || strings.TrimSpace(attempt.ID) == "" || strings.TrimSpace(attempt.ID) != attempt.ID || attempt.StartedAt.IsZero() {
		return ErrAttemptInvalidPayload
	}
	return nil
}

// RecordAttempt inserts the in-flight attempt before any external side
// effect. It requires an acknowledged insertion receipt carrying the pinned
// string ID; a duplicate key surfaces as ErrAttemptDuplicateID so an
// uncertain outcome never authorizes a blind retry.
func (r *Repository) RecordAttempt(ctx context.Context, attempt *Attempt) error {
	if err := r.validateStorageEntry(ctx); err != nil {
		return err
	}
	if err := validateAttemptRecord(attempt); err != nil {
		return err
	}
	if !attempt.FinishedAt.IsZero() || attempt.Success || attempt.StatusCode != 0 || attempt.Outcome != OutcomeInFlight || attempt.FailureMessage != "" || attempt.Retryable || attempt.RetryAfter != 0 {
		return ErrAttemptInvalidPayload
	}

	document := *attempt
	document.URLs = append([]string(nil), attempt.URLs...)
	id := document.ID

	collection, err := r.GetAttemptCollection(ctx)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	result, err := r.Store.ExecuteInsertOneCommand(ctx, collection, &document, "indexnow_attempt")
	if err != nil {
		if isMongoDuplicateKey(err) {
			return ErrAttemptDuplicateID
		}
		return err
	}
	if result == nil || !result.Acknowledged {
		return ErrAttemptUnavailable
	}
	insertedID, ok := result.InsertedID.(string)
	if !ok || insertedID != id {
		return ErrAttemptUnavailable
	}
	return nil
}

// FinishAttempt applies the final outcome fields to the stored attempt
// selected by ID. It never upserts: an unknown ID is ErrAttemptNotFound and no
// receipt is fabricated. Completion is applied once; replaying a completion
// with the same persisted fields is idempotent. A conflicting completion
// reports ErrAttemptAlreadyFinished rather than silently rewriting history.
func (r *Repository) FinishAttempt(ctx context.Context, attempt *Attempt) error {
	if err := r.validateStorageEntry(ctx); err != nil {
		return err
	}
	if err := validateAttemptRecord(attempt); err != nil {
		return err
	}
	if attempt.FinishedAt.IsZero() || attempt.FinishedAt.Before(attempt.StartedAt) || attempt.Outcome == OutcomeInFlight || attempt.Outcome == "" {
		return ErrAttemptInvalidPayload
	}

	document := *attempt
	document.URLs = append([]string(nil), attempt.URLs...)

	collection, err := r.GetAttemptCollection(ctx)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	// The guard filter matches only in-flight (not yet finished) attempts, so
	// a replay cannot overwrite a final record and cannot upsert.
	filter := bson.M{"_id": document.ID, "finished_at": bson.M{"$exists": false}, "started_at": document.StartedAt, "method": document.Method, "urls": document.URLs, "outcome": OutcomeInFlight}
	completion := bson.M{"finished_at": document.FinishedAt, "status_code": document.StatusCode, "outcome": document.Outcome, "success": document.Success, "failure_message": document.FailureMessage, "retryable": document.Retryable, "retry_after": document.RetryAfter}
	result, err := r.Store.ExecuteUpdateOneCommandResult(ctx, collection, filter, bson.M{"$set": completion})
	if err != nil {
		return err
	}
	if result == nil || !result.Acknowledged || result.UpsertedID != nil || result.UpsertedCount != 0 {
		return ErrAttemptUnavailable
	}
	if result.MatchedCount < 0 || result.ModifiedCount < 0 || result.MatchedCount > 1 || result.ModifiedCount > result.MatchedCount {
		return ErrAttemptUnavailable
	}
	if result.MatchedCount == 0 {
		// Distinguish "never existed" from "already final".
		stored, gerr := r.findAttempt(ctx, document.ID)
		if gerr != nil {
			if errors.Is(gerr, ErrAttemptNotFound) {
				return ErrAttemptNotFound
			}
			return gerr
		}
		if stored.FinishedAt.IsZero() {
			return ErrAttemptInvalidPayload
		}
		// Mongo stores times at millisecond precision. Compare the persisted
		// representation when acknowledging an identical completion replay.
		previous, replay := *stored, document
		previous.StartedAt = previous.StartedAt.Truncate(time.Millisecond)
		previous.FinishedAt = previous.FinishedAt.Truncate(time.Millisecond)
		replay.StartedAt = replay.StartedAt.Truncate(time.Millisecond)
		replay.FinishedAt = replay.FinishedAt.Truncate(time.Millisecond)
		if sameFinalState(&previous, &replay) {
			return nil
		}
		return ErrAttemptAlreadyFinished
	}
	// The guard selects only records without finished_at. A first completion
	// always adds that field, so a matched no-op cannot prove this transition.
	if result.ModifiedCount != 1 {
		return ErrAttemptUnavailable
	}
	return nil
}

// GetAttempt returns the stored attempt selected by id, or
// ErrAttemptNotFound for confirmed absence. Native decode/driver failures are
// preserved.
func (r *Repository) GetAttempt(ctx context.Context, id string) (*Attempt, error) {
	if err := r.validateStorageEntry(ctx); err != nil {
		return nil, err
	}
	if strings.TrimSpace(id) == "" {
		return nil, ErrAttemptInvalidPayload
	}
	return r.findAttempt(ctx, id)
}

// findAttempt resolves one record, mapping only confirmed absence.
func (r *Repository) findAttempt(ctx context.Context, id string) (*Attempt, error) {
	collection, err := r.GetAttemptCollection(ctx)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	var result Attempt
	err = r.Store.ExecuteFindOneCommandDecodeResult(ctx, collection, bson.M{"_id": id}, &result, "indexnow_attempt", true, nil)
	if err != nil {
		if attemptAbsent(err) {
			return nil, ErrAttemptNotFound
		}
		return nil, err
	}
	if result.ID != id {
		return nil, ErrAttemptUnavailable
	}
	result.URLs = append([]string(nil), result.URLs...)
	return &result, nil
}

// attemptAbsent recognizes only mongo.ErrNoDocuments or the domain sentinel
// through a bounded single-cause unwrap chain, so an operational failure can
// never be mistaken for authoritative absence.
func attemptAbsent(err error) bool {
	for depth := 0; err != nil && depth < 64; depth++ {
		if err == mongo.ErrNoDocuments || err == ErrAttemptNotFound {
			return true
		}
		err = errors.Unwrap(err)
	}
	return false
}

// ListAttempts returns up to limit attempts ordered by start time descending
// with attempt ID ascending as the stable tiebreaker. Nonpositive limits use
// the bounded default of 1000; limits above 1000 are capped. The result is a
// point-in-time page, not a transactional snapshot.
func (r *Repository) ListAttempts(ctx context.Context, limit int) ([]Attempt, error) {
	if err := r.validateStorageEntry(ctx); err != nil {
		return nil, err
	}
	if limit <= 0 || limit > repositoryListLimit {
		limit = repositoryListLimit
	}

	collection, err := r.GetAttemptCollection(ctx)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	findOptions := options.Find().
		SetSort(bson.D{{Key: "started_at", Value: -1}, {Key: "_id", Value: 1}}).
		SetLimit(int64(limit))

	cursor, err := r.Store.ExecuteFindCommand(ctx, collection, bson.M{"_id": bson.M{"$exists": true}}, findOptions)
	if err != nil {
		return nil, err
	}
	if cursor == nil {
		return nil, ErrAttemptUnavailable
	}

	var result []Attempt
	if err := r.Store.MapAllInCursorToResult(ctx, cursor, &result, "indexnow_attempts"); err != nil {
		closeAttemptCursor(cursor)
		return nil, err
	}
	closeAttemptCursor(cursor)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(result) > limit {
		return nil, fmt.Errorf("%w: over-limit page", ErrAttemptUnavailable)
	}
	for i := range result {
		result[i].URLs = append([]string(nil), result[i].URLs...)
	}
	return result, nil
}

// closeAttemptCursor bounds best-effort cleanup even after caller
// cancellation. Cursor.Close is idempotent when the shared mapper already
// consumed the cursor.
func closeAttemptCursor(cursor *mongo.Cursor) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = cursor.Close(ctx)
}

// isMongoDuplicateKey detects a duplicate-key write failure without retaining
// the raw driver message (which can echo document content) in domain errors.
func isMongoDuplicateKey(err error) bool {
	var mongoErr mongo.WriteException
	if errors.As(err, &mongoErr) {
		for _, we := range mongoErr.WriteErrors {
			if we.Code == 11000 {
				return true
			}
		}
	}
	return false
}
