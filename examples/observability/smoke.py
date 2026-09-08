#!/usr/bin/env python3
"""Exercise the reference service and verify its metrics, traces, and logs.

Uses only the Python standard library. Grafana credentials default to the local
kit's admin/admin and may be overridden with GRAFANA_USER/GRAFANA_PASSWORD.
Diagnostics deliberately omit URLs, credentials, response bodies, and errors.
"""

import argparse
import base64
import binascii
import json
import math
import os
import re
import sys
import time
import urllib.error
import urllib.parse
import urllib.request


MAX_RESPONSE_BYTES = 4 * 1024 * 1024
IDENTITY = re.compile(r"[A-Za-z0-9][A-Za-z0-9._:/-]{0,254}\Z")


class SmokeError(Exception):
    """A failure whose details are intentionally not printed."""


class SafeParser(argparse.ArgumentParser):
    def error(self, message):
        self.exit(2, "smoke: invalid arguments; use --help for supported options\n")


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, request, response, code, message, headers, new_url):
        # Do not forward local Grafana credentials or synthetic traffic elsewhere.
        return None


def arguments(argv=None):
    parser = SafeParser(description=__doc__)
    parser.add_argument("--app-url", default="http://localhost:8080")
    parser.add_argument("--grafana-url", default="http://localhost:3000")
    parser.add_argument("--prometheus-url", default="http://localhost:9090")
    parser.add_argument("--service", default="example-api")
    parser.add_argument("--environment", default="local")
    parser.add_argument("--timeout", type=float, default=90,
                        help="total time budget in seconds (positive, at most 600; default: 90)")
    args = parser.parse_args(argv)
    if not math.isfinite(args.timeout) or not 0 < args.timeout <= 600:
        parser.error("invalid timeout")
    if not IDENTITY.fullmatch(args.service) or not IDENTITY.fullmatch(args.environment):
        parser.error("invalid identity")
    for value in (args.app_url, args.grafana_url, args.prometheus_url):
        try:
            url = urllib.parse.urlsplit(value)
            if (len(value) > 2048 or url.scheme not in ("http", "https")
                    or not url.hostname or url.username is not None
                    or url.password is not None or url.query or url.fragment):
                parser.error("invalid URL")
            _ = url.port
        except ValueError:
            parser.error("invalid URL")
    return args


class Client:
    def __init__(self, deadline):
        self.deadline = deadline
        self.opener = urllib.request.build_opener(NoRedirect())
        user = os.environ.get("GRAFANA_USER", "admin")
        password = os.environ.get("GRAFANA_PASSWORD", "admin")
        self.auth = "Basic " + base64.b64encode((user + ":" + password).encode()).decode()

    def get(self, base, path, query=None, status=200, grafana=False):
        remaining = self.deadline - time.monotonic()
        if remaining <= 0:
            raise SmokeError()
        url = base.rstrip("/") + path
        if query:
            url += "?" + urllib.parse.urlencode(query)
        headers = {"Accept": "application/json"}
        if grafana:
            headers["Authorization"] = self.auth
        request = urllib.request.Request(url, headers=headers)
        try:
            try:
                response = self.opener.open(request, timeout=min(3, remaining))
            except urllib.error.HTTPError as error:
                response = error  # The rejection fixture intentionally returns 429.
            with response:
                if response.status != status:
                    raise SmokeError()
                chunks = []
                size = 0
                while size <= MAX_RESPONSE_BYTES:
                    if time.monotonic() >= self.deadline:
                        raise SmokeError()
                    chunk = response.read1(min(65536, MAX_RESPONSE_BYTES + 1 - size))
                    if not chunk:
                        break
                    chunks.append(chunk)
                    size += len(chunk)
                payload = b"".join(chunks)
            if len(payload) > MAX_RESPONSE_BYTES:
                raise SmokeError()
            data = json.loads(payload)
            if not isinstance(data, dict):
                raise SmokeError()
            return data
        except (OSError, ValueError, urllib.error.URLError) as error:
            raise SmokeError() from error


def identifier(value, size):
    """Tempo JSON can encode protobuf byte identifiers as hex or base64."""
    if not isinstance(value, str):
        return ""
    if re.fullmatch(r"[0-9a-fA-F]{%d}" % (size * 2), value):
        decoded = bytes.fromhex(value)
    else:
        try:
            decoded = base64.b64decode(value, validate=True)
        except (ValueError, binascii.Error):
            return ""
    return decoded.hex() if len(decoded) == size and any(decoded) else ""


