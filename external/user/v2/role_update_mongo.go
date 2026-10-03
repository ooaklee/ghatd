package user

import (
	"context"
	"slices"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

var _ AccountRolesRepository = (*Repository)(nil)

// SetAccountRoles uses the shared acknowledged-postimage helper with binary
// security and array equality. Mongo's scalar/array element matching must not
// admit a nested or merely overlapping roles value. Missing/null legacy roles
// compare as empty; unrelated profile, verification and metadata fields survive.
func (r *Repository) SetAccountRoles(ctx context.Context, req *SetAccountRolesRequest) (*UniversalUser, error) {
	if r == nil || ctx == nil || nilUserDependency(r.Store) {
		return nil, ErrRoleUpdateUnavailable
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if req == nil || !validRoleCommand(*req) {
		return nil, ErrInvalidUserBody
	}
	c := copyRoleCommand(*req)
	store, ok := r.Store.(atomicUserMongoStore)
	if !ok {
		return nil, ErrRoleUpdateUnavailable
	}
	collection, err := r.GetUserCollection(ctx)
	if err != nil {
		return nil, err
	}
	filter := accountSnapshotFilter(c.Account)
	if c.Account.Email == "" {
		filter["email"] = profileSnapshotField("")
	}
	previous := append([]string{}, c.PreviousRoles...)
	filter["$expr"] = bson.M{"$eq": bson.A{bson.M{"$ifNull": bson.A{"$roles", bson.A{}}}, bson.M{"$literal": previous}}}
	// No-op writes preserve even the raw null/missing representation and do not
	// touch updated_at. The acknowledged post-image still confirms current state.
	fields := bson.M{"roles": "$roles"}
	if !slices.Equal(c.PreviousRoles, c.Roles) {
		fields["roles"] = bson.M{"$literal": append([]string{}, c.Roles...)}
		fields["metadata"] = bson.M{"$mergeObjects": bson.A{bson.M{"$ifNull": bson.A{"$metadata", bson.M{}}}, bson.M{"$literal": bson.M{"updated_at": c.At}}}}
	}
	var result UniversalUser
	err = store.ExecuteFindOneAndUpdateCommandDecodeResult(ctx, collection, filter, mongo.Pipeline{bson.D{{Key: "$set", Value: fields}}}, &result, options.FindOneAndUpdate().SetReturnDocument(options.After).SetCollation(&options.Collation{Locale: "simple"}))
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, ErrRoleUpdateConflict
		}
		return nil, err
	}
	if !validRoleReceipt(&result, c) {
		return nil, ErrRoleUpdateUnavailable
	}
	return &result, nil
}
