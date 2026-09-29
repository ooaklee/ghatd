#!/usr/bin/env python3
"""Check the shipped dashboard queries against Prometheus, using synthetic data.

Run with Python 3 and Docker. No application, credentials or backend is needed.
Fixtures use the expressions from both dashboards, including their actual label
selectors, so they exercise query behavior rather than duplicated test queries.
"""

import json
from pathlib import Path
import subprocess

import common


IMAGE = "prom/prometheus:v3.5.0"
CONFIG = {}


def render(expression):
    for variable, value in (("service", "fixture-api"), ("environment", "testing")):
        for formatter in ("doublequote", "json"):
            expression = expression.replace("${" + variable + ":" + formatter + "}", json.dumps(value))
    expression = expression.replace("$__rate_interval", "5m").replace("$__range", "5m")
    assert "$" not in expression, f"Unexpanded query variable: {expression}"
    return expression


def series(metric, values, **labels):
    labels = {"service_name": "fixture-api", "deployment_environment_name": "testing", **labels}
    selector = ",".join(f"{key}={json.dumps(value)}" for key, value in sorted(labels.items()))
    return {"series": metric + "{" + selector + "}", "values": values}


def sample(expression, value=None, labels="{}", at="10m"):
    return {"expr": expression, "eval_time": at,
            "exp_samples": [] if value is None else [{"labels": labels, "value": value}]}


def labelled_samples(expression, expected, at="10m"):
    """Expected parallel groups, retaining labels so aggregation mistakes fail."""
    return {"expr": expression, "eval_time": at, "exp_samples": [
        {"labels": "{" + ",".join(f"{key}={json.dumps(label_value)}" for key, label_value in sorted(labels.items())) + "}",
         "value": value} for labels, value in expected
    ]}


def counter_values(increment, reset=False):
    return " ".join(str(increment * (i if not reset or i < 8 else i - 7)) for i in range(11))


def near(expression, expected, labels):
    return labelled_samples(f"abs(({expression}) - ({expected})) < bool 1e-9", [(labels, 1)])


def fixture_tests(expressions):
    count = "http_server_request_duration_seconds_count"
    base = {"http_request_method": "GET", "http_route": "/probe"}
    reset = series(count, "0 10 20 30 40 50 60 70 10 20 30", instance="a", http_response_status_code="200", **base)
    steady = series(count, "0+20x10", instance="b", http_response_status_code="200", **base)
    traffic = [reset, steady]
    ratio = expressions[12][0]
    tests = [
        {"name": "counter reset and two replicas; traffic without any error series", "input_series": traffic,
         "promql_expr_test": [sample(expressions[2][0], 150), sample(expressions[3][0], 0), sample(ratio, 0)]},
        {"name": "errors divided by all requests across replicas", "input_series": [reset,
             series(count, "0+18x10", instance="b", http_response_status_code="200", **base),
             series(count, "0+2x10", instance="b", http_response_status_code="503", **base)],
         "promql_expr_test": [sample(expressions[2][0], 150), sample(expressions[3][0], 10), sample(ratio, 1 / 15)]},
        {"name": "missing telemetry must not look like zero errors", "input_series": [],
         "promql_expr_test": [sample(expressions[2][0]), sample(expressions[3][0]), sample(ratio),
                              sample(expressions[13][0], 0), sample(expressions[14][0])]},
        {"name": "idle counter has no defined error ratio", "input_series": [series(count, "10x10", **base)],
         "promql_expr_test": [sample(expressions[2][0], 0), sample(f"({ratio}) == ({ratio})")]},
        {"name": "runtime delivery freshness is independent of request traffic",
         "input_series": [series("go_goroutine_count", "12x9", instance="a")],
         "promql_expr_test": [sample(expressions[13][0], 1), sample(expressions[14][0], 60),
                              sample(expressions[13][0], 0, at="15m"), sample(expressions[14][0], at="15m")]},
    ]
    histograms = []
    for instance in ("a", "b"):
        for boundary, increment in (("0.1", 9), ("1", 10), ("+Inf", 10)):
            values = [increment * (i if i < 8 or instance == "b" else i - 7) for i in range(11)]
            histograms.append(series("http_server_request_duration_seconds_bucket", " ".join(map(str, values)),
                                     instance=instance, le=boundary, **base))
    # 90% of observations are <=100ms, the remainder <=1s. The distribution
    # remains unchanged through one replica's reset; rate must precede sum.
    tests.append({"name": "recent histogram quantiles survive replica resets", "input_series": histograms,
                  "promql_expr_test": [sample(f"abs(({expressions[7][0]}) - 0.55) < bool 1e-9", 1, '{http_route="/probe"}'),
                                       sample(f"abs(({expressions[7][1]}) - (1 / 18)) < bool 1e-9", 1, '{http_route="/probe"}')]})
    return tests


