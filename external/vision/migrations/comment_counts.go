package migrations

import (
	"context"
	"log"

	"github.com/ooaklee/ghatd/external/toolbox"
	"github.com/ooaklee/ghatd/external/vision"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

// BackfillVisionCommentCountsUp stores the existing embedded comment count on
// vision documents that predate the denormalised counter.
func BackfillVisionCommentCountsUp(db *mongo.Database) error { //Up
	log.SetFlags(0)
	log.Default().Println(toolbox.OutputBasicLogString("info", "starting-task-to-backfill-vision-comment-counts"))

	update := mongo.Pipeline{
		bson.D{{Key: "$set", Value: bson.D{{
			Key: "comment_count",
			Value: bson.D{{
				Key: "$size",
				Value: bson.D{{
					Key:   "$ifNull",
					Value: bson.A{"$comments", bson.A{}},
				}},
			}},
		}}}},
	}
	if _, err := db.Collection(vision.VisionCollection).UpdateMany(
		context.Background(),
		bson.M{"comment_count": bson.M{"$exists": false}},
		update,
	); err != nil {
		log.Default().Println(toolbox.OutputBasicLogString("error", "failed-task-to-backfill-vision-comment-counts"))
		return err
	}

	log.Default().Println(toolbox.OutputBasicLogString("info", "completed-task-to-backfill-vision-comment-counts"))
	return nil
}

// BackfillVisionCommentCountsDown removes the denormalised comment counter.
func BackfillVisionCommentCountsDown(db *mongo.Database) error { //Down
	_, err := db.Collection(vision.VisionCollection).UpdateMany(
		context.Background(),
		bson.M{},
		bson.M{"$unset": bson.M{"comment_count": ""}},
	)
	return err
}
