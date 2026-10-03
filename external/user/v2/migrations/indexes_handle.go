package migrations

import (
	"context"

	"github.com/ooaklee/ghatd/external/repository"
	user "github.com/ooaklee/ghatd/external/user/v2"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// InitUsersHandleIndexesUp installs global nonempty-handle uniqueness without
// backfilling accounts or replacing conflicting indexes. Resolve duplicates and
// noncanonical legacy values before enabling handle writers. Returned driver
// errors may contain private values and must not be exposed in HTTP/log payloads.
func InitUsersHandleIndexesUp(ctx context.Context, db *mongo.Database) error {
	store, err := repository.NewMongoDbRepositoryFromDatabase(db, nil)
	if err != nil {
		return err
	}
	return store.EnsureMongoIndexes(ctx, db.Collection(user.UserCollection), []mongo.IndexModel{{
		Keys:    bson.D{{Key: "handle", Value: 1}},
		Options: options.Index().SetName(user.UserHandleIndexName).SetUnique(true).SetCollation(&options.Collation{Locale: "simple"}).SetPartialFilterExpression(bson.M{"handle": bson.M{"$gt": ""}}),
	}})
}
