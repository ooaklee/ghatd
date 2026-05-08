package migrations

import (
	"context"
	"log"

	"github.com/ooaklee/ghatd/external/notifier"
	"github.com/ooaklee/ghatd/external/toolbox"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// InitNotifierIndexesUp creates notifier indexes.
func InitNotifierIndexesUp(db *mongo.Database) error { //Up
	log.SetFlags(0)
	log.Default().Println(toolbox.OutputBasicLogString("info", "starting-task-to-notifier-indexes"))

	_, err := db.Collection(notifier.NotificationAddressesCollection).Indexes().CreateMany(
		context.Background(),
		[]mongo.IndexModel{
			{
				Keys: bson.D{{Key: "channel", Value: 1}, {Key: "address_hash", Value: 1}},
				Options: options.Index().
					SetName("idx_notification_addresses_channel_address_hash").
					SetUnique(true),
			},
			{
				Keys:    bson.D{{Key: "user_id", Value: 1}, {Key: "status", Value: 1}},
				Options: options.Index().SetName("idx_notification_addresses_user_status"),
			},
			{
				Keys:    bson.D{{Key: "metadata.updated_at", Value: -1}},
				Options: options.Index().SetName("idx_notification_addresses_updated_at"),
			},
		},
	)
	if err != nil {
		log.Default().Println(toolbox.OutputBasicLogString("error", "failed-task-to-notification-addresses-indexes"))
		return err
	}

	_, err = db.Collection(notifier.NotificationPreferencesCollection).Indexes().CreateMany(
		context.Background(),
		[]mongo.IndexModel{
			{
				Keys:    bson.D{{Key: "enabled", Value: 1}},
				Options: options.Index().SetName("idx_notification_preferences_enabled"),
			},
		},
	)
	if err != nil {
		log.Default().Println(toolbox.OutputBasicLogString("error", "failed-task-to-notification-preferences-indexes"))
		return err
	}

	log.Default().Println(toolbox.OutputBasicLogString("info", "completed-task-to-notifier-indexes"))
	return nil
}

// InitNotifierIndexesDown drops notifier indexes.
func InitNotifierIndexesDown(db *mongo.Database) error { //Down
	log.SetFlags(0)
	log.Default().Println(toolbox.OutputBasicLogString("info", "rolling-back-task-to-notifier-indexes"))

	addressIndexNames := []string{
		"idx_notification_addresses_channel_address_hash",
		"idx_notification_addresses_user_status",
		"idx_notification_addresses_updated_at",
	}
	for _, indexName := range addressIndexNames {
		if _, err := db.Collection(notifier.NotificationAddressesCollection).Indexes().DropOne(context.TODO(), indexName); err != nil {
			log.Default().Println(toolbox.OutputBasicLogString("error", "failed-rolling-back-index: "+indexName))
			return err
		}
	}

	if _, err := db.Collection(notifier.NotificationPreferencesCollection).Indexes().DropOne(context.TODO(), "idx_notification_preferences_enabled"); err != nil {
		log.Default().Println(toolbox.OutputBasicLogString("error", "failed-rolling-back-index: idx_notification_preferences_enabled"))
		return err
	}

	log.Default().Println(toolbox.OutputBasicLogString("info", "completed-rolling-back-task-to-notifier-indexes"))
	return nil
}
