#!/usr/bin/env python3
"""Validate template defaults and explicit direct routing without credentials."""
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile

import yaml

import common




def main():
    root, profile, _ = common.options()
    helm = profile["helm"]
    with tempfile.TemporaryDirectory(prefix="service-routing-") as name:
        directory = Path(name)
        chart = common.chart_copy(root, directory, profile)
        env = common.environment(ENVIRONMENT="production", SERVICE_NAME=profile["service"]["name"],
                                 GIT_COMMIT="fixture", DOCKER_IMAGE_REGISTRY="example/api",
                                 ENABLE_OTEL_COLLECTOR="0", OTEL_EXPORTER_OTLP_HEADERS="synthetic-must-not-expand")

        def render():
            result = subprocess.run(["sh", str(common.resolve(root, helm["render_script"]))],
                                    cwd=directory, env=env, text=True, capture_output=True, timeout=30)
            assert result.returncode == 0, "Production render failed; diagnostics withheld"
            text = common.resolve(directory, helm["render_output"]).read_text()
            assert "synthetic-must-not-expand" not in text
            return [item for item in yaml.safe_load_all(text) if item]

        documents = render()
        assert not any(item["kind"] == "StatefulSet" for item in documents)
        def app_containers(documents):
            deployments = [item for item in documents if item["kind"] == "Deployment"]
            assert len(deployments) == common.deployment_count(profile), "Every declared deployment must be checked"
            containers = []
            for deployment in deployments:
                pod = deployment["spec"]["template"]["spec"]
                assert pod["terminationGracePeriodSeconds"] >= 60
                containers += pod["containers"] + pod.get("initContainers", [])
            assert sorted(c["name"] for c in containers) == sorted(common.application_names(profile)), "Every declared process role must be checked"
            return containers

        containers = app_containers(documents)
        values_file = common.resolve(chart, helm["service_values"])
        values = yaml.safe_load(values_file.read_text())
        for container in containers:
            entries = container["env"]
            assert len(entries) == len({item["name"] for item in entries})
            config = {item["name"]: item.get("value") for item in entries}
            for signal in ("TRACES", "METRICS", "LOGS"):
                assert config["OTEL_" + signal + "_EXPORTER"] == "none"
            assert config["OTEL_TRACES_SAMPLER"] == "always_on"
            assert not config.get("OTEL_TRACES_SAMPLER_ARG")
            assert config["HTTP_TRACE_NOISE_SUPPRESSION"] == "false"
            assert config["BROWSER_TELEMETRY_ENABLED"] == "false"
            assert config["HTTP_REQUEST_LOG_DETAILS"] == "false"
            assert config["HTTP_REQUEST_LOG_PRESERVE_PATH_PARAMETERS"] == "false"
            assert config["HTTP_REQUEST_LOG_TRUSTED_PROXY_CIDRS"] == ""
            assert config["OTEL_METRIC_CARDINALITY_LIMIT"] == "200"
            assert not any("NEWRELIC" in key for key in config)
        # Direct trace-only routing is shared by all roles and supports Secret references.
        entries = values["servicePodConf"]["env"]
        next(item for item in entries if item["name"] == "OTEL_TRACES_EXPORTER")["value"] = "otlp"
        entries.extend([
            {"name": "OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "value": "https://traces.example.com/v1/traces"},
            {"name": "OTEL_EXPORTER_OTLP_TRACES_HEADERS", "valueFrom": {"secretKeyRef": {"name": "fixture-telemetry", "key": "headers"}}},
        ])
        values_file.write_text(yaml.safe_dump(values))
        for container in app_containers(render()):
            config = {item["name"]: item for item in container["env"]}
            assert config["OTEL_TRACES_EXPORTER"]["value"] == "otlp"
            assert config["OTEL_TRACES_SAMPLER"]["value"] == "always_on"
            assert config["HTTP_TRACE_NOISE_SUPPRESSION"]["value"] == "false"
            assert config["OTEL_LOGS_EXPORTER"]["value"] == "none"
            assert config["OTEL_METRICS_EXPORTER"]["value"] == "none"
            assert config["OTEL_EXPORTER_OTLP_TRACES_HEADERS"]["valueFrom"]["secretKeyRef"]["name"] == "fixture-telemetry"
        assert "NEWRELIC" not in json.dumps(documents)
        print("Production defaults and explicit trace-only Secret routing passed for all declared application roles.")


if __name__ == "__main__":
    common.run(main)
