package contacter

import (
	"context"
	"time"

	"github.com/ooaklee/ghatd/external/repository"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// FindCommsEntries resolves at most 100 immutable entries within one contact.
// Missing/cross-contact IDs fail the entire read. Only identities and authors
// are projected for private vote views, never message contents.
func (r *Repository) FindCommsEntries(ctx context.Context, commsID string, ids []string) ([]CommsEntry, error) {
	if !boundedIdentifier(commsID, 128) || len(ids) == 0 || len(ids) > 100 {
		return nil, ErrCommsEntryInvalid
	}
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		if !validEntryID(id) || seen[id] {
			return nil, ErrCommsEntryInvalid
		}
		seen[id] = true
	}
	c, _, err := r.conversationCollection(ctx)
	if err != nil {
		return nil, err
	}
	cur, err := r.Store.ExecuteFindCommand(ctx, c, bson.M{"comms_id": commsID, "_id": bson.M{"$in": append([]string(nil), ids...)}}, options.Find().SetProjection(bson.M{"_id": 1, "comms_id": 1, "actor_id": 1}).SetLimit(int64(len(ids))))
	if err != nil {
		return nil, err
	}
	if cur == nil {
		return nil, ErrCommsConversationUnavailable
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
		defer cancel()
		_ = cur.Close(cleanup)
	}()
	var entries []CommsEntry
	if err := cur.All(ctx, &entries); err != nil {
		return nil, err
	}
	if len(entries) != len(ids) {
		return nil, ErrCommsNotFound
	}
	for _, entry := range entries {
		if !seen[entry.ID] || entry.CommsID != commsID {
			return nil, ErrCommsConversationUnavailable
		}
		delete(seen, entry.ID)
	}
	return entries, nil
}

// CommsEntriesCollection stores one immutable document per note/reply/email.
// MongoDB's built-in unique _id index enforces both deduplication namespaces.
const CommsEntriesCollection = "comms_entries"

// conversationMongoReader is the additional shared-helper capability required
// by conversation storage. Older stores continue to satisfy MongoDbStore.
type conversationMongoReader interface {
	// ExecuteFindOneCommandDecodeResult is the shared-helper single-document
	// find-and-decode capability required by conversation storage, mirroring
	// MongoDbStore semantics for reading one entry.
	ExecuteFindOneCommandDecodeResult(context.Context, *mongo.Collection, interface{}, interface{}, string, bool, error) error
}

