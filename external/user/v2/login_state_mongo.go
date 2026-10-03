package user

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

var _ LoginStateRepository = (*Repository)(nil)

// accountSnapshotFilter centralizes binary security selectors for narrow writes.
// The command's raw empty type/revision zero also match absent/null legacy values.
func accountSnapshotFilter(a AccountSnapshot) bson.M {
	f := bson.M{"_id": a.UserID, "email": a.Email, "status": a.Status, "type": profileSnapshotField(a.Type), "email_revision": a.EmailRevision}
	if a.EmailRevision == 0 {
		f["email_revision"] = bson.M{"$in": bson.A{nil, int64(0)}}
	}
	return f
}

// SetFreshLogin atomically checks ACTIVE state and changes only login timestamps.
// Distinct valid proofs may both succeed; login timestamps are last-writer-wins.
func (r *Repository) SetFreshLogin(ctx context.Context, req *SetLoginStateRequest) (*UniversalUser, error) {
	return r.setLoginState(ctx, req, false)
}

// SetVerifiedEmailActivation atomically consumes the PROVISIONED account state.
// Only one competing activation can succeed; unrelated fields are merged intact.
func (r *Repository) SetVerifiedEmailActivation(ctx context.Context, req *SetLoginStateRequest) (*UniversalUser, error) {
	return r.setLoginState(ctx, req, true)
}

// setLoginState uses the shared acknowledged-postimage helper without retries,
// broad snapshots, upserts or raw driver fallbacks. Native errors stay native.
func (r *Repository) setLoginState(ctx context.Context, req *SetLoginStateRequest, activate bool) (*UniversalUser, error) {
	if r == nil || ctx == nil || nilUserDependency(r.Store) {
		return nil, ErrLoginStateUnavailable
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if req == nil || !validLoginSnapshot(req.Account, activate) {
		return nil, ErrInvalidUserBody
	}
	command := *req
	at, err := time.Parse(time.RFC3339Nano, command.At)
	if err != nil || at.IsZero() || command.At != at.UTC().Format(time.RFC3339Nano) {
		return nil, ErrInvalidUserBody
	}
	store, ok := r.Store.(atomicUserMongoStore)
	if !ok {
		return nil, ErrLoginStateUnavailable
	}
	collection, err := r.GetUserCollection(ctx)
	if err != nil {
		return nil, err
	}
	metadata := bson.M{"last_login_at": command.At, "last_fresh_login_at": command.At}
	fields := bson.M{}
	if activate {
		metadata["updated_at"], metadata["activated_at"], metadata["status_changed_at"] = command.At, command.At, command.At
		fields["status"] = AccountStatusKeyActive
		fields["verification"] = bson.M{"$mergeObjects": bson.A{bson.M{"$ifNull": bson.A{"$verification", bson.M{}}}, bson.M{"$literal": bson.M{"email_verified": true, "email_verified_at": command.At}}}}
	}
	fields["metadata"] = bson.M{"$mergeObjects": bson.A{bson.M{"$ifNull": bson.A{"$metadata", bson.M{}}}, bson.M{"$literal": metadata}}}
	update := mongo.Pipeline{bson.D{{Key: "$set", Value: fields}}}
	var result UniversalUser
	err = store.ExecuteFindOneAndUpdateCommandDecodeResult(ctx, collection, accountSnapshotFilter(command.Account), update, &result, options.FindOneAndUpdate().SetReturnDocument(options.After).SetCollation(&options.Collation{Locale: "simple"}))
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, ErrLoginStateConflict
		}
		return nil, err
	}
	if !validLoginReceipt(&result, command, activate) {
		return nil, ErrLoginStateUnavailable
	}
	return &result, nil
}
