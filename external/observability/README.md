# OpenTelemetry observability

This package gives GHATD applications one provider-neutral telemetry layer for
traces, metrics, and logs. It uses the OpenTelemetry SDK and standard exporter,
resource, and sampler `OTEL_*` configuration, so applications can export to any
compatible Collector and backend without embedding a vendor agent.

## Bootstrap

Start the SDK before constructing instrumented clients, then shut it down with
a bounded context so buffered signals are flushed:

```go
telemetry, err := observability.Start(ctx, observability.Config{
    ServiceName: "my-service",
    Version:     gitCommit,
    Environment: environment,
})
if err != nil {
    return err
}
defer telemetry.Shutdown(shutdownCtx)
```

`Start` installs W3C Trace Context propagation, OTLP exporters for all three
signals, and Go runtime and host metrics. Exporter endpoint, protocol, headers,
TLS, sampling, and per-signal enablement are controlled by the relevant
OpenTelemetry environment variables. Baggage is deliberately not propagated
across service boundaries because inbound values may contain sensitive data.

The most useful local settings are:

```sh
OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4318
OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf
OTEL_TRACES_EXPORTER=otlp
OTEL_METRICS_EXPORTER=otlp
OTEL_LOGS_EXPORTER=otlp
```

Set any signal exporter to `none` to disable it explicitly. This package does
not interpret `OTEL_PROPAGATORS` or `OTEL_SDK_DISABLED`; propagation is fixed to
Trace Context, and signals are disabled individually through their exporter
variables.

## Service identity

Every signal carries the same service resource. `service.instance.id` defaults
to a random UUID generated once per process: repeated SDK construction in that
process retains the ID, while a new process receives a new ID. This separates
replica metric streams without collecting executable paths, command arguments,
usernames, or host identifiers.

Applications can set `Config.Namespace` to group related services and
`Config.InstanceID` when the deployment already assigns a unique instance ID.
The standard environment variables also work:

```sh
OTEL_SERVICE_NAME=my-service
OTEL_RESOURCE_ATTRIBUTES=service.namespace=example-platform,deployment.environment.name=staging
```

Configuration precedence, from highest to lowest, is:

| Resource attribute | Precedence |
| --- | --- |
| `service.name` | Nonblank `Config.ServiceName`, `OTEL_SERVICE_NAME`, `service.name` in `OTEL_RESOURCE_ATTRIBUTES`, then `ghatd` |
| `service.namespace` | Nonblank `Config.Namespace`, then `service.namespace` in `OTEL_RESOURCE_ATTRIBUTES` |
| `service.instance.id` | Nonblank `Config.InstanceID`, `service.instance.id` in `OTEL_RESOURCE_ATTRIBUTES`, then the process UUID |
| `service.version` | Nonblank `Config.Version`, then `service.version` in `OTEL_RESOURCE_ATTRIBUTES` |
| `deployment.environment.name` | Nonblank `Config.Environment`, then `deployment.environment.name` in `OTEL_RESOURCE_ATTRIBUTES` |

Namespace, version, and environment have no automatic default. Configuration
values are trimmed, and blank application fields allow environment settings to
apply. Environment attributes are read at each startup, independently of the
SDK's cached default resource. Other explicitly supplied resource attributes
are retained. Malformed attributes, invalid percent encoding, and control
characters fail startup with a diagnostic that excludes the supplied value.

Use a stable namespace for a group of related services, stable service names
for each executable role, and a separate deployment environment such as
`development`, `staging`, or `production`. Let the process UUID identify each
replica unless an explicit instance ID is unique among concurrently running
instances. A deployment-wide constant would merge distinct metric streams.
Keep identifiers, credentials, request values, and personal data out of
resource attributes; resources are attached to all exported signals.

## Instrumentation

