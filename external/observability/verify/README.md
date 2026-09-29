# Shared observability verification

Run the reusable checks against the deployment assets shipped by your host
application. Python sources and synthetic fixtures are embedded in the Go
command, so a downloaded GHATD module is sufficient; no sibling checkout or
copied test scripts are needed.

From the host repository, using its pinned GHATD module:

```sh
go run github.com/ooaklee/ghatd/external/observability/verify \
  --root . --profile monitoring/verification.json --suite all
```

Use an exact GHATD revision in `go.mod`. All five suites must be configured;
`all` never silently skips a missing profile section. The per-suite deadline
defaults to ten minutes (`--timeout 10m`). `--python` selects an interpreter.
Errors in profile structure or local paths fail before the interpreter runs.

## Requirements and scope

Install Python 3 with PyYAML 6.0.3, Docker with Compose, Helm 3, Make and
`envsubst`. Pre-pull the fixture images; checks use `--pull=never`:

```sh
docker pull prom/prometheus:v3.5.0
docker pull grafana/loki:3.5.0
docker pull python:3.12-alpine
docker pull otel/opentelemetry-collector-contrib:0.160.0@sha256:799dc6cf12c96192af37b5bdba804da8c10b3bc563b43cb90c3f3c58d9572ad6
```

These suites implement the shared GHATD observability asset contract. They are
not validators for arbitrary charts or dashboards. The supplied profile adapts
identity, metric names, panel IDs, asset paths and process topology. Changes to
the common contract belong in the shared suites and their tests.

| Suite | Checks |
| --- | --- |
| `collector-compose` | Disabled/direct/queued routing for all roles, readiness, private ports, queue storage, monitoring and provenance hashes |
| `collector-render` | Private persistent Collector, worker overrides, tail/monitoring variants, fixed invalid-value diagnostics, schema validation and atomic rendering |
| `production-render` | Declared disabled or trace-only defaults, Secret references and ordering, alternate direct routing and exporter-off rollback across every configured process |
| `dashboards` | Shipped PromQL with replica resets, idle/missing data, freshness, histograms, cache and consumer outcomes |
| `production-logs` | Real Loki queries with plain/wrapped logs, duplicates, stream isolation, summaries and Loki-backed variables |

Suites create synthetic configurations and disposable fixture containers, and
remove them on ordinary completion or failure. They do not apply cluster
resources. Caller credential environment variables are not forwarded; the
profile's `fixture_env` must contain only synthetic substitution values.
Docker connection settings and tool selectors remain available. Force-killing
the runner can interrupt cleanup; remove test-owned containers if that occurs.

## Profile

Start with [examples/example.json](examples/example.json). Version 1 uses:

- `service.name` and `service.namespace` for application log selectors.
- `dashboards.paths`: exactly two files, ordered local then production. The
  common HTTP/dependency metric contract is fixed; configure the application
  cache/queue metric names and a queue operation. `panels` optionally remaps
  semantic names from `python/common.py`; effective IDs must remain unique.
- `compose`: base files, monitoring and Collector overlays; all process roles
  and the subset that runs the application; synthetic environment values; and
  the directory containing copied Collector assets and `source.json`.
- `helm`: chart and render-script/output paths; value filenames relative to
  the chart; service port; enabled migrator/sidekick; and optional extra workers
  with their chart value key and generated name suffix.

By default `production-render` expects all production exporters disabled. A
host with direct trace-only OTLP/HTTP export declares `helm.production_trace`:

```json
{
  "endpoint": "https://traces.example.com/v1/traces",
  "header_name": "x-example-team",
  "credential_env": "TRACE_API_KEY",
  "secret_name": "example-telemetry",
  "secret_key": "api-key",
  "remote_key": "/example/live/TRACE_API_KEY"
}
```

These are routing identifiers, never credential values. The suite verifies the
exact HTTPS endpoint, `http/protobuf` signal protocol, header expansion from a
preceding Secret-backed environment entry, and its ExternalSecret remote key.
The ordering assertion follows Kubernetes' [dependent environment variable
rules](https://kubernetes.io/docs/tasks/inject-data-application/define-interdependent-environment-variables/).
Every declared process must have the same route with `always_on`, no HTTP
suppression, and metrics/log exporters disabled. It also tests an alternate
synthetic trace-header Secret and disabling all exporters. No backend requests,
secret retrieval or cluster changes are performed.

All asset paths are relative to the host root. Existing files and symlinks must
resolve inside that root. The output may not exist yet. Unknown fields, invalid
vocabularies and topology mistakes are rejected with value-free diagnostics.
The example paths are placeholders; point them at your actual assets.

The chart contract uses `servicePodConf`, `initJobs`, `sidekickContainer` and
`otelCollector`. Extra worker keys and suffixes come from the profile. The
render script accepts `ENVIRONMENT`, `SERVICE_NAME`, `GIT_COMMIT`,
`DOCKER_IMAGE_REGISTRY` and `ENABLE_OTEL_COLLECTOR` and writes the configured
output below its working directory. Compose uses `otel-collector`, `otel-collector-ready`,
`otel-collector-queue-init`, `otel-lgtm` and `collector-monitor`, with Make’s
`start DOCKER_ENV=local ENABLE_MONITORING= ENABLE_OTEL_COLLECTOR=0|1` contract.
Dashboards use `DS_METRICS`, `DS_TRACES`, `DS_LOGS`, `service`, `environment`
and production `log_pod`/`log_container` variables, standard
`service_name`/`deployment_environment_name` metric labels and the reference
Tempo trace panel. Keep those contracts when
adopting the reference assets, or extend the harness explicitly.

Vendored asset hashes are checked against `source.json`. Its `source_commit`
records asset provenance and may differ from the current Go dependency when
those files are unchanged. Optionally add `--foundation-dir /path/to/checkout`
to compare the copies with `examples/observability/collector` in a source checkout.

## Develop the harness

```sh
go test ./external/observability/verify
python3 -m unittest discover -s external/observability/verify/python -p 'test_*.py'
```

Run all five suites against the host after changing its profile or deployment
assets. Package unit tests validate the runner and configuration boundaries;
they do not substitute for executing the shipped queries and render scripts.
See [ADR022](../../../docs/adr/adr022-shared-browser-and-verification-distribution.md)
for distribution and application ownership.
