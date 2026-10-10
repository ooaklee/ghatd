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
If the host replaces a contact service already composed by `starter/v0`, bind
both user-manager ports to the replacement before constructing handlers:

```go
contacts.WithVoterService(services.Voter)
services.Contacter = contacts
services.UserManager.ContacterService = contacts
services.UserManager.WithCommsVotingService(contacts)
```

`NewCommsService` and `NewCommsServiceWithConfig` construct a fresh contact
service; they do not copy the previous instance's optional voter dependency.
Voting also requires the explicit shared-vote index migration, the existing live
administrator authorizer and `EnableCommsVoting: true` when attaching routes.
Follow the [native User Manager voting composition](../usermanager/README.md#private-conversation-voting)
and [voter index setup](../voter/README.md) when enabling that capability.
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

`CampaignID` is fixed to `prerelease-v1`; the default `ConsentVersion` is
`prerelease-v1`. Trusted host configuration may select a different consent
version for newly inserted contacts and audience records. Signup and contact records use the deterministic ID scheme above. The package
performs no conversion, backfill or deletion of records keyed differently.

| Collection | Purpose |
| --- | --- |
| `waitlist_signups` | Canonical address, original join time, consent, source and optional enrollment sequence |
| `waitlist_counters` | Optional transactional enrollment counter; never an embedded audience |
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

The `waitlist.*` operation IDs are fixed wire identifiers. Signup `OPTIONS` uses a
dedicated public `waitlist.Preflight` operation returning 204 without JSON
decoding or rate-limit consumption. Optional custom announcement fields and
variants are additive; neutral responses omit them. Private
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

## Host policy and presentation

Hosts can use shared storage and delivery while owning eligibility, copy and
presentation. No cohort limit, discount, commercial offer or application palette
is built into the framework.

```go
audience, err := waitlist.NewMongoStoreWithConfig(database, waitlist.StoreConfig{
    ConsentVersion: "example-consent-v2",
    SequenceEnrollment: true,
})
// Handle err; then call audience.Initialize(ctx) before serving.
contacts, err := waitlist.NewCommsServiceWithConfig(contactRepository, audience,
    waitlist.CommsConfig{ConsentVersion: "example-consent-v2"})
// Handle err and install contacts in the host's contact composition.
```

Both constructors validate consent identifiers (1–64 ASCII letters, digits,
periods, underscores or hyphens, beginning with a letter/digit). Empty selects
the neutral default. Configure both with the same promise; neither rewrites an
existing consent, changes communication identity, or treats configuration as
renewed consent.

Sequenced enrollment is optional and requires Mongo transactions. `Initialize`
creates the unsubscribe lookup and unique partial `waitlist_signup_sequence`
index, then validates/initializes the `waitlist_counters` campaign counter inside
a transaction. A missing counter is initialized to zero only when no positive
sequences exist. A counter inconsistent with the highest assigned sequence fails
closed; it is never repaired automatically. Keep signups and the counter together
in backups and restore them consistently. Do not delete enrollment rows or reset
the counter to reclaim positions.

Each new signup increments the counter and inserts its `waitlist_signups` row in
one transaction. Concurrent retries allocate one position per canonical address;
a failed insert rolls back the increment. Existing rows retain their consent,
time and sequence, including after unsubscribe. Rows without a sequence remain
zero and are not retroactively enrolled. Hosts interpret a positive sequence;
GHATD does not assign eligibility. Initialization and new enrollment fail on an
unsupported standalone Mongo deployment. The default unsequenced store keeps its
existing standalone-compatible behavior.

`RouteConfig.Columns` supplies optional `CSVColumn{Header, Value}` projections.
Nil retains the six standard columns; an empty/invalid list fails before any
routes are registered. Columns are bounded to 32, headers use the same safe
identifier grammar, callbacks are mandatory, and headers must be unique. The
writer applies formula-prefix escaping to every emitted cell, including formulas
preceded by whitespace. Configure callbacks
once; they must be safe for concurrent use and should perform no I/O.

`Announcement.Data` is optional immutable custom copy. Keys use the same safe
identifier grammar; at most 16 fields, 4,000 bytes per value and 8,000 total key/value
bytes are allowed. Control characters are forbidden except LF in values. The HTTP
request also retains its 10,000-byte total limit. Default presentation rejects
nonempty custom data rather than silently discarding it.

An optional `AnnouncementService.Content` implements `AnnouncementContent`:

- `Validate` applies the host's additional copy rules.
- `Preview` produces default HTML and up to 16 named variants/counts.
- `Render` chooses HTML for one stored recipient and its unsubscribe URL.

Hooks are trusted host code, must escape administrator text, and must perform no
I/O or mutate shared application state. Maps and slices passed to hooks are
independent copies. Preview content is validated before persistence, custom data
is stored with the frozen campaign, and caller/response edits cannot change that
command. Variant keys must be unique safe identifiers, labels are bounded plain
text, counts must be between zero and the current audience size, and each HTML
body must be nonblank and at most 1 MiB. Variants may overlap; hosts own their
segmentation semantics. The shared service owns total recipient counts.

Recipient rendering happens before the durable send claim. Failure does not
spend that recipient's claim or call the provider, and never falls back to another
template. A campaign may already be frozen, and earlier recipients in the batch
may already have been accepted: an error is not a rollback. Existing no-resend,
suppression and uncertain-provider rules remain in force. Custom payloads are
revalidated after loading; a changed host renderer must remain compatible with
its saved commands or require a separately reviewed transition.
