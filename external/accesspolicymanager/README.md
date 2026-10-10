# Access policy manager

An opt-in administrative boundary over the [access-policy domain](../accesspolicy/README.md).
It previews and applies **explicit token-inventory allowances for one stored user**.
Separately opted-in capability routes review and replace explicit scopes,
permissions, enabled state and expiry for a selected stored user. It does not
infer authority from roles, issue credentials, scan accounts or provision users
during sign-up. Token-limit operations do not change scopes or permissions.

`Service` coordinates narrow user/inventory ports and the policy domain. `Handler`
owns bounded HTTP parsing and shared `reply/v2` manifests. `AttachRoutes` declares
administrator-session requirements. Persistence stays in the lower domains.

## Host setup

1. Initialize `accesspolicy.MongoStore` and the API-token repository's
   `InitializeTokenInventory` on the **same managed Mongo client and database**.
   Use bounded startup contexts; transaction/collection failures stop startup.
2. Construct an enforcement policy service and install
   `middleware.NewRoutePolicyGuard(...).Install(routes)` before any route group.
   Wire `accesspolicy.TokenPolicy` into Access Manager (or `starter/v0`). Missing
   grants/fences must deny issuance, never fall back to role thresholds.
3. Obtain `accessManager.PolicyManagementAuthorizer(configuredSystem)`. It rechecks
   the live session and current active administrator, not a signed flag alone.
4. Build `NewService(Config{System: configuredSystem, Store: policyStore,
   Authorize: authorize, Users: userRepository, Inventory: tokenRepository})`.
   The system is trusted configuration. The standard user repository preserves
   dependency errors instead of classifying every failure as a missing user.
5. Build `NewHandler(service)` and call `AttachRoutes(routes, handler,
   middlewareSuite.BearerSession)`. Check its error and validate the complete
   route inventory before serving.

Routes also require the private transport-origin marker minted by GHATD's
`BearerSessionRequired`. Accidentally supplying cookie middleware fails closed,
even if it publishes an authenticated administrator. Custom adapters should
compose the shared bearer adapter, not manufacture context. Direct service
callers still require live management authority; the HTTP marker is not permission.

## Review and apply

| Method | Path | Behavior |
| --- | --- | --- |
| POST | `/api/v1/access-policies/users/{userID}/token-limits/preview` | Read-only before/after report, `Changed`, and current strong `ETag` |
| PUT | `/api/v1/access-policies/users/{userID}/token-limits` | Explicit provisioning/update with mandatory `If-Match`; resulting grant and `ETag` |

Both accept one `Authorization: Bearer <session-access-token>` header and one
`Content-Type: application/json`. Cookies alone and API-token headers are rejected.
The adapter never refreshes cookies or substitutes them for the explicit bearer.
Responses use the reply envelope and `Cache-Control: no-store`.

Send all five integer fields (TTL values are seconds):

```json
{
  "permanent": 2,
  "ephemeral": 1,
  "minimum_ttl": 60,
  "maximum_ttl": 3600,
  "ttl_increment": 60
}
```

These are example allowances, **not defaults**. Zero disables the corresponding
creation capability; it is never unlimited. Bodies are capped at 4 KiB; unknown,
duplicate, missing, null, fractional or trailing values are rejected. Limits must
satisfy `accesspolicy.ValidateTokenLimits` before inventory preparation.

Use the preview's ETag verbatim in `If-Match`. `"0"` means create-only, not an
unconditional update. Otherwise a canonical quoted decimal revision is required;
weak tags, lists, wildcard, signs and leading zeros fail. PUT re-plans on the
server and rejects stale revisions, including identical values at a later revision.
A preview is a report, not a serialized approval. Direct PUT with explicit limits
and the correct revision is allowed; the API cannot prove human review occurred.

Existing scopes, permissions, budgets, enabled state and expiry are preserved.
First provisioning creates a token-only grant, with no scopes, permissions or
usage budgets. The target must exist in storage. Its account status is not changed
or included in the preview: suspended users remain unable to issue credentials.

## Consistency, audit and recovery

Live authority is rechecked before target lookup, before inventory preparation,
and at policy apply/replacement boundaries. These repeated checks favor revocation
sensitivity over fewer reads. They do not make Redis/current-user checks atomic
with Mongo commits. Policy and owner fences serialize **limits and inventory**,
not every identity lifecycle change in an external store.

Inventory preparation is insert-only, outside the grant transaction. Failed
applies may leave an authority-neutral fence; do not delete it during cleanup or
count fences as provisioned users. Audited replacements record the write-time actor
and policy revision transactionally. Previews, denials and no-op observations do
not create mutation audit records. Administrative access telemetry is a host
responsibility; never log bearers or raw bodies.

