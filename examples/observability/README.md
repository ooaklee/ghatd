# Runnable observability reference

This example runs a small HTTP service and a standalone Cobra action using
GHATD's shared runtime, operation instrumentation, request logging and recovery.
The optional local stack contains an OpenTelemetry Collector, Grafana, Tempo,
Prometheus and Loki. No vendor account, application database or API key is
required.

For persistent exporter queues, deployment enrichment, independent export-health
alerts and optional tail sampling, use the [Collector overlay](collector/README.md).

The successful request follows this chain:

```text
GET /api/v1/work
  → process-work business operation and correlated log
    → outbound HTTP client
      → GET /dependency
```

`GET /api/v1/rejected` demonstrates an expected quota rejection with the static
code `EXAMPLE-001`. Both routes return a trace ID for checking their exact trace.
Request values are not used as span names, metric labels or log fields.

## Run the complete local stack

Requirements: the Go toolchain declared in the repository's `go.mod`, Docker
with Compose, and Python 3 for the smoke check. Run these commands from the
repository root.

Copy the example configuration and start the stack:

```sh
cp examples/observability/.env.example examples/observability/.env
docker compose --env-file examples/observability/.env -f examples/observability/compose.yaml up -d --wait
```

In the terminal that will run the example, export those settings and start the
HTTP service:

```sh
set -a
. examples/observability/.env
set +a
go run ./examples/observability serve --listen 127.0.0.1:8080
```

In another terminal, run the smoke check:

```sh
python3 examples/observability/smoke.py
```

It sends successful and rejected requests, waits for real backend ingestion,
then verifies positive operation counts and duration buckets, HTTP metrics,
the exact server → operation → client → dependency trace relationships, and
native log trace/span IDs matching the request and operation spans. A timeout
or incomplete relationship returns a nonzero exit code. This checks the
shipped local configuration; it does not inspect a production service.

