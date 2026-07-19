package migrations

import (
	"context"
	"log"
	"strings"

	"github.com/ooaklee/ghatd/external/seo"
	"github.com/ooaklee/ghatd/external/toolbox"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const sitemapItemsURIIndexName = "sitemap_items_uri_unique"

// InitSitemapItemIndexesUp creates indexes needed by sitemap item persistence.
func InitSitemapItemIndexesUp(db *mongo.Database) error {
	ctx := context.Background()
	collection := db.Collection(seo.SitemapItemsCollection)

	log.Default().Println(toolbox.OutputBasicLogString("info", "starting-task-to-create-sitemap-item-indexes"))

	_, err := collection.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys: bson.D{{Key: "uri", Value: 1}},
		Options: options.Index().
			SetName(sitemapItemsURIIndexName).
			SetUnique(true).
			SetSparse(true),
	})
	if err != nil {
		log.Default().Println(toolbox.OutputBasicLogString("error", "failed-to-create-sitemap-item-uri-index"))
		return err
	}

	log.Default().Println(toolbox.OutputBasicLogString("info", "completed-task-to-create-sitemap-item-indexes"))
	return nil
}

// InitSitemapItemIndexesDown removes indexes created for sitemap item persistence.
func InitSitemapItemIndexesDown(db *mongo.Database) error {
	ctx := context.Background()
	collection := db.Collection(seo.SitemapItemsCollection)

	log.Default().Println(toolbox.OutputBasicLogString("info", "rolling-back-task-to-create-sitemap-item-indexes"))

	if _, err := collection.Indexes().DropOne(ctx, sitemapItemsURIIndexName); err != nil && !strings.Contains(err.Error(), "index not found") {
		log.Default().Println(toolbox.OutputBasicLogString("error", "failed-to-drop-sitemap-item-uri-index"))
		return err
	}

	log.Default().Println(toolbox.OutputBasicLogString("info", "completed-rolling-back-task-to-create-sitemap-item-indexes"))
	return nil
}
