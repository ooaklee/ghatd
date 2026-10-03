package user

import (
	"context"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// profileSnapshotField compares empty legacy values with missing/null scalars.
func profileSnapshotField(value string) any {
	if value == "" {
		return bson.M{"$in": bson.A{nil, ""}}
	}
	return value
}

// SetProfileNames performs one acknowledged conditional write through GHATD's
// shared Mongo helper. It merges only name fields and updated_at, preserving
// concurrent changes to roles, verification, providers, handles, avatar and phone.
// A competing name/security change causes conflict, not an automatic retry.
func (r *Repository) SetProfileNames(ctx context.Context, req *SetProfileNamesRequest) (*UniversalUser, error) {
	if r == nil || ctx == nil || nilUserDependency(r.Store) {
		return nil, ErrProfileUpdateUnavailable
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if req == nil {
		return nil, ErrInvalidUserBody
	}
	command := *req
	a := command.Account
	if strings.TrimSpace(a.UserID) == "" || a.ExpectedEmail == "" || a.ExpectedStatus == "" || a.ExpectedRevision < 0 {
		return nil, ErrInvalidUserBody
	}
	if _, err := time.Parse(time.RFC3339Nano, command.UpdatedAt); err != nil {
		return nil, ErrInvalidUserBody
	}
	store, ok := r.Store.(atomicUserMongoStore)
	if !ok {
		return nil, ErrProfileUpdateUnavailable
	}
	collection, err := r.GetUserCollection(ctx)
	if err != nil {
		return nil, err
	}
	filter := accountSnapshotFilter(AccountSnapshot{UserID: a.UserID, Email: a.ExpectedEmail, Status: a.ExpectedStatus, Type: a.ExpectedType, EmailRevision: a.ExpectedRevision})
	filter["personal_info.first_name"] = profileSnapshotField(command.Before.FirstName)
	filter["personal_info.last_name"] = profileSnapshotField(command.Before.LastName)
	filter["personal_info.full_name"] = profileSnapshotField(command.Before.FullName)
	update := mongo.Pipeline{bson.D{{Key: "$set", Value: bson.M{
		"personal_info": bson.M{"$mergeObjects": bson.A{bson.M{"$ifNull": bson.A{"$personal_info", bson.M{}}}, bson.M{"$literal": bson.M{"first_name": command.After.FirstName, "last_name": command.After.LastName, "full_name": command.After.FullName}}}},
		"metadata":      bson.M{"$mergeObjects": bson.A{bson.M{"$ifNull": bson.A{"$metadata", bson.M{}}}, bson.M{"updated_at": command.UpdatedAt}}},
	}}}}
	var account UniversalUser
	err = store.ExecuteFindOneAndUpdateCommandDecodeResult(ctx, collection, filter, update, &account, options.FindOneAndUpdate().SetReturnDocument(options.After).SetCollation(&options.Collation{Locale: "simple"}))
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, ErrProfileUpdateConflict
		}
		return nil, err
	}
	if !validProfileAccount(&account, a) || profileNames(&account) != command.After || account.Metadata == nil || account.Metadata.UpdatedAt != command.UpdatedAt {
		return nil, ErrProfileUpdateUnavailable
	}
	return &account, nil
}
