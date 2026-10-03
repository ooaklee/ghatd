# Reuse authenticated session context

Access-manager middleware publishes identity through
`external/accessmanager/helpers`. A non-empty `AcquireFrom(ctx)` value alone
does not prove authentication: optional-auth routes can carry placeholder IDs.
Use `AcquireAuthenticatedUserIDFrom(ctx)` or check `AcquireAuthenticatedFrom(ctx)`
before an identity drives a user lookup or permission decision.

## Verified session metadata

`AcquireSessionFrom(ctx)` returns a defensive copy of verified access-token
metadata only when its user ID matches an explicitly authenticated context.
Anonymous, API-token, legacy middleware without metadata, and inconsistent
contexts return nil. Legacy JWTs can have zero authentication time or no audience;
do not upgrade these into recent or audience-bound sessions.

Metadata includes access-session ID, signed authentication time, signed audience,
issuer, user type, token purpose, signing algorithm, email revision and the existing authorization claims. It
contains no raw bearer. The access-manager response excludes this field from
JSON. Treat metadata as sensitive request context, not response/log content.

Context snapshots do not prove that a session remains live or that a user owns
a particular resource. A host must apply its current account, resource and
audience policies and recheck live authority before consequential work or replay.

## Explicit credential selection

For a host that cannot use cookie-refresh middleware, call:

```go
result, err := accessService.AuthenticateSession(ctx, credential)
if err != nil {
    return err
}
trusted, err := middleware.ContextWithAuthentication(ctx, result)
if err != nil {
    return err
}
actorID := accessmanagerhelpers.AcquireAuthenticatedUserIDFrom(trusted)
session := accessmanagerhelpers.AcquireSessionFrom(trusted)
```

Here `middleware` refers to `external/accessmanager/middleware`, and
`accessmanagerhelpers` to `external/accessmanager/helpers`.

`AuthenticateSession` accepts one already selected bearer. It verifies signature,
expiry, live session presence and stored identity, current user identity, and
email revision and persisted user type. It does not read HTTP headers, choose among competing cookies,
refresh a token, downgrade to anonymous, or assert that the account is ACTIVE or
verified. Those are explicit host policies. Never accept a user ID or session
metadata supplied in a request body as a substitute.

The ordinary, active and administrator service guards now use the same
live-session verification. Active guards additionally require current ACTIVE
status; administrator guards additionally require the current stored admin role.
The authenticated branch of the optional service guard also checks live session
ownership. Missing session records and wrong owners are rejected; dependency
failures remain operational errors. See [live session authority](../../external/accessmanager/README.md#live-session-authority)
for cookie-refresh and logout limitations: access-record revocation is not
refresh-token-family revocation.

`ContextWithAuthentication` is the common context publisher used by preloaded
middleware and custom adapters. It checks consistency, clears inherited context
on failure, and defensively copies audience values. Preloaded middleware stops
before calling the next handler when a service returns inconsistent identity.

For compatibility, this publisher accepts trusted legacy results without
credential metadata; it is not itself a verifier. The declarative route-policy
guard rejects credential-less identities for protected routes, and the explicit
bearer adapter requires a matching session ID. Never build a trusted result from
client-provided identity fields or assume an authenticated legacy context carries
session or delegation evidence.

## API credential context and route requirements

`AcquireAPITokenFrom(ctx)` returns a defensive credential-ID/owner-ID snapshot
only for an explicitly authenticated, matching owner without competing JWT
metadata. It contains no secret or digest. API grants must select the credential
ID, not silently inherit all grants assigned to the owning user.

Current metadata is a request snapshot, not a durable capability. See the
[route-policy guard](../../external/accessmanager/middleware/README.md#route-policy-guard)
for current account checks, live requirements and admission contracts, and the
[API-token migration notes](../../external/apitoken/README.md#repository-and-adapter-migration)
for custom verifiers and repository changes. The policy guard is not installed
automatically; persistent grants and host adoption remain separate work.

## Validation

```sh
go test ./...
go test -race ./external/accessmanager/... ./external/auth/...
```

Tests cover HTTP/credential verification parity, revocation, email-revision and
identity mismatch, anonymous/legacy contexts, defensive copies, malformed
audiences and the distinction between login time and token issuance time.
