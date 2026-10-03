package user

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// emailKeyPattern accepts the exact single ascending email constraint, not a
// similarly worded duplicate message or an unrelated composite unique index.
func emailKeyPattern(raw bson.Raw) bool {
	elements, err := raw.Elements()
	if err != nil || len(elements) != 1 || elements[0].Key() != "email" {
		return false
	}
	value := elements[0].Value()
	switch value.Type {
	case bson.TypeInt32:
		n, ok := value.Int32OK()
		return ok && n == 1
	case bson.TypeInt64:
		n, ok := value.Int64OK()
		return ok && n == 1
	case bson.TypeDouble:
		n, ok := value.DoubleOK()
		return ok && n == 1
	default:
		return false
	}
}

// emailChangeIndexReady accepts a complete, exact unique constraint. With
// includeBuildUUIDs, Mongo represents unfinished indexes as {spec, buildUUID};
// those are not ready. A hidden but complete index still enforces uniqueness.
func emailChangeIndexReady(index bson.Raw) bool {
	name, _ := index.Lookup("name").StringValueOK()
	unique, _ := index.Lookup("unique").BooleanOK()
	sparse, _ := index.Lookup("sparse").BooleanOK()
	key, _ := index.Lookup("key").DocumentOK()
	locale, _ := index.Lookup("collation", "locale").StringValueOK()
	return index.Lookup("buildUUID").Type == 0 && index.Lookup("spec").Type == 0 &&
		name == "idx_users_email" && unique && !sparse && emailKeyPattern(key) &&
		index.Lookup("partialFilterExpression").Type == 0 && (locale == "" || locale == "simple")
}

// requireEmailChangeIndex verifies the existing migration, never creates it.
// Index removal while writers are active is an unsupported operational change.
func (r *Repository) requireEmailChangeIndex(ctx context.Context, collection *mongo.Collection) error {
	// The driver index-list options do not expose includeBuildUUIDs. Request it
	// explicitly: an in-progress build then has a nested spec/buildUUID instead
	// of a ready index's top-level name/key, and cannot satisfy this gate.
	cursor, err := collection.Database().RunCommandCursor(ctx, bson.D{
		{Key: "listIndexes", Value: collection.Name()},
		{Key: "includeBuildUUIDs", Value: true},
		{Key: "cursor", Value: bson.D{}},
	})
	if err != nil {
		if command, ok := err.(mongo.CommandError); ok && command.Code == 26 {
			return ErrEmailIndexesRequired
		}
		return err
	}
	var indexes []bson.Raw
	if err := r.Store.MapAllInCursorToResult(ctx, cursor, &indexes, "user-email-indexes"); err != nil {
		return err
	}
	for _, index := range indexes {
		if emailChangeIndexReady(index) {
			return nil
		}
	}
	return ErrEmailIndexesRequired
}

// ChangeUserEmail performs a single conditional write through the managed
// repository helper. Missing/null legacy revisions are zero. Email/status are
// aggregation literals, including mailboxes beginning with '$'. No manager-level
// retry or read-after-write can turn an uncertain outcome into claimed success.
func (r *Repository) ChangeUserEmail(ctx context.Context, req *ChangeUserEmailRequest, status string, at time.Time) (*UniversalUser, error) {
	if ctx == nil || r == nil || nilUserDependency(r.Store) {
		return nil, ErrEmailChangeUnavailable
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	command, err := normalizeEmailChange(req)
	if err != nil {
		return nil, err
	}
	if status == "" || at.IsZero() {
		return nil, ErrInvalidUserBody
	}
	store, ok := r.Store.(atomicUserMongoStore)
	if !ok {
		return nil, ErrEmailChangeUnavailable
	}
	collection, err := r.GetUserCollection(ctx)
	if err != nil {
		return nil, err
	}
	if err := r.requireEmailChangeIndex(ctx, collection); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	filter := bson.M{"_id": command.UserID, "email": command.ExpectedEmail, "status": command.ExpectedStatus, "type": command.ExpectedType, "email_revision": command.ExpectedRevision}
	if command.ExpectedRevision == 0 {
		filter["email_revision"] = bson.M{"$in": bson.A{nil, int64(0)}}
	}
	if command.ExpectedType == "" {
		filter["type"] = bson.M{"$in": bson.A{nil, ""}}
	}
	stamp := at.UTC().Format(time.RFC3339Nano)
	update := mongo.Pipeline{bson.D{{Key: "$set", Value: bson.M{
		"email": bson.M{"$literal": command.Email}, "status": bson.M{"$literal": status},
		"email_revision": bson.M{"$add": bson.A{bson.M{"$ifNull": bson.A{"$email_revision", int64(0)}}, int64(1)}},
		"verification":   bson.M{"$mergeObjects": bson.A{bson.M{"$ifNull": bson.A{"$verification", bson.M{}}}, bson.M{"email_verified": false, "email_verified_at": ""}}},
		"metadata":       bson.M{"$mergeObjects": bson.A{bson.M{"$ifNull": bson.A{"$metadata", bson.M{}}}, bson.M{"updated_at": stamp, "status_changed_at": stamp}}},
	}}}}
	var account UniversalUser
	err = store.ExecuteFindOneAndUpdateCommandDecodeResult(ctx, collection, filter, update, &account,
		options.FindOneAndUpdate().SetReturnDocument(options.After).SetCollation(&options.Collation{Locale: "simple"}))
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, ErrEmailChangeConflict
		}
		if pattern, exact := singleWriteDuplicatePattern(err); exact && emailKeyPattern(pattern) {
			return nil, ErrEmailAlreadyExists
		}
		return nil, err
	}
	if !validEmailChangeReceipt(&account, command, status) {
		return nil, ErrEmailChangeUnavailable
	}
	return &account, nil
}
