#!/usr/bin/env python3
"""Check local Collector routing and copies of the pinned foundation config."""

import argparse
import hashlib
import json
import os
import re
from pathlib import Path
import subprocess
import sys

import common


ROOT = None
BASE = []
MONITOR = ""
COLLECTOR = ""


def check(condition, message):
    if not condition:
        raise RuntimeError(message)


def run(arguments, environment):
    result = subprocess.run(arguments, cwd=ROOT, env=environment, capture_output=True, timeout=45)
    check(result.returncode == 0, "A configuration command failed; output withheld to avoid exposing environment values")
    return result.stdout


def compose(files, environment):
    command = ["docker", "compose", "--env-file", os.devnull]
    for file in files:
        command.extend(["-f", file])
    command.extend(["config", "--format", "json"])
    return json.loads(run(command, environment))


def main():
    global ROOT, BASE, MONITOR, COLLECTOR
    ROOT, profile, foundation_dir = common.options()
    config = profile["compose"]
    BASE, MONITOR, COLLECTOR = config["base"], config["monitor"], config["collector"]
    for relative in [*BASE, MONITOR, COLLECTOR]:
        assert common.resolve(ROOT, relative).is_file()
    # Only explicitly synthetic values may reach compose interpolation.
    environment = common.environment(**config["fixture_env"])
    disabled = compose(BASE, environment)
    direct = compose(BASE + [MONITOR], environment)
    queued = compose(BASE + [MONITOR, COLLECTOR], environment)
    for profile in (disabled, direct, queued):
        for role in config["roles"]:
            values = profile["services"][role]["environment"]
            check(values["OTEL_TRACES_SAMPLER"] == "always_on", "Trace defaults must record unsampled incoming parents too")
            check(not values.get("OTEL_TRACES_SAMPLER_ARG"), "Default always_on policy must not imply a tunable ratio")
            check(values["HTTP_TRACE_NOISE_SUPPRESSION"] == "false", "Default trace policy must include health/static requests")
    for role in config["application_roles"]:
        check(disabled["services"][role]["environment"]["OTEL_TRACES_EXPORTER"] == "none", "Ordinary local mode changed")
        check(direct["services"][role]["environment"]["OTEL_EXPORTER_OTLP_ENDPOINT"] == "http://otel-lgtm:4318", "Direct monitoring route changed")
    for role in config["roles"]:
        values = queued["services"][role]["environment"]
        for signal in ["", "TRACES_", "METRICS_", "LOGS_"]:
            prefix = "OTEL_EXPORTER_OTLP_" + signal
            endpoint = "http://otel-collector:4318"
            if signal:
                endpoint += "/v1/" + signal[:-1].lower()
            check(values[prefix + "ENDPOINT"] == endpoint, "A process bypasses the private Collector")
            check(values[prefix + "PROTOCOL"] == "http/protobuf", "Unexpected private protocol")
            check(values[prefix + "INSECURE"] == "true", "Unexpected private TLS setting")
            for suffix in ["HEADERS", "CERTIFICATE", "CLIENT_CERTIFICATE", "CLIENT_KEY"]:
                check(values[prefix + suffix] == "", "Hosted authorization or TLS overrides remain on a client")
        check(queued["services"][role]["depends_on"]["otel-collector-ready"]["condition"] == "service_completed_successfully", "A process bypasses local readiness")
    collector = queued["services"]["otel-collector"]
    check(not collector.get("ports"), "Collector ports must not be published")
    check(collector["user"] == "10001:10001" and collector["read_only"], "Collector privileges changed")
    check(collector["environment"]["COLLECTOR_BACKEND_ENDPOINT"] == "http://otel-lgtm:4318", "Local backend changed")
    check(any(volume["type"] == "volume" and volume["target"] == "/var/lib/otelcol" for volume in collector["volumes"]), "Queue is not on a named volume")
    initializer = queued["services"]["otel-collector-queue-init"]
    check(initializer["network_mode"] == "none" and set(initializer["cap_add"]) == {"CHOWN", "FOWNER"}, "Queue initialization privileges changed")
    for service in ["otel-lgtm", "collector-monitor"]:
        check(all(port.get("host_ip") == "127.0.0.1" for port in queued["services"][service]["ports"]), "Local diagnostic ports must bind to loopback")
    monitor = queued["services"]["collector-monitor"]
    check(any(volume["target"] == "/etc/prometheus/alerts.yaml" for volume in monitor["volumes"]), "Independent monitor has no alert rules")
    for flag, expected in [("0", False), ("1", True)]:
        command = run(["make", "-n", "start", "DOCKER_ENV=local", "GIT_COMMIT=fixture",
                       "ENABLE_MONITORING=", "ENABLE_OTEL_COLLECTOR=" + flag], environment).decode()
        check((COLLECTOR in command) == expected, "Makefile Collector opt-in changed")
        check((MONITOR in command) == expected, "Collector must select its local backend")
    # Go module zips exclude these nested example modules. Check the vendored
    # asset hashes by default; a local foundation checkout enables byte comparison.
    baseline = common.resolve(ROOT, config["assets_dir"])
    manifest = json.loads((baseline / "source.json").read_text())
    check(isinstance(manifest.get("source_commit"), str) and re.fullmatch(r"[0-9a-f]{40}", manifest["source_commit"]), "Asset provenance must record an immutable source commit")
    check(isinstance(manifest.get("sha256"), dict) and bool(manifest["sha256"]), "Asset provenance must include file hashes")
    for name, expected in manifest["sha256"].items():
        check(isinstance(expected, str) and re.fullmatch(r"[0-9a-f]{64}", expected), "Asset provenance digest is invalid")
        copied = common.resolve(baseline, name)
        check(hashlib.sha256(copied.read_bytes()).hexdigest() == expected, "Vendored foundation asset changed: " + name)
        if foundation_dir is not None:
            source = common.resolve(foundation_dir / "examples/observability/collector", name)
            check(source.is_file() and copied.read_bytes() == source.read_bytes(), "Local configuration differs from foundation: " + name)
    print("Collector Compose checks passed: direct/off/queued routing, all process roles, private ports, persistent storage, independent rules and foundation copies")


if __name__ == "__main__":
    common.run(main)
