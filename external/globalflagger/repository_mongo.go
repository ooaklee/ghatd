package globalflagger

import (
	"context"
	"fmt"

	"github.com/ooaklee/ghatd/external/catalogue"
	"github.com/ooaklee/ghatd/external/cataloguestore"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

// MongoRepository adapts the generic cataloguestore Mongo adapter to the
// typed Repository port. It holds no datastore logic: mapping and
// collection wiring only. The Service never sees Mongo types.
type MongoRepository struct {
	store *cataloguestore.MongoStore
}

// Compile-time port conformance.
var _ Repository = (*MongoRepository)(nil)

// NewMongoRepository wires the adapter over a cataloguestore.MongoStore
// configured with Collection on the host's managed database.
func NewMongoRepository(store *cataloguestore.MongoStore) (*MongoRepository, error) {
	if store == nil {
		return nil, fmt.Errorf("%w: nil cataloguestore", catalogue.ErrUnavailable)
	}
	return &MongoRepository{store: store}, nil
}

// NewMongoRepositoryFromDatabase is the host convenience constructor: it
// builds the GHATD shared repository over the managed database and binds
// this package's collection. No additional connection pool is created.
func NewMongoRepositoryFromDatabase(db *mongo.Database) (*MongoRepository, error) {
	if db == nil {
		return nil, fmt.Errorf("%w: nil database", catalogue.ErrUnavailable)
	}
	store, err := cataloguestore.OpenMongo(db, Collection)
	if err != nil {
		return nil, err
	}
	return NewMongoRepository(store)
}

// flagPayload is the domain payload stored inside the generic Document.
type flagPayload struct {
	Name string `json:"name" bson:"name"`
	SVG  string `json:"svg" bson:"svg"`
}

// toDocument maps a Record into the generic storage image. The record ID
// is the stable flag code so foreign flag_id references stay stable across
// replays; the Key carries the code for natural-key queries.
func toDocument(record *Record) cataloguestore.Document {
	return cataloguestore.Document{
		ID:      record.Code,
		Key:     record.Code,
		Enabled: record.Enabled,
		Hidden:  record.Hidden,
		Payload: flagPayload{Name: record.Name, SVG: record.SVG},
		Audit:   record.Audit,
	}
}

// fromDocument maps the generic storage image back into a Record. Payload
// shapes vary between the typed value (direct use) and BSON round-trips
// (bson.D / bson.M / map), so decoding goes through
// cataloguestore.DecodePayload; a decode failure is returned, never
// silently swallowed into a record with an empty SVG.
func fromDocument(doc cataloguestore.Document) (*Record, error) {
	payload, err := cataloguestore.DecodePayload[flagPayload](doc)
	if err != nil {
		return nil, fmt.Errorf("%w: decoding flag payload: %v", catalogue.ErrUnavailable, err)
	}
	return &Record{
		ID:      doc.Key,
		Code:    doc.Key,
		Enabled: doc.Enabled,
		Hidden:  doc.Hidden,
		Name:    payload.Name,
		SVG:     payload.SVG,
		Audit:   doc.Audit,
	}, nil
}

// Get returns one record by code, including deleted and hidden records.
func (r *MongoRepository) Get(ctx context.Context, code string) (*Record, error) {
	doc, err := r.store.GetByKey(ctx, code)
	if err != nil {
		return nil, err
	}
	return fromDocument(doc)
}

// GetByID returns one record by identifier (identifier equals the code).
func (r *MongoRepository) GetByID(ctx context.Context, id string) (*Record, error) {
	doc, err := r.store.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	return fromDocument(doc)
}

// List returns a bounded page plus total.
func (r *MongoRepository) List(ctx context.Context, query catalogue.ListQuery, publicSelectableOnly bool) ([]Record, int64, error) {
	docs, total, err := r.store.List(ctx, query, publicSelectableOnly)
	if err != nil {
		return nil, 0, err
	}
	records := make([]Record, 0, len(docs))
	for i := range docs {
		record, err := fromDocument(docs[i])
		if err != nil {
			return nil, 0, err
		}
		records = append(records, *record)
	}
	return records, total, nil
}

// Create persists a new record; duplicate codes map to ErrAlreadyExists.
func (r *MongoRepository) Create(ctx context.Context, record *Record) (*Record, error) {
	_, inserted, err := r.store.InsertIfAbsent(ctx, toDocument(record))
	if err != nil {
		return nil, err
	}
	if !inserted {
		return nil, catalogue.ErrAlreadyExists
	}
	return record, nil
}

// ReplaceWithRevision applies the compare-and-swap replace.
func (r *MongoRepository) ReplaceWithRevision(ctx context.Context, record *Record, expectedRevision int) (*Record, error) {
	updated, err := r.store.ReplaceWithRevision(ctx, toDocument(record), expectedRevision)
	if err != nil {
		return nil, err
	}
	return fromDocument(updated)
}

// Count returns non-deleted record count.
func (r *MongoRepository) Count(ctx context.Context) (int64, error) {
	return r.store.Count(ctx)
}

// InsertIfAbsent seeds one record and never overwrites existing state.
func (r *MongoRepository) InsertIfAbsent(ctx context.Context, record *Record) (*Record, bool, error) {
	doc, inserted, err := r.store.InsertIfAbsent(ctx, toDocument(record))
	if err != nil {
		return nil, false, err
	}
	record2, err := fromDocument(doc)
	if err != nil {
		return nil, false, err
	}
	return record2, inserted, nil
}
