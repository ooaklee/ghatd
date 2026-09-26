#!/usr/bin/env python3
"""Validate the shipped Collector and Compose configurations using pinned tools."""

import json
import os
from pathlib import Path
import subprocess


KIT = Path(__file__).resolve().parent
COLLECTOR = "otel/opentelemetry-collector-contrib:0.160.0"


def run(command, *, env=None):
    result = subprocess.run(command, env=env, capture_output=True, text=True, timeout=45)
    if result.returncode:
        # Resolved configurations can contain authorization; never dump them.
        raise AssertionError("Pinned configuration validation command failed")
    return result.stdout


def resolved_collector(tail=False, explicit_instance=False):
    command = ["docker", "run", "--rm", "--network=none", "--hostname=collector-fixture",
               "--mount", f"type=bind,source={KIT},target=/config,readonly"]
    if explicit_instance:
        command += ["--env", "COLLECTOR_INSTANCE_ID=collector-explicit-fixture"]
    command += [COLLECTOR, "print-config", "--format=json", "--config=/config/collector.yaml"]
    if tail:
        command += ["--config=/config/collector-tail.yaml"]
    # print-config validates by default. JSON is pinned-version specific.
    return json.loads(run(command))


def validate_collector():
    for tail in (False, True):
        config = resolved_collector(tail)
        pipelines = config["service"]["pipelines"]
        assert set(pipelines) == {"traces", "metrics", "metrics/self", "logs"}
        for name, pipeline in pipelines.items():
            assert pipeline["processors"][0] == "memory_limiter"
            assert pipeline["processors"][-1] == "batch"
            assert ("tail_sampling" in pipeline["processors"]) == (tail and name == "traces")
            assert ("resource/self" in pipeline["processors"]) == (name == "metrics/self")
        resource = config["processors"]["resource/deployment"]["attributes"]
        assert {item["key"] for item in resource} == {"service.namespace", "deployment.environment.name"}
        assert all(item["action"] == "insert" for item in resource)
        backend = config["exporters"]["otlp_http/backend"]
        queue = backend["sending_queue"]
        assert queue["storage"] == "file_storage/queue"
        # The pinned print-config JSON emits the internal sizer enum as {}.
        # Validate its public input spelling as well as the resolved capacity.
        assert "      sizer: bytes\n" in (KIT / "collector.yaml").read_text()
        assert queue["queue_size"] == 268435456
        assert queue["num_consumers"] == 4 and not queue["block_on_overflow"]
        assert backend["retry_on_failure"]["max_elapsed_time"] == 300_000_000_000
        assert config["extensions"]["file_storage/queue"]["fsync"]
        assert config["extensions"]["file_storage/queue"]["max_size"] == 1073741824
        assert not config["extensions"]["health_check"].get("check_collector_pipeline", {}).get("enabled", False)
        reader = config["service"]["telemetry"]["metrics"]["readers"][0]["pull"]["exporter"]["prometheus"]
        assert reader["port"] == 8888 and reader["without_type_suffix"] and reader["without_units"]
    for explicit in (False, True):
        config = resolved_collector(explicit_instance=explicit)
        self_attributes = config["processors"]["resource/self"]["attributes"]
        instance = next(item["value"] for item in self_attributes if item["key"] == "service.instance.id")
        assert instance == ("collector-explicit-fixture" if explicit else "collector-fixture")


def compose_config(*overlays):
    env = dict(os.environ)
    for key in tuple(env):
        if key.startswith("COLLECTOR_"):
            del env[key]
    env.update(GRAFANA_PORT="3000", OTLP_GRPC_PORT="4317", OTLP_HTTP_PORT="4318", PROMETHEUS_PORT="9090")
    command = ["docker", "compose", "--env-file", os.devnull, "-f", str(KIT.parent / "compose.yaml"),
               "-f", str(KIT / "compose.yaml")]
    for overlay in overlays:
        command += ["-f", str(KIT / overlay)]
    return json.loads(run(command + ["config", "--format", "json"], env=env))


def validate_compose():
    base = compose_config()
    collector = base["services"]["otel-collector"]
    monitor = base["services"]["collector-monitor"]
    initializer = base["services"]["otel-collector-volume-init"]
    assert not collector.get("ports") and not monitor.get("ports")
    assert collector["user"] == "10001:10001" and collector["read_only"]
    assert int(collector["mem_limit"]) == 536870912
    assert initializer["network_mode"] == "none"
    assert set(initializer["cap_add"]) == {"CHOWN", "FOWNER"}
    assert "healthcheck" not in collector, "The Collector image has no shell probe tools"
    assert collector["depends_on"]["otel-collector-volume-init"]["condition"] == "service_completed_successfully"
    for service in (collector, monitor):
        for volume in service["volumes"]:
            if volume["type"] == "bind":
                assert Path(volume["source"]).is_file(), "Overlay paths must resolve from the reference base"
    queue = next(volume for volume in collector["volumes"] if volume["target"] == "/var/lib/otelcol/queue")
    assert queue["type"] == "volume" and queue["source"] == "collector-queue"
    local = compose_config("compose-local.yaml")
    for name in ("otel-collector", "collector-monitor"):
        assert all(port["host_ip"] == "127.0.0.1" for port in local["services"][name]["ports"])
    assert len(local["services"]["otel-collector"]["ports"]) == 3
    assert len(local["services"]["collector-monitor"]["ports"]) == 1
    tail = compose_config("compose-tail.yaml")
    assert tail["services"]["otel-collector"]["command"] == [
        "--config=/etc/otelcol/collector.yaml", "--config=/etc/otelcol/collector-tail.yaml"]


def validate_prometheus():
    run(["docker", "run", "--rm", "--network=none", "--entrypoint=promtool", "--mount",
         f"type=bind,source={KIT},target=/etc/prometheus,readonly", "prom/prometheus:v3.5.0",
         "check", "config", "/etc/prometheus/prometheus.yaml"])


if __name__ == "__main__":
    try:
        validate_collector()
        validate_compose()
        validate_prometheus()
    except (AssertionError, OSError, subprocess.SubprocessError, ValueError, KeyError):
        raise SystemExit("Collector configuration validation failed; no resolved configuration was printed")
    print("Collector base/tail, self-instance identity and private/local Compose configurations validated")
