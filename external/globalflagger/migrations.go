// Package-owned MongoDB migration for globalflagger.
//
// There is no global auto-registry and no derived timestamp: Migrate is a
// plain callable the host wraps in its own timestamped migration. It
// creates the collection indexes (cataloguestore.EnsureIndexes, identical
// to sibling catalogue packages) and then idempotently seeds the embedded
// flag catalogue through the real Service — seeds pass the strict
// sanitiser and never overwrite administrator edits.
package globalflagger

import (
	"context"
	"fmt"

	"github.com/ooaklee/ghatd/external/catalogue"
	"github.com/ooaklee/ghatd/external/cataloguestore"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

// Collection is owned here; index names match the shared cataloguestore
// conventions used by sibling packages.
const Collection = "i18n_flags"

// EnsureIndexes creates the package's collection indexes idempotently via
// the shared cataloguestore helper: a unique natural-key index on key and
// a compound selection index covering the public list query.
func EnsureIndexes(ctx context.Context, db *mongo.Database) error {
	return cataloguestore.EnsureIndexes(ctx, db, Collection)
}

// Migrate is the callable migration entry point for hosts: indexes first,
// then the seeding pass. It is insertion-only and idempotent; existing
// records (edited, disabled, hidden or soft-deleted) are preserved.
func Migrate(ctx context.Context, db *mongo.Database) error {
	if db == nil {
		return fmt.Errorf("%w: nil database", catalogue.ErrUnavailable)
	}
	if err := EnsureIndexes(ctx, db); err != nil {
		return err
	}
	repo, err := NewMongoRepositoryFromDatabase(db)
	if err != nil {
		return err
	}
	service := NewService(repo, catalogue.RealClock{})
	if _, err := service.Seed(ctx); err != nil {
		return err
	}
	return nil
}
