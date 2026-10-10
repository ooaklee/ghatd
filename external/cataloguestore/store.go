// Package cataloguestore provides a small reusable generic catalogue
// persistence adapter behind the GHATD result-aware Mongo helpers, plus an
// in-memory repository with equivalent revision semantics for isolated
// tests.
//
// The adapter is deliberately storage-only. Domain validation belongs in
// each owning child-domain service; this package moves Documents.
//
// Result semantics (mandatory, preserved from the GHATD blueprint):
//   - cancellation is checked before and after driver calls;
//   - absence (ErrNotFound) is distinct from native store failure;
//   - acknowledged/uncertain outcomes are distinguished — an insert or CAS
//     update without an authoritative receipt returns ErrUnavailable and is
//     never retried by callers;
//   - expected-revision concurrency: only a matching compare-and-swap
//     succeeds, otherwise ErrStaleWrite;
//   - seeding uses insert-if-absent and never resets admin edits, hidden,
//     deleted or disabled state, or audit attribution.
package cataloguestore

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ooaklee/ghatd/external/catalogue"
	"github.com/ooaklee/ghatd/external/repository"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// ErrClosed reports use of a memory store after Close.
var ErrClosed = errors.New("cataloguestore/closed")

// MongoDbStore is the subset of the GHATD shared helper used by the generic
// adapter. The production implementation is *repository.MongoDbRepository
// built with repository.NewMongoDbRepositoryFromDatabase over the host's
// managed database (no additional connection pool).
type MongoDbStore interface {
	// ExecuteCountDocuments counts documents in the mongo collection matching
	// filter through the MongoDbStore helper subset used by the generic adapter.
	// Returns the count or an error; ctx governs cancellation.
	ExecuteCountDocuments(ctx context.Context, collection *mongo.Collection, filter interface{}, opts ...options.Lister[options.CountOptions]) (int64, error)
	// ExecuteFindOneCommandDecodeResult runs a single-document find on the given
	// collection and decodes the match into result, using resultObjectName for
	// diagnostics and returning onFailureErr when no document is found.
	ExecuteFindOneCommandDecodeResult(ctx context.Context, collection *mongo.Collection, filter interface{}, result interface{}, resultObjectName string, logError bool, onFailureErr error) error
	// ExecuteFindCommand runs a find on the given collection with the supplied
	// filter and find options and returns the driver cursor for caller-side
	// iteration.
	ExecuteFindCommand(ctx context.Context, collection *mongo.Collection, filter interface{}, opts ...options.Lister[options.FindOptions]) (*mongo.Cursor, error)
	// ExecuteInsertOneCommand inserts one document into the given collection and
	// returns the driver insert receipt, naming the target via resultObjectName in
	// diagnostics.
	ExecuteInsertOneCommand(ctx context.Context, collection *mongo.Collection, document interface{}, resultObjectName string) (*mongo.InsertOneResult, error)
	// ExecuteReplaceOneCommandResult replaces the first document matching filter
	// with replacement, honouring replace options, and returns the driver update
	// result.
	ExecuteReplaceOneCommandResult(ctx context.Context, collection *mongo.Collection, filter, replacement any, opts ...options.Lister[options.ReplaceOptions]) (*mongo.UpdateResult, error)
	// ExecuteDeleteOneCommandResult deletes the first document matching filter,
	// honouring delete-one options, and returns the driver delete result.
	ExecuteDeleteOneCommandResult(ctx context.Context, collection *mongo.Collection, filter any, opts ...options.Lister[options.DeleteOneOptions]) (*mongo.DeleteResult, error)
	// MapAllInCursorToResult exhausts the cursor and decodes every document into
	// the caller-supplied result slice, using resultObjectName in diagnostics.
	MapAllInCursorToResult(ctx context.Context, cursor *mongo.Cursor, result interface{}, resultObjectName string) error
}

// Document is the storage image of any catalogue record. Natural key is
// carried in Key; payload holds the domain-specific fields as BSON/JSON
// friendly maps or structs marshalled by the owning domain.
type Document struct {
	ID              string      `json:"id" bson:"_id"`
	Key             string      `json:"key" bson:"key"`
	Enabled         bool        `json:"enabled" bson:"enabled"`
	Hidden          bool        `json:"hidden" bson:"hidden"`
	Payload         interface{} `json:"payload,omitempty" bson:"payload,omitempty"`
	catalogue.Audit `bson:",inline"`
}

// MongoStore adapts one Mongo collection of Documents using the GHATD
// result-aware helpers. Configure Collection before concurrent use.
type MongoStore struct {
	Store      MongoDbStore
	Collection *mongo.Collection
}

