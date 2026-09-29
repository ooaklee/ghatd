#!/usr/bin/env python3
"""Render synthetic deployment values and validate the pinned Collector schema.

Requires Helm 3, Docker, envsubst, Python 3 and PyYAML (`pip install PyYAML`).
No cluster, application credentials or production secret values are used. All
rendered files live in a temporary directory; failures never print manifests or
tool diagnostics that could contain supplied values.
"""

from copy import deepcopy
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile

import yaml

import common


ROOT = None
PROFILE = {}
IMAGE = "otel/opentelemetry-collector-contrib:0.160.0@sha256:799dc6cf12c96192af37b5bdba804da8c10b3bc563b43cb90c3f3c58d9572ad6"
CANARY = "synthetic-sensitive-canary"
CONFLICTS = [
    f"OTEL_EXPORTER_OTLP{signal}_{setting}"
    for signal in ("", "_TRACES", "_METRICS", "_LOGS")
    for setting in ("ENDPOINT", "PROTOCOL", "HEADERS", "INSECURE", "CERTIFICATE", "CLIENT_CERTIFICATE", "CLIENT_KEY")
    if not (signal and setting == "ENDPOINT")
]


def checked(command, *, cwd=None, env=None, input=None):
    result = subprocess.run(command, cwd=cwd, env=env, input=input, text=True, capture_output=True, timeout=60)
    assert result.returncode == 0, f"{Path(command[0]).name} failed; diagnostic output withheld"
    return result.stdout


def deep_merge(target, values):
    for key, value in values.items():
        if isinstance(value, dict) and isinstance(target.get(key), dict):
            deep_merge(target[key], value)
        else:
            target[key] = deepcopy(value)


class Fixture:
    def __init__(self, directory):
        self.directory = Path(directory)
        self.chart = common.chart_copy(ROOT, self.directory, PROFILE)
        self.helm = PROFILE["helm"]
        chart = yaml.safe_load((self.chart / "Chart.yaml").read_text())
        chart["name"] = "fixture-api"
        (self.chart / "Chart.yaml").write_text(yaml.safe_dump(chart))
        self.base = yaml.safe_load((common.resolve(self.chart, self.helm["base_values"])).read_text())
        self.base.update(fullnameOverride="", namespace="fixture-production", image={"repository": "example/api", "tag": "fixture", "pullPolicy": "IfNotPresent"}, externalSecrets={})
        self.environment = [
            {"name": "OTEL_RESOURCE_ATTRIBUTES", "value": "service.namespace=fixture"},
            {"name": "OTEL_TRACES_EXPORTER", "value": "none"},
            {"name": "OTEL_METRIC_EXPORT_INTERVAL", "value": "5000"},
            {"name": "APPLICATION_SETTING", "value": "retained"},
        ] + [{"name": name, "value": CANARY} for name in CONFLICTS]
        self.pods = {
            "servicePodConf": {"env": deepcopy(self.environment), "ports": [{"containerPort": self.helm["port"]}]},
            "initJobs": {"enabled": self.helm["migrator"], "migrationCmd": ["/application", "migrate"]},
            "sidekickContainer": {"enabled": self.helm["sidekick"], "sidekickCmd": ["/application", "work"]},
        }
        for worker in self.helm["workers"]:
            self.pods[worker["values_key"]] = {"enabled": True, "command": ["/application", "work"], "env": []}
        (common.resolve(self.chart, self.helm["base_values"])).write_text(yaml.safe_dump(self.base))
        (common.resolve(self.chart, self.helm["service_values"])).write_text(yaml.safe_dump(self.pods))
        self.enabled = {"otelCollector": {"enabled": True, "monitoring": {"exportSelfMetrics": True}, "backend": {"endpoint": "https://collector.example.com/otlp", "authorization": {"existingSecret": "fixture-collector", "key": "authorization"}}, "resource": {"namespace": "fixture", "environment": "testing"}}}

        (common.resolve(self.chart, self.helm["collector_values"])).write_text(yaml.safe_dump(self.enabled))

    def render(self, overrides=None, *, enabled=True, failure=None):
        values = deepcopy(self.enabled if enabled else {})
        deep_merge(values, overrides or {})
        overlay = self.directory / "fixture-values.yaml"
        overlay.write_text(yaml.safe_dump(values))
        result = subprocess.run(["helm", "template", "fixture", str(self.chart), "-f", str(common.resolve(self.chart, self.helm["base_values"])), "-f", str(common.resolve(self.chart, self.helm["service_values"])), "-f", str(overlay)], text=True, capture_output=True, timeout=30)
        if failure:
            assert result.returncode != 0, "Invalid Collector values unexpectedly rendered"
            assert failure in result.stderr, "Expected fixed field validation diagnostic is missing"
            assert CANARY not in result.stdout + result.stderr, "Invalid supplied value leaked in render diagnostics"
            return None
        assert result.returncode == 0, "Helm render failed; diagnostic output withheld"
        return [document for document in yaml.safe_load_all(result.stdout) if document]


