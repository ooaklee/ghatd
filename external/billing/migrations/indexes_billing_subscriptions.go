package migrations

import (
	"context"
	"log"

	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/toolbox"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// InitSubscriptionIndexes initializes indexes for the billing subscriptions collection
func InitBillingSubscriptionIndexesUp(db *mongo.Database) error { //Up
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

	// Partial index on email for orphaned subscriptions (email without user_id)
	emailNoUserIndexModel := mongo.IndexModel{
		Keys: bson.D{{Key: "email", Value: 1}},
		Options: options.Index().
			SetName("idx_subscriptions_email_no_user").
			SetPartialFilterExpression(bson.M{
				"$or": []bson.M{
					{"user_id": ""},
					{"user_id": bson.M{"$exists": false}},
					{"user_id": nil},
				},
			}),
	}

	// Unique compound index on integrator and subscription ID
	integratorUniqueIndexModel := mongo.IndexModel{
		Keys: bson.D{
			{Key: "integrator", Value: 1},
			{Key: "integrator_subscription_id", Value: 1},
		},
		Options: options.Index().
			SetName("idx_subscriptions_integrator").
			SetUnique(true),
	}

	// Index on created_at for sorting/filtering
	createdAtIndexModel := mongo.IndexModel{
		Keys:    bson.D{{Key: "created_at", Value: -1}},
		Options: options.Index().SetName("idx_subscriptions_created_at"),
	}

	// Create all indexes
	_, err := db.Collection(mongoCollectionName).Indexes().CreateMany(
		context.Background(),
		[]mongo.IndexModel{
			userIdIndexModel,
			emailIndexModel,
			emailNoUserIndexModel,
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
	log.SetFlags(0)
	const mongoCollectionName = billing.BillingSubscriptionsCollection

	log.Default().Println(toolbox.OutputBasicLogString("info", "rolling-back-task-to-billing-subscriptions-indexes"))

	// Drop all indexes by name
	indexNames := []string{
		"idx_subscriptions_user_id",
		"idx_subscriptions_email",
		"idx_subscriptions_email_no_user",
		"idx_subscriptions_integrator",
		"idx_subscriptions_created_at",
	}

	for _, indexName := range indexNames {
		_, err := db.Collection(mongoCollectionName).Indexes().DropOne(context.TODO(), indexName)
		if err != nil {
			log.Default().Println(toolbox.OutputBasicLogString("error", "failed-rolling-back-index: "+indexName))
			return err
		}
	}

	log.Default().Println(toolbox.OutputBasicLogString("info", "completed-rolling-back-task-to-billing-subscriptions-indexes"))
	return nil
}
