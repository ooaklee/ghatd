// Package recordstore provides encrypted, revisioned records and guarded
// transactions using GHATD's caller-owned Mongo repository. It owns persistence
// mechanics only; domain services supply identities, schemas and business rules.
package recordstore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/ooaklee/ghatd/external/encryption"
	"github.com/ooaklee/ghatd/external/repository"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

var (
	ErrNotFound    = errors.New("recordstore/not-found")
	ErrConflict    = errors.New("recordstore/conflict")
	ErrInvalid     = errors.New("recordstore/invalid")
	ErrUnavailable = errors.New("recordstore/unavailable")
	ErrUncertain   = errors.New("recordstore/uncertain")
)

// Record keeps indexed non-secret references separate from an encrypted typed
// payload. Hosts must not put emails, provider references or raw evidence in
// indexed Kind, ID, Partition or State fields.
type Record struct {
	ID        string
	Kind      string
	Partition string
	Revision  int64
	Sequence  int64
	State     string
	Data      json.RawMessage
	// ExpiresAt is opt-in ephemeral-record lifetime metadata. Nil records never
	// expire through this store. Domain adapters set it only for approved raw
	// analytics/session data, never financial journals, ownership or receipts.
	ExpiresAt *time.Time
}

func NewRecord(kind, id, partition string, revision int64, payload any) (Record, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return Record{}, err
	}
	r := Record{ID: id, Kind: kind, Partition: partition, Revision: revision, Data: body}
	return r, validate(r)
}
func (r Record) Decode(out any) error { return json.Unmarshal(r.Data, out) }
func validate(r Record) error {
	if r.ExpiresAt != nil && r.ExpiresAt.IsZero() {
		return ErrInvalid
	}
	if strings.TrimSpace(r.ID) == "" || len(r.ID) > 256 || strings.TrimSpace(r.Kind) == "" || len(r.Kind) > 64 || strings.TrimSpace(r.Partition) == "" || len(r.Partition) > 256 || r.Revision < 1 || r.Sequence < 0 || len(r.State) > 256 || len(r.Data) > 1<<20 || !json.Valid(r.Data) {
		return ErrInvalid
	}
	return nil
}

// WithExpiration returns a copy with an explicit absolute UTC expiration. It
// rounds UP to BSON millisecond precision, so storage cannot shorten the
// domain-selected lifetime. Expiry is background cleanup, not read authority.
func (r Record) WithExpiration(at time.Time) (Record, error) {
	if at.IsZero() {
		return Record{}, ErrInvalid
	}
	at = at.UTC()
	rounded := at.Truncate(time.Millisecond)
	if rounded.Before(at) {
		rounded = rounded.Add(time.Millisecond)
	}
	r.ExpiresAt = &rounded
	return r, validate(r)
}

type Query struct {
	Kind, Partition, State, AfterID string
	Limit                           int
}
type Tx interface {
	Get(context.Context, string, string) (Record, error)
	Find(context.Context, Query) ([]Record, error)
	Insert(context.Context, Record) error
	Replace(context.Context, Record, int64) error
}

// Store callbacks use only their bound Tx and perform no external effects.
// Financial callers provide the same guard identity for all competing commands.
type Store interface {
	Transact(context.Context, string, func(Tx) error) error
	Read(context.Context, func(Tx) error) error
}

// FindAll reads complete history through bounded pages. It does not silently
// truncate a ledger; domains must use indexed projections if history grows
// beyond their chosen operational budget rather than return a partial balance.
func FindAll(ctx context.Context, tx Tx, q Query) ([]Record, error) {
	if ctx == nil || nilDependency(tx) {
		return nil, ErrInvalid
	}
	q.Limit = 200
	q.AfterID = ""
	out := []Record{}
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		page, err := tx.Find(ctx, q)
		if err != nil {
			return nil, err
		}
		out = append(out, page...)
		if len(page) < q.Limit {
			return out, nil
		}
		next := page[len(page)-1].ID
		if next <= q.AfterID {
			return nil, ErrUnavailable
		}
		q.AfterID = next
	}
}

