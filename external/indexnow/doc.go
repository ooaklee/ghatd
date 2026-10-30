// Package indexnow submits URLs to IndexNow-compatible search endpoints and
// keeps an auditable, privacy-scrubbed record of every submission.
//
// A Service is configured with an API key, a strict canonical site origin
// (no userinfo, query, or path), and an optional key location and repository.
// The Handler serves the public key verification endpoint (GET/HEAD only) at
// the configured path; submissions are outbound-only and are never accepted
// over HTTP.
//
// Persistence follows a record-before-send / finish-after protocol: the
// in-flight record is written before the HTTP call and completed after the
// outcome is known. A retained in-flight record means the outcome is unknown;
// the package performs no automatic replay. Audit failures surface as typed
// AuditError values (Dispatched distinguishes pre-send from post-dispatch
// failures) joined with any protocol error, never overriding it.
//
// Attempt records never contain the API key, key location, request payload,
// response body, or raw transport diagnostics. Production persistence uses
// NewRepository with the shared repository.MongoDbRepository store;
// NewMemoryRepository offers a non-durable single-process option, and a nil
// repository disables persistence entirely.
//
// See the package README for configuration details, the key verification
// endpoint, status mapping, and limitations (no queue, dedupe, exactly-once,
// scheduler, or indexing guarantee; Google is not reachable via IndexNow).
package indexnow
