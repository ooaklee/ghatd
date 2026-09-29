# Email Provider

This package provides a provider-neutral email abstraction with a SparkPost
implementation, a local capture provider for development, and helpers host
applications can reuse without writing their own per-app adapters.

## Injecting an HTTP client

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

## Context propagation

`SparkPostEmailProvider.Send(ctx, email)` propagates the caller's context:

- If the underlying client implements the optional context-aware interface
  (`SendContext(ctx, transmission)` — which `*gosparkpost.Client` does),
  cancellation, deadlines and tracing parents flow into the outbound HTTP
  request.
- Otherwise the provider falls back to the legacy `Send(transmission)` call,
  so existing mocks and custom implementations keep working.

The per-call context is never stored on the provider, so a single provider is
safe for concurrent sends with distinct contexts.

## Error and log hygiene

Send failures map to the package's stable public error
(`ErrEmailProviderSendFailed`); underlying SDK response bodies, full email
addresses, subjects and message bodies are not surfaced in errors or logs.
Existing metadata such as sender/recipient domains, presence flags, subject
length and the successful provider message ID remains available.

## Known behaviour note: text-only emails

Validation accepts an email with `TextBody` but no `HTMLBody`, but the
SparkPost transmission currently sets only the HTML part, so a text-only
email may be sent without body content. This is preserved for compatibility
with existing callers; changing it is left to a deliberate follow-up change.

## Running the tests

```sh
asdf exec go test -race ./external/emailprovider
```
