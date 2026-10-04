# Email Provider

This package provides a provider-neutral email abstraction with SparkPost and
Bird implementations, a local capture provider for development, and helpers host
applications can reuse without writing their own per-app adapters.

## Bird transactional email

`NewBirdClient` and `NewBirdEmailProvider` implement inline transactional sending
through Bird's regional v1 REST API. Existing SparkPost and local capture
providers are unchanged; choosing Bird is an explicit host configuration change.

```go
httpClient := observability.NewHTTPClient(nil, 15*time.Second)
client, err := emailprovider.NewBirdClient(&emailprovider.NewBirdClientRequest{
    APIKey:     apiKey, // Load a workspace key from approved secret storage.
    HTTPClient: httpClient,
    // BaseURL is optional: the supported key prefix selects the region.
})
if err != nil {
    return err
}
provider := emailprovider.NewBirdEmailProvider(client)
result, err := provider.Send(ctx, &emailprovider.Email{
    From:     "Notifications <notifications@example.com>",
    To:       "recipient@example.com",
    ReplyTo:  "support@example.com",
    Subject:  "Your requested confirmation",
    TextBody: "Your confirmation details.",
    HTMLBody: "<p>Your confirmation details.</p>",
})
// Success means API acceptance, not inbox delivery. Never blindly retry err.
```

Import `time`, `external/emailprovider` and `external/observability` from this
module as appropriate. `.WithHTTPClient(httpClient)` also returns a configuration
copy, following the SparkPost configuration convention.

### Configuration, ownership and telemetry

- `bk_us1_` selects `https://us1.platform.bird.com`; `bk_eu1_` selects
  `https://eu1.platform.bird.com`. An optional `BaseURL` must match exactly (one
  trailing slash is allowed). HTTP, custom hosts, paths, queries and userinfo
  are rejected. This avoids accidentally sending credentials to another service.
