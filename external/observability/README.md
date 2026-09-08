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
