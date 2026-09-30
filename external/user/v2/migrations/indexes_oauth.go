package migrations

import (
	"context"
	"errors"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// InitUsersOAuthIndexesUp enforces email and stable identity uniqueness before enabling OAuth.
func InitUsersOAuthIndexesUp(ctx context.Context, db *mongo.Database) error {
	_, err := db.Collection("users").Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "email", Value: 1}}, Options: options.Index().SetName("idx_users_email").SetUnique(true)},
		{Keys: bson.D{{Key: "oauth_identity_keys", Value: 1}}, Options: options.Index().SetName("idx_users_oauth_identity").SetUnique(true).SetSparse(true)},
	})
	if err != nil {
		return errors.New("cannot create OAuth account indexes: resolve existing duplicate emails or identities before enabling providers")
	}
	return nil
}

// InitUsersOAuthIndexesDown removes the provider index while preserving email uniqueness.
func InitUsersOAuthIndexesDown(ctx context.Context, db *mongo.Database) error {
	return db.Collection("users").Indexes().DropOne(ctx, "idx_users_oauth_identity")
}
