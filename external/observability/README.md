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

- Wrap Gorilla Mux handlers with `HTTPServerMiddleware`.
- Construct outbound clients with `NewHTTPClient` or wrap a transport with
  `NewRoundTripper`.
- Attach `NewMongoCommandMonitor` to MongoDB v2 client options.
- Attach `NewRedisHook` to Redis v7 clients and always execute commands with
  the caller's context.
- Use `TeeLogger` to preserve existing Zap output while exporting sanitised
  records, and `WithTraceContext` when placing a logger into a traced context.

The OTLP logging branch exports only an explicit allowlist of low-cardinality
operational fields; arbitrary primitive fields and opaque values are dropped.
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
