package migrations

import (
	"context"
	"log"

	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/toolbox"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// InitBillingSubscriptionIndexesUp initialises indexes for the billing subscriptions collection.
func InitBillingSubscriptionIndexesUp(db *mongo.Database) error { //Up
	return InitBillingSubscriptionIndexesUpWithContext(context.Background(), db)
}

// InitBillingSubscriptionIndexesUpWithContext initialises billing subscription
// indexes using ctx for MongoDB operations.
func InitBillingSubscriptionIndexesUpWithContext(ctx context.Context, db *mongo.Database) error { //Up
	if ctx == nil {
		ctx = context.Background()
	}
	log.SetFlags(0)
	const mongoCollectionName = billing.BillingSubscriptionsCollection

	log.Default().Println(toolbox.OutputBasicLogString("info", "starting-task-to-billing-subscriptions-indexes"))

	// Index on user_id for efficient user lookups
	userIdIndexModel := mongo.IndexModel{
		Keys:    bson.D{{Key: "user_id", Value: 1}},
		Options: options.Index().SetName("idx_subscriptions_user_id"),
	}

	// Index on email for email-based queries
	emailIndexModel := mongo.IndexModel{
		Keys:    bson.D{{Key: "email", Value: 1}},
		Options: options.Index().SetName("idx_subscriptions_email"),
	}

	// Unique compound index on integrator and subscription ID
	integratorUniqueIndexModel := mongo.IndexModel{
		Keys: bson.D{
			{Key: "integrator", Value: 1},
			{Key: "integrator_subscription_id", Value: 1},
		},
		Options: options.Index().
			SetName("idx_subscriptions_integrator").
			SetUnique(true).
			SetPartialFilterExpression(bson.M{"integrator_subscription_id": bson.M{"$gt": ""}}),
	}

	// Index on created_at for sorting/filtering
	createdAtIndexModel := mongo.IndexModel{
		Keys:    bson.D{{Key: "created_at", Value: -1}},
		Options: options.Index().SetName("idx_subscriptions_created_at"),
	}

	// Create all indexes
	_, err := db.Collection(mongoCollectionName).Indexes().CreateMany(
		ctx,
		[]mongo.IndexModel{
			userIdIndexModel,
			emailIndexModel,
			integratorUniqueIndexModel,
			createdAtIndexModel,
		},
	)
	if err != nil {
		log.Default().Println(toolbox.OutputBasicLogString("error", "failed-task-to-billing-subscriptions-indexes"))
		return err
	}

	log.Default().Println(toolbox.OutputBasicLogString("info", "completed-task-to-billing-subscriptions-indexes"))
	return nil

}

// InitBillingSubscriptionIndexesDown rolls back the billing subscriptions indexes
func InitBillingSubscriptionIndexesDown(db *mongo.Database) error { //Down
	return InitBillingSubscriptionIndexesDownWithContext(context.Background(), db)
}

// InitBillingSubscriptionIndexesDownWithContext rolls back billing subscription
// indexes using ctx for MongoDB operations.
func InitBillingSubscriptionIndexesDownWithContext(ctx context.Context, db *mongo.Database) error { //Down
	if ctx == nil {
		ctx = context.Background()
	}
	log.SetFlags(0)
	const mongoCollectionName = billing.BillingSubscriptionsCollection

	log.Default().Println(toolbox.OutputBasicLogString("info", "rolling-back-task-to-billing-subscriptions-indexes"))

	// Drop all indexes by name
	indexNames := []string{
		"idx_subscriptions_user_id",
		"idx_subscriptions_email",
		"idx_subscriptions_integrator",
		"idx_subscriptions_created_at",
	}

	for _, indexName := range indexNames {
		err := db.Collection(mongoCollectionName).Indexes().DropOne(ctx, indexName)
		if err != nil {
			log.Default().Println(toolbox.OutputBasicLogString("error", "failed-rolling-back-index: "+indexName))
			return err
		}
	}

	log.Default().Println(toolbox.OutputBasicLogString("info", "completed-rolling-back-task-to-billing-subscriptions-indexes"))
	return nil
}