def kind(documents, name):
    return [document for document in documents if document["kind"] == name]


def collector_config(documents):
    return yaml.safe_load(kind(documents, "ConfigMap")[0]["data"]["collector.yaml"])


def application_containers(documents):
    deployments = kind(documents, "Deployment")
    assert len(deployments) == common.deployment_count(PROFILE)
    containers = []
    for deployment in deployments:
        pod = deployment["spec"]["template"]["spec"]
        containers += pod["containers"] + [c for c in pod.get("initContainers", []) if c["name"] != "wait-for-otel-collector"]
    assert sorted(c["name"] for c in containers) == sorted(common.application_names(PROFILE, "fixture-api"))
    return containers


def test_disabled(fixture):
    documents = fixture.render(enabled=False)
    assert not any(kind(documents, name) for name in ("StatefulSet", "ConfigMap", "NetworkPolicy", "PodMonitor", "PrometheusRule"))
    pod = kind(documents, "Deployment")[0]["spec"]["template"]["spec"]
    containers = application_containers(documents)
    assert all(container["env"] == fixture.environment for container in containers), "Disabled mode changed the existing environment"


def test_private_persistent_deployment(fixture):
    documents = fixture.render()
    pod = kind(documents, "Deployment")[0]["spec"]["template"]["spec"]
    for container in application_containers(documents):
        environment = container["env"]
        names = [entry["name"] for entry in environment]
        assert len(names) == len(set(names))
        assert environment[:4] == fixture.environment[:4], "Identity, explicit disable or unrelated settings changed"
        assert all(entry["value"] != CANARY for entry in environment)
        assert {entry["name"]: entry["value"] for entry in environment if entry["name"].startswith("OTEL_EXPORTER_OTLP")} == {
            "OTEL_EXPORTER_OTLP_ENDPOINT": "http://fixture-fixture-api-otel:4318", "OTEL_EXPORTER_OTLP_PROTOCOL": "http/protobuf"}
    readiness = pod["initContainers"][0]
    assert readiness["name"] == "wait-for-otel-collector"
    assert "nc -z -w 1 fixture-fixture-api-otel 4318" in readiness["command"][-1]
    assert '"$attempt" -lt 60' in readiness["command"][-1]
    assert readiness["securityContext"]["readOnlyRootFilesystem"]
    assert "env" not in readiness, "Readiness must not inherit application credentials"
    stateful = kind(documents, "StatefulSet")[0]
    assert stateful["spec"]["persistentVolumeClaimRetentionPolicy"] == {"whenDeleted": "Retain", "whenScaled": "Retain"}
    assert stateful["spec"]["replicas"] == 1
    claim, = stateful["spec"]["volumeClaimTemplates"]
    assert claim["metadata"]["name"] == "queue"
    assert claim["spec"]["accessModes"] == ["ReadWriteOnce"]
    assert claim["spec"]["resources"]["requests"]["storage"] == "10Gi"
    collector_pod = stateful["spec"]["template"]["spec"]
    assert collector_pod["securityContext"]["fsGroup"] == 10001
    assert collector_pod["securityContext"]["runAsUser"] == 10001
    assert collector_pod["automountServiceAccountToken"] is False
    assert "queue" not in [volume["name"] for volume in collector_pod["volumes"]], "Queue must use the per-replica claim"
    container, = collector_pod["containers"]
    assert container["securityContext"]["readOnlyRootFilesystem"] is True
    assert container["env"][0] == {"name": "COLLECTOR_AUTHORIZATION", "valueFrom": {"secretKeyRef": {"name": "fixture-collector", "key": "authorization"}}}
    assert len(container["env"]) == 2
    assert "@sha256:" in container["image"] and ":0.160.0@" in container["image"]
    assert all("hostPort" not in port for port in container["ports"])
    services = [service for service in kind(documents, "Service") if "-otel" in service["metadata"]["name"]]
    assert len(services) == 2
    assert all(service["spec"]["type"] == "ClusterIP" for service in services)
    assert all({port["port"] for port in service["spec"]["ports"]} == {4317, 4318} for service in services)
    policy, = kind(documents, "NetworkPolicy")
    assert policy["spec"]["policyTypes"] == ["Ingress"]
    assert policy["spec"]["ingress"] == [{"from": [
        {"podSelector": {"matchLabels": {"app": name}}}
        for name in ["fixture-api"] + ["fixture-api-" + worker["suffix"] for worker in PROFILE["helm"]["workers"]]
    ], "ports": [{"protocol": "TCP", "port": 4317}, {"protocol": "TCP", "port": 4318}]}]
    config = collector_config(documents)
    assert config["processors"]["resource/deployment"]["attributes"] == [
        {"key": "service.namespace", "value": "fixture", "action": "insert"},
        {"key": "deployment.environment.name", "value": "testing", "action": "insert"}]
    assert all("resource/self" not in pipeline["processors"] for name, pipeline in config["service"]["pipelines"].items() if name != "metrics/self")
    exporter = config["exporters"]["otlp_http/backend"]
    assert exporter["headers"] == {"Authorization": "${env:COLLECTOR_AUTHORIZATION}"}
    assert exporter["sending_queue"]["storage"] == "file_storage/queue"
    assert exporter["sending_queue"]["sizer"] == "bytes"
    assert exporter["sending_queue"]["queue_size"] == 268435456
    assert exporter["retry_on_failure"]["max_elapsed_time"] == "300s"
    assert "tail_sampling" not in config["processors"]
    assert not kind(documents, "PodMonitor") and not kind(documents, "PrometheusRule")
    return documents