- Use a Bird workspace key with `emails` write permission. Prefix/header-syntax
  validation is not checksum validation or a live authorization check.
  `IsHealthy` means configured with a usable context, not verified delivery.
  See [Bird authentication](https://bird.com/docs/guides/authentication).
- `Transport` overrides `HTTPClient.Transport`. Otherwise the supplied client's
  transport is retained; a nil transport gets GHATD's privacy-safe OTel wrapper.
  The client policy is privately copied. Cookie jars and redirects are disabled;
  timeout defaults to, and is capped at, 30 seconds. Shorter positive timeouts
  remain effective. No global/default client is mutated.
- Inject the host's instrumented client/transport to retain its parent spans and
  propagation. Custom transports/propagators are trusted host code: they must not
  retry sends, log bodies/headers or propagate private baggage. Instrumentation
  helpers do not configure a production exporter or prove backend receipt.
- Provider logs contain fixed outcome events, not addresses, subjects, bodies,
  verification links, response diagnostics or message IDs. Public failures use
  existing error-map sentinels; cancellation/deadline errors also preserve the
  standard context sentinel. Never log the configuration object or email.

### Sending and uncertainty

The adapter maps one `To`, one `From`, optional `ReplyTo`, `Subject`, and HTML
and/or plain text. It rejects malformed addresses/header injection and invalid
UTF-8 before dispatch. Input and encoded JSON are each bounded to 1 MiB; accepted
response bodies are bounded to 64 KiB. Oversized or malformed receipts fail closed.

The request explicitly sets `category: transactional`, with open/click tracking
disabled. Use it only for operational mail, never to bypass marketing consent.
`Success` requires HTTP 202, status `accepted`, one accepted recipient and a
bounded `em_` message ID. Bird may subsequently suppress or fail delivery.
See the [send contract](https://bird.com/docs/api/reference/create-email-message).

Each call makes one attempt. The provider does not follow redirects, retry, or
attach an `Idempotency-Key`; the shared `Email` contract has no durable operation
identity. Repeating `Send` is a new attempt that may produce duplicate mail.
A timeout, cancellation, malformed receipt or 5xx after dispatch is an uncertain
outcome, not proof of failure or rollback. Reconcile before manually retrying.
Any outbox/retry extension must preserve a stable logical request and account for
[Bird's finite idempotency window and replay limits](https://bird.com/docs/api/idempotency);
idempotency does not establish exactly-once delivery.

### SparkPost migration compatibility

[Bird's SparkPost migration FAQ](https://bird.com/email-api/features/sparkpost)
explicitly distinguishes the integrations. Shared ownership does not make keys,
request bodies or webhook events interchangeable.

| Area | This adapter and migration boundary |
| --- | --- |
| Authentication and sending | Regional Bearer authentication and `/v1/email/messages`, not a SparkPost transmission or raw-key Authorization header. Existing SparkPost callers remain unchanged. |
| Inline content | Shared From/To/Reply-To/subject/HTML/text supported. Preserve rendered message semantics in controlled tests before switching traffic. |
| Stored templates | No template IDs, substitutions, version/language selection or migration. Render content in the host or separately evaluate [Bird templates](https://bird.com/docs/api/reference/create-email-template). |
| Sender domains and DNS | Not provisioned by this adapter. Verify the selected workspace's sender and required DNS records before cutover; do not assume existing SparkPost verification carries over. See [sending domains](https://bird.com/email-api/features/domains). |
| Suppressions and preferences | No automatic import, removal or override. Review [Bird suppressions](https://bird.com/docs/api/reference/list-suppressions) and recipient preferences separately. Transactional classification is not a marketing opt-out bypass. |
| Delivery and threading | No webhook receiver, event polling, inbox, attachments or RFC thread-header mapping. Acceptance is separate from [message events](https://bird.com/docs/api/reference/list-email-message-events). A future [webhook integration](https://bird.com/docs/guides/webhooks) needs authentication, replay controls, deduplication and recipient/mailbox ownership checks. |
| Marketing | No campaigns, audiences, broadcasts, batch sending or tracking migration. Use a separately reviewed integration if required. |

### Host adoption and controlled validation

Pin a published immutable framework revision, explicitly select Bird, and retain
the previous provider/configuration for rollback. Keep sending disabled until
regional credentials, permissions and sender readiness are independently checked.
Neither client construction nor a successful read-only API call proves sending.

Only with explicit live-test approval, send one non-sensitive transactional test
to a controlled recipient. Record API acceptance and observed delivery separately,
then verify the intended sign-in/verification flow. Confirm that the host actually
enables its OTel exporter/Collector and that the telemetry backend receives the
expected redacted trace. These are release checks, not ordinary unit tests; they
are not implied by the synthetic tests below. Never place keys or private receipt
content in a public validation record.

## SparkPost: injecting an HTTP client

`NewSparkPostClient` still returns `*sp.Client` (github.com/SparkPost/gosparkpost)
and every previously supported initialisation input (BaseURL, APIKey,
APIVersion, Transport) continues to work unchanged.

Host applications can now also inject their own `*http.Client`:

```go
client, err := emailprovider.NewSparkPostClient(&emailprovider.NewSparkPostClientRequest{
    BaseURL:    "https://api.sparkpost.com",
    APIKey:     apiKey,
    HTTPClient: httpClient, // optional
})
```

A fluent, request-scoped option is also available and never mutates the
receiver:

```go
request := (&emailprovider.NewSparkPostClientRequest{
    BaseURL: "https://api.sparkpost.com",
    APIKey:  apiKey,
}).WithHTTPClient(httpClient)

client, err := emailprovider.NewSparkPostClient(request)
```

### Precedence and mutation rules

1. If `HTTPClient` is set, a private copy of its policy (timeout, redirect
   behaviour, cookie jar and transport) is used. The supplied client is never
   mutated or aliased.
2. If `HTTPClient` is nil, a private copy of the process-wide default policy
   (`http.DefaultClient`) is used, so `http.DefaultClient` itself is never
   aliased or mutated.
3. If `Transport` is also set, the explicit transport override is applied to
   the private copy from step 1 or 2, overriding only its transport. The
   supplied client, its transport, and `http.DefaultClient` are left
   untouched.

The copy is shallow: transport, jar and redirect function are shared by
reference but never mutated by the provider. Concurrent providers built this
way never mutate global state or each other.

## SparkPost: context propagation

`SparkPostEmailProvider.Send(ctx, email)` propagates the caller's context:

- If the underlying client implements the optional context-aware interface
  (`SendContext(ctx, transmission)` — which `*gosparkpost.Client` does),
  cancellation, deadlines and tracing parents flow into the outbound HTTP
  request.
- Otherwise the provider falls back to the legacy `Send(transmission)` call,
  so existing mocks and custom implementations keep working.

The per-call context is never stored on the provider, so a single provider is
safe for concurrent sends with distinct contexts.

## SparkPost: error and log hygiene

Send failures map to the package's stable public error
(`ErrEmailProviderSendFailed`); underlying SDK response bodies, full email
addresses, subjects and message bodies are not surfaced in errors or logs.
Existing metadata such as sender/recipient domains, presence flags, subject
length and the successful provider message ID remains available.

## SparkPost: known behaviour note for text-only emails

Validation accepts an email with `TextBody` but no `HTMLBody`, but the
SparkPost transmission currently sets only the HTML part, so a text-only
email may be sent without body content. This is preserved for compatibility
with existing callers; changing it is left to a deliberate follow-up change.

## Running the tests

```sh
asdf exec go test -race ./external/emailprovider
asdf exec go test -race ./external/emailmanager ./external/observability
```

Bird tests use synthetic credentials and controlled transports only. Their test
style audit is: `bird_client_test.go` — table-driven configuration, mapping,
failure, cancellation and concurrency cases; `bird_telemetry_test.go` — sequential
table-driven trace/metric/log privacy cases with isolated OTel providers.
The observability suite also exercises actual OTLP serialization to a local test
receiver; it does not establish production Collector/backend delivery.
