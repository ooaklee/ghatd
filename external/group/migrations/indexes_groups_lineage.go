package migrations

import (
	"context"
	"log"

	"github.com/ooaklee/ghatd/external/group"
	"github.com/ooaklee/ghatd/external/toolbox"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// InitGroupsLineageIndexUp adds the lineage index used by descendants queries.
func InitGroupsLineageIndexUp(db *mongo.Database) error { //Up
	log.SetFlags(0)
	const mongoCollectionName = group.GroupCollection

	log.Default().Println(toolbox.OutputBasicLogString("info", "starting-task-to-add-groups-lineage-index"))

	lineageIndexModel := mongo.IndexModel{
		Keys:    bson.D{{Key: "lineage", Value: 1}},
		Options: options.Index().SetName("idx_groups_lineage"),
	}

	_, err := db.Collection(mongoCollectionName).Indexes().CreateOne(context.Background(), lineageIndexModel)
	if err != nil {
		log.Default().Println(toolbox.OutputBasicLogString("error", "failed-task-to-add-groups-lineage-index"))
		return err
	}

	log.Default().Println(toolbox.OutputBasicLogString("info", "completed-task-to-add-groups-lineage-index"))
	return nil
}

// InitGroupsLineageIndexDown rolls back the lineage index.
func InitGroupsLineageIndexDown(db *mongo.Database) error { //Down
	log.SetFlags(0)
	const mongoCollectionName = group.GroupCollection

	log.Default().Println(toolbox.OutputBasicLogString("info", "rolling-back-task-to-add-groups-lineage-index"))

	err := db.Collection(mongoCollectionName).Indexes().DropOne(context.TODO(), "idx_groups_lineage")
	if err != nil {
		log.Default().Println(toolbox.OutputBasicLogString("error", "failed-rolling-back-groups-lineage-index"))
		return err
	}

	log.Default().Println(toolbox.OutputBasicLogString("info", "completed-rolling-back-task-to-add-groups-lineage-index"))
	return nil
}
