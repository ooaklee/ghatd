package repository

import (
	"context"
	"crypto/rand"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/mongo/readconcern"
	"go.mongodb.org/mongo-driver/v2/mongo/readpref"
	"go.mongodb.org/mongo-driver/v2/mongo/writeconcern"
)

// WithMongoTransaction executes a snapshot/majority/primary transaction using
// the supplied host database's client, never a newly connected client. Existing
// caller sessions are rejected rather than silently detached or nested.
//
// The callback may run repeatedly. All operations must use its context and the
// same client, propagate database errors, and perform no external side effects.
// Never manually end/commit/abort its session. Publish results only after nil is
// returned. An error does not prove that an uncertain commit never happened.
//
// Supply a bounded context for callback work. The driver owns retry and commit
// resolution, which can outlive cancellation; this is not a hard wall-time bound.
// Cancellation observed before commit rejects the callback result; cancellation
// during commit still requires the caller to reconcile an uncertain outcome.
// Session cleanup receives a detached five-second context, even on panic.
func (r *MongoDbRepository) WithMongoTransaction(ctx context.Context, db *mongo.Database, callback func(context.Context) error) error {
	if r == nil || r.helper == nil || ctx == nil || db == nil || callback == nil || mongo.SessionFromContext(ctx) != nil {
		return ErrInvalidMongoOperation
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	session, err := db.Client().StartSession()
	if err != nil {
		return err
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		session.EndSession(cleanup)
	}()
	_, err = session.WithTransaction(ctx, func(tx context.Context) (any, error) {
		if err := tx.Err(); err != nil {
			return nil, err
		}
		if err := callback(tx); err != nil {
			return nil, err
		}
		if !session.TransactionRunning() {
			return nil, ErrInvalidMongoOperation
		}
		return nil, tx.Err()
	}, options.Transaction().SetReadConcern(readconcern.Snapshot()).SetWriteConcern(writeconcern.Majority()).SetReadPreference(readpref.Primary()))
	observeMongo(ctx, r.helper, "transaction", err)
	return err
}

// ProbeMongoTransactions verifies actual transactional read/write/commit support
// on a precreated caller-owned collection, not merely a topology advertisement.
// The collection must permit an _id-only document; choose a suitable probe
// collection when domain validators or other unique indexes disallow it.
// A random probe is inserted, read and deleted atomically, leaving no committed
// document. No collections/indexes/grants are implicitly created. Call explicitly
// at startup with a bounded context; lack of support must not select a fallback.
func (r *MongoDbRepository) ProbeMongoTransactions(ctx context.Context, collection *mongo.Collection) error {
	if collection == nil {
		return ErrInvalidMongoOperation
	}
	return r.WithMongoTransaction(ctx, collection.Database(), func(tx context.Context) error {
		id := "ghatd-transaction-probe:" + rand.Text()
		if _, err := r.ExecuteInsertOneCommand(tx, collection, bson.M{"_id": id}, "transaction-probe"); err != nil {
			return err
		}
		var found bson.M
		if err := r.ExecuteFindOneCommandDecodeResult(tx, collection, bson.M{"_id": id}, &found, "transaction-probe", true, nil); err != nil {
			return err
		}
		result, err := r.ExecuteDeleteOneCommandResult(tx, collection, bson.M{"_id": id})
		if err != nil {
			return err
		}
		if result.DeletedCount != 1 {
			return ErrInvalidMongoOperation
		}
		return nil
	})
}

// closeMongoCursor releases server cursor resources even after request
// cancellation. It never closes or takes ownership of the host Mongo client.
func closeMongoCursor(ctx context.Context, cursor *mongo.Cursor) {
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	_ = cursor.Close(cleanup)
}