def attributes(items):
    result = {}
    for item in items if isinstance(items, list) else []:
        if isinstance(item, dict) and isinstance(item.get("value"), dict):
            result[item.get("key")] = item["value"].get("stringValue")
    return result


def trace_spans(payload, trace_id, service, environment):
    trace = payload.get("trace", payload)
    if not isinstance(trace, dict):
        return []
    batches = trace.get("resourceSpans", trace.get("batches", []))
    spans = []
    for batch in batches if isinstance(batches, list) else []:
        if not isinstance(batch, dict):
            continue
        resource = attributes(batch.get("resource", {}).get("attributes", []))
        if (resource.get("service.name") != service
                or resource.get("deployment.environment.name") != environment):
            continue
        scopes = batch.get("scopeSpans", batch.get("instrumentationLibrarySpans", []))
        for scope in scopes if isinstance(scopes, list) else []:
            for raw in scope.get("spans", []) if isinstance(scope, dict) else []:
                if not isinstance(raw, dict) or identifier(raw.get("traceId"), 16) != trace_id:
                    continue
                span_id = identifier(raw.get("spanId"), 8)
                if span_id:
                    spans.append({
                        "id": span_id,
                        "parent": identifier(raw.get("parentSpanId"), 8),
                        "name": raw.get("name"),
                        "kind": raw.get("kind"),
                        "attributes": attributes(raw.get("attributes", [])),
                    })
    return spans


def kind(span, value, name):
    return span["kind"] in (value, name, "SPAN_KIND_" + name)


def trace_relationships(spans, rejected):
    route = "/api/v1/rejected" if rejected else "/api/v1/work"
    outcome = "rejected" if rejected else "success"
    entries = [span for span in spans if kind(span, 2, "SERVER")
               and span["attributes"].get("http.route") == route and not span["parent"]]
    for entry in entries:
        for operation in spans:
            if (operation["name"] != "process-work" or operation["parent"] != entry["id"]
                    or not kind(operation, 1, "INTERNAL") or operation["id"] == entry["id"]
                    or operation["attributes"].get("outcome") != outcome):
                continue
            if rejected:
                if operation["attributes"].get("error.type") == "EXAMPLE-001":
                    return {entry["id"], operation["id"]}
                continue
            for client in spans:
                if not kind(client, 3, "CLIENT") or client["parent"] != operation["id"]:
                    continue
                for dependency in spans:
                    if (kind(dependency, 2, "SERVER") and dependency["parent"] == client["id"]
                            and dependency["attributes"].get("http.route") == "/dependency"
                            and len({entry["id"], operation["id"], client["id"], dependency["id"]}) == 4):
                        # Require logs on both entry and business spans, proving
                        # the child operation rebound the invocation logger.
                        return {entry["id"], operation["id"]}
    return set()


def correlated_log_spans(payload, trace_id):
    data = payload.get("data", {})
    if payload.get("status") != "success" or data.get("resultType") != "streams":
        return set()
    found = set()
    for stream in data.get("result", []):
        labels = stream.get("stream", {})
        for value in stream.get("values", []):
            if not isinstance(value, list) or len(value) < 2:
                continue
            # Native Loki structured metadata can appear on the entry or in
            # the returned stream labels. Never extract IDs from log text.
            metadata = dict(labels)
            if len(value) >= 3 and isinstance(value[2], dict):
                metadata.update(value[2])
            if identifier(metadata.get("trace_id"), 16) == trace_id:
                span_id = identifier(metadata.get("span_id"), 8)
                if span_id:
                    found.add(span_id)
    return found


def positive_metric(payload):
    if payload.get("status") != "success":
        return False
    data = payload.get("data", {})
    if data.get("resultType") != "vector":
        return False
    for sample in data.get("result", []):
        try:
            value = float(sample["value"][1])
            if math.isfinite(value) and value > 0:
                return True
        except (KeyError, IndexError, TypeError, ValueError):
            continue
    return False