def application_fixture_tests(expressions):
    metric = CONFIG["cache_metric"]
    inputs = [series(metric, counter_values(7), cache="http-response", event="hit"),
              series(metric, counter_values(2), cache="http-response", event="miss"),
              series(metric, counter_values(1), cache="http-response", event="stale"),
              series(metric, counter_values(100), cache="http-response", event="store")]
    return [
        {"name": "cache ratio counts lookups, not response stores", "input_series": inputs,
         "promql_expr_test": [near(expressions[26][0], .7, {"cache": "http-response"})]},
        {"name": "cache misses without hit series produce zero", "input_series": [inputs[1]],
         "promql_expr_test": [near(expressions[26][0], 0, {"cache": "http-response"})]},
        {"name": "no lookup telemetry stays missing", "input_series": [inputs[3]],
         "promql_expr_test": [sample(expressions[26][0])]},
        {"name": "per-instance cache counter resets", "input_series": [
            series(metric, counter_values(10, reset=True), cache="http-response", event="miss", instance="a"),
            series(metric, counter_values(20), cache="http-response", event="miss", instance="b")],
         "promql_expr_test": [near(expressions[25][0], .5, {"cache": "http-response", "event": "miss"})]},
        {"name": "browser intake outcome counters", "input_series": [
            series("ghatd_browser_intake_batch_count_total", counter_values(3), outcome="accepted")],
         "promql_expr_test": [near(expressions[35][0], .05, {"outcome": "accepted"})]},
    ]


def worker_fixture_tests(expressions):
    labels = {"queue_operation": CONFIG["queue_operation"], "queue_outcome": "acked"}
    buckets = [series(CONFIG["queue_bucket_metric"], counter_values(increment),
                      le=boundary, **labels) for boundary, increment in (("1", 9), ("5", 10), ("+Inf", 10))]
    return [
        {"name": "worker settlement counters preserve outcomes across replica resets", "input_series": [
            series(CONFIG["queue_count_metric"], counter_values(10, reset=True), instance="a", **labels),
            series(CONFIG["queue_count_metric"], counter_values(20), instance="b", **labels)],
         "promql_expr_test": [near(expressions[36][0], .5, labels)]},
        {"name": "worker p95 includes actual job duration", "input_series": buckets,
         "promql_expr_test": [near(expressions[37][0], 3, {"queue_operation": CONFIG["queue_operation"]})]},
        {"name": "absent worker telemetry remains missing", "input_series": [],
         "promql_expr_test": [sample(expressions[36][0]), sample(expressions[37][0])]},
    ]


def main():
    global CONFIG
    root, profile, _ = common.options()
    CONFIG = profile["dashboards"]
    panel_ids = common.panels(profile)
    # Remap host panel IDs to the conventional semantic IDs used by the
    # shared scenario definitions; no scenario is copied into host config.
    original_id = {panel_ids[key]: value for key, value in common.PANEL_IDS.items()}
    rules, tests = [], []
    for index, relative in enumerate(CONFIG["paths"]):
        dashboard_path = common.resolve(root, relative)
        dashboard = json.loads(dashboard_path.read_text())
        ids = [panel["id"] for panel in dashboard["panels"]]
        assert len(ids) == len(set(ids)), "Duplicate panel IDs"
        assert {value for key, value in panel_ids.items() if key != "request_summary"} <= set(ids), "An existing or required metric panel is missing"
        variables = {variable["name"] for variable in dashboard["templating"]["list"]}
        assert {"DS_METRICS", "DS_TRACES", "DS_LOGS", "service", "environment"} <= variables
        expressions = {}
        for panel in dashboard["panels"]:
            if panel.get("datasource", {}).get("type") == "prometheus":
                assert panel["datasource"]["uid"] == "${DS_METRICS}"
                expressions[original_id.get(panel["id"], panel["id"]) ] = []
                for target in panel["targets"]:
                    assert target["datasource"]["uid"] == "${DS_METRICS}"
                    assert target["interval"] == ("5s" if index == 0 else "1m")
                    assert "service_name=${service:doublequote}" in target["expr"]
                    assert "deployment_environment_name=${environment:doublequote}" in target["expr"]
                    if original_id.get(panel["id"], panel["id"]) >= 15:
                        assert target["exemplar"] is True
                    expression = render(target["expr"])
                    expressions[original_id.get(panel["id"], panel["id"]) ].append(expression)
                    rules.append({"record": f"dashboard_fixture_{len(rules)}", "expr": expression})
        tests.extend(fixture_tests(expressions))
        tests.extend(application_fixture_tests(expressions))
        tests.extend(worker_fixture_tests(expressions))
    # JSON is valid YAML; using the standard library avoids a PyYAML dependency.
    # Stream fixtures through stdin so remote Docker daemons and Docker Desktop
    # do not need access to the workstation's temporary directories.
    command = ["docker", "run", "--rm", "--pull=never", "--network=none", "-i", "--entrypoint", "/bin/sh", IMAGE,
               "-c", 'cat > /tmp/input.yaml && exec /bin/promtool "$@" /tmp/input.yaml', "promtool"]
    subprocess.run(command + ["check", "rules"], check=True, text=True,
                   input=json.dumps({"groups": [{"name": "dashboards", "rules": rules}]}))
    subprocess.run(command + ["test", "rules"], check=True, text=True,
                   input=json.dumps({"evaluation_interval": "1m", "fuzzy_compare": True, "tests": tests}))
    print(f"Validated {len(rules)} shipped expressions and {len(tests)} scenario groups.")


if __name__ == "__main__":
    common.run(main)
