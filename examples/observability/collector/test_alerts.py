#!/usr/bin/env python3
"""Execute the shipped alert rules against reset, outage and capacity fixtures."""

import json
from pathlib import Path
import re
import subprocess
import tempfile


KIT = Path(__file__).resolve().parent
PROMETHEUS = "prom/prometheus:v3.5.0"
JOB = "otel-collector"


def rule_metadata():
    # Read only fixed single-line labels/annotations to avoid duplicating prose.
    # Promtool parses and executes the complete original YAML and expressions.
    metadata = {}
    current = None
    for line in (KIT / "alerts.yaml").read_text().splitlines():
        match = re.fullmatch(r"\s+- alert: (\w+)", line)
        if match:
            current = match[1]
            metadata[current] = {}
        elif current:
            match = re.fullmatch(r"\s+(severity|summary): (.+)", line)
            if match:
                metadata[current][match[1]] = match[2]
    assert len(metadata) == 11 and all(set(item) == {"severity", "summary"} for item in metadata.values())
    return metadata


def series(name, values, instance="collector-a", **labels):
    labels.update(job=JOB, instance=instance)
    rendered = ",".join(f"{key}={json.dumps(value)}" for key, value in sorted(labels.items()))
    return {"series": f"{name}{{{rendered}}}", "values": values}


def healthy(instance="collector-a"):
    data = [series("up", "1+0x12", instance), series("otelcol_process_uptime", "1+60x12", instance)]
    for signal in ("traces", "metrics", "logs"):
        labels = {"exporter": "otlp_http/backend", "data_type": signal}
        data += [series("otelcol_exporter_queue_capacity", "100+0x12", instance, **labels),
                 series("otelcol_exporter_queue_size", "0+0x12", instance, **labels)]
    return data


def replace(data, replacement):
    data[:] = [item for item in data if item["series"] != replacement["series"]]
    data.append(replacement)


def labelset(instance="collector-a", **extra):
    return dict(job=JOB, instance=instance, **extra)


