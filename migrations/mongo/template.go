package migrations

// import (
// 	"context"
// 	"log"

// 	"github.com/ooaklee/ghatd/external/toolbox"
// 	migrate "github.com/xakep666/mongo-migrate"
// 	"go.mongodb.org/mongo-driver/bson"
// 	"go.mongodb.org/mongo-driver/mongo"
// 	"go.mongodb.org/mongo-driver/mongo/options"
// )

func init() {

	// log.SetFlags(0)
	// const mongoCollectionName = "users"

	// //nolint - ignored as handled else where
	// migrate.Register(func(db *mongo.Database) error { //Up

	// 	log.Default().Println(toolbox.OutputBasicLogString("info", "starting-task-to-create-users-created-at-index"))

	// 	opt := options.Index().SetName("users-created-at-index")
	// 	keys := bson.D{{"created_at", 1}}
	// 	model := mongo.IndexModel{Keys: keys, Options: opt}
	// 	_, err := db.Collection(mongoCollectionName).Indexes().CreateOne(context.TODO(), model)
	// 	if err != nil {
	// 		log.Default().Println(toolbox.OutputBasicLogString("error", "failed-task-to-create-users-created-at-index"))
	// 		return err
	// 	}

	// 	log.Default().Println(toolbox.OutputBasicLogString("info", "completed-task-to-create-users-created-at-index"))
	// 	return nil

	// }, func(db *mongo.Database) error { //Down
	// 	log.Default().Println(toolbox.OutputBasicLogString("info", "rolling-back-task-to-create-users-created-at-index"))

	// 	_, err := db.Collection(mongoCollectionName).Indexes().DropOne(context.TODO(), "users-created-at-index")
	// 	if err != nil {
	// 		log.Default().Println(toolbox.OutputBasicLogString("error", "failed-rolling-back-task-to-create-users-created-at-index"))
	// 		return err
	// 	}

	// 	log.Default().Println(toolbox.OutputBasicLogString("info", "completed-rolling-back-task-to-create-users-created-at-index"))
	// 	return nil
	// })
}