def metric_queries(args):
    identity = ("service_name=" + json.dumps(args.service)
                + ",deployment_environment_name=" + json.dumps(args.environment))
    operations = "example_service_operation_count_total"
    requests = "http_server_request_duration_seconds_count"
    buckets = "example_service_operation_duration_seconds_bucket"
    return [
        f'sum({operations}{{{identity},operation="process-work",outcome="success"}})',
        f'sum({operations}{{{identity},operation="process-work",outcome="rejected"}})',
        f'sum({requests}{{{identity},http_route="/api/v1/work",http_response_status_code="200"}})',
        f'sum({requests}{{{identity},http_route="/api/v1/rejected",http_response_status_code="429"}})',
        f'sum({buckets}{{{identity},operation="process-work",outcome="success",le="+Inf"}})',
    ]


def run(args):
    deadline = time.monotonic() + args.timeout
    client = Client(deadline)
    start_ns = time.time_ns() - 5_000_000_000
    fixtures = [("/api/v1/work", 200), ("/api/v1/work", 200), ("/api/v1/rejected", 429)]
    traces = []
    metrics = [False] * len(metric_queries(args))
    last_phase = None
    while time.monotonic() < deadline:
        phase = "synthetic requests"
        try:
            while len(traces) < len(fixtures):
                path, status = fixtures[len(traces)]
                body = client.get(args.app_url, path, status=status)
                supplied_id = body.get("trace_id")
                trace_id = (identifier(supplied_id, 16)
                            if isinstance(supplied_id, str) and re.fullmatch(r"[0-9a-fA-F]{32}", supplied_id)
                            else "")
                rejected = status == 429
                if (not trace_id or any(item["id"] == trace_id for item in traces)
                        or body.get("status") != ("rejected" if rejected else "ok")
                        or (rejected and body.get("code") != "EXAMPLE-001")):
                    raise SmokeError()
                traces.append({"id": trace_id, "rejected": rejected,
                               "required_logs": set(), "logs": False})
            phase = "metrics"
            for index, query in enumerate(metric_queries(args)):
                if not metrics[index]:
                    metrics[index] = positive_metric(client.get(
                        args.prometheus_url, "/api/v1/query", {"query": query}))
            phase = "trace relationships"
            for trace in traces:
                if not trace["required_logs"]:
                    payload = client.get(args.grafana_url,
                                         "/api/datasources/proxy/uid/tempo/api/traces/" + trace["id"],
                                         grafana=True)
                    spans = trace_spans(payload, trace["id"], args.service, args.environment)
                    trace["required_logs"] = trace_relationships(spans, trace["rejected"])
            phase = "correlated logs"
            for trace in traces:
                if trace["required_logs"] and not trace["logs"]:
                    selector = ("{service_name=" + json.dumps(args.service)
                                + ",deployment_environment_name=" + json.dumps(args.environment) + "}")
                    payload = client.get(args.grafana_url,
                                         "/api/datasources/proxy/uid/loki/loki/api/v1/query_range",
                                         {"query": selector + " | trace_id=" + json.dumps(trace["id"]),
                                          "start": str(start_ns), "end": str(time.time_ns()),
                                          "limit": "100", "direction": "forward"}, grafana=True)
                    trace["logs"] = trace["required_logs"].issubset(correlated_log_spans(payload, trace["id"]))
            if all(metrics) and all(trace["logs"] for trace in traces):
                print("PASS: synthetic success and rejection responses")
                print("PASS: positive operation counts, duration buckets, and HTTP metrics")
                print("PASS: exact trace IDs and server/operation/client/dependency parentage")
                print("PASS: native log trace/span IDs match request and operation spans")
                return 0
            phase = ("metrics" if not all(metrics) else "trace relationships"
                     if any(not trace["required_logs"] for trace in traces) else "correlated logs")
        except SmokeError:
            pass
        if phase != last_phase:
            print("Waiting for " + phase + ".", flush=True)
            last_phase = phase
        remaining = deadline - time.monotonic()
        if remaining > 0:
            time.sleep(min(1, remaining))
    print("FAIL: timed out waiting for " + (last_phase or "telemetry") + ".", file=sys.stderr)
    return 1


def main(argv=None):
    args = arguments(argv)
    try:
        return run(args)
    except KeyboardInterrupt:
        print("FAIL: smoke check interrupted.", file=sys.stderr)
        return 130
    except Exception:
        # Unexpected backend shapes must not leak response data or URL-bearing
        # exception text through a traceback.
        print("FAIL: unable to validate telemetry response.", file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(main())
