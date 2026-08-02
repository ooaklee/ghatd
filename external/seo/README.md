# SEO Sitemap

The `external/seo` package provides reusable sitemap persistence, management,
generation, and HTTP routes for GHATD host applications.

Sitemap items are stored in MongoDB and contain a URI, last-modified timestamp,
priority, change frequency, and creator metadata. Relative URIs are rendered
against the frontend domain configured by the host application. Generated XML
is sorted and de-duplicated by absolute URL.

The public sitemap route is `GET /sitemap.xml`. Administrative routes under
`/api/v1/seo` support listing, creating, updating, deleting, batch ingestion,
generation, and safe downloads. The host application supplies its existing
admin-only middleware when attaching the routes.

Generated files are restricted to safe relative paths below a configured
writable root. An embedded static sitemap can be supplied as a read fallback.
Protected route groups such as `/admin`, `/api`, `/app`, `/auth`, `/offline`,
`/portal`, and `/settings` are excluded from generated XML.

The `migrations` subpackage provides the sitemap-item index and a configurable
starter-route seed. Host applications can add or remove paths when registering
the seed migration. Product-specific content discovery and crawler policy stay
in the host application and feed the generic batch-ingestion service.

Register `InitSitemapItemIndexesUp` and `InitSitemapItemIndexesDown` in the
host application's `migrations/mongo` package before the matching seed
functions. Use `InitDefaultSitemapItemsUpWithPaths` and
`InitDefaultSitemapItemsDownWithPaths` when the host needs to customise the
starter routes. Apply all pending registrations with:

```sh
asdf exec go run main.go mongo-migrator up
```

The host command adapter must blank-import its migration package. The shared
`down` action reverts every applied registered migration, including seeded
sitemap items, so review the resolved paths before rollback. See
[Managing MongoDB Migrations](../../docs/how-to/manage-mongodb-migrations.md)
for the registration adapter, settings, and rollback workflow.
