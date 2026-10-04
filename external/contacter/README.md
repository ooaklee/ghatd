# Communications

`contacter` manages communication records, configured communication types and
aggregate statistics. Its handler uses the package error manifest for failures.

Optional [conversation ownership and voting](../usermanager/README.md#private-conversation-voting)
add private session-bound feedback without changing immutable history. The
[waitlist integration](../waitlist/README.md) adds prerelease consent/enrollment
while keeping ordinary contact behavior intact.

## HTTP routes

`AttachRoutes` registers these exact routes through the shared route registry:

| Path | Methods | Access |
| --- | --- | --- |
| `/api/v1/comms/types` | GET, OPTIONS | Public capability labels and accepted values. |
| `/api/v1/comms/stats` | GET, OPTIONS | Administrator middleware required. |

Supply `AdminOnlyMiddleware` when attaching routes. `AdminAccess` defaults to
`router.AdminSession`; use `router.AdminSessionOrAPI` for an administrator
API-token-or-session adapter. Other modes are rejected. Metadata describes the
adapter; it does not authenticate callers or replace the middleware.

Configure any policy authorizer before attachment. Check
`Router.ValidateRoutePolicies()` after **all** route registration and refuse
startup on error. Missing admin middleware or an invalid mode invalidates the
registry. Its runtime backstop then returns `ROUTE_CONFIGURATION` (503) on every
descriptor, including public type discovery. With valid configuration, type
discovery does not run admin middleware; a configured policy authorizer still
receives its public descriptor. Existing methods and paths are unchanged.

See the [router guide](../router/README.md#declarative-route-policies) for
registration order, inventory and policy enforcement. Record creation, delivery
and user-facing communication workflows remain service/manager responsibilities;
these two routes do not expose arbitrary communication records publicly.

## Conversations and email integration hooks

An existing `Comms` record is the conversation root. `Service.AppendCommsEntry`
adds an immutable, attributed `internal_note` or `reply`; several administrators
can contribute without replacing one another's messages. A reply **records**
correspondence: it does not send mail or prove delivery. Corrections and later
replies are new entries, optionally linked by `parent_entry_id` within the same
conversation. This first version has no editing, deletion, draft or outbox API.

Entries use a separate `comms_entries` collection, avoiding an unbounded array
inside the original contact. `Service.ListCommsConversation` returns:

- `legacy`: the original contact, including existing single-value `AdminReply`
  and `AdminNotes` snapshots. Their history, authors and dates are not invented.
- `entries`: a bounded page of immutable notes, recorded replies and imported
  emails, each with an actor, body, identifier and server recording time.
- `next_cursor`: an opaque next-page cursor when another page was observed.

Legacy updates leave the entry collection intact. New entries do **not** update
the old `AdminReply`, `AdminNotes`, `ReachedOutAt` or aggregate statistics. Old
clients therefore continue to see their original snapshot/statistics semantics;
use the conversation API for new history, not legacy counters. Root existence
and insertion are not transactional: a concurrent privileged legacy deletion
can leave retained entries. Reads and appends through the service then return
not-found. Hosts own retention/deletion policy; there is no automatic cascade.

### Administrative HTTP API

The standard `usermanager` attachment adds these routes:

| Path | Method | Access |
| --- | --- | --- |
| `/api/v1/ums/comms/{id}/conversation` | GET | Administrator session and live manager authorization |
| `/api/v1/ums/comms/{id}/conversation` | POST | Administrator session and live manager authorization |

Both use `Cache-Control: no-store`. Internal notes and email metadata are private;
the routes are not a user inbox or an API-token ingress. The manager reuses the
shared `AdministratorAuthorizer` to recheck current authority, not just claims
copied from a token. Standard starter composition wires it. Custom composition
must supply it with `WithAdministratorAuthorizer`; missing optional capabilities
fail closed with 503. Route middleware must still be configured and validated.

For POST, submit a canonical UUIDv4 `request_id`, `kind` (`internal_note` or
`reply`), `body`, and optionally `parent_entry_id`. The path supplies the contact
ID and verified context supplies the actor. Body-supplied identities, email
imports, unknown fields and trailing JSON are rejected.

```json
{
  "request_id": "<canonical-uuid-v4>",
  "kind": "internal_note",
  "body": "I am checking the account details before our next reply."
}
```

A successful append returns 200 with `data.entry` and `data.replayed`. Keep the
same request ID and content when retrying: exact replay returns the original
entry, timestamp and attribution. Different content under the same contact,
actor and request ID returns 409; use a new ID for a new reply/correction. Different
administrators have independent request namespaces. A best-effort audit event
records entry identity/kind, not message text. Audit failure does not turn an
acknowledged append into failure, and exact replay does not repeat that event.

GET accepts `limit` (1–100, default 25) and the prior `next_cursor` as `cursor`.
The order is descending `(recorded_at, id)` at MongoDB millisecond precision.
Cursors are contact-bound and malformed/cross-contact values return 400. This is
a deterministic keyset walk, **not** a snapshot across concurrent late inserts;
refresh from the first page to discover new activity. No total-count scan occurs.

The existing `POST /api/v1/ums/comms` creation contract remains 201 with a direct
`Comms` receipt. An explicit public projection excludes admin notes/replies,
linked-contact IDs and reached-out metadata even if an adapter returns them.
It does not return entries or conversation pages. Submission `meta` is retained;
adapters must never put private history in that host-defined metadata. Never add
internal notes to customer-facing notifications or mail projections.

### Trusted email ingestion

`Service.ImportCommsEmail(ctx, *ImportCommsEmailRequest)` is a separate in-process
hook for a future provider adapter, not an HTTP/webhook endpoint. The adapter
must authenticate the mailbox/webhook, authorize ingestion and explicitly
select an existing contact and trusted ingestion actor **before** invoking it.
Calling a lower domain method with an arbitrary actor ID is not authentication.
If a context publishes authentication/user evidence, it must match that actor.

Use `email_inbound` or `email_outbound` and populate `CommsEntryEmailMetadata`:

- Verified `Provider`, `Mailbox`, `ProviderMessageID` form the deduplication
  identity. A provider message cannot silently move to another contact.
- `ThreadID`, RFC `MessageID`, `InReplyTo` and `References` preserve threading
  context. These sender-controlled headers never grant access, deduplicate mail
  or automatically merge contacts. Provider adapters own safe thread selection.
- `From`, `To`, `Subject` and `OccurredAt` preserve display metadata. Addresses
  do not verify user ownership. Server `RecordedAt` is separate ingestion time.

Exact provider redelivery returns the original entry even if a different worker
performs ingestion. Changed content, direction, metadata, parent or destination
under the same provider identity conflicts; reconcile explicitly rather than
silently rewriting history. RFC header reuse on different provider messages does
not collapse those messages. Imported outbound mail is not a delivery receipt.

Bodies are nonblank UTF-8 plain text, up to 64 KiB; render as text, never raw HTML.
Metadata is bounded, CR/LF/NUL header values are rejected, `To` supports up to 50
recipients and `References` up to 64 values. The hook does not parse MIME, fetch
attachments, send mail, verify webhook signatures or connect a live mailbox.

### Storage and migration

The optional `ConversationRepository` port preserves existing repository/service
interfaces. The standard Mongo repository implements it using shared repository
helpers, including their logging/error handling. No duplicate storage stack is
required. A built-in unique `_id` index atomically deduplicates namespaced hashes
of the admin request or verified provider identity. Exact duplicate errors are
reconciled against immutable content; uncertain/mixed/wrapped driver failures
remain native, and writes are never automatically retried by this layer.

Run `EnsureCommsConversationIndexes(ctx, db)` as an explicit host migration to
create the `(comms_id, recorded_at desc, _id desc)` paging index. Normal reads and
writes never create it. It improves performance; deduplication correctness already
uses MongoDB's `_id` index. Existing contact documents require no rewrite.

Validate with `go test -race ./external/contacter ./external/usermanager`. Set
`GHATD_TEST_MONGO_URI` to a disposable MongoDB server for the conversation Mongo
tables; each case creates and removes its own uniquely named database. Without
that variable those integration cases explicitly skip. Unit tables cover native
errors and ambiguous acknowledgements separately from real concurrency tests.

## Contact voting

`Service` owns contact/entry admission and delegates generic vote operations to
the injected [voter service](../voter/README.md). There is no contact-specific
vote store or user lookup in this layer:

```go
contactService := contacter.NewService(contactRepository).
    WithVoterService(voterService)
userManager.WithCommsVotingService(contactService)
```

Configure dependencies before serving. `NewService` remains usable for ordinary
contact CRUD without a voter. Voting requires the optional `VotingRepository`
capability (`FindCommsEntry` and bounded `FindCommsEntries`) on that same contact
repository; absent/typed-nil capabilities fail closed when voting is called.
The standard repository implements both. No automatic fallback or index
migration occurs.

`GetCommsVotes`, `SetCommsVote` and `RemoveCommsVote` take explicit manager-supplied
`ActorID` values. These IDs are not credentials: the manager must authorize the
operation first. Contradictory verified context remains rejected. Parent
existence and every requested entry's membership are checked before shared voting;
unknown targets never turn into authoritative zero counts.

`CommsVoteResult` contains aggregate counts and the viewer's own vote, never the
voter inventory. Read results additionally contain `EntryAuthors`, with exactly
one reference per requested validated entry (at most 100). Empty stored authors
remain empty; the original sender is not added. This trusted service metadata is
excluded from JSON and is not independent proof of authorship. Mutations return
no author references. User Manager alone resolves users and builds the private
participant response; it does not query the contact repository again.

Votes use the `contacter` domain discriminator and shared `votes`
collection. Contact admission, voting writes and result reads are not one
transaction; a failed post-write read does not prove rollback. Run the explicit
voter index migration before enabling the feature.

Custom lower-domain adapters use `GetCommsVotesRequest`,
`ChangeCommsVoteRequest` and `CommsVoteResult`; they do not resolve users or
construct HTTP participants. HTTP composition, owner guards and participant
models are documented in [User Manager's service contracts](../usermanager/README.md#conversation-service-contracts).

Voting test-style audit: `service.voting_test.go` uses named delegation,
capability, membership and receipt tables with driver-free probes. Signed-session
and persistence lifecycles live in User Manager's integration tests.
