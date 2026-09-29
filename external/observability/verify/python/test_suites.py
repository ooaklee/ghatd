"""Contract tests for hosts with different identities, panel IDs and topology."""
import contextlib
from copy import deepcopy
import importlib.util
import io
import json
from pathlib import Path
import tempfile
import unittest
from unittest import mock

import yaml

import common
import dashboards


def load_suite(name):
    spec = importlib.util.spec_from_file_location(name.replace("-", "_"), Path(__file__).with_name(name + ".py"))
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


collector = load_suite("collector-render")
production = load_suite("production-render")


def host_profile():
    return {
        "version": 1,
        "service": {"name": "example-api", "namespace": "live"},
        "dashboards": {
            "paths": ["local.json", "remote.json"],
            "cache_metric": "example_cache_event_count_total",
            "queue_count_metric": "example_worker_count_total",
            "queue_bucket_metric": "example_worker_duration_seconds_bucket",
            "queue_operation": "queue.invoice.consume",
            "panels": {name: value + 200 for name, value in common.PANEL_IDS.items()},
        },
        "helm": {
            "chart_dir": "infra/chart", "base_values": "base.yaml",
            "service_values": "pods.yaml", "collector_values": "telemetry.yaml",
            "render_script": "render.sh", "render_output": "out/rendered.yaml",
            "port": 8080, "migrator": False, "sidekick": False,
            "workers": [],
        },
    }


