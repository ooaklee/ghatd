# Teleprovider

Stateless messaging service with typed provider ports and an OpenWA HTTP adapter.
`service.go` owns validation and orchestration; `openwa.go` owns HTTP credentials,
DTOs and safe error translation. Hosts retain consent, current destination
authority, durable delivery, reply acceptance and checkpoint policy. This package
contains no datastore, cache, handler, registry or background worker. Importing
or constructing a service makes no network request and starts no process.

```go
service, err := teleprovider.NewTeleProvider(teleprovider.Config{
    Provider: "openwa",
    OpenWA: teleprovider.OpenWAConfig{
        Endpoint: endpoint, APIKey: key, SessionID: sessionUUID,
        Engine: "baileys", // optional capability hint; not readiness evidence
    },
    Timeout: 15 * time.Second,
    ReadRetryLimit: 0,
})
// Handle err before use. The host obtains configuration from environment.
result, err := service.CheckNumber(ctx, "+12025550123")
// A completed negative has Status == "not_registered" and err == nil.
// An error remains unknown. Neither outcome proves ownership.
```

Numbers must already be complete canonical E.164 including `+` and a real country calling code, validated with libphonenumber metadata. No locale/default-country inference or implicit formatting occurs in this package. A host may accept local input with an explicit country; the [internationalisation manager](../internationalisationmanager/README.md) normalises and rechecks catalogue availability before calling this canonical-only service. Only the adapter removes `+`. Canonical contact IDs (`@c.us`, `@s.whatsapp.net`, `@lid`) and actual `@g.us` group IDs remain opaque identities. Callers own their provenance, authorisation, freshness and consent. Never derive a JID or phone number from an opaque ID.

Operations:

- `CheckNumber(ctx, number)` checks registration without sending or changing a draft.
- `SendDirect(ctx, numberOrCanonicalID, text)` resolves new phone destinations first.
- `SendGroup(ctx, groupID, text)` and `ReplyToMessage(ctx, chatID, quotedMessageID, text)` preserve identities. Receipts say `accepted`, never delivered.
- `CreateGroup(ctx, name, participants)` validates every participant before I/O, resolves phone inputs, and deduplicates canonical IDs. OpenWA returns group ID/name without per-participant membership receipts: do not infer that every participant joined.
- `JoinGroup(ctx, inviteCode)` returns the actual group ID. Invalid invite, provider access denial, unsupported engine and uncertain outcome are separate codes.
- `ListReplies(ctx, MessageQuery{ChatID: id, Limit: 50, After: cursor})` returns incoming rows and raw-page traversal state. No browser/group API is added for these operations.

`Config.Adapter` demonstrates a future provider seam with an application-owned implementation of the typed ports; no OpenWA DTO leaks into callers. The host only needs the narrower registration port. Disabled mode returns `unknown`/`TELE_PROVIDER_UNAVAILABLE`. Enabled malformed configuration fails construction without network calls. The constructor copies an injected HTTP client and disables redirects, protecting API keys from forwarding even on same-origin redirects. Response bodies are limited to 1 MiB and one JSON document. Deadlines include retry backoff. Optional read retries are 0–2, only on network/429/502/503/504 failures, with exponential delay (100/200 ms) and respect for `Retry-After`; a longer delay than the remaining deadline ends the operation. Authentication/shape failures are not retried. Mutations always have zero retries.

Typed `Error.Code`, `RetryAfter` and `Uncertain` are safe outcomes. Error strings omit provider body/URL/number/key. Never log config, raw causes, requests, text, invite codes or result identities. Cancelled context checks may return the raw context error rather than `*Error`. Use `errors.Is` for cancellation; do not serialise wrapped diagnostics. HTTP 401/403 from OpenWA is a provider credential/access failure, never an app-login failure. Ambiguous mutation responses, timeouts and 5xx return `TELE_OUTCOME_UNCERTAIN`; stop and reconcile using authorised provider history/group state or an operator. A new request key is not provider idempotency. Never silently replay a send/create/join.

## Reply polling and recovery

Baseline OpenWA v0.24.0 (`e9afb97da44fa1f3c1fdc469a134a706394eda46`) stores newest-first rows. `after` is a storage row ID that walks **older** pages; `MessageID` is a separate engine-specific WhatsApp identity. Group `Sender` may be the group, while `Author` identifies the person. Quote IDs come from `metadata.quotedMessage.id`; absence remains unknown. Outgoing echoes are removed, but the cursor advances from the raw page, including outgoing-only pages. A full page means there may be more; an empty extra page is normal. Equal message timestamps do not define ordering.

The [tested consumer example](example_polling_test.go) rescans the newest page on every invocation and then resumes a separately retained older backlog. Its illustrative lookback is two-row pages, one older page per pass; production consumers must choose page/time budgets and explicit overlap/retention policies for their use case. Persist deduplication by provider + session + row ID (and WhatsApp ID where present) in the consumer's repository. Accept each page durably before advancing its checkpoint; retries/overlap are normal. Rows can arrive while traversing old pages, so keep rescanning the head and periodically rescan the retained overlap/backlog for late or backfilled records. Do not stop purely at a message timestamp.

A remaining continuation means backlog, not completeness. A missing/expired anchor returns `TELE_CURSOR_UNAVAILABLE`: retain the previous checkpoint, report a gap and reconcile/rescan with explicit operator policy. An exhausted provider result proves only the end of currently retained rows, not historical completeness; upstream deletion/retention can erase unseen rows without an observable marker. Record the chosen initial lookback, last successful poll and retention assumptions; never claim no gaps solely because a page ended. In-memory example state does not guarantee restart recovery or exactly-once processing. A durable consumer requires its own repository tests, retention/encryption decisions and migrations. No business action may be inferred from reply text.

Webhook ingestion is deferred. Future work must use raw-body `X-OpenWA-Signature: sha256=<hex>`, authenticated session/chat binding and durable deduplication. A timestamp-plus-newline callback format is incompatible with that provider format.


## Verification and adoption

Run `go test -race ./external/teleprovider -count=1` for local controlled HTTP
fixtures covering malformed registration, credentials and redirects, deadlines,
bounded reads, single-attempt mutations, opaque identity and reply-history gaps.
The consumer example is in-memory; it does not implement durable exactly-once
processing. The separately gated read-only live test skips unless
`TELEPROVIDER_LIVE_READ_ONLY=true`; enabling it also requires an authorised
`OPENWA_SMOKE_NUMBER`, endpoint/key/session configuration. It makes an actual
registration lookup, so ordinary package checks do not qualify live readiness.

Hosts import `github.com/ooaklee/ghatd/external/teleprovider`, pass resolved
`Config`, and choose when to invoke operations. Construction never reads the
environment. No storage migration or compatibility aliases are included.