// MongoPort reuses managed helpers, native result receipts and transaction
// lifecycle. The original database/client owner remains responsible for close.
type MongoPort interface {
	EnsureMongoIndexes(context.Context, *mongo.Collection, []mongo.IndexModel) error
	EnsureMongoCollection(context.Context, *mongo.Database, string) error
	WithMongoTransaction(context.Context, *mongo.Database, func(context.Context) error) error
	ProbeMongoTransactions(context.Context, *mongo.Collection) error
	ExecuteFindOneCommandDecodeResult(context.Context, *mongo.Collection, any, any, string, bool, error) error
	ExecuteFindCommand(context.Context, *mongo.Collection, any, ...options.Lister[options.FindOptions]) (*mongo.Cursor, error)
	ExecuteInsertOneCommand(context.Context, *mongo.Collection, any, string) (*mongo.InsertOneResult, error)
	ExecuteUpdateOneCommandResult(context.Context, *mongo.Collection, any, any, ...options.Lister[options.UpdateOneOptions]) (*mongo.UpdateResult, error)
	ExecuteReplaceOneCommandResult(context.Context, *mongo.Collection, any, any, ...options.Lister[options.ReplaceOptions]) (*mongo.UpdateResult, error)
}
type MongoStore struct {
	db              *mongo.Database
	port            MongoPort
	cipher          *encryption.PayloadCipher
	records, guards *mongo.Collection
}

// NewMongoStore binds a pre-existing caller-owned database and explicit cipher.
// Call EnsureIndexes then Probe before admitting financial commands. No fallback
// to nontransactional storage or generated encryption keys exists.
func NewMongoStore(db *mongo.Database, port MongoPort, cipher *encryption.PayloadCipher) (*MongoStore, error) {
	if db == nil || nilDependency(port) || cipher == nil {
		return nil, ErrUnavailable
	}
	return &MongoStore{db: db, port: port, cipher: cipher, records: db.Collection("ghatd_owned_records"), guards: db.Collection("ghatd_record_guards")}, nil
}

// NewMongoStoreFromDatabase borrows the existing host pool through shared helpers.
func NewMongoStoreFromDatabase(db *mongo.Database, cipher *encryption.PayloadCipher) (*MongoStore, error) {
	port, err := repository.NewMongoDbRepositoryFromDatabase(db, nil)
	if err != nil {
		return nil, err
	}
	return NewMongoStore(db, port, cipher)
}

// EnsureIndexes is additive. Duplicate history fails index creation; operators
// must reconcile it explicitly rather than deleting financial records.
func (s *MongoStore) EnsureIndexes(ctx context.Context) error {
	if err := s.ready(ctx); err != nil {
		return err
	}
	err := s.port.EnsureMongoIndexes(ctx, s.records, []mongo.IndexModel{
		{Keys: bson.D{{Key: "expires_at", Value: 1}}, Options: options.Index().SetName("owned_record_expiration").SetExpireAfterSeconds(0).SetPartialFilterExpression(bson.M{"expires_at": bson.M{"$type": "date"}})},
		{Keys: bson.D{{Key: "kind", Value: 1}, {Key: "id", Value: 1}}, Options: options.Index().SetUnique(true).SetName("owned_record_identity")},
		{Keys: bson.D{{Key: "kind", Value: 1}, {Key: "partition", Value: 1}, {Key: "id", Value: 1}}, Options: options.Index().SetName("owned_record_partition")},
		{Keys: bson.D{{Key: "kind", Value: 1}, {Key: "partition", Value: 1}, {Key: "state", Value: 1}, {Key: "id", Value: 1}}, Options: options.Index().SetName("owned_record_state")},
		{Keys: bson.D{{Key: "kind", Value: 1}, {Key: "partition", Value: 1}, {Key: "sequence", Value: 1}}, Options: options.Index().SetUnique(true).SetPartialFilterExpression(bson.M{"sequence": bson.M{"$gt": int64(0)}}).SetName("owned_record_sequence")},
	})
	if err != nil {
		return err
	}
	return s.port.EnsureMongoCollection(ctx, s.db, s.guards.Name())
}

