package indexnow

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// MongoJobRepository persists leases and checkpoints through the blueprint
// MongoDbStore port. Collection initialization is cached by its private adapter;
// native store errors remain available to direct repository callers, while
// Runner reports only fixed safe outcomes. The caller owns the store/client.
type MongoJobRepository struct{ attempts *Repository }

// NewJobRepository uses the same shared-helper store and managed database as
// attempt auditing. No local files, environment checkpoints or URL lists are
// used. The new collection needs no TTL index or destructive migration.
func NewJobRepository(store MongoDbStore) *MongoJobRepository {
	return &MongoJobRepository{attempts: NewRepository(store)}
}

func (r *MongoJobRepository) collection(ctx context.Context) (*mongo.Collection, error) {
	if r == nil || r.attempts == nil {
		return nil, ErrJobUnavailable
	}
	if ctx == nil {
		return nil, ErrJobInvalid
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if nilDependency(r.attempts.Store) {
		return nil, ErrJobUnavailable
	}
	collection, err := r.attempts.GetAttemptCollection(ctx)
	if err != nil {
		return nil, err
	}
	return collection.Database().Collection(JobCollection), nil
}

// Claim initializes a missing scope once, then atomically acquires only an
// absent or expired lease. MongoDB server time determines lease expiry and all
// ownership checks, so a stale process clock cannot extend another lease.
func (r *MongoJobRepository) Claim(ctx context.Context, scope, token string, activation time.Time, lease time.Duration) (*JobLease, error) {
	if !jobScopePattern.MatchString(scope) || !validJobToken(token) || activation.IsZero() || lease < time.Millisecond || lease > 24*time.Hour {
		return nil, ErrJobInvalid
	}
	collection, err := r.collection(ctx)
	if err != nil {
		return nil, err
	}
	store := r.attempts.Store
	seed, err := store.ExecuteUpdateOneCommandResult(ctx, collection, bson.M{"_id": scope},
		bson.M{"$setOnInsert": bson.M{"checkpoint_at": activation.UTC().Truncate(time.Millisecond), "initialized": false}}, options.UpdateOne().SetUpsert(true))
	if err != nil {
		return nil, err
	}
	if !validSeedReceipt(seed, scope) {
		return nil, ErrJobUnavailable
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	filter := bson.M{"_id": scope, "$and": bson.A{
		bson.M{"$or": bson.A{
			bson.M{"lease_until": bson.M{"$exists": false}},
			bson.M{"$expr": bson.M{"$lte": bson.A{"$lease_until", "$$NOW"}}},
		}},
		bson.M{"$or": bson.A{
			bson.M{"next_attempt_at": bson.M{"$exists": false}},
			bson.M{"$expr": bson.M{"$lte": bson.A{"$next_attempt_at", "$$NOW"}}},
		}},
	}}
	change := mongo.Pipeline{bson.D{{Key: "$set", Value: bson.M{
		"lease_token": token,
		"lease_until": bson.M{"$add": bson.A{"$$NOW", int64(lease / time.Millisecond)}},
	}}}}
	claimed, err := store.ExecuteUpdateOneCommandResult(ctx, collection, filter, change)
	if err != nil {
		return nil, err
	}
	if err := checkedJobReceipt(claimed); err != nil {
		return nil, err
	}
	if claimed.MatchedCount == 0 {
		return nil, ErrJobBusy
	}
	if claimed.ModifiedCount != 1 {
		return nil, ErrJobUnavailable
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var result JobLease
	if err := store.ExecuteFindOneCommandDecodeResult(ctx, collection, bson.M{"_id": scope, "lease_token": token}, &result, "indexnow_job", false, ErrJobLeaseLost); err != nil {
		return nil, err
	}
	if !validJobLease(&result) || result.Scope != scope || result.Token != token {
		return nil, ErrJobUnavailable
	}
	return &result, nil
}

// Complete advances one acknowledged window only while this exact acquisition
// still owns an unexpired lease and the prior checkpoint is unchanged. A lost
// or expired holder cannot advance progress, including after a process pause.
func (r *MongoJobRepository) Complete(ctx context.Context, lease *JobLease, through time.Time) error {
	if !validJobLease(lease) || through.IsZero() || through.Before(lease.Checkpoint) ||
		(lease.Initialized && !through.After(lease.Checkpoint)) || through.After(lease.Checkpoint.Add(maxReconcileWindow)) {
		return ErrJobInvalid
	}
	if !lease.Initialized && !through.Equal(lease.Checkpoint) {
		return ErrJobInvalid
	}
	collection, err := r.collection(ctx)
	if err != nil {
		return err
	}
	filter := leaseOwnershipFilter(lease)
	filter["checkpoint_at"] = lease.Checkpoint.UTC().Truncate(time.Millisecond)
	filter["initialized"] = lease.Initialized
	result, err := r.attempts.Store.ExecuteUpdateOneCommandResult(ctx, collection, filter,
		bson.M{"$set": bson.M{"checkpoint_at": through.UTC().Truncate(time.Millisecond), "initialized": true}})
	if err != nil {
		return err
	}
	if err := checkedJobReceipt(result); err != nil {
		return err
	}
	if result.MatchedCount == 0 {
		return ErrJobLeaseLost
	}
	if result.ModifiedCount != 1 {
		return ErrJobUnavailable
	}
	return nil
}

// Release clears only this token while it remains unexpired. It never unsets
// a successor's ownership or silently treats a stale release as success.
func (r *MongoJobRepository) Release(ctx context.Context, lease *JobLease) error {
	if !validJobLease(lease) {
		return ErrJobInvalid
	}
	collection, err := r.collection(ctx)
	if err != nil {
		return err
	}
	result, err := r.attempts.Store.ExecuteUpdateOneCommandResult(ctx, collection, leaseOwnershipFilter(lease),
		bson.M{"$unset": bson.M{"lease_token": "", "lease_until": ""}})
	if err != nil {
		return err
	}
	if err := checkedJobReceipt(result); err != nil {
		return err
	}
	if result.MatchedCount == 0 {
		return ErrJobLeaseLost
	}
	if result.ModifiedCount != 1 {
		return ErrJobUnavailable
	}
	return nil
}

// Defer persists a protocol retry floor using MongoDB server time, and never
// changes the checkpoint. Positive durations are rounded UP to milliseconds;
// even the maximum time.Duration is representable without overflow. A later
// shorter request cannot lower the existing floor. Ownership is fenced exactly
// as completion and release are, including server-side expiry.
func (r *MongoJobRepository) Defer(ctx context.Context, lease *JobLease, delay time.Duration) error {
	if !validJobLease(lease) || delay <= 0 {
		return ErrJobInvalid
	}
	collection, err := r.collection(ctx)
	if err != nil {
		return err
	}
	ms := int64(delay / time.Millisecond)
	if delay%time.Millisecond != 0 {
		ms++
	}
	change := mongo.Pipeline{bson.D{{Key: "$set", Value: bson.M{
		"next_attempt_at": bson.M{"$max": bson.A{"$next_attempt_at", bson.M{"$add": bson.A{"$$NOW", ms}}}},
	}}}}
	result, err := r.attempts.Store.ExecuteUpdateOneCommandResult(ctx, collection, leaseOwnershipFilter(lease), change)
	if err != nil {
		return err
	}
	if err := checkedJobReceipt(result); err != nil {
		return err
	}
	if result.MatchedCount == 0 {
		return ErrJobLeaseLost
	}
	if result.ModifiedCount != 1 {
		return ErrJobUnavailable
	}
	return nil
}

func leaseOwnershipFilter(lease *JobLease) bson.M {
	return bson.M{"_id": lease.Scope, "lease_token": lease.Token,
		"$expr": bson.M{"$gt": bson.A{"$lease_until", "$$NOW"}}}
}

func validJobToken(token string) bool {
	if len(token) != 32 {
		return false
	}
	for _, r := range token {
		if !(r >= 'a' && r <= 'f') && !(r >= '0' && r <= '9') {
			return false
		}
	}
	return true
}

func validSeedReceipt(receipt *mongo.UpdateResult, scope string) bool {
	if receipt == nil || !receipt.Acknowledged || receipt.ModifiedCount != 0 {
		return false
	}
	if receipt.UpsertedCount == 1 {
		id, ok := receipt.UpsertedID.(string)
		return ok && id == scope && receipt.MatchedCount == 0
	}
	return receipt.UpsertedCount == 0 && receipt.UpsertedID == nil && receipt.MatchedCount == 1
}

func checkedJobReceipt(receipt *mongo.UpdateResult) error {
	if receipt == nil || !receipt.Acknowledged || receipt.UpsertedID != nil || receipt.UpsertedCount != 0 ||
		receipt.MatchedCount < 0 || receipt.MatchedCount > 1 || receipt.ModifiedCount < 0 || receipt.ModifiedCount > receipt.MatchedCount {
		return ErrJobUnavailable
	}
	return nil
}

var _ JobRepository = (*MongoJobRepository)(nil)
