package migrations

import (
	"context"
	"log"

	"github.com/ooaklee/ghatd/external/toolbox"
	"github.com/ooaklee/ghatd/external/vision"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// InitVisionIndexesUp initialises indexes for the visions collection.
func InitVisionIndexesUp(db *mongo.Database) error { //Up
	log.SetFlags(0)
	const collectionName = vision.VisionCollection

	log.Default().Println(toolbox.OutputBasicLogString("info", "starting-task-to-add-vision-indexes"))

	indexes := []mongo.IndexModel{
		{
			Keys: bson.D{{Key: "_nano_id", Value: 1}},
			Options: options.Index().
				SetName("idx_visions_nano_id").
				SetUnique(true).
				SetSparse(true),
		},
		{
			Keys: bson.D{
				{Key: "type", Value: 1},
				{Key: "status", Value: 1},
				{Key: "created_at", Value: -1},
			},
			Options: options.Index().SetName("idx_visions_type_status_created_at"),
		},
		{
			Keys: bson.D{
				{Key: "status", Value: 1},
				{Key: "created_at", Value: -1},
			},
			Options: options.Index().SetName("idx_visions_status_created_at"),
		},
		{
			Keys:    bson.D{{Key: "created_at", Value: -1}},
			Options: options.Index().SetName("idx_visions_created_at"),
		},
	}

	if _, err := db.Collection(collectionName).Indexes().CreateMany(context.Background(), indexes); err != nil {
		log.Default().Println(toolbox.OutputBasicLogString("error", "failed-task-to-add-vision-indexes"))
		return err
	}

	log.Default().Println(toolbox.OutputBasicLogString("info", "completed-task-to-add-vision-indexes"))
	return nil
}

// InitVisionIndexesDown rolls back the visions indexes.
func InitVisionIndexesDown(db *mongo.Database) error { //Down
	log.SetFlags(0)
	const collectionName = vision.VisionCollection

	for _, indexName := range []string{
		"idx_visions_nano_id",
		"idx_visions_type_status_created_at",
		"idx_visions_status_created_at",
		"idx_visions_created_at",
	} {
		if err := db.Collection(collectionName).Indexes().DropOne(context.TODO(), indexName); err != nil {
			return err
		}
	}
	return nil
}
