# Publication composition

This package composes an anonymous CMS reader, build-aware sitemap filtering and
IndexNow using the contentmanager, SEO, SPA and IndexNow owners. Supply a trusted
origin, build filesystem, public `ArticleSource`, bootstrap ID and presentation
settings. `AttachPublicArticlePages` registers the reader; `NewPages` exposes a
handler for independent assembly. Source calls do not carry an actor ID and
published state is checked again before rendering. A missing build policy retains
older build behavior; malformed policy and description inventories fail startup.

`SitemapFilter` combines canonical origin and build constraints with a host
`PathAllowed` predicate and current CMS publication. SEO retains its independent
row schedule. Bind a source through `SetArticles` before serving if assembly
requires the filter before the CMS. Pass the same route predicate to `PagesConfig`.
For Markdown heading IDs and standalone CTA markup, configure
`MarkdownPresentation` and `MarkdownSanitizer` together, including a restrictive
host-owned target predicate. A custom sanitizer must preserve the safety boundary.

IndexNow is disabled by default: `BuildIndexNow` does no work when disabled.
Apply the cache guards around the host cache middleware and register the key route
through the existing IndexNow service. `ReconcileWindow` validates manual windows.
`ReconciliationScope` preserves the version-1 durable identity using raw policy
bytes; changing component, environment, origin or policy intentionally changes it.
Construct a `ReconciliationRuntime` with an already-owned Mongo runtime, then a
`NewReconciliationRunner`; explicitly run and stop it outside the HTTP server.
The caller closes Mongo after the worker stops. The existing runner owns lease,
checkpoint, fencing and retry behavior and seeds its initial checkpoint at first
activation, without historical reconciliation. Construction does not notify a
search engine. A manual reconciliation must coordinate with the worker and retain
its own confirmed checkpoint.
