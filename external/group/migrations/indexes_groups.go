package migrations

import (
	"context"
	"log"

	"github.com/ooaklee/ghatd/external/group"
	"github.com/ooaklee/ghatd/external/toolbox"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// InitGroupsIndexesUp initializes indexes for the groups collection
func InitGroupsIndexesUp(db *mongo.Database) error { //Up
	log.SetFlags(0)
	const mongoCollectionName = group.GroupCollection

	log.Default().Println(toolbox.OutputBasicLogString("info", "starting-task-to-add-groups-indexes"))

	// Unique compound index on name and type for fast lookups and uniqueness
	nameTypeIndexModel := mongo.IndexModel{
		Keys: bson.D{
			{Key: "name", Value: 1},
			{Key: "type", Value: 1},
		},
		Options: options.Index().
			SetName("idx_groups_name_type").
			SetUnique(true),
	}

	// Unique index on nano_id for alternative identifier lookups
	nanoIDIndexModel := mongo.IndexModel{
		Keys: bson.D{{Key: "_nano_id", Value: 1}},
		Options: options.Index().
			SetName("idx_groups_nano_id").
			SetUnique(true).
			SetSparse(true), // Sparse because nano_id is optional
	}

	// Index on type for filtering groups by type
	typeIndexModel := mongo.IndexModel{
		Keys:    bson.D{{Key: "type", Value: 1}},
		Options: options.Index().SetName("idx_groups_type"),
	}

	// Index on status for filtering groups by status
	statusIndexModel := mongo.IndexModel{
		Keys:    bson.D{{Key: "status", Value: 1}},
		Options: options.Index().SetName("idx_groups_status"),
	}

	// Compound index on status and created_at for efficient filtered sorting
	statusCreatedAtIndexModel := mongo.IndexModel{
		Keys: bson.D{
			{Key: "status", Value: 1},
			{Key: "metadata.created_at", Value: -1},
		},
		Options: options.Index().SetName("idx_groups_status_created_at"),
	}

	// Index on members.id for finding groups by member (supports $elemMatch)
	membersIdIndexModel := mongo.IndexModel{
		Keys:    bson.D{{Key: "members.id", Value: 1}},
		Options: options.Index().SetName("idx_groups_members_id"),
	}

	// Compound index on members.id and members.type for filtered member queries
	membersIdTypeIndexModel := mongo.IndexModel{
		Keys: bson.D{
			{Key: "members.id", Value: 1},
			{Key: "members.type", Value: 1},
		},
		Options: options.Index().SetName("idx_groups_members_id_type"),
	}

	// Index on leadership.owner_id for finding groups by owner
	ownerIdIndexModel := mongo.IndexModel{
		Keys:    bson.D{{Key: "leadership.owner_id", Value: 1}},
		Options: options.Index().SetName("idx_groups_owner_id"),
	}

	// Index on leadership.head_id for finding groups by head
	headIdIndexModel := mongo.IndexModel{
		Keys:    bson.D{{Key: "leadership.head_id", Value: 1}},
		Options: options.Index().SetName("idx_groups_head_id"),
	}

	// Index on leadership.lead_id for finding groups by lead
	leadIdIndexModel := mongo.IndexModel{
		Keys:    bson.D{{Key: "leadership.lead_id", Value: 1}},
		Options: options.Index().SetName("idx_groups_lead_id"),
	}

	// Index on leadership.admin_ids for finding groups by admin
	adminIdsIndexModel := mongo.IndexModel{
		Keys:    bson.D{{Key: "leadership.admin_ids", Value: 1}},
		Options: options.Index().SetName("idx_groups_admin_ids"),
	}

	// Index on settings.visibility for filtering by visibility
	visibilityIndexModel := mongo.IndexModel{
		Keys:    bson.D{{Key: "settings.visibility", Value: 1}},
		Options: options.Index().SetName("idx_groups_visibility"),
	}

	// Text index on name for text search capabilities
	nameTextIndexModel := mongo.IndexModel{
		Keys:    bson.D{{Key: "name", Value: "text"}},
		Options: options.Index().SetName("idx_groups_name_text"),
	}

	// Index on created_at for sorting/filtering by creation date
	createdAtIndexModel := mongo.IndexModel{
		Keys:    bson.D{{Key: "metadata.created_at", Value: -1}},
		Options: options.Index().SetName("idx_groups_created_at"),
	}

	// Index on updated_at for sorting by last update
	updatedAtIndexModel := mongo.IndexModel{
		Keys:    bson.D{{Key: "metadata.updated_at", Value: -1}},
		Options: options.Index().SetName("idx_groups_updated_at"),
	}

	// Index on archived_at for filtering archived groups
	archivedAtIndexModel := mongo.IndexModel{
		Keys: bson.D{{Key: "metadata.archived_at", Value: -1}},
		Options: options.Index().
			SetName("idx_groups_archived_at").
			SetSparse(true), // Sparse since most groups won't be archived
	}

	// Index on deleted_at for soft-delete filtering
	deletedAtIndexModel := mongo.IndexModel{
		Keys: bson.D{{Key: "metadata.deleted_at", Value: -1}},
		Options: options.Index().
			SetName("idx_groups_deleted_at").
			SetSparse(true), // Sparse since most groups won't be deleted
	}

	// Compound index on type and status for common filtered queries
	typeStatusIndexModel := mongo.IndexModel{
		Keys: bson.D{
			{Key: "type", Value: 1},
			{Key: "status", Value: 1},
		},
		Options: options.Index().SetName("idx_groups_type_status"),
	}

	// Compound index on visibility and status for access control queries
	visibilityStatusIndexModel := mongo.IndexModel{
		Keys: bson.D{
			{Key: "settings.visibility", Value: 1},
			{Key: "status", Value: 1},
		},
		Options: options.Index().SetName("idx_groups_visibility_status"),
	}

	// Create all indexes
	_, err := db.Collection(mongoCollectionName).Indexes().CreateMany(
		context.Background(),
		[]mongo.IndexModel{
			nameTypeIndexModel,
			nanoIDIndexModel,
			typeIndexModel,
			statusIndexModel,
			statusCreatedAtIndexModel,
			membersIdIndexModel,
			membersIdTypeIndexModel,
			ownerIdIndexModel,
			headIdIndexModel,
			leadIdIndexModel,
			adminIdsIndexModel,
			visibilityIndexModel,
			nameTextIndexModel,
			createdAtIndexModel,
			updatedAtIndexModel,
			archivedAtIndexModel,
			deletedAtIndexModel,
			typeStatusIndexModel,
			visibilityStatusIndexModel,
		},
	)
	if err != nil {
		log.Default().Println(toolbox.OutputBasicLogString("error", "failed-task-to-add-groups-indexes"))
		return err
	}

	log.Default().Println(toolbox.OutputBasicLogString("info", "completed-task-to-add-groups-indexes"))
	return nil
}

// InitGroupsIndexesDown rolls back the groups indexes
func InitGroupsIndexesDown(db *mongo.Database) error { //Down
	log.SetFlags(0)
	const mongoCollectionName = group.GroupCollection

	log.Default().Println(toolbox.OutputBasicLogString("info", "rolling-back-task-to-add-groups-indexes"))

	// Drop all indexes by name
	indexNames := []string{
		"idx_groups_name_type",
		"idx_groups_nano_id",
		"idx_groups_type",
		"idx_groups_status",
		"idx_groups_status_created_at",
		"idx_groups_members_id",
		"idx_groups_members_id_type",
		"idx_groups_owner_id",
		"idx_groups_head_id",
		"idx_groups_lead_id",
		"idx_groups_admin_ids",
		"idx_groups_visibility",
		"idx_groups_name_text",
		"idx_groups_created_at",
		"idx_groups_updated_at",
		"idx_groups_archived_at",
		"idx_groups_deleted_at",
		"idx_groups_type_status",
		"idx_groups_visibility_status",
	}

	for _, indexName := range indexNames {
		_, err := db.Collection(mongoCollectionName).Indexes().DropOne(context.TODO(), indexName)
		if err != nil {
			log.Default().Println(toolbox.OutputBasicLogString("error", "failed-rolling-back-index: "+indexName))
			return err
		}
	}

	log.Default().Println(toolbox.OutputBasicLogString("info", "completed-rolling-back-task-to-add-groups-indexes"))
	return nil
}
