package cataloguestore

import (
	"context"
	"reflect"

	"github.com/ooaklee/ghatd/external/catalogue"
	"github.com/ooaklee/ghatd/external/repository"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// Store is the adapter's document port; services depend on typed repositories.
type Store interface {
	// GetByKey fetches one Store document by its key; absence is reported as
	// ErrNotFound by the supplied implementations. Returns the document or an
	// error; ctx governs cancellation.
	GetByKey(context.Context, string) (Document, error)
	// InsertIfAbsent seeds one Store document keyed by Key; implementations return
	// the existing document untouched with inserted=false so stored state is never
	// reset. Returns the document, whether inserted, or an error.
	InsertIfAbsent(context.Context, Document) (Document, bool, error)
	// ReplaceWithRevision performs a compare-and-swap replace of a Store document
	// against the expected revision. Returns the stored document or an error
	// distinguishing absence from stale revision; ctx governs cancellation.
	ReplaceWithRevision(context.Context, Document, int) (Document, error)
	// List returns a bounded, ordered page of Store documents plus the total
	// matching count; publicSelectableOnly excludes deleted, hidden and disabled
	// records unless the query overrides. Returns the documents, total, or an
	// error.
	List(context.Context, catalogue.ListQuery, bool) ([]Document, int64, error)
}

// TypedRepository adapts a generic document store into the typed catalogue
// Repository, projecting each record's shared entry via the supplied accessor.
type TypedRepository[T any] struct {
	store Store
	entry func(*T) *catalogue.Entry
}

// NewRepository validates the store and entry accessor before use, returning
// catalogue.ErrUnavailable for nil wiring.
func NewRepository[T any](store Store, entry func(*T) *catalogue.Entry) (*TypedRepository[T], error) {
	if store == nil || (reflect.ValueOf(store).Kind() == reflect.Pointer && reflect.ValueOf(store).IsNil()) || entry == nil {
		return nil, catalogue.ErrUnavailable
	}
	return &TypedRepository[T]{store: store, entry: entry}, nil
}

// DecodePayload round-trips a document's payload through BSON to reconstruct
// the typed record, reporting marshalling errors unchanged.
func DecodePayload[T any](doc Document) (T, error) {
	var value T
	encoded, err := bson.Marshal(doc.Payload)
	if err != nil {
		return value, err
	}
	err = bson.Unmarshal(encoded, &value)
	return value, err
}

// document projects a typed record into its storage image, keying the document
// by entry code and copying shared visibility and audit fields.
func (r *TypedRepository[T]) document(value T) Document {
	entry := r.entry(&value)
	return Document{ID: entry.Code, Key: entry.Code, Enabled: entry.Enabled, Hidden: entry.Hidden, Audit: entry.Audit, Payload: value}
}

// Get reads one record by code, decoding the stored document's payload and
// forwarding absence and failure errors from the underlying store.
func (r *TypedRepository[T]) Get(ctx context.Context, code string) (T, error) {
	doc, err := r.store.GetByKey(ctx, code)
	if err != nil {
		var zero T
		return zero, err
	}
	return DecodePayload[T](doc)
}

// InsertIfAbsent seeds a record's document; when a document already exists the
// stored record is returned with inserted=false so existing state is untouched.
func (r *TypedRepository[T]) InsertIfAbsent(ctx context.Context, value T) (T, bool, error) {
	doc, inserted, err := r.store.InsertIfAbsent(ctx, r.document(value))
	if err != nil {
		var zero T
		return zero, false, err
	}
	record, err := DecodePayload[T](doc)
	return record, inserted, err
}

// Replace performs a revision-guarded document replacement and decodes the
// stored result, forwarding the store's revision-conflict errors.
func (r *TypedRepository[T]) Replace(ctx context.Context, value T, revision int) (T, error) {
	doc, err := r.store.ReplaceWithRevision(ctx, r.document(value), revision)
	if err != nil {
		var zero T
		return zero, err
	}
	return DecodePayload[T](doc)
}

// List reads a non-admin page of documents and decodes each payload; any decode
// failure fails the whole page rather than returning partial results.
func (r *TypedRepository[T]) List(ctx context.Context, q catalogue.ListQuery) ([]T, int64, error) {
	docs, total, err := r.store.List(ctx, q, false)
	if err != nil {
		return nil, 0, err
	}
	out := make([]T, 0, len(docs))
	for _, doc := range docs {
		value, err := DecodePayload[T](doc)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, value)
	}
	return out, total, nil
}

// OpenMongo borrows the host pool. Index ownership remains with each package's
// migration helper; this function creates no connection and closes none.
func OpenMongo(db *mongo.Database, collection string) (*MongoStore, error) {
	if db == nil || collection == "" {
		return nil, catalogue.ErrUnavailable
	}
	core, err := repository.NewMongoDbRepositoryFromDatabase(db, repository.NewNoOpRepositoryLogger())
	if err != nil {
		return nil, err
	}
	return NewMongoStore(core, db.Collection(collection))
}

// EnsureIndexes creates the unique key index and the selection index used for
// public filtering on a catalogue collection, returning an error for nil
// databases or empty collection names.
func EnsureIndexes(ctx context.Context, db *mongo.Database, collection string) error {
	if db == nil || collection == "" {
		return catalogue.ErrUnavailable
	}
	_, err := db.Collection(collection).Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "key", Value: 1}}, Options: options.Index().SetName("catalogue_key").SetUnique(true)},
		{Keys: bson.D{{Key: "enabled", Value: 1}, {Key: "hidden", Value: 1}, {Key: "deleted_at", Value: 1}, {Key: "key", Value: 1}}, Options: options.Index().SetName("catalogue_selection")},
	})
	return err
}
