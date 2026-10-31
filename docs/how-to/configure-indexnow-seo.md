---
title: Configure IndexNow and SEO
description: Compose content lifecycle notifications, live sitemaps and crawlable pages in a GHATD host application
---

# Configure IndexNow and SEO

Compose three responsibilities around one canonical public URL:

| Responsibility | Owner | Canonical guide |
| --- | --- | --- |
| Create, update and remove public content | Content Manager and the host's public renderer | [Content Manager](../../external/contentmanager/README.md) |
| Publish the current crawlable URL inventory | SEO live sitemap and the host's visibility filter | [SEO](../../external/seo/README.md) |
| Notify participating engines about URL changes | IndexNow | [IndexNow](../../external/indexnow/README.md) |

IndexNow complements discoverable pages and sitemaps. A 200 acknowledges a
notification and a 202 means key verification is pending; neither proves
crawling, indexing or ranking. See the
[IndexNow protocol](https://www.indexnow.org/documentation) and
[Bing setup guide](https://www.bing.com/indexnow/getstarted).

## Establish the public URL contract

Use the externally visible canonical origin consistently for IndexNow
`Config.SiteURL`, SEO `FrontendDomain`, public page canonicals and any host URL
resolver. Do not derive the canonical origin from an untrusted request `Host`
header. A notification for a renamed or removed page must identify the old
public URL so an engine can discover its redirect or removal.

Serve the page's main content, title, description and matching canonical URL in
the initial HTML. Keep metadata and structured data consistent with what users
can actually read. Missing or withdrawn pages need real HTTP 404 or 410
responses, and intentional slug moves need redirects. Returning the application
shell with HTTP 200 for every missing route creates soft 404s. These rendering
concerns remain the host's responsibility; notifications cannot repair them.
See [Google's JavaScript SEO guidance](https://developers.google.com/search/docs/crawling-indexing/javascript/javascript-seo-basics).

## Register ownership verification

Construct `indexnow.NewService(apiKey, config)` with the deployment's key and
canonical `SiteURL`, then register `indexnow.NewHandler(service)` on the root
Gorilla router with `RegisterRoutes`. The key-file GET route must be publicly
reachable at its configured canonical location. Do not put it behind the CMS's
administrator middleware or an SPA fallback.

Use the default root key-file location unless the host needs a custom location.
A nested key file limits the URLs that the key can cover; keep its path scope
aligned with your public URL resolver. Configure the endpoint, key and
key-file route together before serving traffic. See the package guide for
configuration validation and custom key-location rules.

The public ownership file does not authorize arbitrary callers to trigger your
submission service. Keep administrative submission or replay controls protected
by the host's verified administrator session or API-token policy. Bind manager
`ActorID` from that verified context and keep it separate from the selected
content or author IDs.

## Compose Content Manager with IndexNow

With the host's existing post service, post repository and user service, add the
optional constructor setting:

```go
notifications, err := indexnow.NewService(apiKey, indexnow.Config{
    SiteURL:    publicOrigin,
    Repository: attemptRepository,
})
if err != nil {
    return err
}
indexnow.NewHandler(notifications).RegisterRoutes(rootRouter)

cms := contentmanager.NewService(postService, userService,
    contentmanager.WithIndexNow(notifications, contentmanager.IndexNowConfig{
        CanonicalOrigin: publicOrigin,
        LookupPost:      postRepository.GetPostById,
        URLForPost: func(item post.Post) string {
            if item.Type != post.PostTypeArticle ||
                !strings.HasPrefix(item.UrlFriendlyId, "article-") {
                return ""
            }
            return "/blog/" + strings.TrimPrefix(item.UrlFriendlyId, "article-")
        },
    }),
)
```

Import `strings` and the `indexnow`, `contentmanager` and `post` packages for this
example. The article-only resolver deliberately maps the host's existing public
route; add other supported post types only when the host exposes their public
pages. The manager validates returned paths and applies publication eligibility.
`LookupPost` reads persisted state before update or deletion so an old slug or
withdrawn URL remains available for notification. A client-supplied replacement
post is not authoritative history. Optional `AllowURL` applies host build or
crawler policy before selecting notification URLs.

Existing `NewService(postService, userService)` calls remain valid and do not
send notifications. With the option enabled, content mutation still returns its
committed result when a subsequent notification fails. Service responses expose
the separate `IndexNow` receipt; HTTP mutation responses preserve their existing
payload and expose `X-IndexNow-Status` and optional `X-IndexNow-Warning` headers.
Inspect these warnings and the attempt repository instead of retrying the entire
content mutation. An invalid optional configuration skips notification with a
safe warning; the host should verify wiring before accepting traffic.

## Keep the live sitemap accurate

Configure SEO with `PublicSitemapModeDatabase` when public sitemap responses
must reflect current shared storage. Use `PublicSitemapItemsFilter` for the
host's public-route allowlist and content visibility rules. Protect the public
sitemap from response caching with `seo.NeverCachePublicSitemap`, and make
upstream caches honor `Cache-Control: no-store`.

SEO entries' `PublishedAt` values are independent publication gates. Copying a
CMS schedule into an SEO entry during ingestion does not create a lifecycle
association. Subsequent slug, schedule, visibility, unpublish and deletion
changes require corresponding SEO actions or a live host filter that excludes
stale rows. A file-based sitemap remains a snapshot until explicitly
regenerated, including when a scheduled entry becomes due. CMS publication time,
SEO publication time and last-modified time express different facts; do not
replace one with another to make dates appear current. Review stale stored rows
and correct inaccurate dates as an explicit editorial or operator action.

Only include current public canonical URLs. Drafts, future publications,
restricted content and deleted posts must remain absent. Keep a changed page's
last-modified time factual. An IndexNow notification for a removed URL can be
correct while that URL is intentionally absent from the sitemap.

## Notify the correct content transition

Configure the host's public URL and visibility rules before enabling CMS
notifications. Apply one rule for whether a post is publicly reachable and a
separate rule for which prior URL needs a change notification:

| Content transition | URL notification to preserve | Current sitemap behavior |
| --- | --- | --- |
| Publish or update a public post | Its current canonical URL | Include its current eligible URL |
| Change a public slug | Both the previous and current canonical URLs | Retain only the current URL |
| Unpublish, delete, restrict or reschedule a public post | Its previous public URL | Exclude the withdrawn URL |
| Create a draft or future publication | No premature notification | Exclude until publicly eligible |
| Restore deleted content | Reevaluate publication; restoration alone is insufficient | Include only when public again |

A removal notification must bypass the predicate that selects current public
sitemap entries. Otherwise the content's disappearance also prevents engines
from being notified about it. Missing or malformed publication dates must not
accidentally make a draft public. Distinguish a genuinely absent public post
(404 or 410) from a failing CMS dependency (503); outages are not evidence of
deletion.

Do not send the entire historical inventory when enabling notifications. Begin
with content changes since activation, and use deliberate, scoped operator
actions when a reconciliation is necessary. Submit to one participating
IndexNow endpoint; the protocol shares notifications with other participants.
Future content becomes due without another CMS write. For automatic operation,
compose `cms.ReconcileIndexNowEligibility` with the optional shared IndexNow
runner. Supply the native bounded `PublicationSource` and `SitemapSource`, plus
the host's `EligibleURL(ctx, url, at)` callback. This checks current public
availability for dynamic routes at the completed window boundary; a missing
target is ineligible and a source outage is an error. Keep this separate from
the mutation `AllowURL` policy so removal notifications remain possible.

The combined scan covers both CMS and independently scheduled SEO entries. A
URL represented by both sources becomes eligible after the later applicable
publication gate; valid creation/update timestamps also identify cleared or
retroactively edited schedules. Static SEO-only routes remain supported when
the host policy permits them. Neither source's dates or metadata are rewritten.

```go
runner, err := indexnow.NewRunner(indexnow.RunnerConfig{
    Scope: publicationPolicySHA256, // stable lowercase hash, not the API key
    Interval: 5 * time.Minute,
    ScanLimit: 10000,
}, indexnow.NewJobRepository(store), func(ctx context.Context, after, through time.Time, limit int) (bool, error) {
    result, err := cms.ReconcileIndexNowEligibility(ctx,
        &contentmanager.ReconcileIndexNowEligibilityRequest{
            After: after, Through: through, Limit: limit,
        })
    return result != nil && result.Complete, err
})
if err != nil {
    return err
}
return runner.Run(ctx) // separately operated worker with bounded shutdown
```

The host supplies the managed store and a stable scope hash of the application,
canonical origin and compiled publication policy. Do not include the build
commit or API key: ordinary rollouts and key rotation must resume existing
progress. The first durable activation seeds its checkpoint at activation time,
without historical submissions. Later passes resume Mongo state, acquire a
server-time lease, scan one consecutive window of at most 24 hours, and advance
only after a complete, acknowledged result. The combined scan limit counts CMS
and SEO records together, up to 10,000, plus bounded native page lookahead.

Lease tokens fence completion and release across replicas and rolling updates.
Failures retain the checkpoint; retry deferrals respect endpoint delays.
Mongo state contains hashed scope identities, tokens and timestamps, not keys or
URL lists. Keep it private and preserve it during rollback. Source reads are not
cross-source transactional snapshots, and a crash or uncertain external/audit
completion can cause replay. This is not an exactly-once or transactional outbox
guarantee.

## Keep audit and retry expectations explicit

Provide an `AttemptRepository` through IndexNow `Config.Repository` to retain
attempts. Leaving it nil disables package persistence. The service records an
in-flight attempt before sending; failure to record prevents that submission.
Completion records hold the protocol outcome, status and retryability rather
than the key or raw response body. The repository implementation owns storage
durability, access control and retention.

The HTTP call and its final audit write are not one transaction. A completion
write can fail after an engine has received the notification, or a process can
stop between those actions. Keep such attempts available for operator review;
blindly treating them as unsent can replay already received notifications.
Likewise, a successfully committed content change cannot be undone by an engine
failure. Surface and reconcile delivery failures separately from content state.

The protocol service performs one dispatch and adds no automatic retries or
deduplication. The separately opted-in reconciliation runner retries incomplete
windows after its interval and any durable `RetryAfter` deferral; it does not
replay arbitrary CMS mutation receipts or replace an outbox. Correct
invalid requests or ownership failures before replaying them. Use a durable
outbox when notification delivery must survive a process exit after content
commit; attempt history alone is not that outbox.

## Verify delivery and discovery separately

Use the [local review example](../../examples/indexnow/README.md) to inspect
single and batch protocol requests without contacting public engines. In a
deployed host, verify that the canonical ownership URL returns the correct key,
the changed page returns the expected HTML and status, and the live sitemap
agrees with current visibility. Then inspect notification receipts and audit
records. Receipt storage is evidence of submission attempts, not evidence of
indexing. Use Bing Webmaster Tools and Google Search Console for live search
engine observations. Tests can verify request construction, public eligibility,
HTML and status contracts; they cannot certify engine indexing, editorial
accuracy, original content or meaningful image descriptions. Historic reports
alone do not establish why an engine chose a canonical or whether a site has
been penalized.

Keep Google discovery through crawlable internal links and sitemaps submitted
with Search Console. Google's Indexing API is restricted to eligible job
postings and livestream pages; do not use it as a general-purpose notification
API for ordinary CMS pages. See the
[Google Indexing API scope](https://developers.google.com/search/apis/indexing-api/v3/using-api).

Use `ReconcileIndexNowEligibility` for both explicit windows and automatic
workers in every derived application that schedules CMS content or SEO entries.
It observes either source becoming due, including an older post whose independent
SEO gate became eligible later. SEO row deletion has no remaining source event
and still needs an explicit removal notification.
