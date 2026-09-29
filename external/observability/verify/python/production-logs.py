#!/usr/bin/env python3
"""Exercise the shipped Promtail full-log and request-summary queries in Loki.

Requires Docker, Python 3 and pre-pulled grafana/loki:3.5.0 and python:3.12-alpine
images. The client shares the isolated Loki network namespace, so the checks also
work inside containerized CI jobs. No published ports, host mounts, production
credentials or real logs are used.
"""
import http.client
import json
from pathlib import Path
import re
import subprocess
import sys
import time
import urllib.parse
import urllib.request

IMAGE = "grafana/loki:3.5.0"
CLIENT_IMAGE = "python:3.12-alpine"


def docker(*args):
    return subprocess.check_output(["docker", *args], text=True, timeout=60).strip()


def exercise(dashboard, identity):
    service, namespace = identity["service"], identity["namespace"]
    worker_names, migrator = identity["worker_names"], identity["migrator"]
    panel_ids = identity["panels"]
    panels = {panel["id"]: panel for panel in dashboard["panels"]}
    variables = {variable["name"]: variable for variable in dashboard["templating"]["list"]}
    expression = panels[panel_ids["full_logs"]]["targets"][0]["expr"]
    summary_expression = panels[panel_ids["request_summary"]]["targets"][0]["expr"]
    assert panels[panel_ids["full_logs"]]["datasource"] == {"type": "loki", "uid": "${DS_LOGS}"}
    assert panels[panel_ids["full_logs"]]["options"]["dedupStrategy"] == "none"
    assert panels[panel_ids["traces"]]["title"] == "Recent traces (Tempo)"
    for name in ("service", "environment"):
        assert variables[name]["type"] == "custom", "Paused metrics must not empty production identity selectors"
    for name in ("log_pod", "log_container"):
        assert variables[name]["datasource"] == {"type": "loki", "uid": "${DS_LOGS}"}
        assert variables[name]["includeAll"] and variables[name]["multi"]
        assert variables[name]["allValue"] == ".*"
    def render(query, pod=".*", container=".*"):
        query = query.replace("${log_pod:regex}", pod).replace("${log_container:regex}", container)
        assert "$" not in query, "Log query must not depend on metric variables"
        return query
    base = "http://127.0.0.1:3100"
    def request(path, query=None, data=None):
        url = base + path + ("?" + urllib.parse.urlencode(query, doseq=True) if query else "")
        req = urllib.request.Request(url, data=json.dumps(data).encode() if data else None,
                                     headers={"Content-Type": "application/json"})
        with urllib.request.urlopen(req, timeout=5) as response:
            body = response.read()
        return json.loads(body) if body else None
    deadline = time.monotonic() + 50
    while True:
        try:
            with urllib.request.urlopen(base + "/ready", timeout=2) as response:
                if response.status == 200: break
        except (OSError, http.client.HTTPException):
            if time.monotonic() > deadline: raise AssertionError("Fixture Loki did not become ready")
            time.sleep(0.5)
    now = time.time_ns()
    trace_id, span_id = "1" * 32, "2" * 16
    record = {"level": "info", "msg": "http request completed", "operation": "http-request",
              "trace_id": trace_id, "span_id": span_id, "method": "GET", "status": 200,
              "route": "/", "url.path": "/service-worker.js", "duration_ms": 12.375,
              "network.peer.address": "10.0.0.7", "client.address": "10.0.0.7",
              "client.address.source": "peer",
              "http.request.header.x_forwarded_for": "198.51.100.8, 192.0.2.9",
              "http.request.header.cf_connecting_ip": "203.0.113.4",
              "user_agent.original": 'SyntheticBrowser/1.0 "quoted"'}
    line = json.dumps(record)
    legacy_record = {**record, "trace_id": "3" * 32}
    del legacy_record["duration_ms"]
    legacy = json.dumps(legacy_record)
    direct_record = {key: value for key, value in record.items()
                     if not key.startswith("http.request.header.")}
    direct_record.update(trace_id="4" * 32, duration_ms=0.031, status=204)
    direct = json.dumps(direct_record)
    panic = "panic: synthetic startup failure"
    worker = json.dumps({"level": "info", "msg": "worker job completed", "trace_id": "5" * 32})
    migration = json.dumps({"level": "info", "msg": "migration completed"})
    def stream(pod, name, namespace=namespace, app=service, lines=None):
        return {"stream": {"app": app, "namespace": namespace, "pod": pod, "container": name},
                "values": [[str(now + i), value] for i, value in enumerate(lines or ["must not match"])]}
    def wrapped(value):
        return json.dumps({"log": value, "stream": "stderr", "time": "2026-01-01T00:00:00Z"})
    workers = [stream("fixture-worker-" + str(i), name, app=name, lines=[worker])
               for i, name in enumerate(worker_names)]
    migrations = [stream("fixture-a", service + "-migration", lines=[migration])] if migrator else []
    streams = workers + [stream("fixture-a", service, lines=[line, panic, legacy]),
               stream("fixture-b", service, lines=[wrapped(line), wrapped(panic), wrapped(direct)]),
               stream("foreign", service, namespace="foreign-namespace"),
               stream("foreign", "unrelated", app="unrelated")] + migrations
    request("/loki/api/v1/push", data={"streams": streams})
    timerange = {"start": str(now - 1_000_000_000), "end": str(now + 1_000_000_000)}
    def run(pod=".*", container=".*", query=expression):
        response = request("/loki/api/v1/query_range", {"query": render(query, pod, container), "limit": 100, **timerange})
        assert response["status"] == "success"
        return response["data"]["result"]
    results = run()
    values = [value for entry in results for _, value in entry["values"]]
    assert sorted(values) == sorted([line, line, legacy, direct, panic, panic] + ([migration] if migrator else []) + [worker] * len(workers)), "All intended logs, including duplicate content and plain crash lines, must survive"
    correlated = [entry for entry in results if entry["stream"].get("trace_id") == trace_id]
    assert len(correlated) == 2 and all(entry["stream"]["span_id"] == span_id for entry in correlated)
    assert all(entry["stream"]["level"] == "info" for entry in correlated)
    assert sum(len(entry["values"]) for entry in run("fixture-a")) == 3 + int(migrator)
    assert sum(len(entry["values"]) for entry in run(container=service + "-migration")) == int(migrator)
    assert sum(len(entry["values"]) for entry in run("(fixture-a|fixture-b)", service)) == 6
    for worker_name in worker_names:
        assert sum(len(entry["values"]) for entry in run(container=worker_name)) == 1
    assert run("missing-pod") == []
    summary = run(query=summary_expression)
    assert all(not entry["stream"].get("__error__") for entry in summary)
    summary_lines = [value for entry in summary for _, value in entry["values"]]
    assert sorted(summary_lines) == sorted([line, line, legacy, direct]), "Only HTTP completions belong in the summary; preserve duplicates and old records"
    rows = [json.loads(value) for value in summary_lines]
    assert sum(row.get("duration_ms") == 12.375 for row in rows) == 2
    assert next(row for row in rows if row["trace_id"] == "3" * 32).get("duration_ms") is None, "Missing historical duration must not become zero"
    assert next(row for row in rows if row["trace_id"] == "4" * 32)["duration_ms"] == 0.031, "Fast requests retain fractional milliseconds"
    assert all(row["route"] == "/" and row["url.path"] == "/service-worker.js" for row in rows)
    assert all(row["network.peer.address"] == "10.0.0.7" for row in rows)
    assert rows[0]["user_agent.original"] == record["user_agent.original"], "The summary source retains full JSON for extraction"
    assert sum(len(entry["values"]) for entry in run("fixture-a", query=summary_expression)) == 2
    assert run(container=service + "-migration", query=summary_expression) == []
    assert run("missing-pod", query=summary_expression) == []
    # Exercise the selectors in the actual Loki-backed variable definitions.
    for name, field, expected in (("log_pod", "pod", {"fixture-a", "fixture-b"} | {"fixture-worker-" + str(i) for i in range(len(workers))}),
                                  ("log_container", "container", {service} | ({service + "-migration"} if migrator else set()) | set(worker_names))):
        query = render(variables[name]["query"])
        matched = re.fullmatch(r"label_values\((.*),\s*(\w+)\)", query)
        assert matched and matched[2] == field
        series = request("/loki/api/v1/series", {"match[]": matched[1], **timerange})
        assert {entry[field] for entry in series["data"]} == expected
    print("Production Loki queries passed: app/namespace isolation, pod/container selections, wrapped/unwrapped JSON, full log preservation, HTTP-only summaries, fractional/missing duration, separate peer/header claims, duplicate content and Loki-backed variables.")


