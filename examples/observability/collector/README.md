# Optional production Collector path

This example adds a private OpenTelemetry Collector with persistent exporter
queues, bounded retries, resource enrichment and independent export-health
monitoring. It uses Collector Contrib **0.160.0**. The reference service and its
direct-to-LGTM defaults remain available without this overlay.

```text
service SDKs → private Collector → OTLP backend
                    ↓
             persistent queues

independent Prometheus → Collector selfmetrics → alert rules
```

The independent scrape continues during a backend outage. The Collector also
forwards its own metrics through OTLP for normal investigation; that route
shares the exporter's failure conditions.

## Try the complete local path

From the repository root, copy the example settings and start the reference
stack plus the Collector and its optional loopback diagnostic ports:

```sh
cp examples/observability/collector/.env.example examples/observability/collector/.env
docker compose --env-file examples/observability/collector/.env \
  -f examples/observability/compose.yaml \
  -f examples/observability/collector/compose.yaml \
  -f examples/observability/collector/compose-local.yaml up -d --wait --wait-timeout 120
```

The Collector overlay itself publishes no host ports. `compose-local.yaml`
explicitly enables loopback-only HTTP OTLP on 14318, gRPC on 14317, health on
13134 and the independent monitor on 9091. These ports are configurable in the
copied settings. Grafana and the reference LGTM ports keep their original
defaults; adjust them separately if another local stack is running.

Export the application's configuration in the shell running the reference
service. Use the Collector port, rather than the reference stack's direct OTLP
port:

```sh
export OTEL_EXPORTER_OTLP_ENDPOINT=http://127.0.0.1:14318
export OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf
export OTEL_TRACES_EXPORTER=otlp
export OTEL_METRICS_EXPORTER=otlp
export OTEL_LOGS_EXPORTER=otlp
export OTEL_METRIC_EXPORT_INTERVAL=5000
export OTEL_TRACES_SAMPLER=parentbased_always_on
go run ./cli telemetry doctor --probe --timeout 10s
go run ./examples/observability serve
```

In another terminal, run `python3 examples/observability/smoke.py` to verify
actual metrics, trace parentage and correlated logs in LGTM. Doctor acceptance
means the Collector accepted the request; queued acceptance alone does not
prove delivery to the final backend. The smoke check verifies that later step.