def test_worker_routing(fixture):
    for role in PROFILE["helm"]["workers"]:
        override = {role["values_key"]: {"env": [
            {"name": "APPLICATION_SETTING", "value": "worker"},
            {"name": "OTEL_EXPORTER_OTLP_HEADERS", "value": CANARY},
        ]}}
        documents = fixture.render(override)
        worker = next(c for c in application_containers(documents) if c["name"] == "fixture-api-" + role["suffix"])
        config = {e["name"]: e.get("value") for e in worker["env"]}
        assert len(config) == len(worker["env"]), "Worker overrides must not duplicate environment entries"
        assert config["APPLICATION_SETTING"] == "worker"
        assert CANARY not in str(worker["env"]), "Worker credentials bypassed collector routing"
        for deployment in kind(documents, "Deployment"):
            first = deployment["spec"]["template"]["spec"]["initContainers"][0]
            assert first["name"] == "wait-for-otel-collector"
            assert "env" not in first
        fixture.render({role["values_key"]: {"env": [
            {"name": "OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "value": CANARY},
        ]}}, failure="cannot replace explicit per-signal endpoints")


def test_replica_tail_and_monitoring(fixture):
    no_self = fixture.render({"otelCollector": {"monitoring": {"exportSelfMetrics": False}}})
    assert "metrics/self" not in collector_config(no_self)["service"]["pipelines"]
    assert "prometheus/self" not in collector_config(no_self)["receivers"]
    named = fixture.render({"otelCollector": {"backend": {"endpoint": "https://collector.example.com:65535/otlp", "authorization": {"existingSecret": "fixture.collector-secret", "key": "_Authorization.Key-1"}}}})
    secret = kind(named, "StatefulSet")[0]["spec"]["template"]["spec"]["containers"][0]["env"][0]["valueFrom"]["secretKeyRef"]
    assert secret == {"name": "fixture.collector-secret", "key": "_Authorization.Key-1"}
    minimal = fixture.render({"servicePodConf": {"env": []}})
    minimal_pod = kind(minimal, "Deployment")[0]["spec"]["template"]["spec"]
    assert all(len(container["env"]) == 2 for container in application_containers(minimal))
    two = fixture.render({"otelCollector": {"replicas": 2, "persistence": {"storageClass": "fixture-storage"}}})
    stateful, = kind(two, "StatefulSet")
    assert stateful["spec"]["replicas"] == 2 and len(stateful["spec"]["volumeClaimTemplates"]) == 1
    assert stateful["spec"]["volumeClaimTemplates"][0]["spec"]["storageClassName"] == "fixture-storage"
    instance = stateful["spec"]["template"]["spec"]["containers"][0]["env"][1]
    assert instance == {"name": "COLLECTOR_INSTANCE_ID", "valueFrom": {"fieldRef": {"fieldPath": "metadata.uid"}}}
    self_attributes = collector_config(two)["processors"]["resource/self"]["attributes"]
    assert {"key": "service.instance.id", "value": "${env:COLLECTOR_INSTANCE_ID}", "action": "upsert"} in self_attributes
    tail = fixture.render({"otelCollector": {"tailSampling": {"enabled": True, "baselinePercent": 0}}})
    config = collector_config(tail)
    assert config["service"]["pipelines"]["traces"]["processors"] == ["memory_limiter", "resource/deployment", "tail_sampling", "batch"]
    assert config["processors"]["tail_sampling"]["policies"][-1]["probabilistic"]["sampling_percentage"] == 0
    for signal in ("logs", "metrics"):
        assert "tail_sampling" not in config["service"]["pipelines"][signal]["processors"]
    monitoring = fixture.render({"otelCollector": {"monitoring": {"podMonitor": {"enabled": True}, "rules": {"enabled": True}, "labels": {"release": "fixture-prometheus"}}, "networkPolicy": {"monitoringNamespaceLabels": {"kubernetes.io/metadata.name": "monitoring"}, "monitoringPodLabels": {"app.kubernetes.io/name": "prometheus"}}}})
    monitor, = kind(monitoring, "PodMonitor")
    assert monitor["metadata"]["labels"]["release"] == "fixture-prometheus"
    assert monitor["spec"]["namespaceSelector"] == {"matchNames": ["fixture-production"]}
    assert {"targetLabel": "job", "replacement": "otel-collector"} in monitor["spec"]["podMetricsEndpoints"][0]["relabelings"]
    policy, = kind(monitoring, "NetworkPolicy")
    monitoring_ingress = policy["spec"]["ingress"][1]
    assert monitoring_ingress["from"][0]["namespaceSelector"]["matchLabels"]
    assert monitoring_ingress["from"][0]["podSelector"]["matchLabels"]
    assert monitoring_ingress["ports"] == [{"protocol": "TCP", "port": 8888}]
    rule, = kind(monitoring, "PrometheusRule")
    assert rule["metadata"]["labels"]["release"] == "fixture-prometheus"
    assert rule["spec"]["groups"]
    for group in rule["spec"]["groups"]:
        for alert in group["rules"]:
            assert 'namespace="fixture-production"' in alert["expr"]
            assert 'job="otel-collector"' not in alert["expr"].replace('job="otel-collector", namespace="fixture-production"', ""), "Every signal branch must remain scoped to this deployment namespace"
    checked(["docker", "run", "--rm", "--pull=never", "--network=none", "-i", "--entrypoint", "/bin/sh", "prom/prometheus:v3.5.0", "-c", "cat > /tmp/rules.json && exec /bin/promtool check rules /tmp/rules.json"], input=json.dumps(rule["spec"]))
    return two, tail, monitoring


def test_invalid(fixture):
    for signal in ("TRACES", "METRICS", "LOGS"):
        fixture.render({"servicePodConf": {"env": fixture.environment + [
            {"name": f"OTEL_EXPORTER_OTLP_{signal}_ENDPOINT", "value": CANARY}
        ]}}, failure="cannot replace explicit per-signal endpoints")
    cases = [
        ({"replicas": 0}, "replicas"), ({"replicas": 33}, "replicas"),
        ({"replicas": 1.5}, "replicas"),
        ({"enabled": CANARY}, "enabled"),
        ({"replicas": 2, "tailSampling": {"enabled": True}}, "tailSampling requires exactly one replica"),
        ({"queue": {"bytesPerSignal": 0}}, "queue.bytesPerSignal"),
        ({"queue": {"bytesPerSignal": CANARY}}, "queue.bytesPerSignal"),
        ({"queue": {"consumers": 33}}, "queue.consumers"),
        ({"queue": {"maxFileBytes": 1024}}, "queue.maxFileBytes must cover"),
        ({"persistence": {"size": "1Gi"}}, "persistence.size must cover"),
        ({"persistence": {"size": CANARY}}, "persistence.size"),
        ({"backend": {"authorization": {"existingSecret": ""}}}, "authorization.existingSecret"),
        ({"backend": {"authorization": {"existingSecret": "UPPER-" + CANARY}}}, "authorization.existingSecret"),
        ({"backend": {"authorization": {"existingSecret": "under_" + CANARY}}}, "authorization.existingSecret"),
        ({"backend": {"authorization": {"existingSecret": CANARY + "."}}}, "authorization.existingSecret"),
        ({"backend": {"authorization": {"existingSecret": CANARY + "-"}}}, "authorization.existingSecret"),
        ({"backend": {"authorization": {"existingSecret": CANARY + "..fixture"}}}, "authorization.existingSecret"),
        ({"backend": {"authorization": {"existingSecret": "a" * 254}}}, "authorization.existingSecret"),
        ({"backend": {"authorization": {"key": ""}}}, "authorization.key"),
        ({"backend": {"authorization": {"key": CANARY + "/invalid"}}}, "authorization.key"),
        ({"backend": {"authorization": {"key": ".." + CANARY}}}, "authorization.key"),
        ({"backend": {"authorization": {"key": "a" * 254}}}, "authorization.key"),
        ({"backend": {"endpoint": "https://user:" + CANARY + "@collector.example.com"}}, "backend.endpoint"),
        ({"backend": {"endpoint": "https://collector.example.com/?token=" + CANARY}}, "backend.endpoint"),
        ({"backend": {"endpoint": "http://collector.example.com"}}, "backend.endpoint"),
        ({"backend": {"endpoint": "https://collector.example.com:0/otlp"}}, "backend.endpoint port"),
        ({"backend": {"endpoint": "https://collector.example.com:65536/otlp"}}, "backend.endpoint port"),
        ({"backend": {"endpoint": "https://collector.example.com:99999/otlp"}}, "backend.endpoint port"),
        ({"export": {"retryElapsedSeconds": 0}}, "export.retryElapsedSeconds"),
        ({"export": {"retryInitialSeconds": 31}}, "export retry durations"),
        ({"tailSampling": {"decisionWaitSeconds": 301}}, "tailSampling.decisionWaitSeconds"),
        ({"tailSampling": {"baselinePercent": 101}}, "tailSampling.baselinePercent"),
        ({"resource": {"namespace": CANARY + "\n"}}, "resource.namespace"),
        ({"monitoring": {"podMonitor": {"enabled": True}}}, "monitoring requires both"),
        ({"networkPolicy": {"monitoringNamespaceLabels": {"name": "monitoring"}}}, "monitoring requires both"),
    ]
    for values, field in cases:
        fixture.render({"otelCollector": values}, failure=field)
    return len(cases) + 3


def validate_schema(fixture, documents, index):
    filename = fixture.directory / f"collector-{index}.yaml"
    filename.write_text(kind(documents, "ConfigMap")[0]["data"]["collector.yaml"])
    # Copy the synthetic configuration into a test-owned container so this also
    # works with remote Docker daemons that cannot bind workstation temp paths.
    container = checked(["docker", "create", "--pull=never", "--network=none", "-e", "COLLECTOR_AUTHORIZATION=fixture", "-e", "COLLECTOR_INSTANCE_ID=fixture-pod", IMAGE, "validate", "--config=/fixture.yaml"]).strip()
    try:
        checked(["docker", "cp", str(filename), container + ":/fixture.yaml"])
        checked(["docker", "start", "--attach", container])
        assert checked(["docker", "inspect", "--format={{.State.ExitCode}}", container]).strip() == "0", "Pinned Collector schema validation failed"
    finally:
        checked(["docker", "rm", "--force", container])


def test_render_script(fixture):
    environment = common.environment()
    environment.update(ENVIRONMENT="production", SERVICE_NAME="fixture-api", GIT_COMMIT="fixture", DOCKER_IMAGE_REGISTRY="example/api")
    script = common.resolve(ROOT, PROFILE["helm"]["render_script"])
    output = common.resolve(fixture.directory, PROFILE["helm"]["render_output"])
    for enabled in ("0", "1"):
        environment["ENABLE_OTEL_COLLECTOR"] = enabled
        stdout = checked(["sh", str(script)], cwd=fixture.directory, env=environment)
        assert CANARY not in stdout
        documents = list(yaml.safe_load_all(output.read_text()))
        assert bool(kind(documents, "StatefulSet")) == (enabled == "1")
    previous = output.read_text()
    environment["ENABLE_OTEL_COLLECTOR"] = CANARY
    result = subprocess.run(["sh", str(script)], cwd=fixture.directory, env=environment, text=True, capture_output=True, timeout=30)
    assert result.returncode != 0 and CANARY not in result.stdout + result.stderr
    assert output.read_text() == previous, "A failed render replaced the last valid artifact"
    environment["ENABLE_OTEL_COLLECTOR"] = "1"
    overlay = common.resolve(fixture.chart, PROFILE["helm"]["collector_values"])
    overlay.write_text(yaml.safe_dump({"otelCollector": {"enabled": True, "backend": {"endpoint": "https://user:" + CANARY + "@example.com"}}}))
    result = subprocess.run(["sh", str(script)], cwd=fixture.directory, env=environment, text=True, capture_output=True, timeout=30)
    assert result.returncode != 0 and CANARY not in result.stdout + result.stderr
    assert output.read_text() == previous


def main():
    global ROOT, PROFILE
    ROOT, PROFILE, _ = common.options()
    with tempfile.TemporaryDirectory(prefix="collector-render-test-") as directory:
        fixture = Fixture(directory)
        test_disabled(fixture)
        default = test_private_persistent_deployment(fixture)
        test_worker_routing(fixture)
        variants = test_replica_tail_and_monitoring(fixture)
        negatives = test_invalid(fixture)
        for index, documents in enumerate((default,) + variants):
            validate_schema(fixture, documents, index)
        test_render_script(fixture)
        print(f"Collector render checks passed: direct mode, all declared application roles, private persistent deployment, two replicas, tail sampling, monitoring, {negatives} negative cases, four pinned schema validations and atomic render selection.")


if __name__ == "__main__":
    common.run(main)