- Wrap the complete router with `HTTPServerMiddleware`, request logging, and
  `HTTPRecoveryMiddleware`, in that outer-to-inner order. This observes generated
  404/405 responses and redirects as well as matched routes. GHATD routers track
  templates automatically; direct Gorilla Mux users must install
  `routecontext.ObserveMiddleware` before other route middleware. See the
  [router integration guide](../router/README.md#getting-started).
  Intentional `http.ErrAbortHandler` panics retain Go's abort semantics and omit
  normal request completion logging and duration recording.
- Construct outbound clients with `NewHTTPClient` or wrap a transport with
  `NewRoundTripper`.
- Attach `NewMongoCommandMonitor` to MongoDB v2 client options.
- Attach `NewRedisHook` to Redis v7 clients and always execute commands with
  the caller's context. Core command names use a fixed vocabulary; additional
  module commands require explicit registration as described below.
- Use `TeeLogger` to preserve existing Zap output while exporting sanitised
  records, and `WithTraceContext` when placing a logger into a traced context.

The OTLP logging branch exports an explicit allowlist of operational fields;
unknown keys, opaque values, and namespace controls are dropped. Some fields
receive additional value validation, as described below. Other operational
values, such as operation names and route templates, must be static values
supplied by application code or configuration, never request payloads.
Log messages must remain static and must not interpolate request or domain data.
HTTP paths and queries, Mongo statements, Redis keys/arguments, request and
response objects, credentials, vehicle registrations, user details, and raw
error messages are deliberately excluded from exported telemetry fields. Route
templates, HTTP methods/statuses, database system/operation names, Redis command
names, error types, trace IDs, and span IDs remain available for diagnosis.

HTTP stream failures retain their original errors for application code while
instrumentation sees only a constant message. This includes response-body reads
and upgraded-connection writes. Exact EOF behavior, partial byte counts, and
optional HTTP writer interfaces are preserved.

## Safe log fields

`TeeLogger` keeps existing local Zap fields, field names, levels, sampling, and
hooks. Its OTLP branch canonicalizes the following fields; punctuation and case
variants of the listed keys are recognized:

| Input key | OTLP key | Accepted value |
| --- | --- | --- |
| `source` | `source` | `ghatd` or an explicitly configured static extension; other values are dropped |
| `provider` | `provider` | `kofi`, `lemonsqueezy`, `stripe`, `SPARKPOST`, `LOCAL`, or a configured extension; other strings become `other` |
| `method`, `http.request.method` | `method` | Standard uppercase HTTP methods; other strings become `OTHER` |
| `status-code`, `status_code`, `statusCode`, `http.status_code`, `http.response.status_code` | `status` | Integer HTTP status codes from 100 through 999; floats, strings, and other types are dropped |
| `status` | `status` | Integer HTTP status codes as above, or existing domain-state strings from static configuration |
| `error.type` | `error.type` | Explicit `panic` classification; other direct values are dropped |

Other accepted keys keep their existing spellings, including `ghatd-package`
and `operation`. Aliases that produce the same OTLP key follow the ordinary
last-value-wins behavior, including fields attached with `With`. Existing
user/group lifecycle logs use configurable `status` strings; these remain
supported for compatibility and must not contain request data or identifiers.
Use an explicit HTTP status alias when string values must be rejected.

By default, `zap.Error` and `zap.NamedError` export only the named concrete error type,
without evaluating an error method in the OTLP branch. Anonymous error types
use `error`, and generic type arguments are omitted, so reflected struct tags
cannot enter telemetry. Local logs retain their existing error behavior.
Keep log messages static; field filtering does not redact interpolated message
text or the logger's automatic caller and stack metadata.

Register custom provider or source names from trusted startup constants:

```go
providerValues, err := observability.WithLogFieldValues("provider", "custom-payment")
if err != nil {
    return err
}
sourceValues, err := observability.WithLogFieldValues("source", "application")
if err != nil {
    return err
}
appLogger = observability.TeeLogger(appLogger, telemetry.LoggerProvider(), providerValues, sourceValues)
```

Only `source` and `provider` support extensions. Each option accepts at most 32
distinct, case-sensitive names, each 1–64 ASCII bytes starting with a letter and
containing letters, digits, dots, underscores, or hyphens. Invalid configuration
returns an error without repeating the supplied value. Options snapshot their
input, and each logger receives an independent immutable policy. The last option
for a field replaces its earlier extensions; an empty list clears them. Built-in
values remain accepted. Request values never register themselves, even when they
look like valid identifiers. Mock and custom providers require their exact
configured names; no name prefixes are automatically trusted.

## Service operations and error classification

Create one `Operations` instance at startup and inject it into your services.
It starts internal spans, rebinds the context logger to each operation span,
and records completed count, duration, and active-operation metrics. Return
constructor errors as startup failures so invalid instrument configuration is
visible immediately:

```go
classifier, err := observability.NewErrorClassifier(
    observability.ErrorRule{
        Err:     ErrQuotaExceeded, // an application-owned static sentinel
        Code:    "APP-001",
        Outcome: observability.OutcomeRejected,
    },
    observability.ErrorRule{
        Err:     ErrDependencyUnavailable,
        Code:    "APP-002",
        Outcome: observability.OutcomeError,
    },
)
if err != nil {
    return err
}
operations, err := observability.NewOperations(observability.OperationConfig{
    Scope:          "example.com/service/internal/services",
    MetricPrefix:   "example.service.operation",
    TracerProvider: telemetry.TracerProvider(),
    MeterProvider:  telemetry.MeterProvider(),
    Errors:         classifier,
})
if err != nil {
    return err
}
appLogger = observability.TeeLogger(appLogger, telemetry.LoggerProvider(),
    observability.WithLogErrorClassifier(classifier))
```

Use static operation names and a named error result. Defer `Finish` directly so
it observes both the returned error and a panic:

```go
func (service *Service) ProcessOrder(ctx context.Context) (err error) {
    ctx, operation := service.operations.Start(ctx, "process-order")
    defer operation.Finish(&err)
    return service.process(ctx)
}
```

Do not put `Finish` inside another deferred closure: Go's `recover` must run
directly in the deferred method. `Finish` records a constant panic classification
and rethrows the identical value; it never formats the payload. `End(err)` is
available for explicit completion, and `Finish(nil)` handles functions without
an error result. Completion is first-wins and safe against duplicate/concurrent
calls. A later panic still propagates after an earlier `End`, but cannot change
an already completed span or metric. A nil `*Operations` returns an inert operation
for optional instrumentation while preserving panic behavior.

The prefix above emits `example.service.operation.count`, `.duration`, and
`.active`. Count and active have no unit, preserving their exported metric names;
duration uses `s`. Count and duration have only `operation` and `outcome` labels,
and active uses only `operation` so every completion balances its increment.
Default duration boundaries in seconds are
`0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30`.
`DurationBuckets` accepts 1–128 finite, positive, strictly increasing boundaries
and is copied at construction. An omitted slice selects defaults; an explicitly
empty slice is invalid. Omitted providers use OTel globals; empty scope/prefix
select GHATD's defaults. Applications can keep their existing scope and prefix
when adopting this helper.

| Result | Outcome | Error code | Span status |
| --- | --- | --- | --- |
| Nil returned error | `success` | None | Unset |
| Registered expected rejection | `rejected` | Registered code | Unset |
| Wrapped cancellation | `cancelled` | `cancelled` | Unset |
| Wrapped deadline | `timeout` | `timeout` | Error |
| Registered internal failure | `error` | Registered code | Error |
| Unregistered error | `error` | `internal` | Error |
| Recovered panic | `panic` | `panic` | Error |

`errors.Is` recognizes wrapped and joined errors. Deadlines take precedence over
cancellation, followed by the first matching registered rule. Nil errors remain
successful even if the context was cancelled. Rules explicitly distinguish
expected rejection from internal failure; an HTTP response status alone cannot
make that decision. Error messages, concrete types, custom `ErrorType` values,
and panic payloads never enter operation telemetry.

Classifiers snapshot up to 128 rules, each with a non-nil comparable sentinel,
a static code of 1–64 ASCII bytes, and either `OutcomeRejected` or `OutcomeError`.
Codes start with a letter and contain only letters, digits, dots, underscores,
or hyphens. Built-in codes and sentinel identities cannot be overridden, and
duplicate sentinel identities are rejected. Sentinel implementations must stay
immutable and support safe `errors.Is` matching. Codes may come from an
application's error manifest without coupling this package to its reply layer.
A nil classifier uses only built-in and unknown-error classifications.

Spans expose the code as `error.type` and the finite outcome category as
`error.category`. `WithLogErrorClassifier` applies the same registry to
`zap.Error`/`zap.NamedError` on the OTLP branch, including native trace/span
correlation. Passing nil explicitly selects default classification; omitting the
option retains the existing concrete-type behavior. Arbitrary string error fields
remain rejected, and local logs keep their existing behavior.

Scope, prefix, operation names, codes, and sentinels are trusted startup/code
constants. `Start` does not enforce a name registry: never pass request values,
identifiers, URLs, or arbitrary error text as operation names or configured codes.
Pass the returned context into downstream calls so their spans and logs retain
the correct parent.

## Redis module commands

Redis spans and metrics recognize a fixed set of core command names, including
the commands issued by go-redis v7. Unrecognized commands use `redis.unknown`
and `db.operation.name=unknown`, even when their names look syntactically valid.
This prevents arbitrary custom command names from becoming telemetry labels.
Pipelines remain one `redis.batch` span with `db.operation.name=BATCH`.

Register additional module commands using trusted constants during startup:

```go
moduleCommands, err := observability.WithRedisModuleCommands("FT.SEARCH", "JSON.GET")
if err != nil {
    return err
}
client.AddHook(observability.NewRedisHook(redisOptions, moduleCommands))
```

Registration accepts at most 64 names, each 1–64 ASCII bytes starting with a
letter and containing only letters, digits, dots, underscores, or hyphens.
Names match case-insensitively and appear lowercase in telemetry. `unknown`
and `batch` are reserved. Each option snapshots its input and each hook receives
an independent immutable set. If multiple module-command options are supplied,
the last replaces the preceding list; an empty list clears it. There is no
runtime registration or automatic Redis command discovery.

Use only static operational names in the registration list. Request values,
keys, identifiers, and query text do not belong there. Command arguments stay
excluded for both recognized and unrecognized operations.
