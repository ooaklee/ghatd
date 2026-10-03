# API tokens

The package issues opaque credentials and verifies them against current stored
records. The header remains `X-Api-Token: <owner-prefix>.<secret>`; it is not a JWT.
New secrets contain 32 bytes from `crypto/rand`, encoded with unpadded base64url.
Storage contains the SHA-256 digest, not the plaintext secret.

Verification requires exactly one bounded header, an exact prefix/digest lookup,
matching stored identity, ACTIVE status and an unexpired, parseable expiry where
present. Multiple headers, combined values, whitespace and malformed segments
fail closed. Authentication no longer scans paginated token lists or logs a
malformed credential fragment. Access Manager separately resolves the current
owner, checks its immutable ID and account status, and applies administrator
requirements when appropriate.

Existing credential strings remain compatible if their current stored record
passes these checks. There is no automatic revocation or reissue of older
credentials. Operators should assess rotation of secrets generated before the
cryptographic-generator change; rotation is a separate operational action.

## Repository and adapter migration

Custom `ApitokenRespository` adapters must implement:

- `GetAPITokenByDigest(ctx, ownerPrefix, digest)`: exact lookup independent of
  pagination, returning the matched record or an error. Do not log digest filters.
- `TouchAPIToken(ctx, tokenID, ownerID, digest, at)`: atomically update **only**
  `last_used_at` on the exact active credential; no upsert or full-record replace.
- `SetAPITokenStatusFor(ctx, ownerID, tokenID, status)`: match both owner and ID,
  update only status/change time, and reject missing or mismatched records.
- `DeleteAPITokenFor` must match both owner and ID and report absent records;
  checking an owner against a paginated list is not sufficient.

`ActivateAPITokenRequest` and `RevokeAPITokenRequest` now require `UserID`.
Custom Mongo store wrappers must also implement the result-bearing update/delete
methods used to verify matched/deleted counts. The shared repository supplies
privacy-safe logging for digest lookups and mutations; no direct-driver bypass is
needed for those operations.

Custom credential verifiers must return `IsValid`, `TokenID`, `UserID` and
`NanoId`. Returning only a public prefix no longer authenticates a user.
`UpdateAPITokenLastUsedAtRequest` now requires the verifier's `TokenID`, owner ID
and digest. Missing identity fails closed rather than falling back to a list scan.
The built-in repository generates the secret and ID during creation; custom
creation adapters must preserve that contract, including the requested owner,
status and expiry and a digest matching the generated secret. Returned snapshots
are copied before presentation. Service dependency failures and malformed store
results map to `ErrServiceUnavailable` / HTTP 503, not credential denial.
Cancellation is checked before dispatch and before returning read/authentication
results. An error after a write is not proof that it failed to commit.

**JSON compatibility:** `value_sha` is no longer serialized. The plaintext
`value` appears only in the one-time creation response; management service reads
remove it, including when a custom adapter supplies it. Do not serialize trusted
repository records directly. Creation secrets must not be logged or cached.

The legacy repository methods `UpdateAPIToken` and `DeleteAPITokenByID` remain
available for trusted administrative callers, but are deprecated. They do not
enforce ownership; a full-record update can overwrite newer authority. They are
not part of the service's management path. Migrate to the owner-bound methods.

Usage timestamp updates are best-effort telemetry, not quota accounting and not
additional authentication. They cannot recreate a deleted credential or overwrite
a concurrent revocation. Per-token scopes/permissions use the separate
[access-policy contract](../accesspolicy/README.md); persistence and automatic
enforcement of those grants are not yet provided by this package.

The exact digest query currently relies on host-owned collection indexing.
Evaluate a compound `created_by_nid, value_sha` index through the normal migration
process. No index or data migration is silently applied during authentication.

## Creation and inventory

Creation requires a nonempty, representable owner prefix and rejects negative or
duration-overflowing TTLs before persistence. Zero TTL means permanent; positive
TTL is seconds. Created and expiry timestamps share one UTC clock instant and
use RFC3339Nano. Token creation does not authorize a caller or enforce inventory
limits on its own. A manager must compose the inventory primitives below with
current authorization and insertion in one transaction. Publishing these
primitives does not enable policy-backed creation automatically.

