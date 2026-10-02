# Ephemeral state

The Redis-backed `Client` stores shared authentication metadata, rotation locks,
cooldowns and other short-lived state. Use `NewRedisRuntime` for managed host
bootstrap. Redis expiry and shared state are separate from the local snapshot
utility below; no production Redis fallback is selected automatically.

## Live session lookup

`Client.FetchAuth(ctx, details)` returns the owner stored for a namespaced
session. The caller must compare it with the signed owner; a successful lookup
is not authorization by itself. Missing/expired records wrap `ErrAuthNotFound`
and retain `errors.Is(err, redis.Nil)` compatibility. Use `IsAuthNotFound` when
supporting older Redis adapters. Custom adapters should return the framework
sentinel for absence and preserve outages/cancellation as distinct errors.

Nil context/client/identity and empty IDs are rejected before storage access.
Cancellation is checked before and after the read, and operational errors retain
their original cause. Automatic lookup logs contain fixed outcome metadata, not
session keys, owner IDs or raw driver diagnostics. Do not rely on direct equality
with `redis.Nil` or parse error strings; use `errors.Is` or the helper instead.

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
