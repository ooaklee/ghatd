# Structured application and request logs

The logger package carries a Zap logger through contexts and adds common
component and operation fields. With OpenTelemetry, acquire the logger from
the current request or operation context to preserve trace correlation. The
[service adoption guide](../../docs/how-to/add-service-observability.md)
explains runtime ownership and dependency wiring.

## Request completion fields

Use `external/observability/otelhttp.Wrap` once around the complete router. It
composes HTTP telemetry, this package's request middleware and panic recovery.
See the [router guide](../router/README.md#getting-started) for plain Gorilla
Mux route observation and middleware ordering.

The middleware emits the static message `http request completed` at info level:

| Field | Meaning |
| --- | --- |
| `method` | Standard HTTP method, or `OTHER` for an unrecognized method |
| `status` | Actual final HTTP response status; implicit writes default to 200 |
| `route` | Matched template, or `unknown` when none was observed |
| `duration_ms` | Time spent in the downstream handler, in fractional milliseconds |
| `correlation-id` | Canonical UUIDv4 from the correlation header, or a newly generated one |
| `trace_id`, `span_id` | Valid active trace context, when available |
| `outcome`, `error.type` | `error` and `panic` for an ordinary panic observed by the recovery wrapper |

The duration includes downstream routing, middleware, cache and handler work.
It excludes the subsequent completion-log write and is not network delivery
time as measured by the client. It is present without enabling request details
and is also allowed on the OTLP log branch when log export is enabled.

Recovery cannot replace a response already committed or hijacked, so a panic
can retain a successful status while the error fields record the failure.
Intentional `http.ErrAbortHandler` aborts skip the normal completion log and
request-duration metric. Configured Zap levels and sampling still apply.

## Original paths and forwarding evidence

Request details are opt-in through
[`HTTPRequestLogConfig`](../observability/CONFIGURATION.md#http-request-log-details).
Use `otelhttp.WrapWithOptions` with `WithHTTPRequestLogPolicy` to add the original
`url.path`, `user_agent.original`, socket peer and supplied forwarding headers.
The policy captures the path before a SPA can rewrite it to `/`. The `route`
field stays a template for grouping.

Queries are always omitted. By default, parameterized paths use their template;
`PreservePathParameters` can retain actual values in local logs. Configured
redaction prefixes take precedence. Supplied forwarding headers are bounded,
unverified claims; only explicitly trusted proxy ranges can change the derived
client address. The configuration guide lists limits and field names.

This replaces the old request-derived completion message and unconditional
`uri`, `clientip`, `forwarded-for`, `host` and `user-agent` fields. Update saved
queries and dashboards to use `route` and the appropriate opt-in fields. There
is no replacement capture of the arbitrary incoming Host header.

If using the lower-level `HTTPLoggerWithCustomUriIgnoreList`, entries now match
the observed route template or `request.URL.Path` after dispatch, rather than
the URI including its query. This is a log policy. HTTP trace suppression is
separate and leaves ordinary request logs and metrics enabled. The composed
`otelhttp.Wrap` does not install a log ignore list.

## Container shipping and OTLP

`StartRuntime` tees the application logger to the configured OTel logger
provider. Setting `OTEL_LOGS_EXPORTER=none` keeps local logging and trace IDs,
so an existing container log shipper can remain the only log ingestion path.
Keep high-cardinality request fields and IDs out of log stream labels.

The OTLP branch applies its own [field policy](../observability/README.md#safe-log-fields).
It excludes the extra request details; the existing local sink preserves them.
Message text and automatic caller/stack metadata are not redacted by that
policy. Use static messages and review what your host logger sends locally.