// Probe verifies an actual snapshot/majority read-write-commit round trip.
func (s *MongoStore) Probe(ctx context.Context) error {
	if err := s.ready(ctx); err != nil {
		return err
	}
	return s.port.ProbeMongoTransactions(ctx, s.guards)
}
func recordID(kind, id string) string {
	body, _ := json.Marshal([]string{kind, id})
	return guardID(string(body))
}
func guardID(key string) string { sum := sha256.Sum256([]byte(key)); return hex.EncodeToString(sum[:]) }
func (s *MongoStore) Transact(ctx context.Context, key string, fn func(Tx) error) error {
	if key == "" || len(key) > 512 || fn == nil {
		return ErrInvalid
	}
	if err := s.ready(ctx); err != nil {
		return err
	}
	id := guardID(key)
	// The durable guard is created before any business callback. An upsert race
	// can be read back safely because no financial work has begun.
	result, err := s.port.ExecuteUpdateOneCommandResult(ctx, s.guards, bson.M{"_id": id}, bson.M{"$setOnInsert": bson.M{"revision": int64(0)}}, options.UpdateOne().SetUpsert(true))
	if mongo.IsDuplicateKeyError(err) {
		var found bson.M
		err = s.port.ExecuteFindOneCommandDecodeResult(ctx, s.guards, bson.M{"_id": id}, &found, "record-guard", false, nil)
	} else if err == nil && (result == nil || !result.Acknowledged || (result.MatchedCount != 1 && result.UpsertedCount != 1)) {
		return ErrUnavailable
	}
	if err != nil {
		return err
	}
	lastCallbackSucceeded := false
	err = s.port.WithMongoTransaction(ctx, s.db, func(txCtx context.Context) error {
		lastCallbackSucceeded = false
		result, err := s.port.ExecuteUpdateOneCommandResult(txCtx, s.guards, bson.M{"_id": id}, bson.M{"$inc": bson.M{"revision": 1}})
		if err != nil {
			return err
		}
		if result == nil || !result.Acknowledged || result.MatchedCount != 1 || result.ModifiedCount != 1 {
			return ErrUnavailable
		}
		if err := fn(&mongoTx{store: s, ctx: txCtx}); err != nil {
			return err
		}
		lastCallbackSucceeded = true
		return nil
	})
	if err != nil {
		var server mongo.ServerError
		if lastCallbackSucceeded || (errors.As(err, &server) && server.HasErrorLabel("UnknownTransactionCommitResult")) {
			return fmt.Errorf("%w: %w", ErrUncertain, err)
		}
	}
	return err
}
func (s *MongoStore) Read(ctx context.Context, fn func(Tx) error) error {
	if fn == nil {
		return ErrInvalid
	}
	if err := s.ready(ctx); err != nil {
		return err
	}
	return s.port.WithMongoTransaction(ctx, s.db, func(txCtx context.Context) error { return fn(&mongoTx{store: s, ctx: txCtx, readOnly: true}) })
}

