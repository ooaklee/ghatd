# Conversation ownership and voting

Import `github.com/ooaklee/ghatd/external/contacter/conversation` (Go package name
`commsconversation`) for opt-in session-owner protection and private feedback.
The parent [contacter package](../README.md) owns immutable entries; the user
manager owns native history and append handlers. This adapter does not replace
authentication, route authorization or live administrator checks.

## Composition

After registering the native user-manager routes and installing the host's route
policy authorizer, and **before serving requests**:

```go
err := commsconversation.AttachVoting(routes, &commsconversation.Voting{
    Database: database, Contacts: contactRepository,
    Users: userService, Authority: accessManager,
}, adminSessionMiddleware)
if err != nil {
    return err
}
if err := commsconversation.RequireRoutes(routes); err != nil {
    return err
}
```

Run `EnsureVotingIndexes(ctx, database)` from an explicit host migration. It
maintains `idx_comms_entry_votes_target` on `comms_entry_votes` with
`comms_id, entry_id` keys. Keep one migration registration; no new implicit
registration or startup data conversion is introduced.

`RequireRoutes` demands exactly one native read/append route and one protected
metadata update, with their expected paths, separate methods, operations and
admin-session policies. It wraps the existing POST/PUT leaves rather than
registering competing handlers. A changed or missing contract fails startup.
Repeated owner wrapping is idempotent. `Attach` is the lower-level variant which
allows no conversation routes; use `RequireRoutes` when exposing the feature.

## Session ownership

Mutations supply `X-Comms-Expected-Owner` with the account ID captured when the
editor opened. It is a precondition, **not authentication**. The shared signed
session remains the authority; a tab opened by one account cannot mutate after
the browser switches to another account. Missing, duplicate, comma-combined,
oversized and malformed values are rejected before reading the command body.
Native route authorization and live authority checks still execute afterward.

The precondition protects native conversation append and metadata updates, plus
the vote set/remove endpoints. Error codes remain stable:

| Code | Status |
| --- | --- |
| `HOST_COMMS_OWNER_REQUIRED` | 428 |
| `HOST_COMMS_OWNER_INVALID` | 400 |
| `HOST_COMMS_OWNER_CHANGED` | 412 |
| `HOST_COMMS_OWNER_SESSION_REQUIRED` | 401 |
| `HOST_COMMS_VOTE_INVALID` | 400 |
| `HOST_COMMS_VOTE_UNAVAILABLE` | 503 |

The `HOST_` prefix is retained as a wire contract; it does not describe a separate
permission system. Private responses are non-cacheable and dependency diagnostics
are canonicalized through shared error manifests.

## Private voting contract

All routes use administrator sessions, not API-token admission:

- `GET /api/v1/ums/comms/{id}/conversation/votes`: original-contact summary plus
  at most 100 explicitly requested `entry_id` values.
- `POST` or `DELETE /api/v1/ums/comms/{id}/vote`: original-contact feedback.
- `POST` or `DELETE /api/v1/ums/comms/{id}/conversation/{entryId}/vote`: entry feedback.

Operations remain `commsconversation.ReadVotes`, `SetVote` and `RemoveVote`.
POST accepts `{"vote":1}` (positive) or `{"vote":0}` (negative). An absent viewer
vote is null, never a fabricated negative. Unknown targets or cross-contact entry
IDs fail the whole request. Stored malformed votes fail unavailable, not zero.
Actor identity is bound from live authority, never decoded from the request.

Each actor/contact/entry tuple has one deterministic vote ID. Majority writes
keep repeat set/remove idempotent. Votes live outside the immutable conversation
history. Participant labels contain only known IDs, short IDs and bounded names;
no email addresses or role inventory are returned. Label lookup failures omit
labels without inventing identities or changing the vote result.

## Verification

```sh
go test -race ./external/contacter/conversation -count=1
```

Set `GHATD_TEST_MONGO_URI` and `GHATD_TEST_REDIS_ADDR` to isolated test services for
real signed-session/persistence checks. Tests clean up only generated databases
and account/session keys. They never connect to live email providers.

Test-style audit: `owner_test.go` uses named denial/route-shape tables with focused
composition checks; `compatibility_test.go` uses method/error-code tables.
`owner_integration_test.go` is an ordered account-switch/replay/demotion lifecycle.
`votes_integration_test.go` combines named input/target tables with an ordered
set/switch/remove/concurrent-voter lifecycle. Stateful exceptions preserve the
history being asserted rather than manufacturing one-row tables.