Known no-ops preserve revisions. Errors return no success receipt. After timeout
or uncertain commit, inspect the live grant and audit history, then preview a newly
reviewed change. An error does not prove rollback; matching state does not prove
this particular request committed.

Cancellation observed after authorization, target lookup or inventory preparation
stops the next operation. Once the policy domain reports a known successful
apply, its receipt is retained even if cancellation races the return; HTTP
delivery can still fail. A known commit and an unknown commit outcome are distinct.

There is no serialized rollback endpoint. To restore old limits over HTTP, review
them against current policy and PUT with the new ETag. This cannot restore absence
or change `Enabled`; disabling requires the domain's separately authorized
management workflow. In-process `RollbackTokenLimits` receipts are not exposed by
HTTP. Lower limits do not revoke issued credentials or undo usage.

Common errors: 400 invalid request; 401 session required; 403 authority denied;
404 target missing; 412 stale revision; 428 missing precondition; 503 unavailable
policy/inventory configuration. Unknown dependency failures produce opaque 500s.
Host manifest overrides are supported without exposing internal diagnostics.
Wrapped target-absence sentinels produce 404. Joined failures and custom `Is`
aliases do not establish absence or permit provisioning; their original causes
reach the response boundary. Nil or wrong-owner lookup results are inconsistent
adapter responses and produce 503, not a fabricated missing-user result.

## Optional capability management

After `AttachRoutes`, explicitly call
`AttachCapabilityRoutes(routes, handler, middlewareSuite.BearerSession)` with
the same configured manager and handler. Token-only integrations do not gain
these routes automatically. Missing optional service methods or a revision
evaluator fails attachment; cookies and API tokens cannot replace an explicit
administrator session. The manager rechecks current live management authority
before resolving the selected stored user and before the owning policy write.

| Method | Path | Behavior |
| --- | --- | --- |
| GET | `/api/v1/access-policies/users/{userID}/capabilities` | Read stored policy, including disabled/expired policies, and strong ETag; `"0"` means known absence |
| PUT | `/api/v1/access-policies/users/{userID}/capabilities` | Explicit authority replacement with mandatory reviewed `If-Match` |

Both use the same explicit bearer and safe reply envelopes described above.
PUT requires all four fields, including empty arrays when removing authority:

```json
{
  "enabled": true,
  "expires_at": null,
  "scopes": ["reviewed-resource"],
  "permissions": ["reviewed-action"]
}
```

Null expiry explicitly removes expiry. Otherwise supply an RFC3339 timestamp;
the adapter normalizes it to UTC milliseconds, matching policy persistence.
Bodies are capped at 128 KiB; each array has at most 128 distinct, bounded exact
names. Unknown/duplicate/missing fields, null booleans/arrays, wildcard names,
malformed expiry and trailing JSON are rejected. Actor, system, token allowances
and usage budgets are not request fields. Responses and grant-review data are
administrator-only and must not be exposed as customer read models.

The [owning capability operation](../accesspolicy/README.md#explicit-capability-administration)
preserves existing token allowances and budgets, and creates neither on first
provisioning. No token inventory is prepared by a capability write. Enabled
state and expiry are explicit administrative choices, so a reviewer can activate
or revoke a previously disabled or expired grant. This is privileged authority
administration, not automatic enrollment eligibility or country verification.
Hosts define the actual scope/action vocabulary and operational approval process.
No new schema or index migration is required beyond policy-store initialization.

Every accepted PUT advances the revision and records the live operator through
the same transactional CAS/audit. A stale ETag fails even for matching fields.
After an uncertain response, GET the selected policy and audit before reviewing
another update; never retry with an unconditional revision. To restore earlier
capabilities, review them against current policy and use its current ETag; this
cannot erase audit history, undo consumption or revoke independent credentials.

## Verification

Table-driven tests cover strict bodies/ETags, error overrides, credential
selection, cookie-adapter miswiring, live authority changes and preserved policy.
Set `GHATD_TEST_MONGO_URI` to an isolated replica set for real shared-repository
provisioning, competing create-only requests and limit reduction racing issuance.
Each database fixture owns its unique temporary database.

```sh
go test -race -count=1 ./external/accesspolicymanager ./external/accessmanager/...
```

Database cases skip without that variable. Production migration, load, failover
fault injection and complete host route coverage are separate gates. Retire legacy
rules only after explicit host migration and rollback decisions are verified.