// NewMongoStore validates wiring before use.
func NewMongoStore(store MongoDbStore, collection *mongo.Collection) (*MongoStore, error) {
	if store == nil || (reflect.ValueOf(store).Kind() == reflect.Pointer && reflect.ValueOf(store).IsNil()) || collection == nil {
		return nil, catalogue.ErrUnavailable
	}
	return &MongoStore{Store: store, Collection: collection}, nil
}

// collectionOrFail keeps absence distinct from wiring failure.
func (m *MongoStore) collectionOrFail() (*mongo.Collection, error) {
	if m == nil || m.Store == nil || m.Collection == nil {
		return nil, catalogue.ErrUnavailable
	}
	return m.Collection, nil
}

// InsertIfAbsent seeds one document keyed by Key. Existing records are
// returned untouched so admin edits, disablement, hidden/deleted state and
// audit attribution are never reset.
func (m *MongoStore) InsertIfAbsent(ctx context.Context, doc Document) (Document, bool, error) {
	col, err := m.collectionOrFail()
	if err != nil {
		return Document{}, false, err
	}
	if err := ctx.Err(); err != nil {
		return Document{}, false, err
	}
	var existing Document
	err = m.Store.ExecuteFindOneCommandDecodeResult(ctx, col, bson.M{"key": doc.Key}, &existing, "catalogue-entry", false, nil)
	if ctx.Err() != nil {
		return Document{}, false, ctx.Err()
	}
	switch {
	case err == nil:
		return existing, false, nil
	case errors.Is(err, mongo.ErrNoDocuments):
		// absent: insert below
	default:
		return Document{}, false, err
	}
	if err := ctx.Err(); err != nil {
		return Document{}, false, err
	}
	result, err := m.Store.ExecuteInsertOneCommand(ctx, col, &doc, "catalogue-entry")
	if err != nil {
		// A concurrent seeder may have won the race; surface as duplicate,
		// never as silent success.
		if mongo.IsDuplicateKeyError(err) {
			// A duplicate is a confirmed non-insert, so reading the winner is safe.
			existing, readErr := m.GetByKey(ctx, doc.Key)
			if readErr != nil {
				return Document{}, false, readErr
			}
			return existing, false, nil
		}
		return Document{}, false, err
	}
	if result == nil || !result.Acknowledged {
		return Document{}, false, catalogue.ErrUnavailable
	}
	if err := ctx.Err(); err != nil {
		return Document{}, false, err
	}
	return doc, true, nil
}

// GetByKey fetches one document; absence is ErrNotFound.
func (m *MongoStore) GetByKey(ctx context.Context, key string) (Document, error) {
	col, err := m.collectionOrFail()
	if err != nil {
		return Document{}, err
	}
	if err := ctx.Err(); err != nil {
		return Document{}, err
	}
	var doc Document
	err = m.Store.ExecuteFindOneCommandDecodeResult(ctx, col, bson.M{"key": key}, &doc, "catalogue-entry", false, nil)
	if ctx.Err() != nil {
		return Document{}, ctx.Err()
	}
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return Document{}, catalogue.ErrNotFound
		}
		return Document{}, err
	}
	if err := ctx.Err(); err != nil {
		return Document{}, err
	}
	return doc, nil
}

// GetByID fetches one document by identifier; absence is ErrNotFound.
func (m *MongoStore) GetByID(ctx context.Context, id string) (Document, error) {
	col, err := m.collectionOrFail()
	if err != nil {
		return Document{}, err
	}
	if err := ctx.Err(); err != nil {
		return Document{}, err
	}
	var doc Document
	err = m.Store.ExecuteFindOneCommandDecodeResult(ctx, col, bson.M{"_id": id}, &doc, "catalogue-entry", false, nil)
	if ctx.Err() != nil {
		return Document{}, ctx.Err()
	}
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return Document{}, catalogue.ErrNotFound
		}
		return Document{}, err
	}
	if err := ctx.Err(); err != nil {
		return Document{}, err
	}
	return doc, nil
}

// ReplaceWithRevision performs a compare-and-swap replace on expected
// revision. Zero match means absence or stale revision; an unacknowledged
// outcome is a failure whose committed state is unknown.
func (m *MongoStore) ReplaceWithRevision(ctx context.Context, doc Document, expectedRevision int) (Document, error) {
	if expectedRevision < 1 || doc.Revision != expectedRevision+1 {
		return Document{}, catalogue.ErrInvalidPayload
	}
	col, err := m.collectionOrFail()
	if err != nil {
		return Document{}, err
	}
	if err := ctx.Err(); err != nil {
		return Document{}, err
	}
	result, err := m.Store.ExecuteReplaceOneCommandResult(ctx, col,
		bson.M{"_id": doc.ID, "key": doc.Key, "revision": expectedRevision},
		doc,
	)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return Document{}, catalogue.ErrStaleWrite
		}
		return Document{}, err
	}
	if result == nil || !result.Acknowledged {
		return Document{}, catalogue.ErrUnavailable
	}
	switch result.MatchedCount {
	case 0:
		// Distinguish absence from stale revision for a better caller error.
		var exists Document
		getErr := m.Store.ExecuteFindOneCommandDecodeResult(ctx, col, bson.M{"_id": doc.ID}, &exists, "catalogue-entry", false, nil)
		if ctx.Err() != nil {
			return Document{}, ctx.Err()
		}
		if errors.Is(getErr, mongo.ErrNoDocuments) {
			return Document{}, catalogue.ErrNotFound
		}
		if getErr != nil {
			return Document{}, getErr
		}
		return Document{}, catalogue.ErrStaleWrite
	case 1:
		if err := ctx.Err(); err != nil {
			return Document{}, err
		}
		if result.ModifiedCount != 1 {
			return Document{}, catalogue.ErrUnavailable
		}
		return doc, nil
	default:
		return Document{}, fmt.Errorf("%w: matched %d documents", catalogue.ErrUnavailable, result.MatchedCount)
	}
}

