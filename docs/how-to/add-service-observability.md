# Add observability to a service

Use GHATD's OpenTelemetry runtime to follow a request through HTTP handlers,
business operations and dependencies while keeping your existing application
logger. The host application chooses destinations, enables each signal, wires
clients and owns shutdown. Updating the GHATD dependency or constructing
`starter/v0` components does not perform that setup automatically.

Start with the [runnable reference service](../../examples/observability/README.md)
if you want to see a complete pipeline first. Use the
[package guide](../../external/observability/README.md) for API details and the
[configuration reference](../../external/observability/CONFIGURATION.md) for
supported environment settings.

## 1. Choose the signals and destinations

Choose all three exporters explicitly. When unset, **each defaults to `otlp`**.
For an initial trace-only integration with a local OTLP receiver:

```sh
export OTEL_SERVICE_NAME=example-api
export OTEL_RESOURCE_ATTRIBUTES=service.namespace=example-platform,deployment.environment.name=local
export OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4318
export OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf
export OTEL_TRACES_EXPORTER=otlp
export OTEL_TRACES_SAMPLER=always_on
export OTEL_METRICS_EXPORTER=none
export OTEL_LOGS_EXPORTER=none
```

This records all trace decisions made by this service unless an explicit HTTP
suppression policy applies. `parentbased_always_on`, the default sampler,
respects an incoming parent's unsampled decision. Neither setting guarantees
delivery or backend retention. Start without path suppression when validating
the integration; introduce sampling after measuring traffic and span volume.

Use a reachable private Collector or an HTTPS backend for deployment, with
credentials supplied by your secret manager. A generic HTTP endpoint is a base
URL; a signal-specific endpoint such as
`OTEL_EXPORTER_OTLP_TRACES_ENDPOINT` includes its complete `/v1/traces` path.
Signal-specific headers replace the generic header map. See
[endpoint and header configuration](../../external/observability/CONFIGURATION.md).

You can send traces to one backend while a container log shipper continues
sending logs to another. `OTEL_LOGS_EXPORTER=none` disables the SDK's log export,
not local Zap output or its trace IDs. Set `OTEL_METRICS_EXPORTER=otlp` only when
you intend to export application, Go runtime and host metrics. This setting does
not disable other cluster scrapers or their metric pipelines.

GHATD configures the SDK through Go and the supported `OTEL_*` variables. It
does not load `OTEL_CONFIG_FILE`, interpret `OTEL_SDK_DISABLED`, or configure
propagation from `OTEL_PROPAGATORS`. It installs W3C Trace Context without
baggage. Host-specific flags for request details or HTTP noise suppression
must be parsed and connected to the corresponding Go options by the host.

## 2. Start one runtime and keep ownership explicit

Create the runtime before instrumented clients, repositories and services:

```go
runtime, err := observability.StartRuntime(ctx, observability.RuntimeConfig{
    Telemetry: observability.Config{
        Version:     buildVersion,
        Environment: environment,
    },
    Logger:          appLogger,
    ShutdownTimeout: 15 * time.Second,
})
if err != nil {
    return err
}
defer func() {
    if err := runtime.Shutdown(ctx); err != nil {
        appLogger.Warn("telemetry shutdown failed")
    }
}()
ctx = runtime.Context()
```

