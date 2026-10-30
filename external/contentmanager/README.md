# Content Manager

The `contentmanager` package is the HTTP orchestration layer for CMS-style content in this project. It sits on top of the `post` package and exposes endpoints for changelog entries, glossary items, FAQ items, articles, and latest-post overviews.

This guide focuses on a practical quick start, based on the current service wiring in this repository.

## Architecture

The package follows the standard route -> handler -> service pattern used across this codebase:

1. **Routes (`routes.go`)**: Defines the `/api/v1/cms` endpoints and middleware requirements.
2. **Handler (`handler.go`)**: Maps HTTP requests to service requests and writes structured responses.
3. **Fender (`fender.go`)**: Decodes body/query/path values and binds `ActorID` from verified middleware context, separately from post IDs and stored authors.
4. **Service (`service.go`)**: Orchestrates post retrieval/mutation and applies access rules (admin-only write, published-only public read).

## Quick Start

### 1. Wire Dependencies in the Server

Create the `post` and `contentmanager` services, then attach routes.

```go
import (
    "github.com/ooaklee/ghatd/external/contentmanager"
    "github.com/ooaklee/ghatd/external/post"
    userV2 "github.com/ooaklee/ghatd/external/user/v2"
)

// repository and user service already initialised earlier
postRepository := post.NewRepository(coreRepository)
postService := post.NewService(postRepository, post.DefaultValidPostTags)
contentManagerService := contentmanager.NewService(postService, userV2Service)

contentManagerHandler := contentmanager.NewHandler(
    contentManagerService,
    appValidator,
    post.PostErrorMap,
    toolbox.ToolboxErrorMap,
    userV2.UserErrorMap,
)

contentmanager.AttachRoutes(&contentmanager.AttachRoutesRequest{
    Router:                                 httpRouter,
    Handler:                                contentManagerHandler,
    MiddlewareAdminApiTokenOrJwtRequired:   middlewareAdminApiTokenOrJwtRequired,
    RateLimitOrActiveMiddleware:            middlewareRateLimitOrActive,
    MiddlewareValidApiTokenOrJWTMiddleware: middlewareActiveAValidApiTokenOrJwt,
})
```

### 2. Endpoint Groups and Access

- **Admin write endpoints**:
  - `POST /api/v1/cms/posts`
  - `PATCH /api/v1/cms/posts/{postId}`
  - `DELETE /api/v1/cms/posts/{postId}`
  - `PATCH /api/v1/cms/posts/{postId}/restore`
- **Open/read endpoints (rate-limited or active user)**:
  - `GET /api/v1/cms/changelog`
  - `GET /api/v1/cms/changelog/{urlFriendlyId}`
  - `GET /api/v1/cms/glossary`
  - `GET /api/v1/cms/faq`
  - `GET /api/v1/cms/articles`
  - `GET /api/v1/cms/articles/{urlFriendlyId}`
  - `GET /api/v1/cms/latest`

- **Administrator ingestion sources** (`AdminSessionOrAPI`, read-only):
  - `GET` or `POST /api/v1/cms/seo/posts/articles/sitemap-items`
  - `GET` or `POST /api/v1/cms/seo/posts/changelogs/sitemap-items`

### Changelog sitemap candidates

The changelog source returns candidates for the separate SEO batch-ingestion
operation; it does not write sitemap records or generate a public sitemap.
`GetChangelogSitemapItemsRequest` and `GetChangelogSitemapItemsResponse` alias the
article request/response types so existing ingestion clients can reuse their
decoder. The wire counters remain `processed_articles`, `seedable_articles` and
`skipped_missing_url_friendly_id`; they count changelog sources for this route.

```http
GET /api/v1/cms/seo/posts/changelogs/sitemap-items?per_page=100&uri_path_prefix=/whats-new/
X-Api-Token: <admin-api-token>
```

The default URI prefix is `/whats-new/`; hosts can supply a clean absolute path
such as `/updates/` or `/news/releases/`. A trailing slash is optional. URL
schemes/hosts, query strings, fragments, backslashes, traversal, escaped aliases
and repeated separators are rejected. Full `changelog-` slugs are retained. Only canonical
lowercase, hyphen-separated slugs are returned. Invalid or missing slugs count
toward `skipped_missing_url_friendly_id`.

