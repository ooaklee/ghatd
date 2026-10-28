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
admin-only middleware when attaching the routes. `AdminAccess` defaults to
`router.AdminSession`; set it to `router.AdminSessionOrAPI` when supplying the
administrator API-token-or-session middleware. Only these two modes are valid;
the declaration must match the actual enforcing adapter.

Both public and administrative routes use the shared route registry. Configure
any policy authorizer before attachment, then check `ValidateRoutePolicies()`
after all routes are attached and refuse startup on error. Missing admin
middleware or an invalid mode fails validation; the runtime backstop returns
`ROUTE_CONFIGURATION` (503) for every descriptor in that router, including the
public sitemap. Public does not mean exempt from configuration validation.
Existing methods and first-match OPTIONS handling are preserved. See the
[router contract](../router/README.md#declarative-route-policies).

Generated files are restricted to safe relative paths below a configured
writable root. An embedded static sitemap can be supplied as a read fallback.
Protected route groups such as `/admin`, `/api`, `/app`, `/auth`, `/offline`,
`/portal`, and `/settings` are excluded from generated XML.

## Public sitemap source

The default `PublicSitemapModeFile` preserves existing behavior: public requests
read the configured local file, with embedded fallback when available. The safe
`path` query selector remains supported. Creating database entries alone does not
regenerate or publish this file.

Hosts can opt into live shared storage instead:

```go
service := seo.NewService(&seo.NewServiceRequest{
    SitemapRepository: repository,
    FrontendDomain:    "https://example.com",
    PublicSitemapMode: seo.PublicSitemapModeDatabase,
    // Optional host function; omit when stored entries are already public.
    PublicSitemapItemsFilter: filterPublicURLs,
})
handler := seo.NewHandler(service, validator)
```

Database mode reads the complete current sitemap inventory for each public
request, optionally filters it, and uses the same XML renderer. It ignores the
file selector and never falls back to a local or embedded sitemap on failure.
Different service instances using the same repository observe current storage
without synchronizing generated files. No database entry is created, updated or
removed by a public request.

`PublicSitemapItemsFilter` has the signature
`func(context.Context, []SitemapItem) ([]SitemapItem, error)`. The host owns
publication checks, application-specific private routes and any build-mode
allowlist. The callback receives a copy of the repository slice and must be safe
for concurrent requests. An empty result intentionally produces an empty
sitemap; an error fails the request closed. Set service configuration before
serving requests. Without a callback, all valid stored entries are eligible,
subject to the renderer's standard exclusions and duplicate removal.

Database responses use `Cache-Control: no-store`. Storage, filter, rendering and
invalid-mode failures return a generic `SEO0-011` HTTP 503 response, without
diagnostic causes. Direct service callers can inspect wrapped causes with
`errors.Is`. When a host installs response caching, wrap that middleware with
`seo.NeverCachePublicSitemap` before registration so existing cache hits cannot
intercept the public route. Other routes keep their original caching behavior;
hosts must also configure upstream caches to honor `no-store`.

`GetPublicSitemap(ctx, request)` is an additive service capability. The standard
handler detects it without extending the original required service interface,
so existing custom service implementations retain their file behavior.
Administrator `GenerateSitemap` and `DownloadSitemapByPath` operations remain
explicit exports in either mode. They do not apply the public host filter, and
file export failures do not change the source of database-backed public output.

## Migrations

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
