package apitoken

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"unicode"
	"unicode/utf8"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/mongo/readconcern"
	"go.mongodb.org/mongo-driver/v2/mongo/readpref"
	"go.mongodb.org/mongo-driver/v2/mongo/writeconcern"
)

// InventoryFenceCollection is the owner-wide admission lock collection for
// ApiTokenCollection in the same database. No system identifier or TTL belongs
// here: every system that counts the same owner's tokens must share this lock.
const InventoryFenceCollection = ApiTokenCollection + "_inventory_fences"

// inventorySetup is required only when enabling transactional token admission.
// It uses the shared repository's lifecycle and privacy-safe setup primitives.
type inventorySetup interface {
	// EnsureMongoCollection prepares the named collection within the supplied
	// database using the shared repository's privacy-safe setup primitives.
	EnsureMongoCollection(context.Context, *mongo.Database, string) error
	// ProbeMongoTransactions verifies that the supplied collection's deployment
	// supports the transactions required for transactional token admission.
	ProbeMongoTransactions(context.Context, *mongo.Collection) error
}

// inventoryFence persists a stable owner identity plus a changing write marker.
// Preparation never grants permission or creates a credential.
type inventoryFence struct {
	// ID hashes the exact owner ID to a bounded Mongo primary key.
	ID string `bson:"_id"`
	// OwnerID detects corrupt or incorrectly prepared lock records.
	OwnerID string `bson:"owner_id"`
	// Fence changes on admission to force a real transaction write conflict.
	Fence string `bson:"fence"`
}

// InitializeTokenInventory pre-creates token/fence collections and probes real
// transaction support. Call explicitly during startup with a bounded context.
// It creates no owner locks or grants and never owns/disconnects the shared client.
// Repositories that cannot supply the setup capability cannot enable admission.
func (r *Repository) InitializeTokenInventory(ctx context.Context) error {
	if ctx == nil || r == nil || r.Store == nil || mongo.SessionFromContext(ctx) != nil {
		return ErrInventoryUnavailable
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	setup, ok := r.Store.(inventorySetup)
	if !ok {
		return ErrInventoryUnavailable
	}
	tokens, err := r.GetApiTokenCollection(ctx)
	if err != nil {
		return err
	}
	for _, name := range []string{ApiTokenCollection, InventoryFenceCollection} {
		if err := setup.EnsureMongoCollection(ctx, tokens.Database(), name); err != nil {
			return err
		}
	}
	if err := setup.ProbeMongoTransactions(ctx, inventoryCollection(tokens.Database())); err != nil {
		return err
	}
	r.inventoryReady.Store(true)
	return nil
}

// PrepareTokenInventory idempotently prepares one owner lock outside a
// transaction. Use during explicit migration/provisioning before enabling token
// creation for that owner. Repeating this step is safe even after an uncertain
// outcome: it uses insert-only fields and never rotates a credential or resets
// an existing fence. The trusted caller must verify the owner exists.
// Preparation is deliberately separate from admission; a missing lock fails
// closed rather than upserting during a count-and-insert transaction.
func (r *Repository) PrepareTokenInventory(ctx context.Context, ownerID string) error {
	if err := r.checkInventoryContext(ctx, ownerID, false); err != nil {
		return err
	}
	tokens, err := r.GetApiTokenCollection(ctx)
	if err != nil {
		return err
	}
	collection := inventoryCollection(tokens.Database())
	id := inventoryOwnerID(ownerID)
	_, err = r.Store.ExecuteUpdateOneCommandResult(ctx, collection, bson.M{"_id": id},
		bson.M{"$setOnInsert": bson.M{"owner_id": ownerID, "fence": rand.Text()}}, options.UpdateOne().SetUpsert(true))
	// Concurrent preparation may report a duplicate _id. Only accept it after
	// reading back the exact expected identity; never swallow other failures.
	if err != nil && !mongo.IsDuplicateKeyError(err) {
		return err
	}
	var record inventoryFence
	if err := r.Store.ExecuteFindOneCommandDecodeResult(ctx, collection, bson.M{"_id": id}, &record, "ApiTokenInventory", true, nil); err != nil {
		return err
	}
	if record.ID != id || record.OwnerID != ownerID || record.Fence == "" {
		return ErrInventoryUnavailable
	}
	return nil
}

// FenceInventory serializes admissions for this owner across all systems.
// It must run before counting in an active transaction on the repository's own
// client. Missing/corrupt locks deny admission. No implicit lock creation,
// transaction nesting, commit, retry loop or session detachment occurs here.
func (r *Repository) FenceInventory(ctx context.Context, ownerID string) error {
	if err := r.checkInventoryContext(ctx, ownerID, true); err != nil {
		return err
	}
	tokens, err := r.GetApiTokenCollection(ctx)
	if err != nil {
		return err
	}
	if mongo.SessionFromContext(ctx).Client() != tokens.Database().Client() {
		return ErrInventoryUnavailable
	}
	result, err := r.Store.ExecuteUpdateOneCommandResult(ctx, inventoryCollection(tokens.Database()),
		bson.M{"_id": inventoryOwnerID(ownerID), "owner_id": ownerID, "fence": bson.M{"$type": "string", "$ne": ""}},
		bson.M{"$set": bson.M{"fence": rand.Text()}})
	if err != nil {
		return err
	}
	if result == nil || result.MatchedCount != 1 {
		return ErrInventoryUnavailable
	}
	return nil
}

// checkInventoryContext rejects unsupported lifecycle or transaction states
// before touching storage. Owner IDs are opaque, bounded, exact UTF-8 values.
func (r *Repository) checkInventoryContext(ctx context.Context, ownerID string, transaction bool) error {
	if ctx == nil || r == nil || r.Store == nil || !r.inventoryReady.Load() || ownerID == "" || len(ownerID) > 256 || !utf8.ValidString(ownerID) || strings.IndexFunc(ownerID, func(c rune) bool { return unicode.IsSpace(c) || unicode.IsControl(c) }) >= 0 {
		return ErrInventoryUnavailable
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	session := mongo.SessionFromContext(ctx)
	if transaction && (session == nil || !session.TransactionRunning()) {
		return ErrInventoryUnavailable
	}
	if !transaction && session != nil {
		return ErrInventoryUnavailable
	}
	return nil
}

// inventoryOwnerID has no system component because token counts have none.
func inventoryOwnerID(ownerID string) string {
	digest := sha256.Sum256([]byte(ownerID))
	return hex.EncodeToString(digest[:])
}

// inventoryCollection keeps preparation durable and its identity readback live,
// even when a host's ordinary query defaults use secondary reads or weak writes.
// The enclosing policy transaction supplies equivalent transaction concerns.
func inventoryCollection(db *mongo.Database) *mongo.Collection {
	return db.Collection(InventoryFenceCollection, options.Collection().SetReadConcern(readconcern.Majority()).SetReadPreference(readpref.Primary()).SetWriteConcern(writeconcern.Majority()))
}
