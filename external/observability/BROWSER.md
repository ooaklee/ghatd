# Bounded browser trace intake

`NewBrowserTraceIntake` accepts a deliberately small browser span contract,
rebuilds trusted OTLP protobuf, and waits for a configured receiver to accept
each batch. It owns no SDK or global provider. Applications explicitly mount
the handler and implement consent-aware browser instrumentation; the package
does not enable browser collection automatically.

## Server setup

Create one intake per service process after the telemetry runtime is ready:

```go
intake, err := observability.NewBrowserTraceIntake(observability.BrowserTraceIntakeConfig{
    ServiceName:    "example-web",
    Namespace:      "example",
    Environment:    environment,
    Version:        gitCommit,
    AllowedOrigins: []string{"https://www.example.com"},
    RouteGroups:    []string{"home", "settings", "other"},
    APIGroups:      []string{"accounts", "other"},
    MeterProvider:  runtime.SDK().MeterProvider(),
})
if err != nil {
    return err
}

router.Handle("/api/v1/telemetry/browser/traces", intake)
handler := otelhttp.Wrap("example-api", runtime.Logger(), router)
```

The example assumes `router` is an `http.Handler` with exact-path registration.
Mount intake outside session authentication and response caching/compression,
within whole-router telemetry, request logging and recovery. Avoid path
redirects or catch-all route patterns. Every response-writer wrapper must expose
`Unwrap()` or support `SetReadDeadline` so `http.ResponseController` can reach
the real connection. The GHATD HTTP composition provides that support.

Stop and drain the HTTP server first, call `intake.Shutdown(ctx)`, then shut down
the telemetry runtime. Intake shutdown rejects new admissions, cancels active
reads and exports, closes owned transports and waits for admitted handlers,
including their final metric and response writes. Its caller supplies the
shutdown deadline; repeated calls are safe. It does not reset providers.

`ServiceName` is required. Namespace, environment and version are optional
server-owned strings, each at most 256 bytes without control characters. Browser
resources and scopes are discarded entirely. The output contains only these
configured identity attributes, with no inherited server or browser
`service.instance.id`, plus the fixed `github.com/ooaklee/ghatd/browser` scope.
`OTEL_RESOURCE_ATTRIBUTES` does not override this separate browser identity.

Supply one through 32 explicit canonical HTTP/HTTPS origins. Match is exact;
wildcards, credentials, paths, queries, fragments, invalid hosts, explicit
default ports and noncanonical port forms are rejected. IPv4 and IPv6 origins
are supported. Add a development frontend origin explicitly when its same-origin
proxy preserves `Origin` while rewriting `Host`.

The destination comes from standard `OTEL_EXPORTER_OTLP_*` settings, with
`OTEL_EXPORTER_OTLP_TRACES_*` taking the same precedence described in the
[configuration reference](CONFIGURATION.md). HTTP/protobuf and gRPC, configured
headers, TLS/mTLS, compression and export timeout are supported. Only the default
or explicit `OTEL_TRACES_EXPORTER=otlp` is supported by this intake. Its
constructor validates and snapshots trace transport configuration without
contacting the endpoint; other signal/sampler settings and custom SDK exporter
registrations are unaffected. A reusable HTTP transport or lazy gRPC client is
owned by the intake. Browser headers never become exporter headers, and HTTP
redirects are refused. There is no intake retry or pending export queue.

## Limits and responses

| Configuration | Default | Supported bound |
| --- | --- | --- |
| `MaxConcurrent` | 4 | 1–32 active handlers |
| `BatchesPerMinute` | 60 | 1–600 per intake |
| `Burst` | 10 | 1–128 token-bucket capacity |
| `Timeout` | 2 seconds | Positive, at most 10 seconds |

Zero selects each default. Admission is immediate: a full concurrency budget or
empty rate bucket returns 429. Invalid payloads admitted for decoding also
consume a rate token. The forwarding deadline uses the smaller remaining
handling budget and configured OTLP timeout. There is no per-user, per-IP or
per-origin tracking; replicas have independent process budgets.

The body bound is fixed at 64 KiB, with at most 16 spans, one resource and one
scope. Compressed input is rejected. Actual connection read deadlines bound
slow bodies; context cancellation alone cannot unblock `Body.Read`. Rejected
requests expire the read deadline and close the connection so post-handler body
draining cannot keep a slow client alive. Successful fully consumed requests
clear that deadline and retain keep-alive. A wrapper that prevents installing a
read deadline receives 503 before the intake attempts a body read; use the
documented wrapper composition and configure server read timeouts as well.

All responses are empty and use `Cache-Control: no-store`:

| Status | Meaning |
| --- | --- |
| 202 | The downstream OTLP receiver accepted the rebuilt batch |
| 400 | Invalid JSON, identity, timing, attributes or span contract |
| 403 | Missing, repeated or unapproved Origin |
| 405 | Method other than POST; `Allow: POST` is returned |
| 408 | The body read timed out or was cancelled |
| 413 | Body exceeds 64 KiB |
| 415 | Unsupported content type or any content encoding |
| 429 | Intake rate or concurrency budget is exhausted |
| 502 | Downstream rejection, malformed success response or partial acceptance |
| 503 | Intake shutdown, unsupported read deadline or downstream unavailability/timeout |