Publication and optional `created_at_from` filters compare parsed instants,
accepting platform UTC timestamps and RFC3339 timestamps with offsets. Drafts,
future-dated, soft-deleted, restricted-visibility, zero-date and malformed-date
posts are excluded. The source deliberately avoids lexical repository date
filters. `limit` counts only eligible public source posts, including those later
skipped for invalid slugs. Zero means no limit; negative limits are invalid.
`per_page` accepts 1–100, with zero using 100. Page size stays fixed across the
scan, and the post repository uses an ID tiebreaker for equal timestamps.
Concurrent content edits can still change a paginated catalogue; run ingestion
against stable content when exact batch completeness is required.

The HTTP mapper preserves query-over-body precedence, binds `ActorID` from
verified context, and rejects malformed or oversized bodies above 8 KiB. The
route uses the existing administrator authentication adapter. Trusted in-process
jobs can call the service without a viewer; `ActorID` is never a credential.

`ChangelogSitemapHandler` and `ChangelogSitemapService` are optional capabilities.
The built-in handler and service implement both. Custom handlers without the
new capability retain their existing route inventory; the built-in handler with
a custom service lacking the capability returns unavailable for the new route.
Existing article ingestion behaviour is unchanged.

## Request Patterns

### Create a Post (Admin)

```bash
curl -X POST http://localhost:8080/api/v1/cms/posts \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer <admin-token>" \
  -d '{
    "title": "Launch of the blog",
    "type": "article",
    "text": "# Hello\nThis is our first post",
    "text_format": "markdown",
    "header_image": "https://example.com/blog-cover.jpg",
    "tags": ["announcement"],
    "publish_now": true
  }'
```

### Query the Latest Notification Feed

Use the `kinds` and `limit` query parameters to retrieve a mixed feed. Supported
kinds include `post_article`, `post_changelog`, `post_faq`, and
`post_glossary`.

```bash
curl "http://localhost:8080/api/v1/cms/latest?kinds=post_article,post_changelog&limit=5"
```

### Paginated Content Queries

The changelog/glossary/faq/articles endpoints support common query params from `post.GetPostsRequest`, for example:

```bash
curl "http://localhost:8080/api/v1/cms/changelog?per_page=10&page=1&meta=true&with_tags=bug-fix&without_tags=announcement"
```

## Behaviour Rules to Know

### ActorID migration

This is a breaking Go source change: the eight reader request fields formerly
named `UserId` now use `ActorID`. The create, update, delete and restore wrappers
inherit the same rename from their embedded `post` requests. There are no legacy
caller aliases. Stored `CreatedByUserId`, `PublishedByUserId`, `UpdatedByUserId`
and `DeletedByUserId` fields are unchanged, as are post target IDs and routes.