Open [the independent monitor](http://localhost:9091) to inspect targets,
queue pressure and alert state. Its rules evaluate locally. Configure an
Alertmanager in `prometheus.yaml` to send notifications; the example supplies
no notification destination or credentials.

## Configure another backend

The application needs only its identity and a reachable private Collector
endpoint. Put upstream configuration on the Collector:

| Collector setting | Purpose |
| --- | --- |
| `COLLECTOR_BACKEND_ENDPOINT` | OTLP HTTP base URL; the exporter appends signal paths. Use HTTPS for a hosted backend. |
| `COLLECTOR_AUTHORIZATION` | One decoded `Authorization` header value supplied by a secret manager. |
| `COLLECTOR_SERVICE_NAMESPACE` | Fallback namespace inserted only when a resource lacks one. |
| `COLLECTOR_ENVIRONMENT` | Fallback deployment environment inserted only when absent. |
| `COLLECTOR_SERVICE_NAME` | Identity for the Collector's own forwarded metrics. |
| `COLLECTOR_INSTANCE_ID` | Unique Collector replica identity for forwarded selfmetrics; defaults to the container's runtime hostname. |

For example, a header value has the form `Basic <encoded-credential>`. Do not
pass an `OTEL_EXPORTER_OTLP_HEADERS` string such as
`Authorization=Basic%20<encoded-credential>` as that value. Do not commit real
credentials or publish resolved configuration containing them.

`resource/deployment` uses **insert**, preserving application service names,
versions, namespaces, environments and instance IDs. `resource/self` runs only
in the self-scrape pipeline and assigns its separate name and instance ID.
Set any explicit `COLLECTOR_INSTANCE_ID` in the Collector container's runtime
environment; the default Compose file deliberately leaves it unset. It must
be nonempty and unique. Give each Collector replica a unique value outside Docker;
for example, use the pod UID in Kubernetes. Scraping each replica's loopback
address must not give every forwarded metric stream the same identity.

Receivers on 4317/4318 and selfmetrics on 8888 need an explicitly restricted
network boundary. Keep them on a trusted private network, and add transport
authentication/TLS where that boundary requires it. Do not expose the receiver
or diagnostic ports through a public ingress. An application container can
use `http://otel-collector:4318` on the example's shared Compose network.

## Capacity, persistence and recovery

The shipped configuration uses a 512 MiB container budget and a memory limiter
before other processors. Each signal has a **256 MiB byte queue**, four export
consumers and a 1 GiB maximum file size per storage database. Metrics from the
application and the self-scrape share the metrics exporter queue. Batches flush
after one second or the configured size boundary; export calls have a ten-second
timeout, with retry intervals from one to thirty seconds and a five-minute
retry budget.

The named `collector-queue` volume holds queue files with owner 10001 and mode
0700. A bounded one-shot initializer prepares this volume; the Collector itself
runs without root privileges. Ordinary `docker compose down` retains the named
volume. Recreate the Collector with the same volume and exporter component IDs
to recover its queued work. Do not remove that volume while it has a backlog.

Persistent queues protect data already handed to the exporter, including
dispatched entries waiting for acknowledgment. They do not persist SDK buffers,
pending processor batches or ordinary tail-sampling decisions. A crash can lose
those earlier buffers. Permanent rejection, retry exhaustion, full queues and
storage failures can still lose telemetry, and uncertain acknowledgments can
cause duplicate delivery. A queue capacity is not a retention-time guarantee.

The Compose overlay runs one Collector. Do not use `--scale` with its shared
named volume and static scrape target. For multiple replicas, give each process
its own persistent volume and scrape target. A StatefulSet with one retained claim per replica is one suitable
Kubernetes arrangement. The replicas do not replicate their queues. Drain and
check each queue before removing a replica, deleting storage, changing exporter
IDs or disabling the export path. Mounting the same database files into multiple
Collector processes is unsupported.

The default alert rules cover queue pressure, missing queue/runtime metrics,
enqueue/export failures, refused/internal receiver failures, high memory,
early tail-buffer eviction and unavailable/missing scrape targets. Ratios retain
each replica and signal. Raw metric names match this configuration's
`without_type_suffix` and `without_units` settings. Forwarded OTLP metrics can
have different backend normalization, so do not assume the same raw alert names
work unchanged in every hosted query engine.

The memory alert assumes this example's 512 MiB budget; adjust it with the
container limit. Add volume-capacity monitoring from the host or orchestrator.
Connect the independent monitor to an external notification path and monitor
the monitor itself; no process can report its own complete disappearance.
The `/healthz` endpoint reports Collector availability, including during a
backend outage. It is not an end-to-end delivery check.

## Optional tail sampling

Add `-f examples/observability/collector/compose-tail.yaml` after the Collector
overlay in the same Compose command to load `collector-tail.yaml`. This is an
explicit single-Collector option. It retains error traces, traces lasting at
least two seconds, and a 10% baseline sample. The default configuration has no
tail sampler.

Send full head input from every contributing service and disable HTTP path
suppression where errors must be retained. A parent-based sampler still honors
an unsampled incoming parent; tail sampling cannot recover discarded spans.
Metrics and logs remain unsampled, so some log IDs and exemplars can refer to
traces deliberately absent from the backend.

Every span of a trace must reach the same deciding Collector. Do not scale this
basic tail overlay to multiple replicas behind an ordinary load balancer. A
larger installation needs routing by trace ID before the sampling tier. The
30-second decision window and bounded trace/decision caches also limit late or
long-running work: a command that fails after the window is not guaranteed to
be retained. Tail decisions are in memory and do not survive restart.

## Validate and stop

The configuration/rule check and real queue-recovery contract use only pinned
containers and synthetic telemetry:

```sh
docker pull otel/opentelemetry-collector-contrib:0.160.0
docker pull busybox:1.37.0
docker pull prom/prometheus:v3.5.0
python3 examples/observability/collector/test_config.py
python3 examples/observability/collector/test_alerts.py
GHATD_TEST_COLLECTOR=1 go test -race ./external/observability -run TestCollectorResilienceContract
```

The rule fixtures exercise all eleven shipped alerts across sixteen scenarios,
including mixed signals, resets, missing data and asymmetric replica capacity.
The real Go contract is opt-in with `GHATD_TEST_COLLECTOR=1`; an opted-in test
fails if Docker or the images are missing. It owns and removes its temporary containers, network and volume.

Stop the example with the same Compose file list and `down`. Keep the queue
volume until delivery is complete. Remove it explicitly only when its data is
no longer needed.

See the pinned upstream [persistent storage documentation](https://github.com/open-telemetry/opentelemetry-collector-contrib/blob/v0.160.0/extension/storage/filestorage/README.md),
[exporter queue/retry behavior](https://github.com/open-telemetry/opentelemetry-collector/blob/v0.160.0/exporter/exporterhelper/README.md),
and [tail-sampling requirements](https://github.com/open-telemetry/opentelemetry-collector-contrib/blob/v0.160.0/processor/tailsamplingprocessor/README.md).
