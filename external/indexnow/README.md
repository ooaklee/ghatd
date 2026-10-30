# indexnow

Package `indexnow` submits URLs to IndexNow-compatible search engine
endpoints and optionally persists a submission history. The key verification endpoint is
served over HTTP; submissions are outbound-only.

This package **does not** guarantee indexing, does not submit to Google
(Google has no IndexNow support), and contains no queue, outbox, dedupe,
exactly-once delivery, or scheduler. Each `Submit*` call performs at most one
HTTP client dispatch, without an application retry loop.

## Configuration

```go
service, err := indexnow.NewService("your-key-8-to-128-chars", indexnow.Config{
    SiteURL:     "https://example.com",
    Endpoint:    "https://api.indexnow.org/indexnow", // optional; this is the default
    KeyLocation: "/your-key-file.txt",          // optional, defaults to /<key>.txt
    HTTPClient:  client,                        // optional
    Repository:  repo,                          // optional; nil disables audit
})
```

- **API key**: 8–128 ASCII letters, digits, or hyphens. The key is public
  verification material: it is served at the key location and sent to the
  endpoint, but it is never stored in attempt records and never appears in
  errors, logs, or audit payloads.
- **SiteURL**: strict canonical origin — absolute http(s), host required, and
  **no userinfo, query, fragment or non-root path** components.
- **Submitted URLs** must share the site's hostname. Nested paths are
  permitted. http/https URLs may be mixed on one hostname; a non-default
  explicit port isolates from other ports (and from the default port).
- **KeyLocation**: an absolute path starting with `/`, or an absolute same-host
  HTTP(S) URL. Paths use ASCII unreserved characters and `/` in canonical form.
  No escaped aliases, `?`/`#`, no
  `..` traversal, no wildcards. With a custom location, the absolute site URL
  of the key file travels as `keyLocation` in the GET query string (single
  URL) or the POST JSON body (batch).
  A file below the root covers only URLs in that directory; for example,
  `/catalog/key.txt` covers `/catalog/item` and excludes `/help/item`.
- **HTTPClient**: a caller-supplied client is shallow-copied, never mutated;
  the copy forces redirect-following off and uses the shorter of a positive
  client timeout and `Config.Timeout` (default 15 seconds). The original client stays
  owned by the caller.

## Key verification endpoint

The search engine must be able to fetch your key at the configured location.
Register it **before** any single-page application (SPA) rewrite rule so
IndexNow requests are served directly:

```go
router := mux.NewRouter()
indexnow.NewHandler(service).RegisterRoutes(router)
```

The handler serves exactly one GET/HEAD route pair at the key location and
answers with the key as UTF-8 plain text (HEAD: headers only). It exposes no
submission, history, or any other route, and no other path reflects the key.

## Submitting URLs

```go
// Single URL: one GET request.
resp, err := service.SubmitURL(ctx, "https://example.com/posts/42")

// Batch: one POST request with up to 10,000 URLs.
resp, err = service.SubmitURLs(ctx, &indexnow.SubmitURLsRequest{
    URLs: []string{
        "https://example.com/posts/42",
        "https://example.com/posts/43",
    },
})
```

Every response carries `Outcome`, `StatusCode`, `Success`, `AttemptID`,
`Retryable`, and `RetryAfter`. Status mapping:

| HTTP | Outcome | Meaning |
|------|---------|---------|
| 200  | `acknowledged` | Notification received; no indexing guarantee |
| 202  | `pending` | Notification received; key validation pending |
| 400  | `bad_request` | Invalid request body |
| 403  | `rejected` | Key not valid (e.g. key file unreachable) |
| 422  | `rejected` | URLs don't belong to this host / malformed |
| 429  | `rate_limited` | Too many requests; `RetryAfter` exposed for the caller |
| 5xx  | `server_error` | Endpoint error; `Retryable` |

`RetryAfter` is parsed from seconds or an HTTP-date. **The package never
automatically retries or re-sends** — retry decisions belong to the caller.

## Audit trail

Configure a repository to retain per-attempt records:

```go
// Production (MongoDB via the shared repository store):
mongoRepo := indexnow.NewRepository(store) // store: repository.MongoDbRepository

// Single-process / test option (no durability across restarts):
memRepo := indexnow.NewMemoryRepository()

service, err := indexnow.NewService(key, indexnow.Config{
    SiteURL:   "https://example.com",
    Repository: mongoRepo,
})
```

- A nil `Repository` disables persistence (submissions still work;
  `AttemptID` is blank). A typed nil (`Repository(nil)` from a concrete
  pointer type) is rejected as invalid configuration.
- **Record before send, finish after**: the in-flight record (outcome
  `in_flight`, zero `FinishedAt`) is written *before* the HTTP call, and the
  final state (status, outcome, retry fields) *after* the outcome is known.
- **Crash windows are honest**: a retained `in_flight` record means the
  outcome is *unknown* — the request may or may not have reached the
  endpoint. The package never automatically replays such attempts.
- Audit failures never masquerade as submission results: an `AuditError` with
  `Dispatched: false` means the pre-send record failed and nothing was sent;
  `Dispatched: true` means the HTTP call was attempted and completion persistence
  failed. A transport failure can leave delivery unknown. The audit error is
  joined (via `errors.Join`) with any protocol error
  so callers can distinguish and act.
- Records store only audit fields: ID, URLs, method, timestamps, status,
  outcome, success, retry fields. **Never stored or logged**: API key, key
  location, request payload, response body, or raw transport diagnostics.
  Submitted URLs may contain your own public query strings; treat the audit
  collection as operational data and apply retention accordingly (it is
  read-protected like other operational stores).

`GetAttempt(ctx, id)` and `ListAttempts(ctx, limit)` (start time descending,
ID ascending tiebreak, capped at 1000) read records. `FinishAttempt` is
idempotent for an identical replay and returns
`ErrAttemptAlreadyFinished` for a conflicting one in MongoDB
(`ErrMemoryRepositoryFinished` in memory); unknown IDs are never upserted.
Original URLs, method and start time cannot be replaced during completion.
MongoDB stores timestamps at millisecond precision. Its built-in `_id` index
provides identity uniqueness; hosts own timeline indexes and retention policy.

## Limitations

- No queue, outbox, dedupe, exactly-once semantics, or scheduling. The service
  makes one client dispatch; custom transports must uphold its timeout,
  credential and retry policy.
- IndexNow acceptance ≠ indexing. No search engine guarantees indexing or
  timing.
- Google is not reachable through IndexNow.
- The optional in-memory repository loses state on process restart.

See the [IndexNow protocol](https://www.indexnow.org/documentation),
[Bing setup guide](https://www.bing.com/indexnow/getstarted),
[CMS composition](../contentmanager/README.md) and
[cross-package guide](../../docs/how-to/configure-indexnow-seo.md).
