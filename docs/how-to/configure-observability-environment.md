# Configure observability through your deployment environment

The operator owns deployment, destinations and secrets. GHATD supplies runtime
configuration; it does not provision a backend, create secrets, change your
orchestrator or deploy the service. The examples below show where to supply the
same variables using different process managers. Adapt them to your application
and apply them through your own deployment process.

For supported settings, validation and precedence, use the
[configuration reference](../../external/observability/CONFIGURATION.md).
For application code, use the [integration guide](add-service-observability.md).

## Choose a runtime profile

Start with an explicit profile. The SDK defaults **each unset exporter to
`otlp`**; an application may ship explicit `none` values instead. Omitting a
variable is not a reliable way to disable a signal.

| Variable | Export disabled | Traces only |
|---|---|---|
| `OTEL_TRACES_EXPORTER` | `none` | `otlp` |
| `OTEL_METRICS_EXPORTER` | `none` | `none` |
| `OTEL_LOGS_EXPORTER` | `none` | `none` |
| `OTEL_TRACES_SAMPLER` | `always_on` | `always_on` |

With traces enabled, `always_on` records local sampling decisions even when an
incoming parent was not sampled. It does not use `OTEL_TRACES_SAMPLER_ARG`.
A host's separate HTTP suppression policy or a Collector's sampling processor
can still discard traces. Export success and retention remain backend concerns.
To change sampling later, choose the intended sampler explicitly; setting only
its ratio cannot change an `always_on` sampler.

For OTLP/HTTP trace export, supply:

```dotenv
OTEL_TRACES_EXPORTER=otlp
OTEL_METRICS_EXPORTER=none
OTEL_LOGS_EXPORTER=none
OTEL_TRACES_SAMPLER=always_on
OTEL_EXPORTER_OTLP_TRACES_PROTOCOL=http/protobuf
OTEL_EXPORTER_OTLP_TRACES_ENDPOINT=https://collector.example/v1/traces
OTEL_RESOURCE_ATTRIBUTES=service.namespace=example-platform
```

The example endpoint must be replaced with your receiver's real URL. A
signal-specific HTTP endpoint includes its full path; a shared
`OTEL_EXPORTER_OTLP_ENDPOINT` is a base URL to which exporters append signal
paths. For gRPC select `grpc` and use an origin URL, without `/v1/traces`.
Inside containers, `localhost` is that container; use a reachable receiver.

Inject `OTEL_EXPORTER_OTLP_TRACES_HEADERS` separately through your existing
secret mechanism. It contains comma-separated `header=value` pairs; special
characters in values must be percent-encoded. Signal-specific headers replace
the generic header map, so include every required header for that signal.
Remove stale generic and per-signal settings when switching destinations.
Backend keys stay server-side, never in browser `VITE_*` variables.

`OTEL_LOGS_EXPORTER=none` preserves normal application logs and their trace IDs.
Keep the existing stdout/container-log shipping path if it already feeds your
log backend. Metrics are independent: `none` prevents this SDK's metric export,
but does not control other collectors or cluster scrapers.

## Set identity in the layer that owns it

An explicit Go `Config.ServiceName` wins over `OTEL_SERVICE_NAME`. Check the
host's settings mapping before using an environment variable to rename a
service. Environment, version and namespace follow the same documented Go
configuration precedence. Give each process role a stable service name and
replicas distinct instance IDs; omit instance ID to use GHATD's process UUID.

The opt-in MongoDB helper `WithTelemetryFromEnvironment(component, scope)` uses
`COMPONENT` or the supplied fallback, appends `-mongo-migrator`, and reads
`ENVIRONMENT`, `GIT_COMMIT`, `LOG_LEVEL` and `GRACEFUL_SERVER_TIMEOUT`. Those
names are helper settings, not universal mappings for every GHATD host.
Migration jobs and workers need their own copy of the chosen runtime variables;
configuring only the HTTP process does not configure separate processes.

## Local shell or service manager

