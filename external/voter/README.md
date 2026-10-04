# Voter

`external/voter` is a reusable lower-domain package for one up/down vote per
actor and target. It follows the blueprint service/repository separation without
adding handlers, routes or fenders that its consumers do not need.

## Composition

```go
voteRepository := voter.NewRepository(mongoRepository)
voterService := voter.NewService(voteRepository)

visionService, err := vision.NewService(visionRepository, voterService, visionConfig)
if err != nil {
    return err
}
contactService := contacter.NewService(contactRepository).
    WithVoterService(voterService)
userManager.WithAdministratorAuthorizer(accessManager)
userManager.WithCommsVotingService(contactService)
```

`mongoRepository` is the existing managed `repository.MongoDbRepository`.
Its client, connection lifetime and configuration remain host-owned.
`starter/v0` creates `Repositories.Voter` and `Services.Voter` and injects the
latter into Vision and the existing contact service. User Manager orchestrates
contact voting and participant enrichment through those service ports. Enable
the private UMS voting routes using `EnableCommsVoting` on the starter route
request (or `usermanager.AttachRoutesRequest` for manual composition).
Custom persistence adapters implement the driver-free `VoteRepository` port;
there is no silent in-memory fallback.

Before enabling either consumer, call `voter.EnsureIndexes(ctx, database)` from
an explicit host migration against the same database. It creates the unique
`idx_votes_target_actor` index on `(scope, domain, resource_id, child_id, actor_id)`.
The leading fields also support target-summary queries. Index creation is
idempotent for a matching definition; conflicting definitions/errors remain
visible. There are no implicit migrations, collection drops or index repairs.

## Responsibilities and identity

- The consuming service owns authentication, authorization, target existence,
  child membership and whether downvotes are allowed. A zero count is not proof
  that a target exists.
- `Service` validates actors, directions, targets and adapter results. `ActorID`
  cannot be decoded from JSON. Published verified/cache identity must agree;
  a bare-context call is trusted composition, not authentication.
- `Repository` owns Mongo schemas, deterministic keys, atomic writes, receipts
  and aggregate queries. It uses GHATD's managed repository helpers and
  metadata-only telemetry; datastore types stay out of service requests/ports.

`Target` separates the server-selected `Domain`, internal `ResourceID`, optional
`ChildID`, and optional tenant/partition `Scope`. The built-in consumers use
`vision` and `contacter`, with empty scope and empty child for parent votes.
Do not let a client select a domain/scope to bypass domain checks. Scope is not a
host branding setting and does not establish tenant authorization. Identity
values are exact, valid UTF-8, at most 256 bytes and contain no surrounding
whitespace or control characters. Do not reuse deleted resource IDs.

## Commands and summaries

- `SetVote`: `Up` is 1 and `Down` is 0. One atomic upsert replaces the actor's
  direction; repeated acknowledged assignments do not increase the count.
- `RemoveVote`: deletes only that actor/target tuple; already absent is success.
- `GetSummaries`: accepts 1–200 distinct targets. It returns an entry for each,
  with `Up`, `Down` and the selected actor's optional `ViewerVote`. A nil vote is
  different from a downvote. Empty actor requests totals only, even with an
  authenticated context. No actor lists are returned.

The shared `votes` collection holds one document per tuple. `_id` is a SHA-256
digest of a JSON tuple, avoiding ambiguous delimiter concatenation; it is not a
credential or anonymization. Records also contain the tuple, direction and an
`updated_at` timestamp. The natural-key index prevents duplicate tuples even
when another writer supplies a different document ID.

Counts and viewer state come from one aggregation, not loaded voter arrays.
Malformed stored directions or inconsistent adapter receipts fail closed.
Writes use majority acknowledgement; native driver failures retain their error
identity, and uncertain outcomes are not retried by the package. Standard driver
retry behavior remains controlled by the managed Mongo client.

`ErrorMap` provides `VOTER-001` (400, invalid input), `VOTER-002` (401, invalid
actor), and `VOTER-003` (503, unavailable). Native dependency failures continue
through shared error manifests and host overrides. Consumers compose this map;
User Manager's conversation handlers retain the `HOST_COMMS_VOTE_*` wire codes.

## Consistency and lifecycle boundaries

Parent validation, vote writes and subsequent summary reads are separate
operations. An authorization/deletion race is not fenced by a transaction;
successful writes followed by failed reads may still have persisted. Counts may
reflect concurrent votes. Never infer rollback from an error or retry blindly.

Votes no longer change a Vision's descriptive `UpdatedAt`/`UpdatedByUserID`.
Deleting a parent currently makes its votes inaccessible through the consumers,
but does not cascade-delete the stored vote rows. Hosts needing physical erasure
must coordinate retention/cleanup with parent deletion and concurrent writers;
this package does not claim an atomic cascade or provide bulk-erasure authority.

## Upgrading existing Vision voting

This is a breaking Go/storage change intended for deployments without existing
votes. Vision and contact voting both read/write only `votes`. There is no
fallback to embedded Vision voter arrays, dual write or automatic backfill.
Existing Vision voter fields are not deleted automatically. Deployments with
stored Vision votes must plan an explicit migration before upgrading.

Replace `vision.NewService(repo, config)` with
`vision.NewService(repo, voterService, config)`. Custom Vision repositories no
longer implement vote mutations. Vision's raw model replaces `Voters` with a
transient `VoteSummary`; UMS vote counts and viewer-vote JSON remain unchanged.

## Contact voting integration

Compose `contacter.Service.WithVoterService` and
`usermanager.Service.WithCommsVotingService` with the same voter service used by
Vision. Custom contact repositories implement `FindCommsEntry` and the bounded
`FindCommsEntries` identity-only membership lookup. User Manager owns HTTP
admission, live authority and participant enrichment; the contact service
validates targets and delegates generic vote operations. See the
[conversation service contracts](../usermanager/README.md#conversation-service-contracts)
for route opt-in and owner preconditions. Use this package's `EnsureIndexes` and
`Collection` for shared vote storage; there is no contact-specific vote store.

## Verification

```sh
go test -race ./external/voter/... ./external/vision/... ./external/contacter/... ./external/usermanager/...
```

Set `GHATD_TEST_MONGO_URI` to a disposable Mongo server to run real storage tests;
conversation signed-session tests also need `GHATD_TEST_REDIS_ADDR`. Tests use
unique databases and clean up only their own data. Missing integration services
can cause skips; unit success alone does not verify persistence.

New tests use named tables for identity/validation boundaries, adapter failures,
receipts, tenant/domain/child isolation, lifecycle/replay, concurrent actors and
malformed stored votes. Consumers separately test their admission and projection
contracts; storage tests do not substitute for authorization tests.
