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
	GetByKey(context.Context, string) (Document, error)
	InsertIfAbsent(context.Context, Document) (Document, bool, error)
	ReplaceWithRevision(context.Context, Document, int) (Document, error)
	List(context.Context, catalogue.ListQuery, bool) ([]Document, int64, error)
}
type TypedRepository[T any] struct {
	store Store
	entry func(*T) *catalogue.Entry
}

func NewRepository[T any](store Store, entry func(*T) *catalogue.Entry) (*TypedRepository[T], error) {
	if store == nil || (reflect.ValueOf(store).Kind() == reflect.Pointer && reflect.ValueOf(store).IsNil()) || entry == nil {
		return nil, catalogue.ErrUnavailable
	}
	return &TypedRepository[T]{store: store, entry: entry}, nil
}
func DecodePayload[T any](doc Document) (T, error) {
	var value T
	encoded, err := bson.Marshal(doc.Payload)
	if err != nil {
		return value, err
	}
	err = bson.Unmarshal(encoded, &value)
	return value, err
}
func (r *TypedRepository[T]) document(value T) Document {
	entry := r.entry(&value)
	return Document{ID: entry.Code, Key: entry.Code, Enabled: entry.Enabled, Hidden: entry.Hidden, Audit: entry.Audit, Payload: value}
}
func (r *TypedRepository[T]) Get(ctx context.Context, code string) (T, error) {
	doc, err := r.store.GetByKey(ctx, code)
	if err != nil {
		var zero T
		return zero, err
	}
	return DecodePayload[T](doc)
}
func (r *TypedRepository[T]) InsertIfAbsent(ctx context.Context, value T) (T, bool, error) {
	doc, inserted, err := r.store.InsertIfAbsent(ctx, r.document(value))
	if err != nil {
		var zero T
		return zero, false, err
	}
	record, err := DecodePayload[T](doc)
	return record, inserted, err
}
func (r *TypedRepository[T]) Replace(ctx context.Context, value T, revision int) (T, error) {
	doc, err := r.store.ReplaceWithRevision(ctx, r.document(value), revision)
	if err != nil {
		var zero T
		return zero, err
	}
	return DecodePayload[T](doc)
}
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