class HostContractTests(unittest.TestCase):
    def test_production_trace_wiring_rejects_misrouting_and_unexpanded_credentials(self):
        trace = {"endpoint": "https://traces.example.com/v1/traces", "header_name": "x-example-team",
                 "credential_env": "TRACE_API_KEY", "secret_name": "example-telemetry",
                 "secret_key": "api-key", "remote_key": "/example/live/TRACE_API_KEY"}
        entries = [
            {"name": "TRACE_API_KEY", "valueFrom": {"secretKeyRef": {"name": "example-telemetry", "key": "api-key"}}},
            {"name": "OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "value": trace["endpoint"]},
            {"name": "OTEL_EXPORTER_OTLP_TRACES_PROTOCOL", "value": "http/protobuf"},
            {"name": "OTEL_EXPORTER_OTLP_TRACES_HEADERS", "value": "x-example-team=$(TRACE_API_KEY)"},
        ]
        documents = [{"kind": "ExternalSecret", "spec": {"target": {"name": "example-telemetry"},
                      "data": [{"secretKey": "api-key", "remoteRef": {"key": trace["remote_key"]}}]}}]
        production.validate_trace_route(documents, entries, trace)
        cases = []
        for index, replacement in [(0, {"name": "TRACE_API_KEY", "value": "synthetic-canary"}),
                                   (1, {"name": entries[1]["name"], "value": "https://wrong.example/v1/traces"}),
                                   (2, {"name": entries[2]["name"], "value": "grpc"}),
                                   (3, {"name": entries[3]["name"], "value": "x-example-team=${TRACE_API_KEY}"})]:
            changed = deepcopy(entries)
            changed[index] = replacement
            cases.append((documents, changed))
        cases.extend([(documents, entries[1:] + entries[:1]), (documents, entries + entries[:1]), ([], entries)])
        changed_documents = deepcopy(documents)
        changed_documents[0]["spec"]["data"][0]["remoteRef"]["key"] = "/another/live/TRACE_API_KEY"
        cases.append((changed_documents, entries))
        for docs, env in cases:
            with self.subTest(case=cases.index((docs, env))), self.assertRaises(AssertionError):
                production.validate_trace_route(docs, env, trace)

    def test_paths_reject_parent_and_symlink_escape(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory) / "host"
            root.mkdir()
            (root / "escape").symlink_to(Path(directory), target_is_directory=True)
            for value in ("../other", str(Path(directory)), "escape/outside"):
                with self.subTest(value=value), self.assertRaises(ValueError):
                    common.resolve(root, value)
            self.assertEqual(root.resolve() / "output/new.yaml", common.resolve(root, "output/new.yaml"))

    def test_panel_contract_rejects_aliases_and_unknown_roles(self):
        profile = host_profile()
        profile["dashboards"]["panels"]["requests"] = profile["dashboards"]["panels"]["errors"]
        with self.assertRaises(ValueError):
            common.panels(profile)
        profile["dashboards"]["panels"] = {"invented": 999}
        with self.assertRaises(ValueError):
            common.panels(profile)

    def test_remapped_panels_run_all_original_scenarios_with_host_metrics(self):
        profile = host_profile()
        captures = []
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            for index, relative in enumerate(profile["dashboards"]["paths"]):
                panels = []
                for name, original in common.PANEL_IDS.items():
                    panel = {"id": original + 200}
                    if name not in {"traces", "full_logs", "request_summary"}:
                        panel["datasource"] = {"type": "prometheus", "uid": "${DS_METRICS}"}
                        expression = name + '{service_name=${service:doublequote},deployment_environment_name=${environment:doublequote}}'
                        target = {"datasource": {"uid": "${DS_METRICS}"}, "interval": "5s" if index == 0 else "1m",
                                  "expr": expression, "exemplar": True}
                        panel["targets"] = [target] * (2 if name == "latency" else 1)
                    panels.append(panel)
                data = {"panels": panels, "templating": {"list": [{"name": name} for name in (
                    "DS_METRICS", "DS_TRACES", "DS_LOGS", "service", "environment")]}}
                (root / relative).write_text(json.dumps(data))
            with mock.patch.object(common, "options", return_value=(root, profile, None)), \
                 mock.patch.object(dashboards.subprocess, "run", side_effect=lambda *args, **kwargs: captures.append(kwargs["input"])), \
                 contextlib.redirect_stdout(io.StringIO()):
                dashboards.main()
        fixtures = json.loads(captures[1])["tests"]
        self.assertEqual(28, len(fixtures))
        for name in ("idle counter has no defined error ratio", "absent worker telemetry remains missing",
                     "cache ratio counts lookups, not response stores", "runtime delivery freshness is independent of request traffic"):
            self.assertEqual(2, sum(item["name"] == name for item in fixtures))
        rendered = json.dumps(fixtures)
        self.assertIn("example_cache_event_count_total", rendered)
        self.assertIn("example_worker_count_total", rendered)
        self.assertIn("queue.invoice.consume", rendered)
        self.assertIn('requests{service_name=', rendered)
        self.assertNotIn("$", rendered)

    def test_alternate_chart_topology_drives_fixtures_and_container_coverage(self):
        for extra_workers in ([], [{"values_key": "invoiceWorker", "suffix": "invoice-worker"},
                                   {"values_key": "archiveWorker", "suffix": "archive-worker"}]):
            with self.subTest(workers=len(extra_workers)), tempfile.TemporaryDirectory() as directory:
                root = Path(directory) / "host"
                profile = host_profile()
                profile["helm"]["workers"] = extra_workers
                chart = root / profile["helm"]["chart_dir"]
                chart.mkdir(parents=True)
                (chart / "Chart.yaml").write_text("name: example\napiVersion: v2\nversion: 0.1.0\n")
                (chart / "base.yaml").write_text("{}\n")
                scratch = Path(directory) / "scratch"
                scratch.mkdir()
                with mock.patch.object(collector, "ROOT", root), mock.patch.object(collector, "PROFILE", profile):
                    fixture = collector.Fixture(scratch)
                    pods = yaml.safe_load((fixture.chart / "pods.yaml").read_text())
                    self.assertFalse(pods["initJobs"]["enabled"])
                    self.assertFalse(pods["sidekickContainer"]["enabled"])
                    self.assertEqual(8080, pods["servicePodConf"]["ports"][0]["containerPort"])
                    for worker in extra_workers:
                        self.assertTrue(pods[worker["values_key"]]["enabled"])
                    names = common.application_names(profile, "fixture-api")
                    documents = [{"kind": "Deployment", "spec": {"template": {"spec": {
                        "containers": [{"name": name}], "initContainers": [{"name": "wait-for-otel-collector"}]
                    }}}} for name in names]
                    self.assertEqual(len(names), len(collector.application_containers(documents)))
                    documents[-1]["spec"]["template"]["spec"]["containers"][0]["name"] = "undeclared-role"
                    with self.assertRaises(AssertionError):
                        collector.application_containers(documents)


if __name__ == "__main__":
    unittest.main()