A shell file may export the non-secret variables above. Source only a trusted
file, inject secrets through your normal tooling, then start your application.
Changing the shell after a process starts does not update its environment.

For a systemd-managed application, the equivalent service fragment is:

```ini
[Service]
EnvironmentFile=/etc/example/observability.env
```

Use systemd environment-file syntax (`NAME=value`, without `export`) and your
existing protected secret-loading mechanism. Review and restart the service
through your normal operating procedure. GHATD does not read `.env` files by
itself; a host or process manager must load them.

## Docker Compose

Compose's project `.env` supports interpolation; it does not automatically pass
all its entries into every container. Supply an `env_file` or explicit
`environment` entries for each application role:

```yaml
services:
  application:
    env_file:
      - ./observability.env
    environment:
      OTEL_EXPORTER_OTLP_TRACES_HEADERS: ${OTEL_EXPORTER_OTLP_TRACES_HEADERS:?supply trace headers}
```

This fragment assumes an existing application service and an authenticated
receiver. Omit the header entry for an unauthenticated local Collector. Supply
the non-secret profile in `observability.env`, keep credentials out of committed
files, and use your own Compose update procedure. Container environment changes
require container recreation; restarting an old container keeps its old config.

## Kubernetes or a chart

Add non-secret variables to the existing workload's `env` or a ConfigMap. Point
the secret-bearing variable at a Secret that the operator already manages:

```yaml
env:
  - name: OTEL_TRACES_EXPORTER
    value: "otlp"
  - name: OTEL_METRICS_EXPORTER
    value: "none"
  - name: OTEL_LOGS_EXPORTER
    value: "none"
  - name: OTEL_TRACES_SAMPLER
    value: "always_on"
  - name: OTEL_EXPORTER_OTLP_TRACES_PROTOCOL
    value: "http/protobuf"
  - name: OTEL_EXPORTER_OTLP_TRACES_ENDPOINT
    value: "https://collector.example/v1/traces"
  - name: OTEL_EXPORTER_OTLP_TRACES_HEADERS
    valueFrom:
      secretKeyRef:
        name: example-telemetry
        key: traces-headers
```

For a chart, place these entries in its supported values field and inspect the
rendered `env` for every API, worker and migration container. Do not assume a
particular chart field or that init containers inherit the main container's
variables. Kubernetes does not expand `${NAME}` in a literal `env.value`;
use `valueFrom` or the chart's documented rendering. The operator owns Secret
creation, rollout and termination-grace settings. Environment sourced from a
Secret changes only when the process/pod is replaced.

## Browser build and intake are separate switches

For hosts using GHATD's browser package, a Vite integration commonly gates the
SDK with `VITE_BROWSER_TELEMETRY_ENABLED` at **build time**. Setting that flag
only on a running Go container does not rebuild embedded assets. The host must
also enable its server-side intake, set exact allowed origins and provide its
consent policy. Host-specific `BROWSER_TELEMETRY_*` names and route/API
vocabularies belong in that host's guide.

A browser receives no exporter credentials. It sends bounded batches to the
host's intake, whose server-side exporter uses the runtime's trace destination.
The browser's service identity is configured in Go by the host and is rebuilt
at intake; client-supplied resource identity is not trusted.

## Check the configuration before the operator deploys

Inspect the effective environment of each process role without printing header
values or secrets. Confirm identity, explicit signal choices, endpoint/protocol
alignment and whether browser settings need a rebuild. `InspectConfiguration`
or GHATD's [doctor](../../external/observability/DOCTOR.md) can validate supported
settings without sending telemetry. Its optional probe does send synthetic data
and is a separate operator choice. Neither proves indexing or retention.

`OTEL_CONFIG_FILE`, `OTEL_SDK_DISABLED` and `OTEL_PROPAGATORS` are not configuration
entry points for this integration. Use supported exporter settings and the host's
Go wiring. After the operator deploys, they can confirm one request's log trace ID
in the chosen trace backend; cross-backend links require a delivered, retained trace.