// List returns a bounded, ordered page of documents and the total count of
// matching (non-paginated) documents. Public list filters exclude deleted
// records and, unless overridden, hidden and disabled ones.
func (m *MongoStore) List(ctx context.Context, q catalogue.ListQuery, publicSelectableOnly bool) ([]Document, int64, error) {
	col, err := m.collectionOrFail()
	if err != nil {
		return nil, 0, err
	}
	q = q.Sanitised()
	filter := bson.M{}
	if !q.IncludeDeleted || publicSelectableOnly {
		filter["deleted_at"] = nil
	}
	if publicSelectableOnly {
		filter["enabled"] = true
		filter["hidden"] = false
	} else {
		if !q.IncludeHidden {
			filter["hidden"] = false
		}
		if !q.IncludeDisabled {
			filter["enabled"] = true
		}
	}
	if q.Search != "" {
		filter["key"] = bson.M{"$regex": fmt.Sprintf("^%s", escapeRegex(q.Search)), "$options": "i"}
	}
	if err := ctx.Err(); err != nil {
		return nil, 0, err
	}
	total, err := m.Store.ExecuteCountDocuments(ctx, col, filter)
	if err != nil {
		return nil, 0, err
	}
	if err := ctx.Err(); err != nil {
		return nil, 0, err
	}
	if total < 0 {
		return nil, 0, catalogue.ErrUnavailable
	}
	opts := options.Find().
		SetSort(bson.D{{Key: "key", Value: 1}}).
		SetSkip(int64(q.Offset())).
		SetLimit(int64(q.PageSize))
	cursor, err := m.Store.ExecuteFindCommand(ctx, col, filter, opts)
	if err != nil {
		return nil, 0, err
	}
	if cursor == nil {
		return nil, 0, catalogue.ErrUnavailable
	}
	var docs []Document
	if err := m.Store.MapAllInCursorToResult(ctx, cursor, &docs, "catalogue-entries"); err != nil {
		return nil, 0, err
	}
	if err := ctx.Err(); err != nil {
		return nil, 0, err
	}
	return docs, total, nil
}