// conversationCollection obtains storage without creating indexes on read paths.
func (r *Repository) conversationCollection(ctx context.Context) (*mongo.Collection, conversationMongoReader, error) {
	if ctx == nil || r == nil || nilConversationPort(r.Store) {
		return nil, nil, ErrCommsConversationUnavailable
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	p, ok := r.Store.(conversationMongoReader)
	if !ok || nilConversationPort(p) {
		return nil, nil, ErrCommsConversationUnavailable
	}
	root, err := r.GetCommsCollection(ctx)
	if err != nil {
		return nil, nil, err
	}
	if root == nil {
		return nil, nil, ErrCommsConversationUnavailable
	}
	return root.Database().Collection(CommsEntriesCollection), p, nil
}

// FindCommsEntry scopes parent resolution to the selected contact. Native driver
// failures retain their identity; only an absent entry becomes domain not-found.
func (r *Repository) FindCommsEntry(ctx context.Context, commsID, id string) (*CommsEntry, error) {
	if !boundedIdentifier(commsID, 128) || !validEntryID(id) {
		return nil, ErrCommsEntryInvalid
	}
	c, p, err := r.conversationCollection(ctx)
	if err != nil {
		return nil, err
	}
	var result CommsEntry
	err = p.ExecuteFindOneCommandDecodeResult(ctx, c, bson.M{"_id": id, "comms_id": commsID}, &result, "comms-entry", false, ErrCommsEntryNotFound)
	if err != nil {
		return nil, err
	}
	return &result, nil
}

// InsertCommsEntry inserts once and reconciles an exact duplicate identity. It
// never retries uncertain writes or overwrites prior content. The one duplicate
// branch serves both administrator keys and trusted provider message identities.
func (r *Repository) InsertCommsEntry(ctx context.Context, v *CommsEntry) (*CommsEntry, bool, error) {
	if err := validateCommsEntry(v); err != nil {
		return nil, false, err
	}
	c, p, err := r.conversationCollection(ctx)
	if err != nil {
		return nil, false, err
	}
	owned := copyCommsEntry(v)
	receipt, err := r.Store.ExecuteInsertOneCommand(ctx, c, copyCommsEntry(owned), "comms-entry")
	if err == nil {
		if receipt == nil || !receipt.Acknowledged || receipt.InsertedID != owned.ID {
			return nil, false, repository.ErrUnacknowledgedMongoWrite
		}
		return owned, false, nil
	}
	// A mixed, wrapped, write-concern or network failure must remain native.
	w, ok := err.(mongo.WriteException)
	if !ok || w.WriteConcernError != nil || len(w.Labels) > 0 || len(w.WriteErrors) != 1 || w.WriteErrors[0].Code != 11000 {
		return nil, false, err
	}
	var existing CommsEntry
	readErr := p.ExecuteFindOneCommandDecodeResult(ctx, c, bson.M{"_id": owned.ID}, &existing, "comms-entry", false, ErrCommsEntryNotFound)
	if readErr == ErrCommsEntryNotFound {
		return nil, false, err
	}
	if readErr != nil {
		return nil, false, readErr
	}
	if !sameCommsEntryContent(&existing, owned) {
		return nil, false, ErrCommsEntryConflict
	}
	return copyCommsEntry(&existing), true, nil
}

// QueryCommsEntries uses a total-order keyset, including digest tie-breaks at
// equal millisecond timestamps. Concurrent late inserts are not snapshot reads.
func (r *Repository) QueryCommsEntries(ctx context.Context, commsID string, after *CommsEntry, limit int) ([]CommsEntry, error) {
	if !boundedIdentifier(commsID, 128) || limit < 1 || limit > 100 {
		return nil, ErrCommsEntryInvalid
	}
	filter := bson.M{"comms_id": commsID}
	if after != nil {
		if after.CommsID != commsID || !validEntryID(after.ID) || after.RecordedAt.IsZero() {
			return nil, ErrCommsEntryInvalid
		}
		filter["$or"] = bson.A{bson.M{"recorded_at": bson.M{"$lt": after.RecordedAt}}, bson.M{"recorded_at": after.RecordedAt, "_id": bson.M{"$lt": after.ID}}}
	}
	c, _, err := r.conversationCollection(ctx)
	if err != nil {
		return nil, err
	}
	cur, err := r.Store.ExecuteFindCommand(ctx, c, filter, options.Find().SetSort(bson.D{{Key: "recorded_at", Value: -1}, {Key: "_id", Value: -1}}).SetLimit(int64(limit+1)))
	if err != nil {
		return nil, err
	}
	rows := []CommsEntry{}
	if err = r.Store.MapAllInCursorToResult(ctx, cur, &rows, "comms-entry"); err != nil {
		return nil, err
	}
	return rows, nil
}

// EnsureCommsConversationIndexes is an explicit host migration step, never run
// by ordinary reads/writes. This index improves paging; built-in _id uniqueness
// already enforces correctness. It neither rewrites nor migrates legacy values.
func EnsureCommsConversationIndexes(ctx context.Context, db *mongo.Database) error {
	if ctx == nil || db == nil {
		return ErrCommsConversationUnavailable
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	_, err := db.Collection(CommsEntriesCollection).Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "comms_id", Value: 1}, {Key: "recorded_at", Value: -1}, {Key: "_id", Value: -1}},
		Options: options.Index().SetName("idx_comms_entries_page"),
	})
	return err
}

var _ ConversationRepository = (*Repository)(nil)
