# Prerelease waitlist

`waitlist` provides opt-in signup, consent persistence, contact integration,
administrator CSV export and one bounded early-access announcement. It is not
a general campaign manager, scheduler, transactional outbox or provider router.

## Composition

Use the host's existing managed Mongo database, contact repository and middleware:

```go
audience := waitlist.NewMongoStore(database)
if err := audience.Initialize(ctx); err != nil {
    return err
}
contacts := waitlist.NewCommsService(contactRepository, audience)
signups := waitlist.NewCommsSignupStore(contacts, audience)
if err := waitlist.AttachRoutes(routes, signups, rateLimitOrActive, adminOnly); err != nil {
    return err
}
announcements := &waitlist.AnnouncementService{
    Store: audience, Provider: provider, From: fromAddress,
    FrontendURL: frontendURL, Enabled: sendEmail, BrandName: applicationName,
}
if err := waitlist.AttachAnnouncementRoutes(routes, announcements, rateLimitOrActive, adminOnly); err != nil {
    return err
}
```

The example's database, repository, provider, exact frontend URL, sending switch
and guards are existing host dependencies. The package does not open or close
clients, install permissive guards, or enable itself through environment variables.
Use the returned contact service wherever the host previously composed its contact
service, including the user manager. Ordinary contact types retain their behavior.
Both public signup paths must display the same prerelease consent before submission;
`CommsType` is a trusted enrollment choice, not permission for arbitrary marketing.

Optional display configuration is deliberately separate from identity:

```go
signups := waitlist.NewCommsSignupStoreWithConfig(contacts, audience,
    waitlist.SignupConfig{Message: "Email me when early access is ready."})
err := waitlist.AttachRoutesWithConfig(routes, signups, rateLimitOrActive, adminOnly,
    waitlist.RouteConfig{ExportFilename: "example-prerelease.csv"})
```

`SignupConfig.Message` changes only the initial recorded copy. `BrandName` is
escaped in preview/sent HTML. The CSV filename must be a plain ASCII `.csv`
basename, never a path or raw header. All have neutral defaults. There is no
configurable identity namespace or additional deployment setting.

## Identity and storage

The same `canonicalEmail` validation feeds both audience and contact persistence.
Email is trimmed, lowercased and validated; display-name mailboxes are rejected.
GHATD owns the fixed communication UUIDv5 seed
`ghatd:waitlist:prerelease-v1:<canonical-email>`. Audience IDs use SHA-256 of the
canonical email. IDs are deterministic identifiers, **not secrets or anonymization**;
the public receipt never exposes them. A different canonical email is a different
signup; no account-email migration or automatic consent transfer is performed.
Host database separation provides application isolation, not the UUID prefix.

`CampaignID` and `ConsentVersion` remain `prerelease-v1`. This package assumes the
new shared ID scheme: it does not silently convert, backfill or delete differently
keyed prototype contact records. Review existing data before adopting it elsewhere.

| Collection | Purpose |
| --- | --- |
| `waitlist_signups` | Canonical address, original join time, consent and source |
| `waitlist_announcement_previews` | Immutable prepared copy |
| `waitlist_announcement` | The single started campaign, frozen on first dispatch |
| `waitlist_deliveries` | Durable claim, acceptance/uncertain state and suppression |

The explicit `Initialize` call maintains `waitlist_unsubscribe_hash` on
`unsubscribeHash`; it is safe to repeat. No automatic migration is registered.
Writes use majority concern. Contact and audience writes are not one transaction:
the contact is insert-only, then enrollment must succeed before acknowledgement.
Retries repair a partial enrollment without overwriting administrator notes,
original timestamps or unsubscribe state. New and repeated public submissions
return the same minimal receipt, never an existing private contact snapshot.

## HTTP contract

| Method/path | Access | Behavior |
| --- | --- | --- |
| `POST /api/v1/waitlist` | Optional active session + rate limiter | Strict JSON email, `consent: true`, `source: "landing"`; generic 201 receipt |
| `GET /api/v1/waitlist/export` | Admin session | Eligible audience CSV; formula-leading cells escaped |
| `GET /api/v1/waitlist/announcement` | Admin session | Frozen preview and current summary, or null before start |
| `POST /api/v1/waitlist/announcement/preview` | Admin session | Validate/save copy without sending |
| `POST /api/v1/waitlist/announcement/send` | Admin session | Dispatch up to ten unclaimed recipients |
| `POST /api/v1/waitlist/unsubscribe` | Public + rate limiter | Opaque token, idempotent 204; no membership disclosure |

Existing `waitlist.*` operation IDs and response shapes remain unchanged. Private
responses use `Cache-Control: no-store`. A GET link never changes unsubscribe
state: the host frontend receives a token in the URL fragment and explicitly
submits it by POST. Only its hash is stored. Never log the token or fragment.

## Sending and recovery

- Remote sending requires `Enabled`, a configured provider, valid sender and HTTPS
  frontend URL. Only a provider explicitly implementing `IsLocalOutputProvider`
  can capture while remote sending is disabled; local loopback HTTP is allowed
  for that capture mode only.
- Preview expires after one hour if dispatch never started. Once started, the
  original copy is frozen; another preview cannot replace it. Current eligible
  new signups can join a later bounded batch of that same announcement.
- A durable per-address claim is written **before** provider I/O. An ambiguous
  result, crash or interrupted claim becomes `needs_review`; it is never retried
  automatically. There is no cross-provider fallback or exactly-once claim.
- `accepted` means provider acceptance, not delivery. In `mode: "local"` it means
  local capture only. Suppression does not erase an earlier acceptance count.
- Each send is bounded to ten seconds and result persistence gets a separate
  five-second context if the caller disconnects. A cancelled final summary can
  leave the caller uncertain; reread state rather than assuming rollback.
- Export/summary currently load the eligible audience and delivery map. This is
  a small prerelease audience feature, not an unbounded bulk marketing engine.

## Verification

```sh
go test -race ./external/waitlist -count=1
```

Set `GHATD_TEST_MONGO_URI` to an isolated test MongoDB to include persistence and
concurrency checks. Tests own randomly named databases only; they do not use live
mail. Missing integration configuration is reported as skipped, not verified.

Test-style audit: `http_test.go` combines validation/route tables and a focused
CSV projection invariant; `announcement_test.go` combines mode/error tables with
documented stateful dispatch/consent/concurrency lifecycles; `store_test.go` and
`comms_test.go` retain ordered persistence/retry lifecycles with isolated fixtures;
`config_test.go` uses named tables for copy, fixed identity, filenames and escaping.
