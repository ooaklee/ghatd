# Diagnose a telemetry connection

Run the doctor from a GHATD checkout with the same `OTEL_*` environment as your
service. Start with inspection, which creates no SDK, changes no global
providers, opens no listener and sends no telemetry:

```sh
go run ./cli telemetry doctor
```

The JSON report describes each signal's exporter, protocol, configuration
source, endpoint class, TLS, header presence and timeout. It reports identity
presence and precedence without printing identity values. It never prints
endpoint hosts or custom paths, header names or values, certificate paths, or
invalid input values. Static issue codes and field names identify settings to
check against the [configuration reference](CONFIGURATION.md). Certificate
validation may read the configured local certificate files.

The standalone CLI inspects environment configuration with an empty
`observability.Config`. If your service supplies resource values in Go, call
`observability.InspectConfiguration(yourConfig)` in its diagnostic command to
inspect the same precedence. Registered custom exporters appear as `custom`;
inspection cannot prove that a registration exists in another executable.

To test receiver acceptance explicitly:

```sh
OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4318 \
OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf \
go run ./cli telemetry doctor --probe --timeout 10s
```

The probe sends one valid synthetic OTLP protobuf request per enabled signal,
using effective endpoint, headers, TLS and compression settings. All requests
share the total timeout, with each also limited by its exporter timeout. The
maximum total timeout is one minute. It does not retry or follow HTTP redirects.
It sends only fixed diagnostic attributes under
`service.name=ghatd-telemetry-doctor`, with fresh random trace/span IDs. It does
not export your configured application identity or sample application traffic.

| Status | Meaning |
| --- | --- |
| `accepted` | The receiver acknowledged this signal without rejecting records. |
| `disabled` | The signal exporter is `none`; no request was sent. |
| `invalid_configuration` | Configuration validation failed before any export. |
| `auth` | HTTP 401/403 or gRPC authentication/permission rejection. |
| `unreachable` | A connection or transport could not reach a usable receiver. |
| `timeout` | The request exceeded its time budget or was cancelled. |
| `partial` | The receiver acknowledged the request but rejected records. |
| `rejected` | The receiver returned another rejection or invalid response. |
| `unsupported` | The exporter is not OTLP; remote acceptance cannot be tested. |

The command succeeds only when configuration is valid and every requested probe
is `accepted` or `disabled`. A failure exits nonzero with a sanitized report;
remote error messages and response bodies are never included. Console,
Prometheus and custom exporters remain usable by the SDK, but the remote probe
does not test them. There is no probe request for a disabled signal.

Acceptance proves that this receiver accepted these synthetic records. It does
not prove that your application's SDK is configured identically, that a
Collector has delivered its queued data, or that a backend has indexed it. Use
the [reference kit's smoke check](../../examples/observability/README.md) to
verify metric queries, trace relationships and native log correlation in LGTM.

## Repeatable SDK and Collector contracts

The ordinary Go suite runs HTTP/protobuf and gRPC receivers against the actual
autoexport SDK. A synthetic request crosses an HTTP server, a business
operation, an instrumented HTTP client and a downstream HTTP server. Synthetic
MongoDB and Redis callbacks exercise their hooks without database containers.
Assertions examine serialized OTLP, including resource identity across signals,
parent IDs, native log IDs, operation counts and duration buckets, runtime
metrics, and absence of private request/query/body/error canaries.

```sh
go test ./external/observability -run TestOTLPWireContract -count=1
```

The Docker contract runs that same fixture through the pinned OpenTelemetry
Collector, then checks its exported records after graceful shutdown. It uses
ephemeral ports on host loopback, no credentials and no host bind mounts. Docker
is required only when this test is explicitly enabled:

```sh
docker pull otel/opentelemetry-collector-contrib:0.160.0
GHATD_TEST_COLLECTOR=1 go test ./external/observability -run TestCollectorContract -count=1
```

When enabled, an unavailable Docker daemon or failed Collector startup fails the
test. The test removes only containers it creates. The
[observability CI workflow](../../.github/workflows/observability-contract.yml)
runs these contracts for changes to the foundation; it does not connect to a
production backend. This checks transport and instrumentation compatibility,
while the LGTM smoke check covers storage and query behavior.

`SDK.ForceFlush(ctx)` flushes all three providers with the supplied context and
joins their errors. It is useful for bounded diagnostics and tests; regular
services should use their normal periodic/batch exporters and call
`Runtime.Shutdown` after draining application work. A flush does not replace
shutdown or guarantee indexing at the final backend.
