package user

import (
	"context"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

var _ AccountStatusRepository = (*Repository)(nil)

// SetAccountStatus compares security state and merges only model-owned fields
// through the shared repository helper. It does not retry, upsert or audit.
// Concurrent unrelated changes survive; no match is a native domain conflict.
func (r *Repository) SetAccountStatus(ctx context.Context, req *SetAccountStatusRequest) (*UniversalUser, error) {
	if r == nil || ctx == nil || nilUserDependency(r.Store) {
		return nil, ErrStatusUpdateUnavailable
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if req == nil || !validStatusCommand(*req) {
		return nil, ErrInvalidUserBody
	}
	c := *req
	store, ok := r.Store.(atomicUserMongoStore)
	if !ok {
		return nil, ErrStatusUpdateUnavailable
	}
	collection, err := r.GetUserCollection(ctx)
	if err != nil {
		return nil, err
	}
	filter := accountSnapshotFilter(c.Account)
	// Configurations may permit email-less legacy accounts. Compare that absence
	// rather than silently requiring an email for a non-email status operation.
	if c.Account.Email == "" {
		filter["email"] = profileSnapshotField("")
	}
	metadata := bson.M{"updated_at": c.At, "status_changed_at": c.At}
	if c.Status == AccountStatusKeyActive {
		metadata["activated_at"] = c.At
	}
	fields := bson.M{
		"status":   bson.M{"$literal": c.Status},
		"metadata": bson.M{"$mergeObjects": bson.A{bson.M{"$ifNull": bson.A{"$metadata", bson.M{}}}, bson.M{"$literal": metadata}}},
	}
	if c.ClearEmailVerification {
		filter["verification.email_verified"] = c.PreviousEmailVerified
		if !c.PreviousEmailVerified {
			filter["verification.email_verified"] = bson.M{"$in": bson.A{nil, false}}
		}
		filter["verification.email_verified_at"] = profileSnapshotField(c.PreviousEmailVerifiedAt)
		fields["verification"] = bson.M{"$mergeObjects": bson.A{bson.M{"$ifNull": bson.A{"$verification", bson.M{}}}, bson.M{"$literal": bson.M{"email_verified": false, "email_verified_at": ""}}}}
	}
	var result UniversalUser
	err = store.ExecuteFindOneAndUpdateCommandDecodeResult(ctx, collection, filter, mongo.Pipeline{bson.D{{Key: "$set", Value: fields}}}, &result, options.FindOneAndUpdate().SetReturnDocument(options.After).SetCollation(&options.Collation{Locale: "simple"}))
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, ErrStatusUpdateConflict
		}
		return nil, err
	}
	if !validStatusReceipt(&result, c) {
		return nil, ErrStatusUpdateUnavailable
	}
	return &result, nil
}
