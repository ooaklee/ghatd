package migrations

import (
	"context"
	"log"

	"github.com/ooaklee/ghatd/external/streaker"
	"github.com/ooaklee/ghatd/external/toolbox"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// InitStreaksIndexesUp initializes indexes for the streaks collection.
func InitStreaksIndexesUp(db *mongo.Database) error { //Up
	log.SetFlags(0)
	const mongoCollectionName = streaker.StreakCollection

	log.Default().Println(toolbox.OutputBasicLogString("info", "starting-task-to-add-streaks-indexes"))

	nanoIDIndexModel := mongo.IndexModel{
		Keys: bson.D{{Key: "_nano_id", Value: 1}},
		Options: options.Index().
			SetName("idx_streaks_nano_id").
			SetUnique(true).
			SetSparse(true),
	}

	scopePeriodIndexModel := mongo.IndexModel{
		Keys: bson.D{
			{Key: "streak_type", Value: 1},
			{Key: "owner_id", Value: 1},
			{Key: "target_type", Value: 1},
			{Key: "target_id", Value: 1},
			{Key: "period_type", Value: 1},
			{Key: "period_key", Value: 1},
		},
		Options: options.Index().
			SetName("idx_streaks_scope_period").
			SetUnique(true),
	}

	latestScopeIndexModel := mongo.IndexModel{
		Keys: bson.D{
			{Key: "streak_type", Value: 1},
			{Key: "owner_id", Value: 1},
			{Key: "target_type", Value: 1},
			{Key: "target_id", Value: 1},
			{Key: "period_type", Value: 1},
			{Key: "occurred_at", Value: -1},
			{Key: "created_at", Value: -1},
		},
		Options: options.Index().SetName("idx_streaks_scope_latest"),
	}

	longestScopeIndexModel := mongo.IndexModel{
		Keys: bson.D{
			{Key: "streak_type", Value: 1},
			{Key: "owner_id", Value: 1},
			{Key: "target_type", Value: 1},
			{Key: "target_id", Value: 1},
			{Key: "period_type", Value: 1},
			{Key: "current_count", Value: -1},
		},
		Options: options.Index().SetName("idx_streaks_scope_longest"),
	}

	typeOwnerIndexModel := mongo.IndexModel{
		Keys: bson.D{
			{Key: "streak_type", Value: 1},
			{Key: "owner_id", Value: 1},
		},
		Options: options.Index().SetName("idx_streaks_type_owner"),
	}

	createdAtIndexModel := mongo.IndexModel{
		Keys:    bson.D{{Key: "created_at", Value: -1}},
		Options: options.Index().SetName("idx_streaks_created_at"),
	}

	_, err := db.Collection(mongoCollectionName).Indexes().CreateMany(
		context.Background(),
		[]mongo.IndexModel{
			nanoIDIndexModel,
			scopePeriodIndexModel,
			latestScopeIndexModel,
			longestScopeIndexModel,
			typeOwnerIndexModel,
			createdAtIndexModel,
		},
	)
	if err != nil {
		log.Default().Println(toolbox.OutputBasicLogString("error", "failed-task-to-add-streaks-indexes"))
		return err
	}

	log.Default().Println(toolbox.OutputBasicLogString("info", "completed-task-to-add-streaks-indexes"))
	return nil
}

// InitStreaksIndexesDown rolls back the streaks indexes.
func InitStreaksIndexesDown(db *mongo.Database) error { //Down
	log.SetFlags(0)
	const mongoCollectionName = streaker.StreakCollection

	log.Default().Println(toolbox.OutputBasicLogString("info", "rolling-back-task-to-add-streaks-indexes"))

	indexNames := []string{
		"idx_streaks_nano_id",
		"idx_streaks_scope_period",
		"idx_streaks_scope_latest",
		"idx_streaks_scope_longest",
		"idx_streaks_type_owner",
		"idx_streaks_created_at",
	}

	for _, indexName := range indexNames {
		err := db.Collection(mongoCollectionName).Indexes().DropOne(context.TODO(), indexName)
		if err != nil {
			log.Default().Println(toolbox.OutputBasicLogString("error", "failed-rolling-back-index: "+indexName))
			return err
		}
	}

	log.Default().Println(toolbox.OutputBasicLogString("info", "completed-rolling-back-task-to-add-streaks-indexes"))
	return nil
}
