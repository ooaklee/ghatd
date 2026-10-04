# Browser token-allowance approval

`adminaccess` provides an **opt-in, token-allowance-only** browser adapter for
[accesspolicymanager](../../accesspolicymanager/README.md). It privately verifies
an existing HttpOnly access cookie through the real bearer-session middleware.
No JWT is returned to JavaScript, no general administrator role is granted, and
the original bearer-only policy API remains unchanged.

This is not a generic step-up framework or a general permissions-management API.
Email confirmation is purpose-bound approval, not independent MFA: compromise
of both the initiating browser session and its verified mailbox is outside this
assurance boundary.

## Host composition

```go
store, err := adminaccess.NewRedisStore(redisClient, component+":"+environment)
if err != nil {
    return err
}
bridge, err := adminaccess.New(adminaccess.Config{
    Origin: frontendOrigin, System: system, Environment: environment,
    CookieName: accessCookieName, Window: 5 * time.Minute,
    Store: store, Manager: tokenPolicyManager, Authorize: liveAuthorizer,
    BearerSession: middlewareSuite.BearerSession, Email: emailService,
})
if err != nil {
    return err
}
return bridge.Attach(routes)
```

All values are trusted host composition, not HTTP parameters. Supply the same
system and live authorization stack used by the shared policy manager. The Redis
client is already managed by the host; this package does not own or close it.
Its namespace isolates deployment/purpose and is not a user-defined grant scope.
The email port uses the existing email manager/provider; it opens no live mailbox.

Construction checks mandatory dependencies, an exact HTTPS origin (loopback HTTP
only in development), an access-cookie name and a review window of one to five
minutes. Hosts control whether routes are attached at all. An unconfigured bridge
must remain unavailable; do not install no-op middleware as a production fallback.

## Flow and stable transport

All commands are under `/api/v1/admin-access`:

| Method/path | Purpose |
| --- | --- |
| `POST /session` | Issue a page-scoped context; no elevation or session renewal |
| `POST /users/{userID}/token-limits/preview` | Persist the exact manager-produced proposal |
| `POST /challenge` | Email a purpose-specific confirmation code to the operator |
| `POST /confirm` | Approve only the current review, without extending its expiry |
| `PUT /users/{userID}/token-limits` | Consume the review, then delegate one CAS write |
| `POST /cancel` | End the review without signing out or claiming rollback |

Every request requires exact `Origin`, `Sec-Fetch-Site: same-origin`,
`Sec-Fetch-Dest: empty`, `X-Admin-Access: 1`, one Cookie header and exactly one
configured access cookie. The browser must not supply `Authorization` or
`X-Api-Token`. Queries, navigation requests, CORS preflights, duplicate security
headers/cookies and cross-origin requests are rejected. Session issuance,
challenge and cancellation have no body; confirm uses strict JSON
`{"code":"<12-uppercase-hex-characters>"}`.

`X-Admin-Context` binds the page; `X-Admin-Review` selects an immutable proposal.
Preview returns those review details and the shared ETag plus `X-Admin-Expires`
and `X-Admin-Remaining-Ms`. Apply must send the same target, limits, review and
`If-Match` revision. Shared manager codecs and reply/error manifests remain the
authority for payloads, revision checks and policy outcomes.

The binding includes system, environment, exact origin, actor, access-session ID,
verified email and email revision. It is rechecked against live account/session
authority on each request. Account switches, revocation, demotion, email changes,
changed proposals, expired reviews and replay fail closed.

## State, throttling and uncertain outcomes

- Page contexts last at most 15 minutes, with ten issuances per actor per window.
- Reviews use Redis time, expire within the configured one-to-five-minute window
  and cannot outlive their page context. Re-preview invalidates an older proof.
- Email challenges have a 60-second actor-wide cooldown and ten-per-hour ceiling.
  Five incorrect code attempts consume the review. Codes are random 48-bit values
  hashed with the review ID; login and OAuth proofs cannot authorize this action.
- One approved review permits at most one dispatch. Redis atomically consumes
  it **before** calling the policy manager. Redis consumption and Mongo CAS are
  not a distributed transaction: a crash or lost response can leave a spent
  review and an unknown write outcome. Never restore approval, automatically
  retry the write or delete the manager's token-inventory fence.
- Reread authoritative policy state after uncertainty. Cancellation is not
  rollback and cannot retract a dispatched mutation.
- Safe operational events include actor/target/system, review digest and expected
  revision. Raw handles, codes, cookies, JWTs, addresses and email bodies are never
  included. The shared manager retains its durable grant audit.

Stable local errors are `ADA0-001` (428 review required), `ADA0-002` (422 invalid
proof), `ADA0-003` (429 cooldown) and `ADA0-004` (503 unavailable). Existing shared
authentication and policy errors retain their own manifests. The bridge creates
neither a browser UI nor broader permission/scopes grant-management endpoints.

## Verification

```sh
go test -race ./external/accesspolicy/adminaccess -count=1
```

For signed-session, Redis and Mongo CAS integration, provide isolated
`GHATD_TEST_MONGO_URI` (a replica set for policy transactions) and
`GHATD_TEST_REDIS_ADDR`. Tests use generated database/key namespaces and synthetic
email capture; no mailbox or live send is required. Without both, integration
tests skip explicitly.

Test-style audit: `http_test.go`, `config_test.go`, `store_integration_test.go`
and `integration_test.go` are table-driven across codecs, configuration, exact
route contracts, state transitions, browser provenance, revocation and one-use
concurrent apply. Each stateful approval case owns an isolated fixture.