Receiver acceptance does not prove backend indexing or retention. Downstream
response messages, error bodies and transport errors are never returned.
`ghatd.browser.intake.batch.count` records a fixed `outcome` vocabulary:
`accepted`, `invalid`, `forbidden`, `too-large`, `media`, `rate-limited`,
`rejected`, `unavailable`, `timeout`, `method`. The configured meter provider,
or current global provider by default, owns this counter. No browser value
becomes a metric label.

## Browser wire contract

Send same-origin `POST` requests with `Content-Type: application/json`, using
the official JavaScript `JsonTraceSerializer` output shape:
`resourceSpans[].scopeSpans[].spans[]`. Browser transport should omit cookies,
authorization, baggage and intake `traceparent`, refuse redirects, avoid
keepalive exports, and use bounded queues, batch sizes and a short timeout.

IDs use the JavaScript OTLP JSON convention: 32 nonzero hexadecimal characters
for `traceId`, and 16 for `spanId` and optional `parentSpanId`. They are not
protobuf-JSON base64 strings. Times are unsigned decimal nanosecond strings.
Span duration must be 0–120 seconds; end time must be within the past ten
minutes or next two minutes of the server clock. Invalid batches are rejected
atomically. Duplicate JSON keys, attributes and span identities in a batch are
rejected; JSON nesting is limited to 32 levels. Each span may have at most
16 input attributes. Unknown attributes and metadata are discarded.

The four span names below are fixed. Kind is normalized from the name; incoming
recognized kind/status enums must have valid numeric types. Status is rebuilt
from the permitted outcome, with no message. Flags are set to sampled, while
native trace/span/parent IDs and validated times are preserved. Events, links,
tracestate, schema URLs and all incoming resource/scope metadata are omitted.

| Span name / normalized kind | Permitted attributes |
| --- | --- |
| `browser.navigation` / INTERNAL | `browser.route.group`; required `browser.outcome`: `complete`, `cancelled`, `redirected`, `error`, `timeout` |
| `browser.document` / INTERNAL | `browser.route.group`; optional finite numeric `browser.document.ttfb_ms`, `browser.document.dom_content_loaded_ms`, `browser.document.load_ms`, each 0–120000 |
| `browser.request` / CLIENT | `browser.route.group`, `browser.api.group`; required `http.request.method`: `GET`, `HEAD`, `POST`, `PUT`, `PATCH`, `DELETE`, `OPTIONS`, `OTHER`; optional integer `http.response.status_code`: 100–599; required `browser.outcome`: `success`, `http-error`, `network-error`, `cancelled`, `timeout` |
| `browser.error` / INTERNAL | `browser.route.group`; required `browser.error.source`: `vue`, `window`, `unhandledrejection`, `navigation`; required `error.type`: `error`, `type-error`, `reference-error`, `range-error`, `syntax-error`, `uri-error`, `eval-error`, `aggregate-error`, `unknown` |

String attributes use `stringValue`; integers use numeric `intValue`, and
fractional document timings use numeric `doubleValue`. A browser error or
`error`, `http-error`, `network-error`, `timeout` outcome produces ERROR status;
other spans use UNSET. Status is not copied from the browser.

Route/API group sets are copied at construction. Each has at most 32 unique
values including the automatically present `other`; configured names are 1–32
lowercase ASCII identifier characters (letters, digits, underscore and hyphen,
starting with a letter). Missing or unknown groups become `other`. Choose
groups from registered route names and fixed API directory boundaries; never
send paths, queries, parameters, user/session IDs, payloads or exception text.

Consent is the browser application's responsibility. Require explicit feature
opt-in and valid unexpired analytics consent at span creation, propagation and
export. Revocation must synchronously close its export gate, abort in-flight
telemetry exports and discard queued/active spans before SDK shutdown can flush them.
Regrant should create a fresh generation. Bytes already transmitted cannot be
recalled. Navigation render-opportunity timing does not establish that every
asynchronous page element has loaded; avoid retroactive document measurements
after a late consent grant.

## Verification

The Go tests exercise strict rebuilding, malformed/duplicate input, privacy
canaries, immutable configuration, actual HTTP/TLS/gzip and gRPC receivers,
partial acceptance, redirects, rate/concurrency limits, shutdown and the finite
counter. Real TCP tests cover slow reads, unfinished oversized bodies,
keep-alive after successful requests and shutdown through GHATD's complete HTTP
wrapper chain.

The separate browser wire contract test uses lockfile-pinned official JavaScript
SDK/serializer bytes, real Go HTTP handlers and a real Collector. It verifies
HTTP/protobuf and gRPC forwarding, native browser-client to backend-server
parentage, correlated logs, and removal of hostile browser metadata. With
Node.js 24 or newer, Go and Docker available, run from the repository root:

```sh
npm ci --prefix external/observability/testdata/browser --ignore-scripts
docker pull otel/opentelemetry-collector-contrib:0.160.0
GHATD_TEST_COLLECTOR=1 go test -race ./external/observability -run TestBrowserWireContract
```
