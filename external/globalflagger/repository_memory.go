package globalflagger

import (
	"context"
	"fmt"
	"time"

	"github.com/ooaklee/ghatd/external/catalogue"
	"github.com/ooaklee/ghatd/external/cataloguestore"
)

// MemoryRepository adapts the in-memory cataloguestore to the typed
// Repository port for isolated tests of the Service and seeding. It must
// exhibit the same semantics as the Mongo adapter: absence distinct from
// failure, CAS on revision, soft-delete visibility rules, and
// insert-if-absent seeding that never overwrites existing state.
type MemoryRepository struct {
	store *cataloguestore.MemoryStore
}

// Compile-time port conformance.
var _ Repository = (*MemoryRepository)(nil)

// NewMemoryRepository returns an empty in-memory repository. The optional
// now function provides deterministic timestamps in tests; nil uses the
// store's deterministic default.
func NewMemoryRepository(now func() time.Time) *MemoryRepository {
	return &MemoryRepository{store: cataloguestore.NewMemoryStore(now)}
}

// toDocument maps a Record into the store's document shape; the payload carries
// name and sanitised SVG while the code doubles as ID and key.
func (r *MemoryRepository) toDocument(record *Record) cataloguestore.Document {
	return cataloguestore.Document{
		ID:      record.Code,
		Key:     record.Code,
		Enabled: record.Enabled,
		Hidden:  record.Hidden,
		Payload: flagPayload{Name: record.Name, SVG: record.SVG},
		Audit:   record.Audit,
	}
}

// fromMemoryDocument maps the storage image back into a Record. The
// MemoryStore deep-clones payloads through BSON, so the payload may arrive
// as bson.D rather than the typed struct; DecodePayload handles every
// shape and decode failures are propagated, never silently reduced to a
// record with an empty SVG.
func fromMemoryDocument(doc cataloguestore.Document) (*Record, error) {
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
func (r *MemoryRepository) Get(ctx context.Context, code string) (*Record, error) {
	doc, err := r.store.GetByKey(ctx, code)
	if err != nil {
		return nil, err
	}
	return fromMemoryDocument(doc)
}

// GetByID returns one record by identifier.
func (r *MemoryRepository) GetByID(ctx context.Context, id string) (*Record, error) {
	doc, err := r.store.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	return fromMemoryDocument(doc)
}

// List returns a bounded page plus total.
func (r *MemoryRepository) List(ctx context.Context, query catalogue.ListQuery, publicSelectableOnly bool) ([]Record, int64, error) {
	docs, total, err := r.store.List(ctx, query, publicSelectableOnly)
	if err != nil {
		return nil, 0, err
	}
	records := make([]Record, 0, len(docs))
	for i := range docs {
		record, err := fromMemoryDocument(docs[i])
		if err != nil {
			return nil, 0, err
		}
		records = append(records, *record)
	}
	return records, total, nil
}

// Create persists a new record; duplicates map to ErrAlreadyExists.
func (r *MemoryRepository) Create(ctx context.Context, record *Record) (*Record, error) {
	_, inserted, err := r.store.InsertIfAbsent(ctx, r.toDocument(record))
	if err != nil {
		return nil, err
	}
	if !inserted {
		return nil, catalogue.ErrAlreadyExists
	}
	return record, nil
}

// ReplaceWithRevision applies the compare-and-swap replace.
func (r *MemoryRepository) ReplaceWithRevision(ctx context.Context, record *Record, expectedRevision int) (*Record, error) {
	updated, err := r.store.ReplaceWithRevision(ctx, r.toDocument(record), expectedRevision)
	if err != nil {
		return nil, err
	}
	return fromMemoryDocument(updated)
}

// Count returns non-deleted record count.
func (r *MemoryRepository) Count(ctx context.Context) (int64, error) {
	return r.store.Count(ctx)
}

// InsertIfAbsent seeds one record and never overwrites existing state.
func (r *MemoryRepository) InsertIfAbsent(ctx context.Context, record *Record) (*Record, bool, error) {
	doc, inserted, err := r.store.InsertIfAbsent(ctx, r.toDocument(record))
	if err != nil {
		return nil, false, err
	}
	decoded, err := fromMemoryDocument(doc)
	if err != nil {
		return nil, false, err
	}
	return decoded, inserted, nil
}
