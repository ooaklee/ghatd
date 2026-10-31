# IndexNow local review example

This example runs the real [`indexnow` service](../../external/indexnow/README.md)
against a private local mock engine. It never notifies a public search engine.
The public ownership key-file route is registered using the package handler.
An HTML form lets you compare single-URL GET and multiple-URL POST submissions,
protocol receipts, validation failures, rate limits and transport failures.

From the repository root, with the Go version required by `go.mod` installed:

```sh
go run ./examples/indexnow -listen 127.0.0.1:8097
```

Open <http://127.0.0.1:8097>. The listener requires an explicit loopback IP;
wildcard addresses and hostname listeners are rejected. The mock engine has its
own loopback listener. A restricted HTTP transport and redirect refusal keep
outbound notifications inside that mock, including when submitted URLs use the
reserved canonical fixture `https://example.com`.

The fixed demonstration key is `local-demo-indexnow-key-2026`. Its local route is
<http://127.0.0.1:8097/local-demo-indexnow-key-2026.txt>. The configured canonical
site is `https://example.com`, so this fixture demonstrates route content and
protocol construction; it does not verify ownership with an actual engine.

## Review flow

1. Open the ownership key-file link and check that the body contains the fixed
   demonstration key.
2. Submit one URL with a 200 mock response. Inspect the service receipt and the
   actual GET request captured by the mock.
3. Choose multiple URLs and 202. Inspect the POST URL list and the pending
   receipt. A pending receipt does not mean the URLs were indexed.
4. Repeat with 400, 403, 422, 429, 500 and a simulated transport failure. The 429
   mock adds `Retry-After: 30`; retries remain explicit.
5. Submit a URL on a different host to see service validation before an engine
   request is sent.

The invocation log retains the latest 200 records in memory and resets on
restart. The service's real `AttemptRepository` port is wired to a demo-only
in-memory adapter. Its audit view retains the latest 200 completed attempts and
any in-flight attempts, including failed deliveries. It is not a durable queue.
The wire view retains the latest request received by the mock, which
can predate an invocation rejected locally or stopped by a transport failure.
The fixed key is omitted from captured wire records. The example's unauthenticated
submission controls are for loopback review only; production applications must
retain their own administrator authorization.

## Curl flow

```sh
curl -fsS http://127.0.0.1:8097/local-demo-indexnow-key-2026.txt

curl -fsS http://127.0.0.1:8097/api/submit \
  -H 'Content-Type: application/json' \
  --data '{"mode":"single","urls":["https://example.com/guides/first-page"],"simulation":"200"}'

curl -fsS http://127.0.0.1:8097/api/submit \
  -H 'Content-Type: application/json' \
  --data '{"mode":"batch","urls":["https://example.com/guides/first-page","https://example.com/guides/second-page"],"simulation":"202"}'

curl -fsS http://127.0.0.1:8097/api/history
```

The demo API returns HTTP 200 for a completed service invocation even when the
engine rejected it: read the JSON `error` and `receipt` fields. Invalid demo API
input receives a normal HTTP 4xx response. This review API is not a package API.

For CMS and live-sitemap composition, see
[Configure IndexNow and SEO](../../docs/how-to/configure-indexnow-seo.md). This
fixture does not start a CMS or database. Exercise publish, slug-change,
unpublish and deletion flows through your host application's authenticated CMS
routes after adopting that wiring.
