# Ephemeral state

The Redis-backed `Client` stores shared authentication metadata, rotation locks,
cooldowns and other short-lived state. Use `NewRedisRuntime` for managed host
bootstrap. Redis expiry and shared state are separate from the local snapshot
utility below; no production Redis fallback is selected automatically.

## Live session lookup

`Client.FetchAuth(ctx, details)` returns the owner stored for a namespaced
session. The caller must compare it with the signed owner; a successful lookup
is not authorization by itself. Missing/expired records wrap `ErrAuthNotFound`
and retain `errors.Is(err, redis.Nil)` compatibility. Use `IsAuthNotFound` for
absence-driven control flow, including when supporting older Redis adapters.
Custom adapters should return or ordinarily wrap the framework
sentinel for absence and preserve outages/cancellation as distinct errors.

The helper accepts only error trees whose leaves are `ErrAuthNotFound` or
`redis.Nil`, including the native dual-sentinel wrapper. A joined operational
failure must not be treated as a missing session. Nil causes, typed-nil errors,
cycles and trees exceeding 64 nodes fail closed; error strings and custom `Is`
methods cannot establish absence. Ordinary `errors.Is` remains useful for
diagnostics, but one matching cause alone is insufficient for absence decisions.

Nil context/client/identity and empty IDs are rejected before storage access.
Cancellation is checked before and after the read, and operational errors retain
their original cause. Automatic lookup logs contain fixed outcome metadata, not
session keys, owner IDs or raw driver diagnostics. Do not rely on direct equality
with `redis.Nil` or parse error strings; use the helper for control flow instead.

## Target-only session cleanup

`DeleteAllTokenExceptedSpecified(ctx, userID, exemptions)` scans only the selected
account's session namespace, escapes Redis glob characters and deletes in batches
of at most 128 keys. Empty IDs, the `:` storage delimiter and missing/typed-nil
adapters are rejected. Exemptions are combined `userID:tokenID` values without the
application/environment prefix; pass none when invalidating all target sessions.
Foreign or nested-namespace results from custom clients are ignored. Do not use
an administrator's cookie values as exemptions for another account.

SCAN is not a snapshot. A nil result acknowledges the completed scan/deletion
work, not that no concurrent session can exist. Deletions completed before a
cancellation or failure are not rolled back; errors preserve their native cause.
Live account-revision checks enforce email-change revocation independently of
this best-effort cleanup. Other Redis namespaces, proof aliases and rotation
receipts are not swept. Logs for this operation omit keys, owners and raw errors.

## Process-local transactional snapshots

`NewMemorySnapshot(initial, clone)` provides reusable copy-on-write state for
tests and explicitly local adapters. `Update` commits all callback mutations or
none; `View` supplies an isolated snapshot and never commits. A cancelled waiter
can leave without waiting for the current callback. Callback errors, panic or
observed cancellation discard the snapshot; panic still propagates.

The host must provide a deep clone covering every map, slice, pointer and mutable
sub-value. State is cloned on construction, callback entry and successful commit.
Consequently, retaining a callback value cannot change committed state later.
Read views deliberately copy too: this costs O(state size) but avoids aliases to
internal state. This utility is intended for bounded fixture datasets, not large
production databases. It has no persistence, TTL eviction, distributed locking,
Redis compatibility or cross-process transaction semantics.

```go
clone := func(values []int) []int { return append([]int(nil), values...) }
snapshot, err := ephemeral.NewMemorySnapshot([]int{0}, clone)
if err != nil {
    return err
}
err = snapshot.Update(ctx, func(tx context.Context, values *[]int) error {
    (*values)[0]++
    return nil
})
```

Callbacks must not spawn concurrent work using their mutable snapshot or re-enter
the same store. Nested entry using the supplied callback context is rejected;
discarding that context can deadlock. Clone functions must be deterministic,
deep, non-reentrant and free of external side effects. Cancellation cannot undo
a commit already completed before cancellation was observed.

Product records, query predicates, revision checks, receipt retention and
authorization stay in the host adapter. Table-driven tests exercise commit,
rollback, isolation, panic cleanup, concurrent writers and cancelled waiters.
