package migrations

// import (
// 	"context"
// 	"log"

// 	"github.com/ooaklee/ghatd/external/toolbox"
// 	migrate "github.com/xakep666/mongo-migrate"
// 	"go.mongodb.org/mongo-driver/v2/bson"
// 	"go.mongodb.org/mongo-driver/v2/mongo"
// 	"go.mongodb.org/mongo-driver/v2/mongo/options"
// )

func init() {

	// log.SetFlags(0)
	// const mongoCollectionName = "users"

	// //nolint - ignored as handled elsewhere
	// if err := migrate.Register(func(ctx context.Context, db *mongo.Database) error { // Up

	// 	log.Default().Println(toolbox.OutputBasicLogString("info", "starting-task-to-create-users-created-at-index"))

	// 	opt := options.Index().SetName("users-created-at-index")
	// 	keys := bson.D{{Key: "created_at", Value: 1}}
	// 	model := mongo.IndexModel{Keys: keys, Options: opt}
	// 	_, err := db.Collection(mongoCollectionName).Indexes().CreateOne(ctx, model)
	// 	if err != nil {
	// 		log.Default().Println(toolbox.OutputBasicLogString("error", "failed-task-to-create-users-created-at-index"))
	// 		return err
	// 	}

	// 	log.Default().Println(toolbox.OutputBasicLogString("info", "completed-task-to-create-users-created-at-index"))
	// 	return nil

	// }, func(ctx context.Context, db *mongo.Database) error { // Down
	// 	log.Default().Println(toolbox.OutputBasicLogString("info", "rolling-back-task-to-create-users-created-at-index"))

	// 	err := db.Collection(mongoCollectionName).Indexes().DropOne(ctx, "users-created-at-index")
	// 	if err != nil {
	// 		log.Default().Println(toolbox.OutputBasicLogString("error", "failed-rolling-back-task-to-create-users-created-at-index"))
	// 		return err
	// 	}

	// 	log.Default().Println(toolbox.OutputBasicLogString("info", "completed-rolling-back-task-to-create-users-created-at-index"))
	// 	return nil
	// }); err != nil {
	// 	panic(err)
	// }
}