`CountTokenInventory` counts every stored credential for an exact owner, without
pagination or status/expiry filtering. Missing, null and empty expiry represent
permanent credentials; all remaining records consume ephemeral inventory.
Revocation/expiry does not free a slot; explicit owner-bound deletion does.

Lists and individual reads are now non-mutating management views, including
retained expired/revoked records. Pagination totals use the same type filters as
the query; contradictory permanent/ephemeral filters are rejected. A stable ID
tiebreaker is used for equal sort values. Separate count/list queries are not a
snapshot guarantee during concurrent changes. Authentication still checks
current status and expiry independently of these views.
Description and status searches are case-insensitive **literal substrings**, not
caller-supplied regular expressions. Update clients that previously sent regexes.

**Compatibility:** clients that assumed GET would delete expired tokens must use
an explicit owner-authorized deletion flow. Management callers must supply the
trusted owner ID when activating, revoking or deleting a credential. This batch
does not change the preloaded routes' accepted credential families or replace
the legacy role-based admission flow. That flow is not concurrency-safe; the
fenced inventory primitive alone does not repair every existing writer.

The API-token header continues to take precedence on preloaded mixed-credential
middleware. This change does not introduce fallback to a JWT after an invalid API
credential. Explicit credential-selection hosts must preserve their own documented
policy. Published JWT and API metadata cannot coexist as two verified identities.

## Transactional inventory setup

Token inventory belongs to the owner, not to an individual resource system.
API-token admission therefore takes two locks in one transaction: the live
system grant (owned by access policy) and a stable owner-wide inventory fence
(owned by this repository). Every admitted insert must follow both. Different
systems can have different limits; each compares total inventory with its own
live allowance, rather than imposing the minimum of all systems' limits.

Before enabling policy-backed creation:

1. Call `tokenRepository.InitializeTokenInventory(ctx)` once per repository
   instance at startup. It explicitly creates `apitokens` and
   `apitokens_inventory_fences`, and probes transactional read/write/commit
   support through the shared repository. There is no standalone fallback.
2. Provision an owner lock with `tokenRepository.PrepareTokenInventory(ctx, ownerID)`
   outside any transaction. The trusted caller verifies the owner exists.
   Migration and new-account provisioning must do this before allowing issuance.
   Preparation is insert-only, idempotent and majority-durable; repeat it safely
   after an uncertain preparation outcome. It neither grants permission nor
   creates a secret, and never resets an existing lock marker.
3. Within the manager's transaction, acquire the current policy grant fence,
   call `CountTokenInventoryFenced`, enforce the grant's limits, then insert
   using the same context and client. `CountTokenInventoryFenced` requires
   repository `FenceInventory` support and
   an active transaction on that repository's client. A missing or corrupt
   owner lock fails with `ErrInventoryUnavailable` / HTTP 503. Admission never
   upserts locks, starts a nested transaction or falls back to unlocked counts.

Custom adapters must honor that same-context/client/transaction contract for
fencing, counts and insertion. The native repository checks the active session
and client; the service cannot verify a custom adapter's hidden storage behavior.

The fence collection has only its default `_id` index and no TTL. Never delete
owner locks while writers can still address that inventory. Lock IDs are derived
from exact owner IDs, and stored owner identities are checked. Preparation uses
primary/majority reads independent of host query defaults. All database
operations use shared repository helpers with metadata-only automatic logging.

These methods do not make arbitrary low-level writers safe. Migrate every
inventory-creating writer before claiming bounded admission. Status changes and
deletion cannot increase stored inventory; they do not require this creation
lock. An uncertain **credential creation** commit is different from preparation:
the secret cannot be recovered through an idempotent retry. Reconcile stored
inventory explicitly instead of blindly issuing another credential.

## Tests

```sh
go test -race ./external/apitoken/... ./external/accessmanager/...
```

Repository integration tests require `GHATD_TEST_MONGO_URI` pointing to an isolated
test MongoDB server. Without it, those cases skip explicitly. Each case creates
and drops its own uniquely named `ghatd_apitoken_test_...` database; never supply a
production server. They verify lookup beyond the old page limit, exact owner and
digest matching, field-only updates and concurrent revocation.
