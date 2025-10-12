package migrations

import (
	"context"
	"log"

	"github.com/ooaklee/ghatd/external/toolbox"
	user "github.com/ooaklee/ghatd/external/user/v2"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// InitUsersIndexesUp initializes indexes for the users collection
func InitUsersIndexesUp(db *mongo.Database) error { //Up
	log.SetFlags(0)
	const mongoCollectionName = user.UserCollection

	log.Default().Println(toolbox.OutputBasicLogString("info", "starting-task-to-add-users-indexes"))

	// Unique index on email for fast user lookups and uniqueness
	emailIndexModel := mongo.IndexModel{
		Keys:    bson.D{{Key: "email", Value: 1}},
		Options: options.Index().SetName("idx_users_email").SetUnique(true),
	}

	// Unique index on nano_id for alternative identifier lookups
	nanoIDIndexModel := mongo.IndexModel{
		Keys: bson.D{{Key: "_nano_id", Value: 1}},
		Options: options.Index().
			SetName("idx_users_nano_id").
			SetUnique(true).
			SetSparse(true), // Sparse because nano_id is optional
	}

	// Index on status for filtering users by status
	statusIndexModel := mongo.IndexModel{
		Keys:    bson.D{{Key: "status", Value: 1}},
		Options: options.Index().SetName("idx_users_status"),
	}

	// Index on roles for role-based queries
	rolesIndexModel := mongo.IndexModel{
		Keys:    bson.D{{Key: "roles", Value: 1}},
		Options: options.Index().SetName("idx_users_roles"),
	}

	// Compound index on status and created_at for efficient filtered sorting
	statusCreatedAtIndexModel := mongo.IndexModel{
		Keys: bson.D{
			{Key: "status", Value: 1},
			{Key: "metadata.created_at", Value: -1},
		},
		Options: options.Index().SetName("idx_users_status_created_at"),
	}

	// Index on email verification status for filtering unverified users
	emailVerifiedIndexModel := mongo.IndexModel{
		Keys:    bson.D{{Key: "verification.email_verified", Value: 1}},
		Options: options.Index().SetName("idx_users_email_verified"),
	}

	// Index on created_at for sorting/filtering by registration date
	createdAtIndexModel := mongo.IndexModel{
		Keys:    bson.D{{Key: "metadata.created_at", Value: -1}},
		Options: options.Index().SetName("idx_users_created_at"),
	}

	// Index on updated_at for sorting by last update
	updatedAtIndexModel := mongo.IndexModel{
		Keys:    bson.D{{Key: "metadata.updated_at", Value: -1}},
		Options: options.Index().SetName("idx_users_updated_at"),
	}

	// Index on last_login_at for activity tracking
	lastLoginAtIndexModel := mongo.IndexModel{
		Keys:    bson.D{{Key: "metadata.last_login_at", Value: -1}},
		Options: options.Index().SetName("idx_users_last_login_at"),
	}

	// Index on activated_at for filtering activated users
	activatedAtIndexModel := mongo.IndexModel{
		Keys:    bson.D{{Key: "metadata.activated_at", Value: -1}},
		Options: options.Index().SetName("idx_users_activated_at"),
	}

	// Index on status_changed_at for status change tracking
	statusChangedAtIndexModel := mongo.IndexModel{
		Keys:    bson.D{{Key: "metadata.status_changed_at", Value: -1}},
		Options: options.Index().SetName("idx_users_status_changed_at"),
	}

	// Index on email_verified_at for verification tracking
	emailVerifiedAtIndexModel := mongo.IndexModel{
		Keys:    bson.D{{Key: "verification.email_verified_at", Value: -1}},
		Options: options.Index().SetName("idx_users_email_verified_at"),
	}

	// Create all indexes
	_, err := db.Collection(mongoCollectionName).Indexes().CreateMany(
		context.Background(),
		[]mongo.IndexModel{
			emailIndexModel,
			nanoIDIndexModel,
			statusIndexModel,
			rolesIndexModel,
			statusCreatedAtIndexModel,
			emailVerifiedIndexModel,
			createdAtIndexModel,
			updatedAtIndexModel,
			lastLoginAtIndexModel,
			activatedAtIndexModel,
			statusChangedAtIndexModel,
			emailVerifiedAtIndexModel,
		},
	)
	if err != nil {
		log.Default().Println(toolbox.OutputBasicLogString("error", "failed-task-to-add-users-indexes"))
		return err
	}

	log.Default().Println(toolbox.OutputBasicLogString("info", "completed-task-to-add-users-indexes"))
	return nil
}

// InitUsersIndexesDown rolls back the users indexes
func InitUsersIndexesDown(db *mongo.Database) error { //Down
	log.SetFlags(0)
	const mongoCollectionName = user.UserCollection

	log.Default().Println(toolbox.OutputBasicLogString("info", "rolling-back-task-to-add-users-indexes"))

	// Drop all indexes by name
	indexNames := []string{
		"idx_users_email",
		"idx_users_nano_id",
		"idx_users_status",
		"idx_users_roles",
		"idx_users_status_created_at",
		"idx_users_email_verified",
		"idx_users_created_at",
		"idx_users_updated_at",
		"idx_users_last_login_at",
		"idx_users_activated_at",
		"idx_users_status_changed_at",
		"idx_users_email_verified_at",
	}

	for _, indexName := range indexNames {
		_, err := db.Collection(mongoCollectionName).Indexes().DropOne(context.TODO(), indexName)
		if err != nil {
			log.Default().Println(toolbox.OutputBasicLogString("error", "failed-rolling-back-index: "+indexName))
			return err
		}
	}

	log.Default().Println(toolbox.OutputBasicLogString("info", "completed-rolling-back-task-to-add-users-indexes"))
	return nil
}
