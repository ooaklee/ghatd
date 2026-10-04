# Vision

Vision stores product feedback and bug reports, then promotes selected items
into roadmap work by assigning a status.

The package deliberately keeps user data at arm's length:

- vision records and comment authors store raw user UUIDs; voter identities
  remain private to the shared vote repository
- comment messages are stored verbatim and may contain tokens such as
  `<@USER_NANO_ID>`
- replies use an optional `parent_comment_id` that must refer to a comment on
  the same vision item
- `external/usermanager` converts those records into privacy-safe,
  NanoID-based user-facing responses

Each Vision has an internal UUID for persistence and a public NanoID for API
requests and shareable links. HTTP routes accept only the public NanoID.

## ActorID migration

All nine mutation commands now carry an explicit `ActorID`. For create, update,
status, vote and comment commands it replaces request fields previously named
`CreatedByUserID`, `UpdatedByUserID` or `UserID`. Deletion also requires `ActorID`.
Update typed callers when adopting this breaking Go API change. Stored authorship,
comment user IDs remain unchanged by ActorID naming. The separate shared-vote
storage change below requires new composition and an index migration.

HTTP mappers bind actors only from explicitly authenticated context. Anonymous
bookkeeping IDs and ID-only context are insufficient. Actors have `json:"-"` and
no query/path tag, while Vision NanoIDs and comment IDs remain route-owned.
Authentication adapters must verify credentials before publishing context; see
[request identity](../../docs/how-to/request-identity.md).

The lower Vision service rejects missing actors and contradictory published
identity, including mismatched cached users. Bare-context in-process calls are
trusted integrations: callers must authenticate and authorize their workflow.
An actor ID is not a permission. Existing administrator routes and User Manager's
owner/administrator checks remain authoritative. User Manager update/delete
requests must agree with the verified caller; descriptive edits copy the request
before excluding internal metadata rather than modifying the caller's input.
User Manager update/delete do not accept bare-context calls; use an authorized
lower-domain workflow for trusted background operations. Identity comparisons are
exact: padded actor IDs are rejected rather than normalized into another identity.

Mutation entry guards reject nil requests/context, cancellation and missing
dependencies. Invalid adapter receipts and missing/inconsistent selected-record
results return `ErrVisionUnavailable` (`VIS0-018`, 503); native dependency errors
retain their identity for shared reply maps and host overrides. Existing
`VIS0-006` authentication failures and other wire codes are unchanged.

Scalar records are copied before configuration or mutation, so a failed update
cannot change an adapter's shared scalar snapshot. Summary enrichment copies
list records and comment slices before attaching viewer-specific state. Nested
metadata remains read-only; custom adapters must not mutate shared nested values.
This does not add transactions, revision
checks or matched-count write receipts. Concurrent owner/lifecycle changes and
uncertain write outcomes still require separate persistence handling; do not
automatically retry a write merely because its confirmation failed.

For explicit Mongo integration verification, set `GHATD_TEST_MONGO_URI` to a
disposable test server. The lifecycle test uses a unique database and drops only
that database during cleanup. Without that setting it attempts an embedded Mongo
process and may skip if one cannot start. A configured but unreachable URI fails.

## Model

`Vision.Type` supports `bugs` and `feedback`. New items have an empty status.
An item becomes a roadmap item once it receives a configured, non-empty status.
The default workflow is:

```text
feedback/bug -> UNDER_REVIEW -> PLANNING -> PLANNED -> IN_PROGRESS -> COMPLETE
                                    \-------> REJECTED
```

Host applications can replace this workflow with `VisionConfig`.

Vote values remain `VisionVoteDownvote` (0) and `VisionVoteUpvote` (1).
Setting a vote atomically replaces the actor's direction through the shared
[Voter service](../voter/README.md). Downvoting can be disabled through Vision
configuration; comment votes use the same mechanics. Vision validates target
existence and comment membership before calling the lower service.

**Breaking composition:** use `vision.NewService(visionRepository, voterService,
visionConfig)`. `starter/v0` supplies that shared service automatically. Custom
Vision repositories no longer implement vote methods. `Vision.Voters` and
`VisionComment.Voters` are replaced by transient `VoteSummary` fields; raw Vision
JSON now has count/viewer summaries in `votes`, not user-ID arrays in `voters`.
`VoteViewerID` is never serialized and protects UMS projections from reusing a
different viewer's summary. User Manager's existing HTTP projections are unchanged.

## Routes

Raw vision routes use `/api/v1/visions`.

Admin-only management routes:

- `GET /api/v1/visions/config`
- `PATCH /api/v1/visions/{visionNanoID}`
- `PATCH /api/v1/visions/{visionNanoID}/status`
- `DELETE /api/v1/visions/{visionNanoID}`

Authenticated interaction routes:

- `POST /api/v1/visions`
- `GET /api/v1/visions`
- `GET /api/v1/visions/{visionNanoID}`
- `PUT /api/v1/visions/{visionNanoID}/votes`
- `DELETE /api/v1/visions/{visionNanoID}/votes`
- `POST /api/v1/visions/{visionNanoID}/comments`
- `PUT /api/v1/visions/{visionNanoID}/comments/{commentID}/votes`
- `DELETE /api/v1/visions/{visionNanoID}/comments/{commentID}/votes`

The usermanager package exposes privacy-safe views under `/api/v1/ums`:

- `GET /api/v1/ums/visions`
- `GET /api/v1/ums/visions/{visionNanoID}`

These two read routes use optional authentication. Guests receive the same
feedback and discussion content as signed-in users, with aggregate vote counts
and public user NanoIDs. Signed-in users additionally receive `viewer_vote`
when they have voted. Raw user UUIDs, voter buckets, and internal metadata are
never included in UMS responses.

All UMS mutation routes remain authenticated. `{visionNanoID}` is the public
Vision NanoID used for reads, shareable links, and subsequent vote or comment
requests. Internal UUIDs are not accepted by HTTP routes.

## Persistence

Comments and replies remain embedded in the Vision document. All parent/comment
votes now live in the shared `votes` collection using domain `vision`, the internal
Vision UUID and an optional comment ID. Summaries are attached after reads in
batches of at most 200 targets and are never written into parent documents.
List queries exclude comment bodies; the detail endpoint returns the full
discussion. The internal UUID is `_id`; the public NanoID is `_nano_id`.

Apply `voter.EnsureIndexes(ctx, database)` in an explicit migration before using
the new vote store. There is no compatibility fallback, data backfill or automatic
deletion of old voter fields; this upgrade targets installations with no existing
votes. See [adoption and lifecycle boundaries](../voter/README.md).

Vote writes no longer update the parent's descriptive timestamps or editor ID.
Authorization/target checks, the vote mutation and summary reads are not one
transaction. Deleting a Vision denies subsequent domain access but does not
cascade-delete vote rows; do not reuse resource IDs. Retention/erasure workflows
must coordinate cleanup separately, including concurrent writers.

Host applications should register `migrations.InitVisionIndexesUp` and
`migrations.InitVisionIndexesDown` from their `migrations/mongo` package, then
apply the registration with:

```sh
asdf exec go run main.go mongo-migrator up
```

The host adapter must blank-import its migration package. The shared `down`
action reverts all applied registered migrations, not only Vision indexes. See
[Managing MongoDB Migrations](../../docs/how-to/manage-mongodb-migrations.md)
for registration adapters, configuration, and rollback precautions.
