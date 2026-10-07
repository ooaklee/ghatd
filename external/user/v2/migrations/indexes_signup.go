package migrations

import (
	"context"

	"github.com/ooaklee/ghatd/external/repository"
	user "github.com/ooaklee/ghatd/external/user/v2"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// InitUsersSignupAttributionIndexesUp adds the bounded owning signup feed index.
// It never backfills existing accounts or deletes immutable creation evidence.
// Hosts register this additive migration before starting attribution workers.
func InitUsersSignupAttributionIndexesUp(ctx context.Context, db *mongo.Database) error {
	store, err := repository.NewMongoDbRepositoryFromDatabase(db, nil)
	if err != nil {
		return err
	}
	return store.EnsureMongoIndexes(ctx, db.Collection(user.UserCollection), []mongo.IndexModel{{
		Keys:    bson.D{{Key: "signup_attribution.program_id", Value: 1}, {Key: "signup_attribution.state", Value: 1}, {Key: "_id", Value: 1}},
		Options: options.Index().SetName("idx_users_signup_attribution_pending").SetPartialFilterExpression(bson.M{"signup_attribution.program_id": bson.M{"$gt": ""}}),
	}})
}
