# OpenTelemetry configuration

Use this guide with GHATD's `StartRuntime` or lower-level `Start`. Resource
identity comes from application configuration and the environment; exporter
settings come from the supported `OTEL_*` variables. For application wiring and
shutdown ownership, see the [package guide](README.md#bootstrap).

The behavior below is checked against the dependencies pinned in
[go.mod](../../go.mod): autoexport v0.69.0, trace/metric exporters v1.44.0, and
log exporters v0.20.0. Exporter implementations differ in some details, so
examples set the transport explicitly and avoid ambiguous configuration.

## Inspect configuration before exporting

`InspectConfiguration(Config)` returns a sanitized `ConfigurationReport` and
an error when a supported setting is invalid. It uses the same validation as
`Start` and `StartRuntime`. Inspection creates no providers, changes no
OpenTelemetry globals, contacts no endpoint, and opens no listener. It reads
configured TLS files to check PEM certificates and matching client keys.

The report includes exporter and protocol choices, precedence sources,
timeouts, identity presence, credential presence, and fixed issue codes. Its
endpoint description contains only scheme, loopback/remote classification,
port, and standard/root/custom/origin path classification. It never returns
service names, instance IDs, endpoint hosts or paths, header names or values,
certificate paths, or invalid input. Startup errors identify the affected
variable and issue code without copying its value.

```go
report, err := observability.InspectConfiguration(observability.Config{})
// report contains only sanitized fields and can be encoded as JSON.
if err != nil {
    return err
}
```

The standalone doctor and its opt-in receiver probe are described in the
[package guide](README.md). A configuration-only check is safe to run before
starting a service with a Prometheus exporter: it does not bind that exporter's
port. Custom exporter and metric-producer names are reported as `custom` or a
fixed warning, and remain available to applications that register them through
autoexport. Their private configuration and registration cannot be verified by
this inspector. Inspecting an unknown name does not prove startup will succeed.

Validation applies to the settings consumed by the selected exporters and
GHATD's providers. Disabling an exporter skips its OTLP settings; providers
still validate resource identity, sampling and applicable SDK limits. The
selected protocol alone is validated, because autoexport does not parse an
overridden generic protocol. Generic endpoint, header, timeout, compression,
insecure and TLS-file settings are checked even when a signal overrides them:
the pinned trace and metric exporters parse those generic settings first.

GHATD uses a stricter portable policy where the pinned exporters differ:

- Endpoints must be absolute HTTP or HTTPS URLs with valid hosts and ports,
  without userinfo, query strings, fragments, or raw/decoded control characters.
  Custom HTTP paths remain supported. gRPC endpoints must be origins.
- Header keys must be HTTP tokens and unique without regard to case. Values
  must use valid percent encoding and contain no control characters. Effective
  gRPC metadata additionally requires letter/digit/hyphen/underscore/dot keys
  and printable ASCII values, except for `-bin` metadata values.
- Endpoint and insecure settings must agree. TLS certificates require TLS;
  client certificates and keys must form a pair at the same configuration
  level. TLS files must be readable regular PEM files of at most 4 MiB each.
- Protocol and compression values cannot have surrounding whitespace.
  Insecure flags accept case-insensitive `true` or `false` only. Positive
  millisecond durations and queue/batch counts are limited to 2147483647.
  Attribute and cardinality limits retain supported zero/negative semantics
  within the signed 32-bit integer range.
- Sampler ratios must be finite numbers in [0, 1]. Metric temporality,
  histogram aggregation, and exemplar filter values must be supported enums.

The inspector warns about a shared HTTP base ending in a slash, a
signal-specific HTTP root path, and exponential histograms used with classic
bucket dashboards. These configurations remain valid when the receiver and
queries support them. It does not test receiver authentication, connectivity,
indexing or query compatibility; use an explicit probe and backend smoke test
for those checks.

## Start with an explicit destination

For an application running on the same machine as a plaintext OTLP/HTTP
Collector, export these variables into the application's environment:

```sh
export OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4318
export OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf
export OTEL_TRACES_EXPORTER=otlp
export OTEL_METRICS_EXPORTER=otlp
export OTEL_LOGS_EXPORTER=otlp
export OTEL_SERVICE_NAME=example-api
export OTEL_RESOURCE_ATTRIBUTES=service.namespace=example-platform,deployment.environment.name=development
```

Existing signal-specific endpoint, protocol, and header variables still take
precedence over these shared settings. Remove stale overrides before reusing a
shell for local testing, and leave authentication headers unset for a local
receiver that does not require them.

Inside a container, `localhost` refers to that container. Use the Collector's
reachable service address when the application and Collector run separately.
For a hosted receiver, use its documented HTTPS endpoint and inject its
authentication headers through your deployment's secret configuration.

An explicit nonblank `Config.ServiceName` overrides `OTEL_SERVICE_NAME`. Leave
that application field blank when the deployment should choose the name.

## Resource identity and precedence

All three signals share one resource. The first nonblank value in each row wins:

| Attribute | Precedence, highest first |
| --- | --- |
| `service.name` | `Config.ServiceName` → `OTEL_SERVICE_NAME` → `service.name` in `OTEL_RESOURCE_ATTRIBUTES` → `ghatd` |
| `service.namespace` | `Config.Namespace` → `service.namespace` in `OTEL_RESOURCE_ATTRIBUTES` |
| `service.instance.id` | `Config.InstanceID` → `service.instance.id` in `OTEL_RESOURCE_ATTRIBUTES` → process UUID |
| `service.version` | `Config.Version` → `service.version` in `OTEL_RESOURCE_ATTRIBUTES` |
| `deployment.environment.name` | `Config.Environment` → `deployment.environment.name` in `OTEL_RESOURCE_ATTRIBUTES` |

Namespace, version, and environment have no automatic default. GHATD trims the
owned identity values and reads environment attributes afresh at startup. The
fallback instance UUID is generated once per process and remains the same if
the SDK is constructed again in that process. Use a unique ID for each
concurrently running replica when supplying an explicit instance ID.

Resource attributes use comma-separated `key=value` pairs. Values support
percent encoding, for example `%2C` for a comma; `+` remains a plus sign. Other
explicitly supplied attributes are retained. Malformed pairs, invalid percent
encoding, and raw or decoded control characters fail startup with a diagnostic
that excludes the supplied value.

Keep resource values limited to deployment metadata. They accompany every
signal and must not contain credentials, request values, user identifiers, or
personal data. GHATD adds SDK metadata without detecting executable paths,
command arguments, usernames, or host identifiers. See the local
[resource implementation](resource.go) for the exact merge and validation.

## Select or disable exporters

| Variable | Built-in choices | Empty or unset |
| --- | --- | --- |
| `OTEL_TRACES_EXPORTER` | `otlp`, `console`, `none` | `otlp` |
| `OTEL_METRICS_EXPORTER` | `otlp`, `console`, `prometheus`, `none` | `otlp` |
| `OTEL_LOGS_EXPORTER` | `otlp`, `console`, `none` | `otlp` |

These values are exact: use lowercase without surrounding whitespace. Lists
such as `otlp,console` are not supported. Custom exporters can be registered
through [autoexport](https://pkg.go.dev/go.opentelemetry.io/contrib/exporters/autoexport@v0.69.0);
their names and configuration belong to the registering application.

Set `none` separately for each signal you want to disable. Disabling traces
forces a never-sample provider; disabling metrics also skips GHATD's runtime
and host metric instrumentation. Disabling OTLP logs leaves the application's
existing logger output intact. Setting all three to `none` avoids their export
traffic, while GHATD still validates its resource and sampler configuration and
installs providers and Trace Context propagation.

`console` exports telemetry to standard output and is useful for local
inspection. `prometheus` starts a metrics HTTP listener during startup, using
`OTEL_EXPORTER_PROMETHEUS_HOST=localhost` and
`OTEL_EXPORTER_PROMETHEUS_PORT=9464` by default, with a `/metrics` route. Its
collection cadence is controlled by the scraper. Choose its bind address
deliberately and include that listener in network access planning.

The experimental `OTEL_METRICS_PRODUCERS` setting can bridge existing Prometheus
metrics into a reader; its built-in choices are `prometheus` and `none` and its
default is `none`. This is separate from selecting the `prometheus` exporter.
Use it only when an application already owns metrics that need bridging.

## Protocol and endpoint rules

For each OTLP signal, a nonempty signal-specific protocol overrides the shared
protocol, then the default applies:

```text
OTEL_EXPORTER_OTLP_TRACES_PROTOCOL  ┐
OTEL_EXPORTER_OTLP_METRICS_PROTOCOL ├─ per signal, before OTEL_EXPORTER_OTLP_PROTOCOL
OTEL_EXPORTER_OTLP_LOGS_PROTOCOL    ┘
```

Supported values are exactly `http/protobuf` and `grpc`; the default is
`http/protobuf`. This autoexport version does not support `http/json`. Protocol
and OTLP endpoint settings do not configure `none`, `console`, or `prometheus`.

With no endpoint or transport-security settings, the pinned Go exporters
connect using TLS to `localhost:4318` for HTTP or `localhost:4317` for gRPC.
An unset endpoint therefore does not select a plaintext local Collector. Set
an explicit `http://` URL for plaintext or `https://` for TLS. The pinned
[Go exporter documentation](https://pkg.go.dev/go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp@v0.20.0#WithInsecure)
describes this secure default.

### HTTP: shared base URL or complete signal URL

`OTEL_EXPORTER_OTLP_ENDPOINT` is a base URL. With
`https://collector.example.com/otlp`, the intended paths are:

| Signal | Request path |
| --- | --- |
| Traces | `/otlp/v1/traces` |
| Metrics | `/otlp/v1/metrics` |
| Logs | `/otlp/v1/logs` |

A nonempty `OTEL_EXPORTER_OTLP_TRACES_ENDPOINT`,
`OTEL_EXPORTER_OTLP_METRICS_ENDPOINT`, or `OTEL_EXPORTER_OTLP_LOGS_ENDPOINT`
overrides that signal's base URL. Supply the complete path:

```sh
export OTEL_EXPORTER_OTLP_LOGS_ENDPOINT=https://logs.example.com/v1/logs
```

The signal-specific form does not append `/v1/logs`; without a path it sends
to `/`. Keep any receiver-specific prefix required by your provider. These
base-versus-signal rules follow the
[OTLP exporter specification](https://opentelemetry.io/docs/specs/otel/protocol/exporter/#endpoint-urls-for-otlphttp).

For this pinned version, omit a trailing slash from the shared base URL. The
log exporter appends `/v1/logs` directly, so a base ending in `/otlp/` produces
`/otlp//v1/logs`. Trace and metric exporters join path segments and produce
`/otlp/v1/traces` and `/otlp/v1/metrics`. Avoid depending on receiver redirects
or slash normalization. The distinction is visible in the pinned
[trace configuration source](https://github.com/open-telemetry/opentelemetry-go/blob/v1.44.0/exporters/otlp/otlptrace/otlptracehttp/internal/otlpconfig/envconfig.go)
and [log configuration source](https://github.com/open-telemetry/opentelemetry-go/blob/exporters/otlp/otlplog/otlploghttp/v0.20.0/exporters/otlp/otlplog/otlploghttp/config.go).

### gRPC: receiver origin, without HTTP signal paths

For a local gRPC receiver:

```sh
export OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4317
export OTEL_EXPORTER_OTLP_PROTOCOL=grpc
```

The `http` scheme selects plaintext transport here; the protocol remains
gRPC. Use `https` for TLS. Supply an origin without `/v1/traces`, `/v1/metrics`,
or `/v1/logs`. The pinned trace/metric and log gRPC exporters handle URL paths
differently, so avoid paths in a shared gRPC endpoint.

Per-signal protocol and endpoint overrides can mix transports. When overriding
a protocol, also check that signal's effective endpoint and port. Port 4318
usually serves OTLP/HTTP and 4317 usually serves OTLP/gRPC; the receiver's
configuration determines the actual ports.

## Authentication, compression, and TLS

The following shared settings have signal-specific equivalents. Replace
`<SIGNAL>` with `TRACES`, `METRICS`, or `LOGS`, for example
`OTEL_EXPORTER_OTLP_LOGS_HEADERS`.

| Shared variable | Signal-specific suffix after `OTEL_EXPORTER_OTLP_<SIGNAL>_` | Value |
| --- | --- | --- |
| `OTEL_EXPORTER_OTLP_HEADERS` | `HEADERS` | Comma-separated header `key=value` pairs |
| `OTEL_EXPORTER_OTLP_COMPRESSION` | `COMPRESSION` | `none` or `gzip`; default is no compression |
| `OTEL_EXPORTER_OTLP_TIMEOUT` | `TIMEOUT` | Export timeout in integer milliseconds; default `10000` |
| `OTEL_EXPORTER_OTLP_CERTIFICATE` | `CERTIFICATE` | PEM file containing trusted server CA certificates |
| `OTEL_EXPORTER_OTLP_CLIENT_CERTIFICATE` | `CLIENT_CERTIFICATE` | PEM client certificate for mTLS |
| `OTEL_EXPORTER_OTLP_CLIENT_KEY` | `CLIENT_KEY` | PEM private key matching the client certificate |
| `OTEL_EXPORTER_OTLP_INSECURE` | `INSECURE` | `true` or `false`; prefer an explicit endpoint scheme |

For valid settings, the signal-specific value takes precedence over the shared
value. A signal-specific header setting replaces the entire shared header map;
it does not merge additional headers into it. Repeat any required shared
authentication header in that signal's secret configuration. Empty values
generally mean unset and do not provide a portable way to clear inherited
credentials; remove or reorganize the shared setting instead.

Header values are percent-decoded, while keys are not. Encode embedded commas
as `%2C`; a literal `+` remains `+`, and `=` can appear after the first separator.
Avoid duplicate header keys, including case variants. Do not place credentials
in endpoint userinfo, query strings, fragments, resource attributes, or command
arguments. The exporters do not use URL userinfo as an authentication header.
When reporting configuration, report only whether headers are present, never
their names or values. Do not paste credentials into terminal transcripts or
support reports.

Use the operating system's trust store for ordinary public TLS endpoints.
Configure `CERTIFICATE` when a receiver needs a custom CA. Configure both
`CLIENT_CERTIFICATE` and `CLIENT_KEY` at the same shared or signal-specific
level for mTLS; these settings name files readable by the application process.
Keep file contents and local paths out of diagnostics.

GHATD rejects contradictory endpoint schemes, `INSECURE` flags, and TLS
certificate settings before constructing an exporter. In the pinned log
exporters, a nonempty endpoint scheme takes
precedence over the insecure flag. Trace/metric exporters apply insecure flags
after the scheme; gRPC TLS credentials introduce another precedence rule.
An insecure HTTP trace endpoint combined with TLS client configuration is
rejected. A clear `http://` or `https://` URL without competing flags is the
portable configuration for these exporters.

## Export cadence and shutdown budgets

Environment durations below are integer **milliseconds**, not Go duration
strings such as `5s`. GHATD accepts positive values through 2147483647
milliseconds. Whitespace, zero, negative, malformed and larger values fail
validation before an upstream parser can fall back or report their contents.

| Variable | Default | Controls |
| --- | --- | --- |
| `OTEL_EXPORTER_OTLP_TIMEOUT` | `10000` | OTLP batch export budget; per-signal timeout overrides it |
| `OTEL_METRIC_EXPORT_INTERVAL` | `60000` | Periodic metric collection/export cadence |
| `OTEL_METRIC_EXPORT_TIMEOUT` | `30000` | Periodic metric collection/export timeout |
| `OTEL_BSP_SCHEDULE_DELAY` | `5000` | Trace batch scheduling delay |
| `OTEL_BSP_EXPORT_TIMEOUT` | `30000` | Trace batch export timeout |
| `OTEL_BLRP_SCHEDULE_DELAY` | `1000` | Log batch scheduling delay |
| `OTEL_BLRP_EXPORT_TIMEOUT` | `30000` | Log batch export timeout |

An exporter timeout does not increase a shorter caller or processor deadline.
Batch queues can export before their scheduling delay when enough records
arrive. Queue and batch sizes are separately configured by
`OTEL_BSP_MAX_QUEUE_SIZE`, `OTEL_BSP_MAX_EXPORT_BATCH_SIZE`,
`OTEL_BLRP_MAX_QUEUE_SIZE`, and `OTEL_BLRP_MAX_EXPORT_BATCH_SIZE`; defaults are
2048 queued records and 512 records per batch for each signal. Larger queues
use more memory and cannot guarantee delivery through a prolonged outage.

For quicker local metric feedback, set `OTEL_METRIC_EXPORT_INTERVAL=5000`.
Use dashboard query windows that contain several export intervals; a window
shorter than the interval can look empty during normal operation. The default
60-second cadence is distinct from a dashboard refresh interval.

`RuntimeConfig.ShutdownTimeout` is a Go `time.Duration`, not an `OTEL_*`
variable. Zero selects 15 seconds. `Runtime.Shutdown` creates that deadline
when shutdown begins, detaches prior cancellation, and shares the budget
across its providers. Close application dependencies before flushing
telemetry. The lower-level `SDK.Shutdown` uses the context you provide. See
[runtime lifecycle](README.md#runtime-lifecycle-and-command-adapters).

`SDK.ForceFlush(ctx)` attempts all three providers without stopping them and
joins their errors. It can be repeated, accepts a nil SDK, and uses the caller's
context budget. End spans and complete operations before flushing them.

## Sampling and propagation

`OTEL_TRACES_SAMPLER` defaults to `parentbased_always_on`. GHATD recognizes:

| Sampler | Behavior |
| --- | --- |
| `always_on` | Sample every trace decision made here |
| `always_off` | Sample no trace decisions made here |
| `parentbased_always_on` | Respect a parent's sampled flag; sample new roots |
| `parentbased_always_off` | Respect a parent's sampled flag; do not sample new roots |
| `traceidratio` | Make decisions using the configured trace-ID ratio |
| `parentbased_traceidratio` | Respect a parent's sampled flag; use the ratio for new roots |

Both ratio samplers require `OTEL_TRACES_SAMPLER_ARG` as a finite number from
0 through 1, for example `0.1`. Supply ordinary numeric values; do not use
`NaN` or infinities. Unlike exporter names and protocols, GHATD trims and
lowercases sampler names. A ratio is a sampling policy, not a guarantee that
exactly that fraction appears in a small time window.

Trace sampling does not disable metrics or logs. A log can contain valid trace
IDs even when its trace was not sampled and is absent from the backend.
`OTEL_TRACES_EXPORTER=none` overrides the selected sampler with never-sample.

For distributed services, prefer parent-based samplers consistently. An
explicit head ratio can be configured with:

```sh
export OTEL_TRACES_SAMPLER=parentbased_traceidratio
export OTEL_TRACES_SAMPLER_ARG=0.1
```

This chooses a ratio for new roots and retains sampled parent traces. It also
discards most error traces before their outcome is known; a head ratio is not
an error-retention policy. The default remains `parentbased_always_on`.

An application can separately opt into exact health/static GET/HEAD suppression
through `NewHTTPTracePolicy` and the outer HTTP wrapper's
`WithHTTPTracePolicy` option. No suppression paths or new environment variables
are implicit. The [HTTP policy guide](README.md#optional-http-trace-suppression)
documents bounded canonical matching, parent handling, and the sampler
decorator installed by `Start`. Middleware options belong to the application
executable; the standalone doctor cannot infer them from the environment.

HTTP suppression retains request metrics and ordinary logs, including valid
correlation IDs for unsampled spans. With the default parent-based sampler,
sampled incoming parents retain their traces. Selected requests can still fail
after the head decision: their 404/500/panic traces may be absent, and the
default trace-based exemplar filter yields fewer trace links. An unrelated
provider configured with `always_on` can resume recording downstream work from
an unsampled propagated parent.

Collector tail sampling can retain errors and slow traces only from spans it
receives. For complete error visibility, send full head input using the
parent-based default and avoid path suppression where errors must be retained.
Every contributing service must cooperate: parent-based sampling still honors
an already unsampled incoming parent. Tail sampling also requires coherent
trace routing and a decision window that covers the work. It cannot recover
spans discarded by application head sampling.

GHATD installs W3C Trace Context propagation (`traceparent` and `tracestate`).
Baggage is omitted to avoid forwarding arbitrary inbound values to other
services. `OTEL_PROPAGATORS` and `OTEL_SDK_DISABLED` are not interpreted by this
package. Use the individual exporter variables to disable signals; setting
either unsupported variable does not change this lifecycle's behavior.

## Metric temporality and histogram compatibility

The OTLP metric exporter accepts
`OTEL_EXPORTER_OTLP_METRICS_TEMPORALITY_PREFERENCE` values `cumulative`,
`delta`, and `lowmemory`; the default is `cumulative`. `lowmemory` applies
different temporalities by instrument kind. Match the setting to the
receiver's documented support. A backend may convert temporality during
ingestion; it must produce cumulative Prometheus counters before ordinary
counter `rate()` queries can be interpreted correctly.

`OTEL_EXPORTER_OTLP_METRICS_DEFAULT_HISTOGRAM_AGGREGATION` accepts
`explicit_bucket_histogram` (default) or
`base2_exponential_bucket_histogram`. Dashboards that query `_bucket` series
with `histogram_quantile()` require classic explicit buckets, or an explicit
conversion in the ingestion pipeline. Choosing exponential aggregation does
not preserve those queries automatically.

Keep both defaults when starting with classic Prometheus dashboards. Explicit
histogram boundaries supplied by GHATD operations apply to the explicit
histogram aggregation; query ranges must still account for the metric export
cadence and aggregation across replicas.

## Troubleshoot without exposing configuration values

First compare the effective signal, protocol, and endpoint source
(signal-specific, shared, or default), then confirm resource selectors and
export cadence. Inspect sensitive settings privately. A useful support report
contains known enum values, integer timeouts, and presence flags such as
`headers_configured=true`; it does not contain an environment dump, arbitrary
endpoint paths, headers, certificate paths, or backend response bodies.

| Symptom | Check |
| --- | --- |
| Connection refused or TLS handshake failure on localhost | Set the intended endpoint scheme and verify that the receiver listens on the matching HTTP/gRPC port |
| HTTP 404 or redirects during export | Check the complete per-signal path, base-path prefix, and shared base trailing slash |
| HTTP 401/403 or gRPC unauthenticated/permission denied | Confirm credentials are present in the effective signal header setting; a signal override replaces shared headers |
| Metrics appear late or briefly disappear | Compare export cadence with dashboard windows and look for a changed instance/resource selector |
| Trace-linked logs open no trace | Check sampling and the trace exporter; valid correlation IDs do not prove the trace was exported |
| Histogram panels are empty | Check explicit versus exponential aggregation and the backend's metric-name conversion |
| Final records disappear when a command exits | Return errors instead of calling `os.Exit`/`Fatal`, end operations, close dependencies, and allow bounded runtime shutdown |
| A signal remains active after a disable setting | Use that signal's exporter value `none`; `OTEL_SDK_DISABLED` is not supported here |

Some pinned upstream parsers report malformed URLs, headers, numbers, metric
preferences, or TLS file errors through OpenTelemetry's global error handler
or logger. GHATD validates its supported settings before calling those parsers,
including configured generic settings that a signal overrides. Malformed
settings fail startup with fixed variable names and issue codes. This prevents
partial malformed header maps or parser fallbacks from making startup appear
successful. Custom exporter code and asynchronous runtime/export errors still
need their own safe diagnostics; the application log bridge does not intercept
every upstream diagnostic.

For application-owned automatic diagnostics, use a static message such as
`telemetry export failed` with a trusted signal name; avoid formatting an
exporter error or copying response bodies. Receiver acceptance is only one
checkpoint: verify a known synthetic request's trace, correlated log, and
metric in the destination before treating the entire pipeline as healthy.