Actor fields have `json:"-"` and no query or path tag. Private mutation mappers
require both a verified authentication flag and a nonempty context ID; public
read mappers use an empty actor for anonymous requests. An optional-auth
rate-limit placeholder never establishes viewer authority. Custom HTTP adapters
must publish verified state, not just a context ID. See the
[request identity guide](../../docs/how-to/request-identity.md#transport-binding).

Services use the explicit request actor. They no longer silently substitute a
different viewer from context. When identity evidence is present in context it
must match: conflicting or anonymous evidence denies mutations and reduces
reads to public-only content. Trusted in-process callers may use an ordinary
context, but must establish authority before assigning `ActorID`; IDs are not
credentials. Mutation authority is resolved against the current actor account,
never a post's author. Empty actors are forbidden; missing or inconsistent
dependency results map to `CM00-02` (503). Native dependency errors keep their
shared mappings and host overrides. Unavailable optional viewer authority falls
back to the public projection without logging private lookup diagnostics.

Public list restrictions operate on a request-local copy, clearing contradictory
visibility flags. Public single-item reads also exclude soft-deleted posts.
Latest-post filtering copies the adapter's overview slice. These changes do not
introduce a new publication scheduling policy. Article sitemap jobs retain their
existing published/non-deleted repository filters. The changelog source applies
parsed-time and public-visibility eligibility after paging so offset-formatted
timestamps are compared correctly. Direct jobs need no viewer, while ingestion
HTTP routes remain administrator-protected.

HTTP updates accept individual editable fields, not an embedded `post` full
replacement. Previously that replacement could override the URI-selected post
and server-owned attribution. Use the field-based update payload over HTTP.
Trusted internal replacements remain available in `post.UpdatePostRequest`,
but their ID must match `PostId` when both are supplied.

The common notification request's `UserID` remains a recipient field, not an
actor. This manager's post feed is global public content and does not use that
field to authorize access to a private notification inbox.

- Non-admin users are blocked from create/update/delete/restore actions.
- Public readers only receive published items.
- Single-item endpoints enforce URL prefix intent:
  - changelog IDs should start with `changelog-`
  - article IDs should start with `article-`
- If `published_as` is not explicitly set, the service resolves a fallback author display value.

## Frontend Integration Notes

If you are implementing a web client for this API:

- Use a dedicated CMS client with base URL `/api/v1/cms`.
- Keep dedicated methods for:
  - `getLatestPostsByType`
  - `getChangelogItems` and `getChangelogItemByUrlFriendlyId`
  - `getGlossaryItems`
  - `getFaqItems`
  - `getArticles` and `getArticleItemByUrlFriendlyId`
- For feed cards/home widgets, use `/latest` and consume post overviews.

## Related Package Docs

- [Post Getting Started](../post/README.md)
- [Router Getting Started](../router/README.md)

## Optional IndexNow notifications

Existing `NewService(postService, userService)` callers retain their mutation
I/O. Opt in with additive service options after constructing an
[IndexNow service](../indexnow/README.md):

```go
cms := contentmanager.NewService(postService, userService,
    contentmanager.WithIndexNow(indexNowService, contentmanager.IndexNowConfig{
        CanonicalOrigin: "https://example.com",
        LookupPost:      postRepository.GetPostById,
        URLForPost: func(item post.Post) string {
            switch item.Type {
            case post.PostTypeArticle:
                return "/blog/" + strings.TrimPrefix(item.UrlFriendlyId, "article-")
            case post.PostTypeChangelog:
                return "/whats-new/" + item.UrlFriendlyId
            default:
                return "" // The host has not exposed a public URL for this type.
            }
        },
        // Optional, concurrency-safe build/crawler/independent SEO policy.
        // Returning false omits a URL; an error records a safe warning.
        AllowURL: allowCanonicalURL,
    }),
)
```

The host supplies the fixed canonical origin, public type-to-path mapping and
persisted lookup. Invalid wiring fails closed for notifications with a
`configuration_invalid` receipt. A nil notifier disables the option. Configure
services before concurrent use. URL mapping accepts clean site-relative paths,
rejects queries/fragments, encoded aliases, traversal and standard private
surfaces, then resolves against the configured origin. No request `Host` value,
caller-selected author or actor is used to infer a URL.

An authorized, successfully acknowledged create/update/delete/restore can send
one notification containing its public URL changes. Updates and deletions first
snapshot the persisted target by ID after authorization. A pre-write public URL
is retained for deletion, unpublication, rescheduling or slug changes even when
it no longer passes the current public sitemap policy. Slug changes notify both
old and new URLs, deduplicated within the request. A host remains responsible
for an actual redirect when old slugs move. A restored draft is not announced
until explicitly published. Future, malformed, zero-date, deleted, restricted
and unmapped posts do not become new public notifications.

`AllowURL` runs on the previous URL before mutation and on the new URL after
mutation. It can reuse host build or independent SEO policy; it must not rewrite
SEO storage or depend on request-supplied identities. SEO publication dates,
modification dates and editor overrides remain independently managed. CMS
notifications do not ingest or delete sitemap rows. Public sitemap eligibility
and removed-URL notification eligibility have different purposes.

A notification failure never turns an acknowledged content mutation into an
apparent failed write. In-process mutation responses have an optional `IndexNow`
receipt: `skipped`, `acknowledged` (HTTP 200), `pending` (HTTP 202 ownership
validation) or `failed`. The HTTP handlers preserve their existing bodies and
expose `X-IndexNow-Status` plus a bounded `X-IndexNow-Warning` category when needed.
No raw dependency errors, ownership keys or transport diagnostics are included.
The IndexNow package owns persistent attempt history. A received notification
whose final audit update fails retains its acknowledged/pending status with an
`audit_unavailable` warning, so a storage problem is not mistaken for proof that
the search engine received nothing. Neither a receipt nor a
sitemap entry guarantees crawling or indexing.

A failed prior-state lookup does not prevent the authorized content write. Its
receipt warns `snapshot_unavailable`; a valid new URL can still be notified,
but the missing old URL requires an explicit operator notification. Notification
calls use the request context, so cancellation may prevent delivery after a
successful write. There is no automatic retry, transactional outbox or atomic
commit across content storage and the remote endpoint. Invoke these mutations
after any host-managed transaction boundary; this service has no transaction
commit callback. Concurrent edits can make a snapshot stale. Hosts requiring
strong delivery guarantees should introduce their own transaction-aware outbox
and explicit replay policy rather than treating a failed receipt as a failed
content mutation.

### Scheduled publication checkpoints

A future publication has no write at the instant it becomes due. A trusted host
job must call `ReconcileIndexNowPublications` explicitly:

```go
result, err := cms.ReconcileIndexNowPublications(ctx,
    &contentmanager.ReconcileIndexNowPublicationsRequest{
        After:   lastSuccessfulCheckpoint,
        Through: runStartedAt,
        Limit:   10000,
    })
if err == nil && result.Complete {
    // Persist result.Checkpoint in the host's durable job state.
}
```

The `(After, Through]` window must be explicit, nonzero, no longer than 24 hours
and not in the future. Set the initial checkpoint when enabling notifications;
do not backfill unchanged historical content. `Limit` bounds catalogue records
inspected (maximum/default 10,000). The scan keeps a fixed page size of 100,
compares publication instants rather than lexical timestamp filters, and
completes all reads before submitting a deduplicated batch. An exhausted bound,
source/policy failure or failed submission leaves `Complete` false and the
checkpoint unchanged. Increase the explicit limit within its bound or provide
a narrower host-owned source/outbox if the catalogue exceeds this API's scope.
Replaying a window may notify URLs again; multiple processes need host-owned
coordination. No distributed exactly-once scheduler is provided, and concurrent
catalogue edits can change pagination. Run against stable source state when
exact scan completeness is required. A pending HTTP 202 receipt confirms
notification receipt with ownership validation pending, not completed indexing.

## Public publication eligibility

Public single-article/changelog reads and all four full-post lists recheck that
the returned post is due, non-deleted and unrestricted. Malformed/zero publication
values fail closed even when a custom domain adapter returns them. Verified
administrator previews retain unpublished and deleted records. Latest overview
and notification feeds also reject future or malformed publication timestamps.
`IsPublicPostAt` exposes the same predicate to host article renderers and sitemap
filters. `WithPublicationClock` can inject one shared clock for deterministic
host behavior. Standard stored platform UTC timestamps and RFC3339 offsets are
supported; a date without a time is not a publication timestamp. Source totals
remain source totals if defensive filtering shortens a returned page, because
one page cannot establish a corrected global count.

These rules provide publication eligibility, not HTML rendering. Hosts should
serve real article content, canonical metadata, standard links and meaningful
404/503 responses before JavaScript runs, use the same output for readers and
crawlers, bypass stale response caches, and check current public CMS availability
when filtering independently stored sitemap rows. See the
[cross-package workflow](../../docs/how-to/configure-indexnow-seo.md).

An independent SEO gate can become due after its post's CMS publication window
has already been reconciled. That later SEO transition requires a separate host
notification or SEO eligibility job using the IndexNow service. CMS publication
reconciliation only scans CMS publication dates; it does not monitor later SEO
schedule changes or replay old content automatically.
