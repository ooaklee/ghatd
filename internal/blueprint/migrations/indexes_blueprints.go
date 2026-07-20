package migrations

import (
	"context"
	"log"

	"github.com/ooaklee/ghatd/external/toolbox"
	"github.com/ooaklee/ghatd/internal/blueprint"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// InitBlueprintIndexesUp initialises indexes for the blueprints collection.
func InitBlueprintIndexesUp(db *mongo.Database) error { //Up
	log.SetFlags(0)
	const mongoCollectionName = blueprint.BlueprintCollection

	log.Default().Println(toolbox.OutputBasicLogString("info", "starting-task-to-add-blueprint-indexes"))

	nameKindIndexModel := mongo.IndexModel{
		Keys: bson.D{
			{Key: "name", Value: 1},
			{Key: "kind", Value: 1},
		},
		Options: options.Index().
			SetName("idx_blueprints_name_kind").
			SetUnique(true),
	}

	nanoIDIndexModel := mongo.IndexModel{
		Keys: bson.D{{Key: "_nano_id", Value: 1}},
		Options: options.Index().
			SetName("idx_blueprints_nano_id").
			SetUnique(true).
			SetSparse(true),
	}

	kindStatusIndexModel := mongo.IndexModel{
		Keys: bson.D{
			{Key: "kind", Value: 1},
			{Key: "status", Value: 1},
		},
		Options: options.Index().SetName("idx_blueprints_kind_status"),
	}

	createdAtIndexModel := mongo.IndexModel{
		Keys:    bson.D{{Key: "created_at", Value: -1}},
		Options: options.Index().SetName("idx_blueprints_created_at"),
	}

	_, err := db.Collection(mongoCollectionName).Indexes().CreateMany(
		context.Background(),
		[]mongo.IndexModel{
			nameKindIndexModel,
			nanoIDIndexModel,
			kindStatusIndexModel,
			createdAtIndexModel,
		},
	)
	if err != nil {
		log.Default().Println(toolbox.OutputBasicLogString("error", "failed-task-to-add-blueprint-indexes"))
		return err
	}

	log.Default().Println(toolbox.OutputBasicLogString("info", "completed-task-to-add-blueprint-indexes"))
	return nil
}

// InitBlueprintIndexesDown rolls back the blueprints indexes.
func InitBlueprintIndexesDown(db *mongo.Database) error { //Down
	log.SetFlags(0)
	const mongoCollectionName = blueprint.BlueprintCollection

	log.Default().Println(toolbox.OutputBasicLogString("info", "rolling-back-task-to-add-blueprint-indexes"))

	indexNames := []string{
		"idx_blueprints_name_kind",
		"idx_blueprints_nano_id",
		"idx_blueprints_kind_status",
		"idx_blueprints_created_at",
	}

	for _, indexName := range indexNames {
		err := db.Collection(mongoCollectionName).Indexes().DropOne(context.TODO(), indexName)
		if err != nil {
			log.Default().Println(toolbox.OutputBasicLogString("error", "failed-rolling-back-index: "+indexName))
			return err
		}
	}

	log.Default().Println(toolbox.OutputBasicLogString("info", "completed-rolling-back-task-to-add-blueprint-indexes"))
	return nil
}
