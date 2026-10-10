package user

import (
	"bytes"
	"context"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/x/mongo/driver"
)

// UserHandleIndexName identifies the explicit, partial, globally unique index.
const UserHandleIndexName = "idx_users_handle"

// atomicUserMongoStore opts into atomic post-image writes without expanding the
// legacy MongoDbStore interface required by existing applications and mocks.
type atomicUserMongoStore interface {
	// ExecuteFindOneAndUpdateCommandDecodeResult atomically finds, updates and
	// decodes one document with the supplied filter, update and options, enabling
	// post-image writes without extending the legacy store interface.
	ExecuteFindOneAndUpdateCommandDecodeResult(context.Context, *mongo.Collection, any, any, any, ...options.Lister[options.FindOneAndUpdateOptions]) error
}

// handleCollection validates the optional adapter before any driver operation.
func (r *Repository) handleCollection(ctx context.Context) (*mongo.Collection, error) {
	if r == nil || r.Store == nil || ctx == nil {
		return nil, ErrHandleUnsupported
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if _, ok := r.Store.(atomicUserMongoStore); !ok {
		return nil, ErrHandleUnsupported
	}
	return r.GetUserCollection(ctx)
}

// RequireHandleStorage verifies the exact migration contract, without creating
// indexes during requests. Operators must not remove indexes while writers run.
// Driver outages remain errors; only absent/incorrect constraints map to readiness.
func (r *Repository) RequireHandleStorage(ctx context.Context) error {
	collection, err := r.handleCollection(ctx)
	if err != nil {
		return err
	}
	cursor, err := collection.Indexes().List(ctx)
	if err != nil {
		if command, ok := err.(mongo.CommandError); ok && command.Code == 26 {
			return ErrHandleIndexesRequired
		}
		return err
	}
	var indexes []bson.Raw
	if err := r.Store.MapAllInCursorToResult(ctx, cursor, &indexes, "user-handle-indexes"); err != nil {
		return err
	}
	for _, index := range indexes {
		name, _ := index.Lookup("name").StringValueOK()
		unique, _ := index.Lookup("unique").BooleanOK()
		sparse, _ := index.Lookup("sparse").BooleanOK()
		key, _ := index.Lookup("key").DocumentOK()
		partial, _ := index.Lookup("partialFilterExpression").DocumentOK()
		locale, _ := index.Lookup("collation", "locale").StringValueOK()
		if name == UserHandleIndexName && unique && !sparse && handleKeyPattern(key) && handlePartialFilter(partial) && (locale == "" || locale == "simple") {
			return nil
		}
	}
	return ErrHandleIndexesRequired
}

// handleKeyPattern accepts only a single ascending handle key, never text from
// an error message or a composite constraint belonging to another domain.
func handleKeyPattern(raw bson.Raw) bool {
	elements, err := raw.Elements()
	if err != nil || len(elements) != 1 || elements[0].Key() != "handle" {
		return false
	}
	value, ok := elements[0].Value().AsInt64OK()
	return ok && value == 1
}

// handlePartialFilter requires exactly {handle: {$gt: ""}} so legacy missing
// handles can coexist without exempting any valid nonempty canonical handle.
func handlePartialFilter(raw bson.Raw) bool {
	elements, err := raw.Elements()
	if err != nil || len(elements) != 1 || elements[0].Key() != "handle" {
		return false
	}
	doc, ok := elements[0].Value().DocumentOK()
	if !ok {
		return false
	}
	conditions, err := doc.Elements()
	if err != nil || len(conditions) != 1 || conditions[0].Key() != "$gt" {
		return false
	}
	value, ok := conditions[0].Value().StringValueOK()
	return ok && value == ""
}

// handleDuplicate classifies only an unambiguous native single-write handle
// collision. Mixed/write-concern errors and wrappers are not safe to retry.
func handleDuplicate(err error) bool {
	pattern, ok := singleWriteDuplicatePattern(err)
	return ok && handleKeyPattern(pattern)
}

// singleWriteDuplicatePattern extracts a constraint only from an unambiguous
// native duplicate response, preserving extra causes and write-concern failures.
func singleWriteDuplicatePattern(err error) (bson.Raw, bool) {
	switch failure := err.(type) {
	case mongo.CommandError:
		pattern, _ := failure.Raw.Lookup("keyPattern").DocumentOK()
		if failure.Code != 11000 || len(failure.Labels) != 0 || len(pattern) == 0 {
			return nil, false
		}
		if failure.Wrapped == nil {
			return pattern, true
		}
		// Driver v2 retains its native command error as Wrapped. Accept only
		// that identical response, with no additional cause or retry labels;
		// arbitrary wrappers and joined/transport failures remain ambiguous.
		native, ok := failure.Wrapped.(driver.Error)
		return pattern, ok && native.Code == failure.Code && native.Wrapped == nil && len(native.Labels) == 0 && bytes.Equal(native.Raw, failure.Raw)
	case mongo.WriteException:
		if failure.WriteConcernError != nil || len(failure.WriteErrors) != 1 || len(failure.Labels) != 0 {
			return nil, false
		}
		item := failure.WriteErrors[0]
		pattern, _ := item.Raw.Lookup("keyPattern").DocumentOK()
		return pattern, item.Code == 11000 && len(pattern) != 0
	default:
		return nil, false
	}
}

// oauthCreationDuplicate permits winner resolution only for the account's
// explicit email, provider identity or handle constraints. An unrelated unique
// index or mixed operational failure must not be reported as a linking conflict.
func oauthCreationDuplicate(err error) bool {
	pattern, ok := singleWriteDuplicatePattern(err)
	if !ok {
		return false
	}
	elements, decodeErr := pattern.Elements()
	if decodeErr != nil || len(elements) != 1 {
		return false
	}
	key := elements[0].Key()
	direction, numeric := elements[0].Value().AsInt64OK()
	return numeric && direction == 1 && (key == "handle" || key == "email" || key == "oauth_identity_keys")
}

// prepareHandleInsert protects direct trusted repository inserts too. An absent
// handle preserves legacy behavior; populated metadata without a handle is invalid.
func (r *Repository) prepareHandleInsert(ctx context.Context, user *UniversalUser) error {
	if user.Handle == "" && user.HandleMetadata == nil {
		return nil
	}
	view, err := handleView(user)
	if err != nil {
		return err
	}
	if view.Metadata.Revision != 1 || view.Metadata.ChangeCount != 0 || view.Metadata.LastUserChangeAt != "" {
		return ErrHandleConflict
	}
	return r.RequireHandleStorage(ctx)
}

// HandleAvailable is an advisory indexed query. Its result never reserves a
// candidate, and a query failure never means the name is available.
func (r *Repository) HandleAvailable(ctx context.Context, handle, exceptID string) (bool, error) {
	canonical, err := NormalizeHandle(handle)
	if err != nil || canonical != handle {
		return false, ErrInvalidHandle
	}
	if err := r.RequireHandleStorage(ctx); err != nil {
		return false, err
	}
	collection, err := r.handleCollection(ctx)
	if err != nil {
		return false, err
	}
	filter := bson.M{"handle": handle}
	if exceptID != "" {
		filter["_id"] = bson.M{"$ne": exceptID}
	}
	count, err := r.Store.ExecuteCountDocuments(ctx, collection, filter, options.Count().SetLimit(1).SetCollation(&options.Collation{Locale: "simple"}))
	return err == nil && count == 0, err
}

// SetUserHandle uses one non-upsert update pipeline for compare-and-swap,
// metadata initialization and a no-op that does not advance revision/timestamps.
// The returned projection belongs to that exact write, not a subsequent read.
func (r *Repository) SetUserHandle(ctx context.Context, req *UpdateUserHandleRequest, at time.Time) (*UserHandle, error) {
	if req == nil || req.UserID == "" {
		return nil, ErrInvalidUserID
	}
	if req.ExpectedRevision < 0 || req.ExpectedRevision >= MaxHandleRevision || at.IsZero() {
		return nil, ErrHandleConflict
	}
	canonical, err := NormalizeHandle(req.Handle)
	if err != nil || canonical != req.Handle {
		return nil, ErrInvalidHandle
	}
	if err := r.RequireHandleStorage(ctx); err != nil {
		return nil, err
	}
	collection, err := r.handleCollection(ctx)
	if err != nil {
		return nil, err
	}
	filter := bson.M{"_id": req.UserID, "status": "ACTIVE", "handle_metadata.revision": req.ExpectedRevision}
	if req.ExpectedRevision == 0 {
		filter["handle_metadata.revision"] = bson.M{"$in": bson.A{nil, int64(0)}}
	}
	stamp := at.UTC().Format(time.RFC3339Nano)
	metadata := bson.M{
		"created_at": bson.M{"$ifNull": bson.A{"$handle_metadata.created_at", stamp}},
		"updated_at": stamp, "last_user_change_at": stamp,
		"revision":     bson.M{"$add": bson.A{bson.M{"$ifNull": bson.A{"$handle_metadata.revision", int64(0)}}, int64(1)}},
		"change_count": bson.M{"$add": bson.A{bson.M{"$ifNull": bson.A{"$handle_metadata.change_count", int64(0)}}, int64(1)}},
	}
	update := mongo.Pipeline{{{Key: "$set", Value: bson.M{
		"handle":          bson.M{"$literal": req.Handle},
		"handle_metadata": bson.M{"$cond": bson.A{bson.M{"$eq": bson.A{"$handle", req.Handle}}, "$handle_metadata", metadata}},
	}}}}
	var updated UniversalUser
	err = r.Store.(atomicUserMongoStore).ExecuteFindOneAndUpdateCommandDecodeResult(ctx, collection, filter, update, &updated, options.FindOneAndUpdate().SetReturnDocument(options.After).SetCollation(&options.Collation{Locale: "simple"}))
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, ErrHandleConflict
		}
		if handleDuplicate(err) {
			return nil, ErrHandleTaken
		}
		return nil, err
	}
	return handleView(&updated)
}

// Compile-time assertions keep the optional capability attached to the concrete
// repository without changing the requirements on third-party UserRepository.
var _ HandleRepository = (*Repository)(nil)
