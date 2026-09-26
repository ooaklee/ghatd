"""End-to-end smoke checker fixtures: backend presence is not correlation."""

import base64
import json
import os
from pathlib import Path
import subprocess
import sys
import threading
import unittest
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import parse_qs, urlsplit


CANARY = "synthetic-sensitive-body-canary"


class SmokeTests(unittest.TestCase):
    def execute_fixture(self, mutation="", encoding="hex"):
        traces = {}

        def wire_id(value):
            return base64.b64encode(bytes.fromhex(value)).decode() if encoding == "base64" else value

        class Handler(BaseHTTPRequestHandler):
            def log_message(self, *args):
                pass

            def respond(self, body, status=200):
                data = json.dumps(body).encode()
                self.send_response(status)
                self.send_header("Content-Type", "application/json")
                self.send_header("Content-Length", str(len(data)))
                self.end_headers()
                self.wfile.write(data)

            def do_GET(self):
                path = urlsplit(self.path).path
                if path.startswith("/api/v1/work") or path.startswith("/api/v1/rejected"):
                    trace_id = f"{len(traces) + 1:032x}"
                    rejected = path.endswith("rejected")
                    traces[trace_id] = rejected
                    self.respond({"status": "rejected" if rejected else "ok", "trace_id": trace_id,
                                  "code": "EXAMPLE-001"}, 429 if rejected else 200)
                elif path == "/api/v1/query":
                    self.respond({"status": "success", "data": {"resultType": "vector", "result": [
                        {"value": [1, "NaN" if mutation == "invalid-metrics" else "2"]}]}})
                elif "/tempo/api/traces/" in path:
                    trace_id = path.rsplit("/", 1)[1]
                    rejected = traces[trace_id]

                    def span(number, parent, name, kind, attrs):
                        return {"traceId": wire_id(trace_id), "spanId": wire_id(f"{number:016x}"),
                                "parentSpanId": wire_id(f"{parent:016x}"), "name": name, "kind": kind,
                                "attributes": [{"key": key, "value": {"stringValue": value}}
                                               for key, value in attrs.items()]}

                    entry = "/api/v1/rejected" if rejected else "/api/v1/work"
                    spans = [span(1, 0, "GET " + entry, "SPAN_KIND_SERVER", {"http.route": entry}),
                             span(2, 1, "process-work", 1, {"outcome": "rejected" if rejected else "success",
                                                            "error.type": "EXAMPLE-001" if rejected else ""})]
                    if not rejected:
                        spans += [span(3, 2, "GET", 3, {}),
                                  span(4, 1 if mutation == "broken-parent" else 3, "GET /dependency", 2,
                                       {"http.route": "/dependency"})]
                    self.respond({"batches": [{"resource": {"attributes": [
                        {"key": "service.name", "value": {"stringValue": "example-api"}},
                        {"key": "deployment.environment.name", "value": {"stringValue": "local"}}]},
                        "scopeSpans": [{"spans": spans}]}]})
                elif "/loki/loki/api/v1/query_range" in path:
                    query = parse_qs(urlsplit(self.path).query)["query"][0]
                    trace_id = query.rsplit('"', 2)[1]
                    values = []
                    for number in [1, 2]:
                        native = {"trace_id": trace_id, "span_id": f"{number:016x}"}
                        if mutation == "unrelated-log-span":
                            native["span_id"] = "ffffffffffffffff"
                        if mutation == "body-only-correlation":
                            native = {}
                        # Text contains convincing IDs, but only native fields count.
                        body = json.dumps({"trace_id": trace_id, "span_id": f"{number:016x}", "message": CANARY})
                        values.append(["1", body, native])
                    self.respond({"status": "success", "data": {"resultType": "streams", "result": [
                        {"stream": {"service_name": "example-api"}, "values": values}]}})
                else:
                    self.respond({"message": CANARY}, 500)

        server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        base = f"http://127.0.0.1:{server.server_port}"
        try:
            result = subprocess.run([sys.executable, str(Path(__file__).with_name("smoke.py")),
                                     "--app-url", base, "--grafana-url", base, "--prometheus-url", base,
                                     "--timeout", "0.5"], capture_output=True, text=True, timeout=5,
                                    env={**os.environ, "GRAFANA_PASSWORD": CANARY})
        finally:
            server.shutdown()
            server.server_close()
            thread.join(timeout=2)
        self.assertNotIn(CANARY, result.stdout + result.stderr)
        self.assertNotIn(base, result.stdout + result.stderr)
        return result

    def test_correlated_fixture_passes_with_both_tempo_id_encodings(self):
        for encoding in ["hex", "base64"]:
            with self.subTest(encoding=encoding):
                result = self.execute_fixture(encoding=encoding)
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertIn("PASS: native log trace/span IDs", result.stdout)

    def test_present_but_invalid_telemetry_cannot_pass(self):
        for mutation in ["broken-parent", "unrelated-log-span", "body-only-correlation", "invalid-metrics"]:
            with self.subTest(mutation=mutation):
                result = self.execute_fixture(mutation)
                self.assertEqual(result.returncode, 1)
                self.assertIn("timed out", result.stderr)
                self.assertNotIn("PASS:", result.stdout)


if __name__ == "__main__":
    unittest.main()