Open [Grafana](http://localhost:3000) with the development credentials
`admin` / `admin`. The **GHATD reference service observability** dashboard is
provisioned as the home dashboard. Select a service, environment and data
sources to explore requests, business operations, runtime metrics, traces and
logs. The HTTP service also exposes `GET /healthz`.

The stack's ports bind to loopback. Its data is local development state and
is discarded when the container is removed. The example makes a real HTTP
dependency call to its own listener; an external service is unnecessary.

## Run the standalone command

From a terminal with the same exported telemetry settings:

```sh
go run ./examples/observability work
```

The command starts an ephemeral local dependency, runs a business operation,
prints a JSON result with its trace ID, closes the dependency, and flushes
telemetry before returning. It runs independently of the HTTP service.
Its default service identity is `example-worker`; the server defaults to
`example-api`. The default namespace is `example` and environment is `local`.
`OTEL_SERVICE_NAME` and matching `OTEL_RESOURCE_ATTRIBUTES` values can override
these example defaults. The resource instance ID is unique per process.

The command's final metrics are exported on shutdown. A short-lived command
may have only one counter sample, so a Prometheus `rate` panel can be empty;
inspect the exact trace and correlated log for that invocation.

## Run without a telemetry backend

The same service and command work with all exporters disabled:

```sh
OTEL_TRACES_EXPORTER=none OTEL_METRICS_EXPORTER=none OTEL_LOGS_EXPORTER=none \
  go run ./examples/observability work
```

Use the same variables with `serve` for an HTTP-only run. The resource and
sampler configuration are still validated; disabling exporters does not bypass
invalid startup settings. The existing application logger remains available.

## Try explicit sampling

Health-check tracing stays enabled by default. To try explicit noise suppression:

```sh
go run ./examples/observability serve --suppress-http-noise
```

The flag selects only `GET`/`HEAD /healthz`. It keeps HTTP metrics and request
logs, but suppresses the server span and local descendants when no sampled
parent exists. A sampled incoming parent retains the existing trace under the
default parent-based sampler. A later health-check failure still has metrics
and logs; its trace cannot be recovered after this head decision. Correlated
logs can therefore contain IDs without a stored trace, and suppressed requests
do not contribute trace exemplars. The work routes and smoke check are unchanged.

For a service-wide root ratio, use the standard sampler variables rather than
changing request instrumentation:

```sh
OTEL_TRACES_SAMPLER=parentbased_traceidratio OTEL_TRACES_SAMPLER_ARG=0.1 \
  go run ./examples/observability serve
```

This also discards most error traces. Tail sampling requires full incoming
traces to retain errors; see the [sampling guide](../../external/observability/CONFIGURATION.md#sampling-and-propagation).

## Customize ports or identity

Edit `GRAFANA_PORT`, `OTLP_GRPC_PORT`, `OTLP_HTTP_PORT` and `PROMETHEUS_PORT` in
the copied `.env` before starting the stack. Source the file again after editing;
its HTTP exporter endpoint uses `OTLP_HTTP_PORT`. Change `--listen` separately
for the reference service, and pass the matching URLs to the smoke check:

```sh
python3 examples/observability/smoke.py \
  --app-url http://127.0.0.1:8080 \
  --grafana-url http://127.0.0.1:3000 \
  --prometheus-url http://127.0.0.1:9090 \
  --service example-api --environment local --timeout 90
```

The smoke check reads optional `GRAFANA_USER` and `GRAFANA_PASSWORD` values
for a customized local Grafana login. It prints fixed progress/results rather
than credentials, endpoint error text or backend response bodies.

For another service, keep the stack and choose that service's identity in the
dashboard. HTTP, runtime, trace and log panels are reusable as supplied.
Business panels use the example's `example.service.operation` prefix; change
their metric names when an application uses a different prefix. The synthetic
smoke check targets this reference service's routes and trace shape; adapt it
to a different application's contract.

## Dashboard behavior and troubleshooting

The example exports metrics every five seconds. Dashboard targets use the
same minimum interval and calculate rates per instance before aggregation.
Latency uses recent histogram bucket rates. Allow at least two metric samples
before expecting a rate, and a full query window before comparing changes.
Sparse observations can make histogram estimates imprecise.
The first export establishes a counter baseline. Run the smoke check again
after that baseline to produce fresh increments for the rate and latency panels.

The 5xx ratio remains empty when there is no request traffic in the window.
Missing business/dependency data remains empty. **Runtime metrics received in
5m** explicitly reports absence, while **Latest runtime metric age** shows the
newest sample's age for any selected instance. These indicators do not certify
every replica or successful delivery of every signal.

The default data source UIDs are `prometheus`, `tempo` and `loki`, supplied by
the pinned LGTM image. Metrics and logs use its `service_name` and
`deployment_environment_name` labels. The trace query uses the original
resource keys. Other backends may need equivalent resource-label promotion.
Identity dropdowns accept letters, digits, periods, underscores, colons,
slashes and hyphens; custom text and URL overrides are disabled.

If the smoke check times out, confirm the service is running and that its
exported endpoint points at the selected OTLP HTTP port. A changed service or
environment needs matching smoke flags. Inspect container health with:

```sh
docker compose --env-file examples/observability/.env -f examples/observability/compose.yaml ps
```

Use the [complete configuration guide](../../external/observability/CONFIGURATION.md)
for HTTP base URLs versus signal-specific paths, gRPC, header precedence,
signal disablement, TLS, sampling, metric cadence and shutdown budgets.
Keep a shared HTTP base URL free of a trailing slash with the pinned exporters.

## Stop and validate

Use Ctrl+C in the HTTP service terminal. It drains the server before flushing
the runtime. Remove the local stack when finished:

```sh
docker compose --env-file examples/observability/.env -f examples/observability/compose.yaml down
```

Validate code and configuration without starting the backend:

```sh
go test ./examples/observability/...
python3 -B -m unittest discover -s examples/observability -p test_smoke.py
docker compose -f examples/observability/compose.yaml config --quiet
python3 -m json.tool examples/observability/grafana/dashboards/service.json
```

The [shared package guide](../../external/observability/README.md) explains the
runtime and adapters used here. MongoDB and Redis hook wiring is included as
compile-checked example code; those examples are wiring guidance and do not
claim to exercise a live database.
