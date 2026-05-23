package migrations

import (
	"context"
	"log"

	"github.com/ooaklee/ghatd/external/reminder"
	"github.com/ooaklee/ghatd/external/toolbox"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// InitRemindersIndexesUp creates indexes for reminder declarations and execution tracking.
func InitRemindersIndexesUp(db *mongo.Database) error {
	log.SetFlags(0)
	const mongoCollectionName = reminder.ReminderCollection

	log.Default().Println(toolbox.OutputBasicLogString("info", "starting-task-to-add-reminders-indexes"))

	nanoIDIndexModel := mongo.IndexModel{
		Keys: bson.D{{Key: "_nano_id", Value: 1}},
		Options: options.Index().
			SetName("idx_reminders_nano_id").
			SetUnique(true).
			SetSparse(true),
	}

	userStatusIndexModel := mongo.IndexModel{
		Keys: bson.D{
			{Key: "user_id", Value: 1},
			{Key: "status", Value: 1},
		},
		Options: options.Index().SetName("idx_reminders_user_status"),
	}

	userTargetTimeIndexModel := mongo.IndexModel{
		Keys: bson.D{
			{Key: "user_id", Value: 1},
			{Key: "target_time", Value: 1},
		},
		Options: options.Index().SetName("idx_reminders_user_target_time"),
	}

	userNextDueAtIndexModel := mongo.IndexModel{
		Keys: bson.D{
			{Key: "user_id", Value: 1},
			{Key: "next_due_at", Value: 1},
		},
		Options: options.Index().SetName("idx_reminders_user_next_due_at"),
	}

	userTargetStatusIndexModel := mongo.IndexModel{
		Keys: bson.D{
			{Key: "user_id", Value: 1},
			{Key: "target_type", Value: 1},
			{Key: "target_id", Value: 1},
			{Key: "status", Value: 1},
			{Key: "target_time", Value: 1},
		},
		Options: options.Index().SetName("idx_reminders_user_target_status_time"),
	}

	userTargetStatusNextDueAtIndexModel := mongo.IndexModel{
		Keys: bson.D{
			{Key: "user_id", Value: 1},
			{Key: "target_type", Value: 1},
			{Key: "target_id", Value: 1},
			{Key: "status", Value: 1},
			{Key: "next_due_at", Value: 1},
		},
		Options: options.Index().SetName("idx_reminders_user_target_status_next_due_at"),
	}

	dueLookupIndexModel := mongo.IndexModel{
		Keys: bson.D{
			{Key: "status", Value: 1},
			{Key: "target_time", Value: 1},
		},
		Options: options.Index().SetName("idx_reminders_due_lookup"),
	}

	dueLookupNextDueAtIndexModel := mongo.IndexModel{
		Keys: bson.D{
			{Key: "status", Value: 1},
			{Key: "next_due_at", Value: 1},
		},
		Options: options.Index().SetName("idx_reminders_due_lookup_next_due_at"),
	}

	createdAtIndexModel := mongo.IndexModel{
		Keys:    bson.D{{Key: "created_at", Value: -1}},
		Options: options.Index().SetName("idx_reminders_created_at"),
	}

	_, err := db.Collection(mongoCollectionName).Indexes().CreateMany(
		context.Background(),
		[]mongo.IndexModel{
			nanoIDIndexModel,
			userStatusIndexModel,
			userTargetTimeIndexModel,
			userNextDueAtIndexModel,
			userTargetStatusIndexModel,
			userTargetStatusNextDueAtIndexModel,
			dueLookupIndexModel,
			dueLookupNextDueAtIndexModel,
			createdAtIndexModel,
		},
	)
	if err != nil {
		log.Default().Println(toolbox.OutputBasicLogString("error", "failed-task-to-add-reminders-indexes"))
		return err
	}

	_, err = db.Collection(reminder.ReminderExecutionsCollection).Indexes().CreateMany(
		context.Background(),
		[]mongo.IndexModel{
			{
				Keys: bson.D{{Key: "_nano_id", Value: 1}},
				Options: options.Index().
					SetName("idx_reminder_executions_nano_id").
					SetUnique(true).
					SetSparse(true),
			},
			{
				Keys: bson.D{
					{Key: "reminder_id", Value: 1},
					{Key: "created_at", Value: -1},
				},
				Options: options.Index().SetName("idx_reminder_executions_reminder_created"),
			},
			{
				Keys: bson.D{
					{Key: "user_id", Value: 1},
					{Key: "target_type", Value: 1},
					{Key: "target_id", Value: 1},
					{Key: "status", Value: 1},
					{Key: "scheduled_for", Value: -1},
				},
				Options: options.Index().SetName("idx_reminder_executions_user_target_status"),
			},
			{
				Keys: bson.D{
					{Key: "status", Value: 1},
					{Key: "scheduled_for", Value: -1},
				},
				Options: options.Index().SetName("idx_reminder_executions_status_scheduled"),
			},
		},
	)
	if err != nil {
		log.Default().Println(toolbox.OutputBasicLogString("error", "failed-task-to-add-reminder-executions-indexes"))
		return err
	}

	log.Default().Println(toolbox.OutputBasicLogString("info", "completed-task-to-add-reminders-indexes"))
	return nil
}

// InitRemindersIndexesDown drops reminder declaration and execution tracking indexes.
func InitRemindersIndexesDown(db *mongo.Database) error {
	log.SetFlags(0)
	const mongoCollectionName = reminder.ReminderCollection

	log.Default().Println(toolbox.OutputBasicLogString("info", "rolling-back-task-to-add-reminders-indexes"))

	indexNames := []string{
		"idx_reminders_nano_id",
		"idx_reminders_user_status",
		"idx_reminders_user_target_time",
		"idx_reminders_user_next_due_at",
		"idx_reminders_user_target_status_time",
		"idx_reminders_user_target_status_next_due_at",
		"idx_reminders_due_lookup",
		"idx_reminders_due_lookup_next_due_at",
		"idx_reminders_created_at",
	}

	for _, indexName := range indexNames {
		_, err := db.Collection(mongoCollectionName).Indexes().DropOne(context.TODO(), indexName)
		if err != nil {
			log.Default().Println(toolbox.OutputBasicLogString("error", "failed-rolling-back-index: "+indexName))
			return err
		}
	}

	executionIndexNames := []string{
		"idx_reminder_executions_nano_id",
		"idx_reminder_executions_reminder_created",
		"idx_reminder_executions_user_target_status",
		"idx_reminder_executions_status_scheduled",
	}
	for _, indexName := range executionIndexNames {
		_, err := db.Collection(reminder.ReminderExecutionsCollection).Indexes().DropOne(context.TODO(), indexName)
		if err != nil {
			log.Default().Println(toolbox.OutputBasicLogString("error", "failed-rolling-back-index: "+indexName))
			return err
		}
	}

	log.Default().Println(toolbox.OutputBasicLogString("info", "completed-rolling-back-task-to-add-reminders-indexes"))
	return nil
}