def main():
    import common
    root, profile, _ = common.options()
    dashboard = json.loads(common.resolve(root, profile["dashboards"]["paths"][1]).read_text())
    identity = {"service": profile["service"]["name"], "namespace": profile["service"]["namespace"],
                "worker_names": [profile["service"]["name"] + "-" + role["suffix"] for role in profile["helm"]["workers"]],
                "migrator": profile["helm"]["migrator"], "panels": common.panels(profile)}
    container = docker("run", "--pull=never", "--rm", "-d", "--network", "none", IMAGE,
                       "-config.file=/etc/loki/local-config.yaml")
    client = None
    try:
        # Docker may live outside the current CI container. Keep HTTP within the
        # fixture's network namespace rather than assuming host loopback access.
        client = docker("create", "--pull=never", "-i", "--network", "container:" + container,
                        CLIENT_IMAGE, "python", "-c", Path(__file__).read_text(), "--fixture-client")
        subprocess.run(["docker", "start", "--attach", "--interactive", client],
                       input=json.dumps({"dashboard": dashboard, "identity": identity}), text=True, check=True, timeout=100)
        assert docker("inspect", "--format", "{{.State.ExitCode}}", client) == "0", "Loki fixture client failed"
    except Exception:
        # Fixture logs contain only synthetic test data and aid startup diagnosis.
        print(docker("logs", container), file=sys.stderr)
        raise
    finally:
        if client:
            docker("rm", "--force", client)
        docker("rm", "--force", container)


if __name__ == "__main__":
    if sys.argv[1:] == ["--fixture-client"]:
        fixture = json.load(sys.stdin)
        exercise(fixture["dashboard"], fixture["identity"])
    else:
        import common
        common.run(main)
