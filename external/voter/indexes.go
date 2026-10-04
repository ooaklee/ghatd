package voter

import (
	"context"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// EnsureIndexes creates the natural-key uniqueness and target-query index as
// an explicit migration. It never drops legacy data, repairs conflicting indexes
// or runs during a vote request. The managed database remains caller-owned.
func EnsureIndexes(ctx context.Context, db *mongo.Database) error {
	if ctx == nil || db == nil {
		return ErrInvalidRequest
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if mongo.SessionFromContext(ctx) != nil {
		return ErrInvalidRequest
	}
	_, err := db.Collection(Collection).Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "scope", Value: 1}, {Key: "domain", Value: 1}, {Key: "resource_id", Value: 1}, {Key: "child_id", Value: 1}, {Key: "actor_id", Value: 1}},
		Options: options.Index().SetName("idx_votes_target_actor").SetUnique(true),
	})
	return err
}
