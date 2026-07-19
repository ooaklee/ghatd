package migrations

import (
	"context"
	"log"
	"strings"

	"github.com/ooaklee/ghatd/external/seo"
	"github.com/ooaklee/ghatd/external/toolbox"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

const defaultIdentityTagSystem = "system"

// Paths customises the default sitemap URI seed list.
// These will be normalised and deduplicated against the defaults and each other.
// If any normalised path matches a default, the default will be used (i.e. normalisation is applied before deduplication).
// Normalisation includes trimming whitespace, ensuring a leading slash, and removing any trailing slash (except for the root path).
//
// Defaults:
//   - /
//   - /blog
//   - /pricing
//   - /faq
//   - /whats-new
//   - /policy/privacy
//   - /policy/terms
//   - /policy/cookies
//   - /policy/security-and-compliance
//   - /about
//   - /contact
//   - /for-business
//   - /glossary
type Paths struct {

	// Add is a list of additional URIs to seed beyond the defaults.
	Add []string

	// Remove is a list of URIs to exclude from seeding, even if they are in the defaults or additions.
	Remove []string
}

var defaultSitemapItemPaths = []string{
	"/",
	"/blog",
	"/pricing",
	"/faq",
	"/whats-new",
	"/policy/privacy",
	"/policy/terms",
	"/policy/cookies",
	"/policy/security-and-compliance",
	"/about",
	"/contact",
	"/for-business",
	"/glossary",
}

// ResolveDefaultSitemapItemPaths returns the default URI list with additions and removals applied.
func ResolveDefaultSitemapItemPaths(paths Paths) []string {
	removed := map[string]struct{}{}
	for _, uri := range paths.Remove {
		if normalised := normaliseSitemapSeedPath(uri); normalised != "" {
			removed[normalised] = struct{}{}
		}
	}

	seen := map[string]struct{}{}
	resolved := make([]string, 0, len(defaultSitemapItemPaths)+len(paths.Add))
	for _, uri := range append(defaultSitemapItemPaths, paths.Add...) {
		normalised := normaliseSitemapSeedPath(uri)
		if normalised == "" {
			continue
		}
		if _, shouldRemove := removed[normalised]; shouldRemove {
			continue
		}
		if _, alreadySeen := seen[normalised]; alreadySeen {
			continue
		}

		seen[normalised] = struct{}{}
		resolved = append(resolved, normalised)
	}

	return resolved
}

// InitDefaultSitemapItemsUp seeds the default public sitemap entries.
func InitDefaultSitemapItemsUp(db *mongo.Database) error {
	return InitDefaultSitemapItemsUpWithPaths(Paths{})(db)
}

// InitDefaultSitemapItemsDown removes the default public sitemap entries.
func InitDefaultSitemapItemsDown(db *mongo.Database) error {
	return InitDefaultSitemapItemsDownWithPaths(Paths{})(db)
}

// InitDefaultSitemapItemsUpWithPaths returns an up migration with optional path customisations.
func InitDefaultSitemapItemsUpWithPaths(paths Paths) func(db *mongo.Database) error {
	return func(db *mongo.Database) error {
		return initDefaultSitemapItemsUp(db, paths)
	}
}

// InitDefaultSitemapItemsDownWithPaths returns a down migration with optional path customisations.
func InitDefaultSitemapItemsDownWithPaths(paths Paths) func(db *mongo.Database) error {
	return func(db *mongo.Database) error {
		return initDefaultSitemapItemsDown(db, paths)
	}
}

func initDefaultSitemapItemsUp(db *mongo.Database, paths Paths) error {
	ctx := context.Background()
	collection := db.Collection(seo.SitemapItemsCollection)

	log.Default().Println(toolbox.OutputBasicLogString("info", "starting-task-to-seed-default-sitemap-items"))

	now := toolbox.TimeNowUTC()
	for _, uri := range ResolveDefaultSitemapItemPaths(paths) {
		_, err := collection.UpdateOne(
			ctx,
			bson.M{"uri": uri},
			bson.M{
				"$setOnInsert": bson.M{
					"_id":              toolbox.GenerateUuidV4(),
					"uri":              uri,
					"last_mod":         now,
					"priority":         DefaultSitemapItemPriority(uri),
					"change_frequency": DefaultSitemapItemChangeFrequency(uri),
					"created_by_id":    defaultIdentityTagSystem,
					"created_at":       now,
				},
			},
			options.UpdateOne().SetUpsert(true),
		)
		if err != nil {
			log.Default().Println(toolbox.OutputBasicLogString("error", "failed-to-seed-default-sitemap-item: "+uri))
			return err
		}
	}

	log.Default().Println(toolbox.OutputBasicLogString("info", "completed-task-to-seed-default-sitemap-items"))
	return nil
}

func initDefaultSitemapItemsDown(db *mongo.Database, paths Paths) error {
	ctx := context.Background()
	collection := db.Collection(seo.SitemapItemsCollection)
	uris := ResolveDefaultSitemapItemPaths(paths)

	log.Default().Println(toolbox.OutputBasicLogString("info", "rolling-back-task-to-seed-default-sitemap-items"))

	if len(uris) == 0 {
		log.Default().Println(toolbox.OutputBasicLogString("info", "completed-rolling-back-task-to-seed-default-sitemap-items"))
		return nil
	}

	if _, err := collection.DeleteMany(ctx, bson.M{"uri": bson.M{"$in": uris}}); err != nil {
		log.Default().Println(toolbox.OutputBasicLogString("error", "failed-to-remove-default-sitemap-items"))
		return err
	}

	log.Default().Println(toolbox.OutputBasicLogString("info", "completed-rolling-back-task-to-seed-default-sitemap-items"))
	return nil
}

func normaliseSitemapSeedPath(uri string) string {
	uri = strings.TrimSpace(uri)
	if uri == "" {
		return ""
	}
	if !strings.HasPrefix(uri, "/") {
		uri = "/" + uri
	}
	if uri != "/" {
		uri = strings.TrimRight(uri, "/")
	}

	return uri
}

// DefaultSitemapItemPriority returns the starter priority for a public route.
func DefaultSitemapItemPriority(uri string) float64 {
	switch uri {
	case "/":
		return 1.0
	case "/pricing", "/contact", "/for-business":
		return 0.9
	case "/blog", "/faq", "/whats-new", "/about":
		return 0.8
	case "/glossary":
		return 0.7
	default:
		if strings.HasPrefix(uri, "/policy/") {
			return 0.4
		}
		return 0.5
	}
}

// DefaultSitemapItemChangeFrequency returns the starter change frequency for a public route.
func DefaultSitemapItemChangeFrequency(uri string) seo.ChangeFrequency {
	switch uri {
	case "/blog", "/whats-new":
		return seo.ChangeFrequencyWeekly
	case "/":
		return seo.ChangeFrequencyWeekly
	default:
		if strings.HasPrefix(uri, "/policy/") {
			return seo.ChangeFrequencyYearly
		}
		return seo.ChangeFrequencyMonthly
	}
}
