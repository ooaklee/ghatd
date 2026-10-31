# SEO Sitemap

The `external/seo` package provides reusable sitemap persistence, management,
generation, and HTTP routes for GHATD host applications.

Sitemap items are stored in MongoDB and contain a URI, last-modified timestamp,
priority, change frequency, optional publication time, and creator metadata.
Relative URIs are rendered against the frontend domain configured by the host
application. Generated XML is sorted and de-duplicated by absolute URL.

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

## Independent publication dates

A sitemap item's optional `PublishedAt` (`published_at` in JSON/BSON) controls
when that SEO entry is eligible for generated XML. It is independent of
`LastMod`, `CreatedAt` and CMS publication. Missing or empty values retain legacy
immediate eligibility. A future value excludes the entry until that instant;
exact equality is eligible. Administrative lists retain scheduled entries.

Create and update requests use `*string` so omitted values and explicit clears
remain distinct. Batch imports use the create-request shape:

| Request value | New item | Update or overriding batch |
| --- | --- | --- |
| Omitted or JSON `null` | Immediately eligible | Preserve the stored date |
| Empty or whitespace-only string | Immediately eligible | Clear the gate; immediately eligible |
| Valid timestamp | Eligible when due | Replace the date |
| Invalid nonempty string | Reject before writing | Reject before writing |

Non-overriding imports preserve existing entries in full, including their
publication dates. The service validates every batch item before starting
writes; this is validation preflight, not a transaction across the batch.
Storage failures can still leave a partially completed batch.

Accepted dates use strict RFC3339/RFC3339Nano with `Z` or a numeric offset, or
the platform's zone-less UTC format, for example `2027-01-05T09:00:00.123456789`.
Seconds are required. Surrounding whitespace is trimmed; numeric offsets need
hours from 00–23 and minutes from 00–59. Fractional seconds have at most nine
digits: excess precision is rejected rather than truncated. Valid request dates
are normalized to UTC while preserving the instant to nanosecond precision.
Invalid calendar dates, year zero, an out-of-range UTC result and the zero
instant are rejected with `ErrSitemapItemInvalidPublishedAt` (`SEO0-012`, HTTP 400
for administrative requests).

`GenerateXML` applies this gate using one evaluation instant for the entire
output. `NewServiceRequest.Now` injects the clock; nil uses `time.Now`.
Administrative generation and live public delivery both use this renderer.
A malformed nonempty stored publication value encountered by the generator
fails the entire output; no partial XML is returned and a previously saved
snapshot is not overwritten. Live delivery maps that failure to the generic
503 described below. Standard private-route exclusions, host filtering and
URL deduplication continue to apply.

Ingestion may copy a CMS URL and publication time once. This does not establish
ownership or an association: no URL matching, CMS lookup or automatic lifecycle
synchronization is performed by SEO. Later source schedule, slug, visibility,
unpublish or deletion changes require an explicit corresponding SEO action.
Clearing the SEO date makes the entry immediately eligible; it does not unpublish
content or remove the sitemap entry. Delete the SEO row to remove it, or set a
future date to defer inclusion. Scheduling an SEO entry does not publish content
at its URL.

### Repository compatibility

The original `SitemapRepository` interface is unchanged. Repositories that
support publication-aware writes additionally implement
`SitemapPublicationRepository`:

```go
UpdateSitemapItemWithPublication(context.Context, *SitemapItem, *string) (*SitemapItem, error)
UpsertSitemapItemWithPublication(context.Context, *SitemapItem, *string) (*SitemapItem, bool, error)
```

Each method must persist metadata and the requested publication change in one
atomic write and return the stored record. A nil publication pointer must omit
`published_at` from the write, ignoring a stale `item.PublishedAt`, so a concurrent
schedule change survives. A non-nil pointer writes the supplied normalized value;
a pointer to an empty string explicitly clears the gate. The upsert boolean
reports whether a new document was created. The built-in Mongo repository
implements this capability.

Older custom adapters remain source compatible and support CRUD for legacy
unscheduled entries. Explicit schedule/clear requests, metadata updates to known
scheduled entries and overriding imports of those entries are rejected before
writing with
`ErrSitemapItemPublicationScheduleUnsupported` (`SEO0-013`, HTTP 500), rather
than silently losing the schedule. Hosts with schedule-aware writers must expose
both capability methods, including through repository wrappers. Reads, deletion
and non-overriding skips retain their existing behavior.

The concrete Mongo repository's legacy `UpdateSitemapItemByURI` and
`UpsertSitemapItemByURI` methods treat an empty `item.PublishedAt` as omission,
preserving an existing date. Use the publication-aware methods with an explicit
empty pointer to clear it deliberately. A nonempty value on the legacy methods
expresses a publication write; direct repository callers remain responsible for
supplying valid values.

## Public sitemap source

The default `PublicSitemapModeFile` preserves existing behavior: public requests
read the configured local file, with embedded fallback when available. The safe
`path` query selector remains supported. Creating database entries alone does not
regenerate or publish this file. A saved file is a snapshot: an entry becoming
due, being rescheduled or being deleted does not refresh it automatically.

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
application-specific crawler rules, private routes and any build-mode allowlist.
The shared renderer owns each SEO entry's publication gate. The callback receives
a copy of the repository slice and must be safe for concurrent requests. An empty result intentionally produces an empty
sitemap; an error fails the request closed. Set service configuration before
serving requests. Without a callback, valid stored entries become eligible when
their own publication dates allow, subject to the renderer's standard exclusions
and duplicate removal.

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
explicit exports in either mode. Generation applies the shared publication gate
at the time it runs, but neither operation applies the public host filter. A
download reads the saved snapshot without reevaluating dates; regenerate it when
a current export is required. File export failures do not change the source of
database-backed public output.

## Bounded publication inventory

`Repository.ListIndexNowSitemapItems` and the service capability of the same name
provide read-only string-ID keyset pages for shared publication reconciliation.
Requests select `AfterID` and a `Limit` from one to 100; `NextID` is empty only
when the read observed no next record. One lookahead record beyond the page is
allowed. Invalid/non-string IDs fail instead of silently shortening the scan.
These are internal trusted-host APIs, with no new HTTP route or publication
write. Services require the optional `SitemapPublicationSource` adapter capability
and never fall back to an unbounded list. Existing CRUD interfaces are unchanged.

`SitemapItem.PublicationTime()` exposes the renderer's strict parser: blank means
no gate and malformed nonempty values fail closed. The combined
[Content Manager reconciliation](../contentmanager/README.md#scheduled-cms-and-seo-eligibility)
merges this gate with CMS publication and host availability, also recognizing
valid creation/update transition times. It preserves independently managed
SEO rows and supports static routes. Inventory reads are not transactional
snapshots, and deleting an SEO row needs a separate removal notification.

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