This fragment belongs in the host's service action. Omitting `ServiceName`
allows `OTEL_SERVICE_NAME` above to select it; a nonblank Go value wins over the
environment. Give API, worker and migration processes distinct service names,
and keep replica instance IDs unique. The default instance ID is a UUID created
once per process. See [identity precedence](../../external/observability/README.md#service-identity).

Construct dependencies using this context and pass them into `starter/v0` or
your own composition. Use `runtime.Logger()` at application boundaries and
acquire request/operation loggers from their contexts downstream. Do not start
another runtime in each handler or service: providers are process-global.

Shut down in this order:

1. Stop admitting work, drain HTTP requests and finish workers.
2. Close application dependencies, including any browser intake forwarder.
3. Call `Runtime.Shutdown` to flush telemetry within its fresh shared timeout.
4. Flush/close the original logger according to the host's logging policy.

Register defers in reverse order. The runtime does not drain your server or
close database connections. Its shutdown budget starts when shutdown is called,
even if the service context is already cancelled. The lower-level
`SDK.Shutdown(ctx)` instead uses the caller's context directly.

The [HTTP server helper](../../external/http/server/README.md) handles bounded
draining, request-context values and forced close on failure directly. Pass
`runtime.Context()` as `StartServerWithRequest.Context`; no copied lifecycle
adapter is needed. A manually configured server can follow the
[reference lifecycle](../../examples/observability/main.go) instead.

For dependency cleanup after the server returns, defer the shared coordinator:

```go
defer observability.ShutdownResources(runtime.Context(), observability.ShutdownConfig{
    Timeout:  15 * time.Second,
    Cleanup:  cleanupGroup.Run,
    Shutdown: runtime.Shutdown,
    Logger:   runtime.Logger(),
})
```

Register dependency callbacks on the host's cleanup group. The coordinator
gives cleanup a fresh deadline and always attempts the final telemetry flush,
even if cleanup panics. It emits fixed failure messages without arbitrary
dependency error bodies. Callbacks must honor their contexts; the helper cannot
forcibly interrupt them. The logger's own final `Sync` remains host-owned.

## 3. Wrap the complete HTTP handler once

After registering routes, pass one outer wrapper to the server:

```go
handler := otelhttp.Wrap("example-api", runtime.Logger(), ghatdRouter.GetRouter())
```

Import `otelhttp` from
`github.com/ooaklee/ghatd/external/observability/otelhttp`. This composes
telemetry, request logging and recovery around the complete dispatch. Remove
duplicate request-logging/recovery middleware from inside the router; keep
authentication and other matched-route middleware there. Wrapping only with
`router.Use(...)` misses router-generated responses such as 404/405s.

GHATD's router records route templates automatically. A plain Gorilla Mux router
needs `routecontext.ObserveMiddleware` installed before its other middleware;
see the [router guide](../../external/router/README.md#getting-started).

The completion log's `route` is the matched template, not necessarily the
requested path. A SPA catch-all can correctly report `/` for many page paths.
Enable a [request-log policy](../../external/observability/CONFIGURATION.md#http-request-log-details)
to capture the original `url.path`, user-agent and forwarding evidence before
SPA rewrites. These opt-in fields stay in local logs. Choose path redaction and
parameter preservation for your service; query strings are always omitted.

## 4. Connect dependencies and preserve context

Install instrumentation when constructing clients, before their first use:

| Dependency | Wiring |
| --- | --- |
| MongoDB v2 | Pass `observability.NewMongoCommandMonitor(...)` to `options.Client().SetMonitor`. For `repository.NewMongoRuntime`, supply those options through `repositoryhelpers.WithCustomOptions` in the request's `Options`. |
| Redis v7 | Add `observability.NewRedisHook(...)` to the client, or pass it in `ephemeral.NewRedisRuntimeRequest.Hooks` so the startup ping is also observed. Pass explicit tracer/meter providers when using a runtime. |
| Outbound HTTP | Use `observability.NewHTTPClient(baseTransport, timeout)` and create requests with the caller's context. Keep the host's transport and timeout policy. |
| Business operations | Create an `Operations` instance and pass the context returned by `Start` into downstream work; call `End` with the action's result. Use fixed operation names. |
| Cobra commands | Use `otelcobra.Instrument` on executable leaf actions, or `otelcobra.Run` inside an action after loading settings. The adapter owns that action's runtime; do not nest it inside another owning runtime. |
| MongoDB migrations | Attach the monitor with `WithMongoCommandMonitor` and register context-aware migration helpers; see [migration tracing](../../external/migrator/mongo/README.md#tracing-migrations). |
| SparkPost | Pass a private HTTP client to `emailprovider.NewSparkPostClient` using its `HTTPClient` field or request's `WithHTTPClient` method. `NewSparkPostEmailProvider` prefers the SDK's context-aware send method. See [client policy and compatibility](../../external/emailprovider/README.md). |
| Cache decisions | Use [`otelcache`](../../external/observability/otelcache/README.md) with the runtime's meter provider and your existing metric name, then attach its HTTP cache observer. |
| Consumer jobs | Use [`otelqueue`](../../external/observability/otelqueue/README.md) with a finite operation vocabulary and host-owned acknowledgement/retry logic. |

The [database wiring example](../../examples/observability/database_wiring.go)
shows the Mongo and Redis provider options. It compiles without opening database
connections. Follow the [operation and command examples](../../external/observability/README.md)
for action spans, error classification and lifecycle boundaries.

Pass `request.Context()` through services and repository calls rather than
replacing it with `context.Background()`. Direct Redis v7 calls need
`client.WithContext(ctx)`; GHATD's ephemeral store already passes the supplied
context. Custom HTTP clients remain host-owned: supplying one to a provider
does not automatically wrap its transport.

## 5. Check correlation and control volume

Query local/container JSON logs by `trace_id` and `span_id`. If a log shipper
adds an envelope, decode that before parsing the application JSON. A log system
can link the extracted `trace_id` to your trace backend without receiving OTLP
logs. Configure that link for the same backend environment that receives the
traces; service resource attributes alone do not choose a vendor account or
credential-scoped environment. Valid IDs can still refer to unsampled, lost or
expired traces.

Keep raw paths, user-agents, forwarded addresses, resource IDs and trace IDs as
log fields rather than metric dimensions or Loki stream labels. Use `route`
templates for aggregate request views. `duration_ms` measures handler execution
in fractional milliseconds; it is not full client-observed latency. See the
[logger guide](../../external/logger/README.md) for fields and compatibility.

Trace sampling does not reduce metric series. Histogram buckets, label-value
combinations and replicas multiply series, even for a quiet service. Increasing
the metric export interval reduces export frequency, not those combinations.
SDK cardinality limits apply per instrument; they are not a backend-wide quota.
Check which instruments and resource labels reach the backend before enabling
metrics broadly, and retain the pinned Mongo instrumentation's
[stable connection labels](../../external/observability/README.md#mongodb-monitor-and-metric-cardinality).

The optional [Collector example](../../examples/observability/collector/README.md)
adds bounded queues, retries and export-health monitoring. Tail sampling is a
separate, explicit option: it needs all contributing spans and cannot recover
spans already dropped by head sampling or HTTP suppression. Browser traces also
require separate client instrumentation and the opt-in
[browser intake](../../external/observability/BROWSER.md); the server wrapper
does not instrument an embedded SPA's browser activity. For browser collection,
consume the [shared browser package](../../browser/observability/README.md) at
the same exact GHATD revision as the Go module. Supply consent callbacks and
finite groups, load the controller lazily, and keep the server intake vocabulary
aligned. Delete migrated infrastructure copies; retain application policy and
integration tests.

## 6. Validate and adopt in an existing service

From a GHATD checkout with the intended environment, inspect configuration, then
explicitly probe your receiver:

```sh
go run ./cli telemetry doctor
go run ./cli telemetry doctor --probe --timeout 10s
```

The [doctor](../../external/observability/DOCTOR.md) emits a sanitized report.
Inspection sends no telemetry; the probe sends synthetic records only for
enabled signals. Receiver acceptance is not proof of backend indexing. Generate
a real service request, find its trace and correlated log, exercise a dependency
call and verify final telemetry after a graceful shutdown. Use the
[local reference smoke check](../../examples/observability/README.md) for a
repeatable three-signal LGTM test; its assertions expect metrics and logs enabled.

Before rolling an existing service forward:

- Update the GHATD dependency and keep the resolved instrumentation versions in
  `go.mod`/`go.sum`. The repository currently uses Go 1.26 and the pinned 1.26.4
  toolchain; consult [go.mod](../../go.mod) for exact dependencies.
- Wire the runtime, outer HTTP handler and client hooks explicitly. Keep signal
  settings explicit in every service and command deployment.
- Update log queries that depend on the old interpolated completion message or
  `uri`, `clientip`, `forwarded-for`, `host` and `user-agent` fields. Use the
  [new schema](../../external/logger/README.md#request-completion-fields).
  Arbitrary incoming correlation IDs are now replaced with canonical UUIDv4s.
- Prefer keyed struct literals when configuring requests such as
  `oauth.NewGoogleProviderRequest` and `ephemeral.NewRedisRuntimeRequest`; new
  `HTTPClient` and `Hooks` fields require changes to positional literals.
- Move existing billing, sitemap and vision index registrations to their
  [`WithContext` variants](manage-mongodb-migrations.md#3-register-up-and-down-functions)
  so deadlines and trace parentage reach database operations. Existing
  registrations retain their identity; observability needs no new database
  migration and does not require rerunning already applied migrations.
- If adopting the new [Stripe settings helper](../../external/paymentprovider/helpers/README.md),
  load settings, call `Configure`, then create or append the provider. This is a
  separate opt-in to shared host validation, not a prerequisite for tracing.

Keep log messages static. GHATD filters fields on its OTLP log branch but does
not redact interpolated messages, automatic caller/stack metadata, or the
existing local sink. Review host-owned instrumentation and logging as part of
adoption; the package's field policy is not a general-purpose data scrubber.

## Share verification without copying the test runner

Keep dashboards, chart values and process topology in the host. Configure the
[shared verification command](../../external/observability/verify/README.md)
with the host’s asset paths, metric names and roles, then invoke it from CI using
the same GHATD module pin. It embeds the synthetic fixtures and assertions.
Retain product-specific integration checks, such as the browser/server group
contract, in the host. Copied deployment assets keep their own provenance hashes;
a library upgrade alone does not require pretending those assets changed.
