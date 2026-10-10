# Access policy

This opt-in domain provides current grants, transactional Mongo persistence,
usage accounting and explicit token-limit migration planning, audited apply and
revision-checked rollback. HTTP management, middleware, token-inventory adapters,
host rollout and legacy-source migration decisions are separate integrations.
Installing this package alone does not protect endpoints or replace credential
creation rules. Do not delete legacy thresholds until host adoption and migration
have been verified.

The optional [browser token-allowance bridge](adminaccess/README.md) provides
exact-origin cookie verification and one reviewed email-confirmed update through
the existing policy manager. It does not grant general permissions or replace
the bearer-only management API.

The package separates verified identity from current, system-specific grants.
`Subject` uses a server-configured system plus an immutable user ID or independent
API-token ID. Identity names are bounded, valid UTF-8 and byte-exact: the package
does not case-fold or Unicode-normalize independently assigned IDs. Hosts must
derive subjects from verified, stable identifiers, never display names.
`Grant` carries exact scopes, permissions, token-inventory limits
and fixed-window usage budgets. Missing, disabled, expired or invalid grants deny
access. Zero limits do not mean unlimited. User type and signed administrator
claims never synthesize a grant.

`Service.Authorize` checks all required names on the same subject. Administrative
replacement uses an injected live `ManagementAuthorizer` and expected revision;
there is no implicit administrator seeding or default role expansion.
The authorizer returns `ErrDenied` for a deliberate refusal; dependency and
context failures retain their original cause. A host transport must sanitize
these errors rather than exposing raw dependency details. Full management
authority includes reading the policy snapshots used for migration review.

Services reject nil dependencies/contexts and already-canceled requests before
dispatch. Read-only resolution rechecks cancellation after custom storage returns.
Stores must still honor context and transaction guarantees after dispatch; an
error cannot establish whether an earlier or uncertain transaction committed.

## Explicit capability administration

`ReviewGrant` provides a live-management-authorized, detached stored snapshot,
including disabled or expired policies. A nil snapshot means known absence;
joined lookup failures never establish absence or permit first provisioning.
Review is read-only and is not a transferable approval or enforcement decision.

`ApplyCapabilities` takes an explicit user subject, reviewed revision and
`Capabilities` replacement: enabled state, expiry, exact scopes and permissions.
It preserves existing token allowances and usage budgets. Revision zero creates
only a missing user grant, with no token or budget entitlement. Replacements use
the same transactional compare-and-swap and administrative audit as other grant
writes; stale revisions fail even when requested fields happen to match.
Current management authority is checked again before writing. An error supplies
no success receipt and does not establish rollback after an uncertain commit.