// Count returns the number of documents excluding soft-deleted ones.
func (m *MongoStore) Count(ctx context.Context) (int64, error) {
	col, err := m.collectionOrFail()
	if err != nil {
		return 0, err
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	total, err := m.Store.ExecuteCountDocuments(ctx, col, bson.M{"deleted_at": nil})
	if err != nil {
		return 0, err
	}
	if ctx.Err() != nil {
		return 0, ctx.Err()
	}
	if total < 0 {
		return 0, catalogue.ErrUnavailable
	}
	return total, nil
}

// escapeRegex escapes regular-expression metacharacters so user input can be
// safely used as a literal in prefix searches.
func escapeRegex(s string) string {
	// Conservative escape of regex metacharacters for prefix search.
	replacer := strings.NewReplacer(
		`\`, `\\`, `^`, `\^`, `$`, `\$`, `.`, `\.`, `|`, `\|`,
		`?`, `\?`, `*`, `\*`, `+`, `\+`, `(`, `\(`, `)`, `\)`,
		`[`, `\[`, `]`, `\]`, `{`, `\{`, `}`, `\}`,
	)
	return replacer.Replace(s)
}

// Compile-time wiring check against the production GHATD helper.
var _ MongoDbStore = (*repository.MongoDbRepository)(nil)

// MemoryStore is an in-memory Document repository with the same revision
// semantics as MongoStore, for isolated tests. It is safe for concurrent
// use. Returned documents are deep copies, matching MongoDB isolation.
type MemoryStore struct {
	mu      sync.Mutex
	byKey   map[string]Document
	byID    map[string]string // id -> key
	closed  bool
	nowFunc func() time.Time
}

// NewMemoryStore returns an empty in-memory store using the supplied clock
// (defaults to a zero-ish deterministic clock for stable tests).
func NewMemoryStore(now func() time.Time) *MemoryStore {
	if now == nil {
		now = func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }
	}
	return &MemoryStore{byKey: map[string]Document{}, byID: map[string]string{}, nowFunc: now}
}

// Close marks the store unusable.
func (m *MemoryStore) Close() {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
}

// InsertIfAbsent seeds one document; existing entries are returned untouched.
func (m *MemoryStore) InsertIfAbsent(ctx context.Context, doc Document) (Document, bool, error) {
	if err := ctx.Err(); err != nil {
		return Document{}, false, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return Document{}, false, ErrClosed
	}
	if existing, ok := m.byKey[doc.Key]; ok {
		copy, err := cloneDocument(existing)
		return copy, false, err
	}
	if _, exists := m.byID[doc.ID]; exists {
		return Document{}, false, catalogue.ErrAlreadyExists
	}
	stored, err := cloneDocument(doc)
	if err != nil {
		return Document{}, false, err
	}
	m.byKey[doc.Key] = stored
	m.byID[doc.ID] = doc.Key
	return doc, true, nil
}

// GetByKey returns the document for key or ErrNotFound.
func (m *MemoryStore) GetByKey(ctx context.Context, key string) (Document, error) {
	if err := ctx.Err(); err != nil {
		return Document{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return Document{}, ErrClosed
	}
	doc, ok := m.byKey[key]
	if !ok {
		return Document{}, catalogue.ErrNotFound
	}
	return cloneDocument(doc)
}

// GetByID returns the document for id or ErrNotFound.
func (m *MemoryStore) GetByID(ctx context.Context, id string) (Document, error) {
	if err := ctx.Err(); err != nil {
		return Document{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return Document{}, ErrClosed
	}
	key, ok := m.byID[id]
	if !ok {
		return Document{}, catalogue.ErrNotFound
	}
	return cloneDocument(m.byKey[key])
}

// ReplaceWithRevision performs a compare-and-swap replace.
func (m *MemoryStore) ReplaceWithRevision(ctx context.Context, doc Document, expectedRevision int) (Document, error) {
	if err := ctx.Err(); err != nil {
		return Document{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return Document{}, ErrClosed
	}
	key, ok := m.byID[doc.ID]
	if !ok {
		return Document{}, catalogue.ErrNotFound
	}
	current := m.byKey[key]
	if current.Revision != expectedRevision {
		return Document{}, catalogue.ErrStaleWrite
	}
	if doc.Key != key || doc.Revision != expectedRevision+1 {
		return Document{}, catalogue.ErrInvalidPayload
	}
	stored, err := cloneDocument(doc)
	if err != nil {
		return Document{}, err
	}
	m.byKey[key] = stored
	return cloneDocument(stored)
}

// List returns a bounded ordered page plus total.
func (m *MemoryStore) List(ctx context.Context, q catalogue.ListQuery, publicSelectableOnly bool) ([]Document, int64, error) {
	if err := ctx.Err(); err != nil {
		return nil, 0, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil, 0, ErrClosed
	}
	q = q.Sanitised()
	var docs []Document
	for _, doc := range m.byKey {
		if !q.IncludeDeleted && doc.DeletedAt != nil {
			continue
		}
		if publicSelectableOnly {
			if !doc.Enabled || doc.Hidden || doc.DeletedAt != nil {
				continue
			}
		} else {
			if !q.IncludeHidden && doc.Hidden {
				continue
			}
			if !q.IncludeDisabled && !doc.Enabled {
				continue
			}
		}
		if q.Search != "" && !strings.HasPrefix(strings.ToLower(doc.Key), strings.ToLower(q.Search)) {
			continue
		}
		copy, err := cloneDocument(doc)
		if err != nil {
			return nil, 0, err
		}
		docs = append(docs, copy)
	}
	sort.Slice(docs, func(i, j int) bool { return docs[i].Key < docs[j].Key })
	total := int64(len(docs))
	start := q.Offset()
	if start >= len(docs) {
		return []Document{}, total, nil
	}
	end := start + q.PageSize
	if end > len(docs) {
		end = len(docs)
	}
	return docs[start:end], total, nil
}

// Count returns documents excluding soft-deleted ones.
func (m *MemoryStore) Count(ctx context.Context) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return 0, ErrClosed
	}
	var total int64
	for _, doc := range m.byKey {
		if doc.DeletedAt == nil {
			total++
		}
	}
	return total, nil
}

// cloneDocument deep-copies a document via a BSON round-trip, matching
// MongoDB's document isolation for returned copies.
func cloneDocument(doc Document) (Document, error) {
	encoded, err := bson.Marshal(doc)
	if err != nil {
		return Document{}, err
	}
	var clone Document
	err = bson.Unmarshal(encoded, &clone)
	return clone, err
}
