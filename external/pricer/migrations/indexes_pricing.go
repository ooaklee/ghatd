package migrations

import (
	"context"
	"log"

	"github.com/ooaklee/ghatd/external/pricer"
	"github.com/ooaklee/ghatd/external/toolbox"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func InitPricingIndexesUp(db *mongo.Database) error { //Up

	log.SetFlags(0)

	log.Default().Println(toolbox.OutputBasicLogString("info", "starting-task-to-pricing-indexes"))

	slugUniqueIndexModel := mongo.IndexModel{
		Keys:    bson.D{{Key: "slug", Value: 1}},
		Options: options.Index().SetName("idx_pricing_plans_slug").SetUnique(true).SetSparse(false),
	}

	statusIndexModel := mongo.IndexModel{
		Keys:    bson.D{{Key: "status", Value: 1}},
		Options: options.Index().SetName("idx_pricing_plans_status"),
	}

	plansCreatedAtIndexModel := mongo.IndexModel{
		Keys:    bson.D{{Key: "created_at", Value: -1}},
		Options: options.Index().SetName("idx_pricing_plans_created_at"),
	}

	plansPublishedAtIndexModel := mongo.IndexModel{
		Keys:    bson.D{{Key: "published_at", Value: -1}},
		Options: options.Index().SetName("idx_pricing_plans_published_at"),
	}

	featureSlugUniqueIndexModel := mongo.IndexModel{
		Keys:    bson.D{{Key: "slug", Value: 1}},
		Options: options.Index().SetName("idx_pricing_features_slug").SetUnique(true).SetSparse(false),
	}

	featureTypeIndexModel := mongo.IndexModel{
		Keys:    bson.D{{Key: "type", Value: 1}},
		Options: options.Index().SetName("idx_pricing_features_type"),
	}

	featuresCreatedAtIndexModel := mongo.IndexModel{
		Keys:    bson.D{{Key: "created_at", Value: -1}},
		Options: options.Index().SetName("idx_pricing_features_created_at"),
	}

	_, err := db.Collection(pricer.PricePlansCollection).Indexes().CreateMany(
		context.Background(),
		[]mongo.IndexModel{
			slugUniqueIndexModel,
			statusIndexModel,
			plansCreatedAtIndexModel,
			plansPublishedAtIndexModel,
		},
	)
	if err != nil {
		log.Default().Println(toolbox.OutputBasicLogString("error", "failed-task-to-pricing-plans-indexes"))
		return err
	}

	_, err = db.Collection(pricer.PriceFeaturesCollection).Indexes().CreateMany(
		context.Background(),
		[]mongo.IndexModel{
			featureSlugUniqueIndexModel,
			featureTypeIndexModel,
			featuresCreatedAtIndexModel,
		},
	)
	if err != nil {
		log.Default().Println(toolbox.OutputBasicLogString("error", "failed-task-to-pricing-features-indexes"))
		return err
	}

	log.Default().Println(toolbox.OutputBasicLogString("info", "completed-task-to-pricing-indexes"))
	return nil

}

func InitPricingIndexesDown(db *mongo.Database) error { //Down
	log.SetFlags(0)

	log.Default().Println(toolbox.OutputBasicLogString("info", "rolling-back-task-to-pricing-indexes"))

	planIndexNames := []string{
		"idx_pricing_plans_slug",
		"idx_pricing_plans_status",
		"idx_pricing_plans_created_at",
		"idx_pricing_plans_published_at",
	}

	for _, indexName := range planIndexNames {
		_, err := db.Collection(pricer.PricePlansCollection).Indexes().DropOne(context.TODO(), indexName)
		if err != nil {
			log.Default().Println(toolbox.OutputBasicLogString("error", "failed-rolling-back-index: "+indexName))
			return err
		}
	}

	featureIndexNames := []string{
		"idx_pricing_features_slug",
		"idx_pricing_features_type",
		"idx_pricing_features_created_at",
	}

	for _, indexName := range featureIndexNames {
		_, err := db.Collection(pricer.PriceFeaturesCollection).Indexes().DropOne(context.TODO(), indexName)
		if err != nil {
			log.Default().Println(toolbox.OutputBasicLogString("error", "failed-rolling-back-index: "+indexName))
			return err
		}
	}

	log.Default().Println(toolbox.OutputBasicLogString("info", "completed-rolling-back-task-to-pricing-indexes"))
	return nil
}