The caller must verify the selected stored user through the owning identity
service and bind the system from trusted configuration. API-token targets are
excluded. These in-process methods do not install an HTTP administration route,
authenticate an operator, seed default grants or infer permissions from roles.
The [optional manager transport](../accesspolicymanager/README.md#optional-capability-management)
supplies explicit bearer administrator routes; host attachment and operational
approval remain required.

## Error responses

`AccessPolicyErrorMap` supplies safe default reply entries. Compose it with the
[shared manifest writer](../errormanifest/README.md), adding operation-specific
and host overrides afterward. Keep maps immutable while serving requests.

| Domain failure | Default HTTP status | Public code |
| --- | --- | --- |
| `ErrDenied` | 403 | `ACP0-001` |
| `ErrConfiguration` | 503 | `ACP0-002` |
| `ErrConflict` | 409 | `ACP0-003` |
| `ErrLimitReached` | 429 | `ACP0-004` |

For example, an If-Match endpoint may override conflict to 412. Configuration
errors are not necessarily bad client input; validate transport requests in the
manager first. Unknown storage failures, including independent joined causes,
must remain generic server failures. Preserve original errors internally for
retry/uncertain-commit handling, and never expose their diagnostic text. Error
mapping is a response operation, not authorization or proof of rollback.

## Mongo startup

Pass the existing `*repository.MongoDbRepository`. The store uses that
repository's default database and managed client, never another connection pool.

```go
store, err := accesspolicy.NewMongoStore(ctx, mongoRepository)
if err != nil {
    return err
}
if err := store.Initialize(ctx); err != nil {
    return err // Stop startup; do not silently disable authorization.
}
policy, err := accesspolicy.NewService(store, authorizePolicyManagement)
if err != nil {
    return err
}
```

Use a bounded startup context. `Initialize` creates collections and probes a
snapshot/majority transaction with an atomic insert, read and delete. It is
idempotent and leaves no probe document or grant behind. Transactions require
a replica set or sharded cluster. There is no non-transactional fallback, and an
uninitialized store fails closed. The host retains client lifecycle ownership.

The store owns `access_policy_grants`, `access_policy_audit`,
`access_policy_counters` and `access_policy_receipts`. Deterministic SHA-256 IDs
encode whole tuples rather than ambiguous concatenations; the built-in `_id`
indexes enforce uniqueness. Metric names are stored as values in named pairs,
not dynamic BSON map keys. Grant expiry is normalized to BSON milliseconds.

Initialization needs collection-creation plus read/write/delete privileges for
its grant-collection probe; runtime operations require
read/write access to these collections and access to any collections changed by
callbacks. Do not expose direct collection writes as a public policy API. Grant
replacements should go through the service's live management authorizer.

## Atomic policy and usage

- `Read` returns an independent snapshot; known absence is `ErrDenied`, optionally
  in a single-cause wrapper. Migration planning does not infer absence from joined
  errors, custom `Is` aliases or excessively deep chains. These failures propagate
  unchanged, even when one cause matches `ErrDenied`.
- `Replace` atomically checks the expected revision, writes the grant and records
  the verified administrative actor and complete resulting policy. Expected zero
  creates only a missing grant. Stale revisions return `ErrConflict`; audit failure
  rolls back the policy. Usage never increments the administrative revision.
- `WithGrant` fences grant changes and token-inventory callbacks in one transaction.
  Callbacks can retry: no external effects or network I/O, and every database
  operation must share the transaction context and client. Its lock is scoped to
  the grant; a callback counting inventory shared across systems also needs the
  inventory owner's fence. The caller must supply an atomic owner-wide
  count-and-insert adapter; this policy lock alone cannot enforce shared inventory.
- `Consume` checks the live grant and required scopes/permissions, quota,
  idempotency key and normalized-operation fingerprint in one transaction. Grant
  revocation and permission changes must be fenced against consumption, including
  receipt replay. Replaying a receipt cannot restore revoked authority.
- `WithConsumption` additionally runs a `ConsumptionAction`: `Check` revalidates
  current resource authority on first use **and replay**; `Apply` commits business
  writes/receipts only for a new admission. `Check` can also run on an attempt later
  denied by the quota or replay fingerprint. Both use the same transaction. Check
  must acquire any domain-specific ownership fence needed to prevent concurrent
  resource changes; the policy fence alone cannot protect unrelated documents.

Transactions use primary/snapshot reads and majority writes. A real write to the
grant serializes policy replacements, admission and inventory callbacks. This
is intentional: snapshot reads alone can observe old authority and commit writes
to unrelated documents. See MongoDB's
[stale-read guidance](https://www.mongodb.com/docs/manual/core/transactions-production-consideration/).
All metrics for one subject consequently share a serialization point; this is a
correctness choice, not a throughput guarantee. Load-test host workloads.

The stored grant's private `fence` marker is concurrency metadata, not policy.
It changes during admissions without increasing `Grant.Revision` and is excluded
from public grants and administrative audit snapshots. A replacement may remove
the old marker; subsequent admissions write a new one. Compare semantic grant
fields and revisions when reconciling audits, not whole-document byte equality.

`Consumption` binds a subject, metric, operation identity, fingerprint and
requirements. An HTTP guard should generate a fresh admission key per attempt; a business
command may bind a stable idempotency key to its normalized operation. Neither
case should blindly trust a client-selected system, subject or quota. Live
resource ownership remains a domain responsibility within the mutation/replay
boundary, not an inference made by the policy store.

For HTTP admission budgets, use `ConsumeAuthorized`: downstream failure does not
refund a committed admission. The `Service.Consume` convenience method supplies
no scope or permission requirements; choose `ConsumeAuthorized` when those checks
are needed. Both still require an eligible grant and configured budget.
For business-unit quotas use `WithConsumption` so
failed business writes roll back the counter and receipt together. Its callbacks
must share the managed Mongo client and supplied transaction context, propagate
database errors, and perform no network calls, messages or external effects.
The driver can retry aborted callbacks; `Apply` may therefore run more than once
before one attempt commits. On an already committed replay, only `Check` runs.
Persist any business response receipt atomically and retrieve it under current
resource authorization rather than re-executing the command.

All public store operations reject an existing Mongo session rather than silently
replacing it. `WithGrant`/`WithConsumption` must be the outer transaction boundary;
their callbacks cannot recursively call the policy store or commit/abort/end the
session themselves. Supplying the session to another client's operations returns
the driver's wrong-client error; omitting the transaction context cannot be made
safe by this package and is a caller contract violation.

## Replay, expiry and retention

- A matching key/fingerprint returns the original `Count`, `Maximum` and
  `ResetsAt`, with `Replayed: true`. It does not return the live window total.
  A changed fingerprint returns `ErrConflict` without charging again.
- Replay still needs an enabled, unexpired grant, all current requirements and a
  present, positive metric. Disabled or missing grants/requirements deny access;
  a zero metric returns `ErrLimitReached`. A positive quota reduction preserves
  the original receipt but applies the reduced maximum to new admissions.
- Ordinary policy revisions do not reset usage. Window duration is part of the
  counter identity: changing it deliberately starts a separate budget. Treat that
  administrative change as a quota reset, not a harmless metadata edit.
- Fixed windows are anchored to Unix epoch in UTC. Time advances during automatic
  retries and callbacks; a successful retry can fall in a later window. Expiry is
  rechecked before callback completion, not promised after the commit completes.
- A denial, timeout, cancellation or uncertain commit result **does not prove a
  previous attempt never committed**. Never execute business effects on an
  admission error or retry an uncertain operation using a new key. Retry the
  same key/fingerprint, and reconcile business receipts under current authority.
  Denial after revocation reveals neither the receipt nor permission to use it.
- Counters, receipts and audit records have no automatic TTL. Receipts remain
  valid across windows; deleting one permits its key to charge again. Storage
  therefore grows until operators implement an explicit lifecycle policy. Any
  pruning must preserve the supported replay horizon and prevent old operations
  from being accepted again. This package does not yet provide pruning tooling.

Storage errors and context cancellation propagate, rather than becoming an allow
decision. Route adapters must map them to a fail-closed structured error without
exposing database details. `ErrDenied`, `ErrConflict`, `ErrLimitReached` and
`ErrConfiguration` can be matched with `errors.Is`; do not classify every error
as one of those four or assume an error means no earlier committed effect.

## Verification and adoption

### Explicit token-limit migration

Use `Service.PlanTokenLimits` for an explicitly selected **stored user** and
reviewed `TokenLimits`. The host must verify the user ID and choose the source
allowances; this package does not scan accounts, interpret role names or infer
allowances from JWTs. Planning, applying and rolling back all require a live
`ManagementAuthorizer`. They are trusted administrative service APIs, not public
HTTP routes or background startup migrations.

```go
// The host has verified targetUserID and reviewed these allowances.
plan, err := policy.PlanTokenLimits(ctx, accesspolicy.Subject{
    System: configuredSystem,
    Kind: accesspolicy.UserSubject,
    ID: targetUserID,
}, reviewedLimits)
if err != nil {
    return err
}
preview := plan.Preview() // Detached Before/After snapshots and Changed flag.
// Present preview for explicit operator review before executing this next step.
_ = preview
receipt, err := policy.ApplyTokenLimits(ctx, plan)
if err != nil {
    return err // No receipt: do not assume the database write was rolled back.
}
stored := receipt.Snapshot()
_ = stored
```

- Planning writes nothing. It reads stored disabled/expired grants directly,
  unlike `Resolve`, which rejects them for enforcement.
- An existing policy changes only `Tokens` and its administrative `Revision`.
  Scopes, permissions, usage budgets, expiry and enabled state remain unchanged.
  No user authority is copied to API credentials; API-token subjects are rejected.
- A missing policy proposes an enabled user grant with only the reviewed token
  limits. It has no scopes, permissions or usage budgets. Apply is create-only;
  another writer creating that grant causes a conflict, never an overwrite.
- Plans and receipts have private immutable state bound to their original
  `Service`. Their detached previews may be serialized as review records, but
  are **not executable approvals**. After a restart, re-plan and review again.
  Editing a preview cannot alter the in-process plan.
- Apply rechecks the complete reviewed policy and uses `ReplaceGrant`'s revision
  compare-and-swap and audit transaction. A changed revision conflicts even if
  the new limits happen to match. A no-op rechecks freshness but creates no new
  revision or audit event; it is a read-time observation, not a policy lease.
- The management authorizer is checked again before a mutating replacement.
  This uses the existing management contract; it does not make an external
  identity service's authorization check part of the Mongo transaction.

For a **known successful** apply, an operator may explicitly call
`RollbackTokenLimits(ctx, receipt)`. It checks live authority and requires the
exact applied policy/revision. Existing policies regain their previous token
limits; newly created policies are disabled with zero token limits. Rollback
creates a new audited revision rather than deleting history. Later edits,
including an identical-value revision, cause a conflict. No-op receipts cause
no rollback write. Usage counters/receipts and owner inventory locks are retained.

Rollback does **not** revoke issued credentials, reverse admitted operations or
make an older application release safe. Review these separately before rollout.
The receipt is retained in memory only; after process loss or an uncertain commit,
there is no automatic rollback. Inspect the live policy and audit history and
prepare a newly reviewed change. A matching policy at `expected+1` is not proof
that this particular operator's attempt committed: a competing writer could have
written the same policy. Failed applies return no receipt and are never silently
retried or classified as success. Reapplying a committed mutation conflicts.

Initialize and prepare the host's separate owner-wide inventory fences before
enabling token creation. This migration API creates no inventory locks or tokens
and does not enable the host's `TokenPolicy` automatically. Retire the
legacy role fallback only after source review, provisioning, host wiring and
rollback verification are complete.

### Verification

```sh
# Set GHATD_TEST_MONGO_URI to an isolated transaction-capable test server first.
go test -race -count=1 -v ./external/accesspolicy
```

Without the environment variable, database cases skip. The suite covers CAS/audit,
replay, concurrent admission, rollback and explicit grant migration. Lost-response
cases wrap successful commits; they do not simulate replica-set failover.
Commit-result uncertainty, production load and host-specific integration require
separate verification.

Before replacing legacy tier rules, migrate reviewed grants, wire route guards
and token inventory through the same managed client, and verify rollback. Do not
seed blanket administrator privileges or retain a missing-grant role fallback.