def fixtures(metadata):
    tests = []

    def add(name, data, expected=None):
        expected = expected or {}
        checks = []
        for alert, info in metadata.items():
            checks.append({"eval_time": "6m", "alertname": alert, "exp_alerts": [
                {"exp_labels": dict(labels, severity=info["severity"]),
                 "exp_annotations": {"summary": info["summary"]}}
                for labels in expected.get(alert, [])]})
        tests.append({"name": name, "interval": "1m", "input_series": data, "alert_rule_test": checks})

    add("healthy idle collector with all three queues", healthy())
    data = healthy() + healthy("collector-b")
    labels = {"exporter": "otlp_http/backend", "data_type": "traces"}
    replace(data, series("otelcol_exporter_queue_size", "95+0x12", **labels))
    replace(data, series("otelcol_exporter_queue_capacity", "900+0x12", "collector-b", **labels))
    add("one full replica is not hidden by another's spare capacity", data,
        {"CollectorQueuePressure": [labelset(**labels)]})
    data = healthy()
    replace(data, series("otelcol_exporter_queue_size", "0 95 95 95 95 0 0", **labels))
    add("short queue pressure does not exceed the five-minute hold", data)
    data = healthy()
    replace(data, series("otelcol_exporter_queue_capacity", "0+0x12", **labels))
    replace(data, series("otelcol_exporter_queue_size", "95+0x12", **labels))
    add("zero capacity is missing queue telemetry not an infinite ratio", data,
        {"CollectorQueueTelemetryMissing": [labelset()]})
    data = healthy()
    missing = series("otelcol_exporter_queue_capacity", "", **labels)["series"]
    data = [item for item in data if item["series"] != missing]
    add("one missing signal is visible even when two remain", data,
        {"CollectorQueueTelemetryMissing": [labelset()]})
    data = healthy() + healthy("collector-b")
    data += [series("otelcol_exporter_enqueue_failed_spans", "50 51 0 1 1 1 1", exporter="otlp_http/backend"),
             series("otelcol_exporter_enqueue_failed_spans", "100+0x12", "collector-b", exporter="otlp_http/backend")]
    add("enqueue failures survive resets without paging the healthy replica", data,
        {"CollectorExporterEnqueueFailures": [labelset(exporter="otlp_http/backend")]})
    data = healthy()
    for signal in ("spans", "metric_points", "log_records"):
        data.append(series(f"otelcol_exporter_send_failed_{signal}", "100+0x12", exporter="otlp_http/backend"))
    add("old lifetime failures do not look like a current outage", data)
    data = healthy()
    for signal in ("spans", "metric_points", "log_records"):
        data.append(series(f"otelcol_exporter_send_failed_{signal}", "0+1x12", exporter="otlp_http/backend"))
    add("current send failures cover all signals", data,
        {"CollectorExporterSendFailures": [labelset(exporter="otlp_http/backend")]})
    data = healthy() + [
        series("otelcol_exporter_send_failed_spans", "0+0x12", exporter="otlp_http/backend"),
        series("otelcol_exporter_send_failed_metric_points", "0+1x12", exporter="otlp_http/backend")]
    add("zero or absent sibling counters cannot hide a failing signal", data,
        {"CollectorExporterSendFailures": [labelset(exporter="otlp_http/backend")]})
    for metric, alert in (("refused", "CollectorReceiverRefused"), ("failed", "CollectorReceiverInternalFailures")):
        data = healthy()
        for signal in ("spans", "metric_points", "log_records"):
            data.append(series(f"otelcol_receiver_{metric}_{signal}", "20 21 0 1 2 2 2", receiver="otlp", transport="http"))
        add(f"receiver {metric} distinguishes processing failures after resets", data,
            {alert: [labelset(receiver="otlp")]})
    data = healthy() + [series("otelcol_process_memory_rss", "480000000+0x12")]
    add("memory warning tracks the documented container budget", data, {"CollectorMemoryHigh": [labelset()]})
    data = healthy() + [series("otelcol_processor_tail_sampling_sampling_trace_dropped_too_early", "0+1x12")]
    add("tail pressure is independent of normal sampled drops", data, {"CollectorTailSamplingPressure": [labelset()]})
    data = healthy()
    replace(data, series("up", "0+0x12"))
    add("scrape outage is not masked by old collector metrics", data, {"CollectorScrapeUnavailable": [labelset()]})
    add("removed scrape configuration is detected independently", [], {"CollectorTargetsMissing": [{"job": JOB}]})
    data = [item for item in healthy() if not item["series"].startswith("otelcol_process_uptime{")]
    add("successful scrape without runtime telemetry is not healthy", data,
        {"CollectorRuntimeTelemetryMissing": [labelset()]})
    return {"rule_files": ["/kit/alerts.yaml"], "evaluation_interval": "1m", "tests": tests}


def main():
    suite = fixtures(rule_metadata())
    # Keep Docker fixtures beneath the already-mounted checkout: some desktop
    # Docker configurations cannot bind the OS's default temporary directory.
    with tempfile.TemporaryDirectory(prefix=".alert-fixtures-", dir=KIT) as temporary:
        Path(temporary).chmod(0o755)
        path = Path(temporary) / "fixtures.json"
        path.write_text(json.dumps(suite))
        path.chmod(0o644)
        command = ["docker", "run", "--rm", "--network=none", "--entrypoint=promtool",
                   "--mount", f"type=bind,source={KIT},target=/kit,readonly",
                   "--mount", f"type=bind,source={temporary},target=/fixtures,readonly", PROMETHEUS]
        for args in (["check", "rules", "/kit/alerts.yaml"], ["test", "rules", "/fixtures/fixtures.json"]):
            result = subprocess.run(command + args, capture_output=True, text=True, timeout=45)
            if result.returncode:
                # These outputs contain only public rules and synthetic fixtures.
                print(result.stdout.strip())
                print(result.stderr.strip())
                raise SystemExit("Collector alert validation failed")
    print(f"Validated 11 shipped Collector alerts across {len(suite['tests'])} reset/outage/capacity scenarios")


if __name__ == "__main__":
    main()
