# Browser policy approval

`adminaccess` provides two **opt-in, purpose-separated** browser adapters for
[accesspolicymanager](../../accesspolicymanager/README.md). It privately verifies
an existing HttpOnly access cookie through the real bearer-session middleware.
No JWT is returned to JavaScript, no general administrator role is granted, and
the original bearer-only policy API remains unchanged.

The default token adapter remains unchanged. A separately configured capability
adapter supports explicit grant review and replacement through the owning manager.
Neither adapter is a generic step-up framework.
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
authentication and policy errors retain their own manifests. The bridge creates no browser UI, administrator account, implicit grant or token.

## Verification

```sh
go test -race ./external/accesspolicy/adminaccess -count=1
```

For signed-session, Redis and Mongo CAS integration, provide isolated
`GHATD_TEST_MONGO_URI` (a replica set for policy transactions) and
`GHATD_TEST_REDIS_ADDR`. Tests use generated database/key namespaces and synthetic
email capture; no mailbox or live send is required. Without both, integration
tests skip explicitly.

## Explicit capability grant approval

Keep the existing token bridge at `/api/v1/admin-access`. To opt in to capability
approval, create a second bridge using the **same RedisStore**, authorization,
manager, real bearer-session middleware and email sender, with trusted
`ReviewKind: adminaccess.Capabilities`. The manager must additionally implement
`CapabilityManager`; missing ports or unknown kinds fail construction. Empty
`ReviewKind` means `TokenLimits` for existing callers. Do not derive the kind,
origin, system or route prefix from a request.

The capability bridge attaches only `/api/v1/admin-capabilities`:

| Method/path | Purpose |
| --- | --- |
| `POST /session` | Create a purpose-bound page context |
| `POST /users/{userID}/review` | Read current grant and strong ETag; no body |
| `POST /users/{userID}/preview` | Review a complete four-field replacement |
| `POST /challenge`, `/confirm`, `/cancel` | Purpose-specific email proof and cancellation |
| `PUT /users/{userID}` | Consume one approved review and delegate owner CAS |

Review and preview have no Mongo writes or index/inventory preparation. Preview
returns detached `before` (absent for a missing grant), `after` and `changed`, plus
the current strong ETag. A semantic no-op invalidates the old review and returns
no `X-Admin-Review`; it cannot authorize apply. Proposal/apply JSON must contain
all four fields: `enabled`, `expires_at`, `scopes`, `permissions`. Null expiry
means none. Arrays must be present, may be empty, and reject duplicate names and
wildcards. The manager owns the strict bounded parser. Sets compare in sorted
order, and expiry uses UTC millisecond precision, matching Mongo storage.

The immutable review binds purpose, complete replacement, selected target and
current revision to the existing verified actor/session/email/page context.
Capability bindings and code hashes use a different domain from the unchanged
legacy token encoding. An older token bridge cannot consume a capability review
by ignoring its new JSON fields, even when token limits are zero. Named fields
and revision must match before consumption; the owner then rechecks current
administrative authority and audited revision CAS. Account-wide context limits,
email cooldown and hourly ceilings are shared across both bridge instances.

Email describes enabled state, expiry, the complete sets and exact additions and
removals. Confirmation approves that replacement only; it grants no administrator
role. Apply preserves token allowances and usage budgets, never seeds privileges,
changes account status or issues credentials. The original bearer-only endpoints
remain unchanged. Capability routes require the same exact-origin cookie/CSRF
headers as token routes; browser review uses POST to require Origin reliably.

Both adapters consume approval before manager dispatch. An error or lost response
can leave an unknown committed outcome. Read current authoritative state before
creating another proposal; never automatically retry or restore the consumed
review. Cleanup is another fresh reviewed CAS operation, not cancellation or
restoration of a stale policy snapshot.