func nilDependency(v any) bool {
	if v == nil {
		return true
	}
	r := reflect.ValueOf(v)
	switch r.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return r.IsNil()
	}
	return false
}
func (s *MongoStore) ready(ctx context.Context) error {
	if ctx == nil {
		return ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if s == nil || s.db == nil || nilDependency(s.port) || s.cipher == nil {
		return ErrUnavailable
	}
	return nil
}

type storedRecord struct {
	Key       string     `bson:"_id"`
	ID        string     `bson:"id"`
	Kind      string     `bson:"kind"`
	Partition string     `bson:"partition"`
	Revision  int64      `bson:"revision"`
	Sequence  int64      `bson:"sequence,omitempty"`
	State     string     `bson:"state,omitempty"`
	Payload   []byte     `bson:"payload"`
	ExpiresAt *time.Time `bson:"expires_at,omitempty"`
}

func aad(r storedRecord) []byte {
	fields := []any{"ghatd-owned-record-v1", r.Kind, r.ID, r.Partition, r.Revision, r.Sequence, r.State}
	if r.ExpiresAt != nil {
		fields = append(fields, r.ExpiresAt.UTC().Format(time.RFC3339Nano))
	}
	body, _ := json.Marshal(fields)
	return body
}
func (s *MongoStore) encode(r Record) (storedRecord, error) {
	if err := validate(r); err != nil {
		return storedRecord{}, err
	}
	if r.ExpiresAt != nil {
		var err error
		r, err = r.WithExpiration(*r.ExpiresAt)
		if err != nil {
			return storedRecord{}, err
		}
	}
	v := storedRecord{Key: recordID(r.Kind, r.ID), ID: r.ID, Kind: r.Kind, Partition: r.Partition, Revision: r.Revision, Sequence: r.Sequence, State: r.State, ExpiresAt: r.ExpiresAt}
	sealed, err := s.cipher.Seal(r.Data, aad(v))
	v.Payload = sealed
	return v, err
}
func (s *MongoStore) decode(v storedRecord) (Record, error) {
	if v.Key != recordID(v.Kind, v.ID) {
		return Record{}, ErrUnavailable
	}
	plain, err := s.cipher.Open(v.Payload, aad(v))
	if err != nil {
		return Record{}, err
	}
	r := Record{ID: v.ID, Kind: v.Kind, Partition: v.Partition, Revision: v.Revision, Sequence: v.Sequence, State: v.State, Data: plain, ExpiresAt: v.ExpiresAt}
	return r, validate(r)
}

// absent accepts only exact native absence, optionally single-cause wrapped.
func absent(err error) bool {
	for depth := 0; err != nil && depth < 32; depth++ {
		if err == mongo.ErrNoDocuments {
			return true
		}
		single, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = single.Unwrap()
	}
	return false
}

type mongoTx struct {
	store    *MongoStore
	ctx      context.Context
	readOnly bool
}

func (t *mongoTx) Get(_ context.Context, kind, id string) (Record, error) {
	var v storedRecord
	err := t.store.port.ExecuteFindOneCommandDecodeResult(t.ctx, t.store.records, bson.M{"kind": kind, "id": id}, &v, "owned-record", false, nil)
	if absent(err) {
		return Record{}, ErrNotFound
	}
	if err != nil {
		return Record{}, err
	}
	if v.Kind != kind || v.ID != id {
		return Record{}, ErrUnavailable
	}
	return t.store.decode(v)
}
func (t *mongoTx) Find(_ context.Context, q Query) ([]Record, error) {
	if q.Kind == "" || len(q.Kind) > 64 || len(q.Partition) > 256 || len(q.State) > 256 || len(q.AfterID) > 256 || q.Limit < 1 || q.Limit > 200 {
		return nil, ErrInvalid
	}
	filter := bson.M{"kind": q.Kind}
	if q.Partition != "" {
		filter["partition"] = q.Partition
	}
	if q.State != "" {
		filter["state"] = q.State
	}
	if q.AfterID != "" {
		filter["id"] = bson.M{"$gt": q.AfterID}
	}
	cursor, err := t.store.port.ExecuteFindCommand(t.ctx, t.store.records, filter, options.Find().SetSort(bson.D{{Key: "id", Value: 1}}).SetLimit(int64(q.Limit)))
	if err != nil {
		return nil, err
	}
	if cursor == nil {
		return nil, ErrUnavailable
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(t.ctx), 5*time.Second)
		defer cancel()
		_ = cursor.Close(cleanup)
	}()
	var rows []storedRecord
	if err := cursor.All(t.ctx, &rows); err != nil {
		return nil, err
	}
	out := make([]Record, 0, len(rows))
	for _, v := range rows {
		r, err := t.store.decode(v)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, nil
}
func (t *mongoTx) Insert(_ context.Context, r Record) error {
	if t.readOnly {
		return ErrInvalid
	}
	v, err := t.store.encode(r)
	if err != nil {
		return err
	}
	result, err := t.store.port.ExecuteInsertOneCommand(t.ctx, t.store.records, v, "owned-record")
	if mongo.IsDuplicateKeyError(err) {
		return ErrConflict
	}
	if err != nil {
		return err
	}
	if result == nil || !result.Acknowledged || result.InsertedID != v.Key {
		return ErrUnavailable
	}
	return nil
}
func (t *mongoTx) Replace(_ context.Context, r Record, expected int64) error {
	if t.readOnly || r.Revision != expected+1 {
		return ErrInvalid
	}
	v, err := t.store.encode(r)
	if err != nil {
		return err
	}
	result, err := t.store.port.ExecuteReplaceOneCommandResult(t.ctx, t.store.records, bson.M{"kind": r.Kind, "id": r.ID, "revision": expected}, v)
	if err != nil {
		return err
	}
	if result == nil || !result.Acknowledged || result.UpsertedCount != 0 || result.MatchedCount < 0 || result.MatchedCount > 1 || result.ModifiedCount < 0 || result.ModifiedCount > 1 {
		return ErrUnavailable
	}
	if result.MatchedCount == 0 {
		return ErrConflict
	}
	return nil
}
