// Package migrations contains MongoDB index setup for the notifier package.
//
// These functions are called by the migrator tool (cmd/migrator) during
// deployment to create the database indexes that the notifier repository
// depends on.
//
// # Indexes
//
// The notification_addresses collection has three indexes:
//
//  1. A unique compound index on (channel, address_hash) that prevents
//     the same browser or device from creating duplicate address records.
//     When the same Push subscription registers again (for example after
//     a different user signs in), the existing document is updated rather
//     than a duplicate being created.
//
//  2. A compound index on (user_id, status) that speeds up the "find all
//     active addresses for a user" query used by the NotifyUser flow.
//
//  3. A descending index on metadata.updated_at for efficient sorting
//     when listing a user's registered devices (most recent first).
//
// The notification_preferences collection has one index:
//
//  1. An index on the enabled field for queries that need to find users
//     who have opted in or out of notifications.
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

// InitNotifierIndexesUp creates all the indexes the notifier package needs.
// It is called during migrations to set up the notification_addresses and
// notification_preferences collections when deploying to a fresh database
// or upgrading an existing one.
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

// InitNotifierIndexesDown drops all notifier indexes in reverse order.
// This is called during migration rollback to undo the changes made by
// InitNotifierIndexesUp.
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
