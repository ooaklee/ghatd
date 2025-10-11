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

func InitBillingEventsIndexesUp(db *mongo.Database) error { //Up

	const mongoCollectionName = billing.BillingEventsCollection

	log.Default().Println(toolbox.OutputBasicLogString("info", "starting-task-to-billing-events-indexes"))

	// Index on user_id for efficient user lookups
	userIdIndexModel := mongo.IndexModel{
		Keys:    bson.D{{Key: "user_id", Value: 1}},
		Options: options.Index().SetName("idx_billing_events_user_id"),
	}

	// Index on email for email-based queries
	emailIndexModel := mongo.IndexModel{
		Keys:    bson.D{{Key: "email", Value: 1}},
		Options: options.Index().SetName("idx_billing_events_email"),
	}

	// Index on integrator_subscription_id for subscription event lookups
	subscriptionIdIndexModel := mongo.IndexModel{
		Keys:    bson.D{{Key: "integrator_subscription_id", Value: 1}},
		Options: options.Index().SetName("idx_billing_events_subscription_id"),
	}

	// Additional useful index on created_at for sorting/filtering
	createdAtIndexModel := mongo.IndexModel{
		Keys:    bson.D{{Key: "created_at", Value: -1}},
		Options: options.Index().SetName("idx_billing_events_created_at"),
	}

	// Create all indexes
	_, err := db.Collection("billing_events").Indexes().CreateMany(
		context.Background(),
		[]mongo.IndexModel{
			userIdIndexModel,
			emailIndexModel,
			subscriptionIdIndexModel,
			createdAtIndexModel,
		},
	)
	if err != nil {
		log.Default().Println(toolbox.OutputBasicLogString("error", "failed-task-to-billing-events-indexes"))
		return err
	}

	log.Default().Println(toolbox.OutputBasicLogString("info", "completed-task-to-billing-events-indexes"))
	return nil

}

func InitBillingEventsIndexesDown(db *mongo.Database) error { //Down
	log.SetFlags(0)
	const mongoCollectionName = billing.BillingEventsCollection

	log.Default().Println(toolbox.OutputBasicLogString("info", "rolling-back-task-to-billing-events-indexes"))

	// Drop all indexes by name
	indexNames := []string{
		"idx_billing_events_user_id",
		"idx_billing_events_email",
		"idx_billing_events_subscription_id",
		"idx_billing_events_created_at",
	}

	for _, indexName := range indexNames {
		_, err := db.Collection(mongoCollectionName).Indexes().DropOne(context.TODO(), indexName)
		if err != nil {
			log.Default().Println(toolbox.OutputBasicLogString("error", "failed-rolling-back-index: "+indexName))
			return err
		}
	}

	log.Default().Println(toolbox.OutputBasicLogString("info", "completed-rolling-back-task-to-billing-events-indexes"))
	return nil
}

// func InitBillingEventsIndexesUp() error {

// 	log.SetFlags(0)
// 	const mongoCollectionName = billing.BillingEventsCollection

// 	//nolint - ignored as handled else where
// 	return migrate.Register(func(db *mongo.Database) error { //Up

// 		log.Default().Println(toolbox.OutputBasicLogString("info", "starting-task-to-billing-events-indexes"))

// 		// Index on user_id for efficient user lookups
// 		userIdIndexModel := mongo.IndexModel{
// 			Keys:    bson.D{{Key: "user_id", Value: 1}},
// 			Options: options.Index().SetName("idx_billing_events_user_id"),
// 		}

// 		// Index on email for email-based queries
// 		emailIndexModel := mongo.IndexModel{
// 			Keys:    bson.D{{Key: "email", Value: 1}},
// 			Options: options.Index().SetName("idx_billing_events_email"),
// 		}

// 		// Index on integrator_subscription_id for subscription event lookups
// 		subscriptionIdIndexModel := mongo.IndexModel{
// 			Keys:    bson.D{{Key: "integrator_subscription_id", Value: 1}},
// 			Options: options.Index().SetName("idx_billing_events_subscription_id"),
// 		}

// 		// Additional useful index on created_at for sorting/filtering
// 		createdAtIndexModel := mongo.IndexModel{
// 			Keys:    bson.D{{Key: "created_at", Value: -1}},
// 			Options: options.Index().SetName("idx_billing_events_created_at"),
// 		}

// 		// Create all indexes
// 		_, err := db.Collection("billing_events").Indexes().CreateMany(
// 			context.Background(),
// 			[]mongo.IndexModel{
// 				userIdIndexModel,
// 				emailIndexModel,
// 				subscriptionIdIndexModel,
// 				createdAtIndexModel,
// 			},
// 		)
// 		if err != nil {
// 			log.Default().Println(toolbox.OutputBasicLogString("error", "failed-task-to-billing-events-indexes"))
// 			return err
// 		}

// 		log.Default().Println(toolbox.OutputBasicLogString("info", "completed-task-to-billing-events-indexes"))
// 		return nil

// 	}, func(db *mongo.Database) error { //Down
// 		log.Default().Println(toolbox.OutputBasicLogString("info", "rolling-back-task-to-billing-events-indexes"))

// 		// Drop all indexes by name
// 		indexNames := []string{
// 			"idx_billing_events_user_id",
// 			"idx_billing_events_email",
// 			"idx_billing_events_subscription_id",
// 			"idx_billing_events_created_at",
// 		}

// 		for _, indexName := range indexNames {
// 			_, err := db.Collection(mongoCollectionName).Indexes().DropOne(context.TODO(), indexName)
// 			if err != nil {
// 				log.Default().Println(toolbox.OutputBasicLogString("error", "failed-rolling-back-index: "+indexName))
// 				return err
// 			}
// 		}

// 		log.Default().Println(toolbox.OutputBasicLogString("info", "completed-rolling-back-task-to-billing-events-indexes"))
// 		return nil
// 	})
// }
