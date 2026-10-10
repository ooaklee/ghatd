# Partners native runtime composition

`partnerruntime` is an optional outer composition package. It constructs the
program, referral, earnings, revenue and work-queue owners over a borrowed Mongo
database and encrypted GHATD record store. The core `partnermanager` package does
not import runtime, HTTP or helper packages. Financial operations remain with
their existing owners.

## Two-step startup

1. Call `NewRuntime(database, config, dependencies)` with trusted typed inputs.
   It validates and binds owners without database I/O, route registration,
   provider calls, policy seeding, grant creation or goroutine startup.
2. Call `runtime.Prepare(ctx)` before admitting HTTP or workers. Preparation
   installs additive record-store indexes and probes a real transaction under
   the caller's deadline, bounded to thirty seconds. Any failure blocks admission.

The runtime borrows the database pool and never closes it. Hosts own scheduling,
cancellation, shutdown drain and the decision to enable a capability. Readiness
does not prove the supplied key can decrypt every retained record, historical
writer drain, provider authentication or production qualification.

## Explicit configuration

`Config` contains the owning program policy, payload encryption key, retained
referral signing keys, admission controls, claim minimum, queue bounds and
optional analytics/reporting and worker/lifecycle scheduling data. Hosts retain
environment parsing, secret loading and commercial launch decisions.

`ReservedKeys` lists other host-purpose keys. Supply between one and 32 distinct
nonzero 32-byte keys; the payload key and every retained signing key must be
independent of them. The runtime does not assume exactly two host purposes.
Active and retained signing keys remain validated by the owning evidence signer.
Keys are not generated or inferred. Persist them securely across restart.

Optional worker and lifecycle config does not activate a worker. The host must
apply the owning worker validation and `billinglifecycle.RuntimeConfig.Validate`
before installation and enforce the relevant current grants even for empty work.
Configuration pauses do not discard financial obligations or recovery evidence.

Set `RequireAcquisitionEligibility` and supply `Dependencies.AcquisitionEligibility`
when the host requires a current paid-partner rule. Missing or typed-nil required
wiring fails construction. The port is retained by worker facades, so new signup
attribution checks the same referring-partner policy as interactive enrollment.
It does not replace worker grants or gate existing financial obligations. See
[paid-partner acquisition](../README.md#optional-paid-partner-acquisition).

## Dependency and authority ownership

`Dependencies` supplies identity, authority, groups, clock and identifier
generation ports. Human session authority must recheck the real credential and
current account admission through [partneraccess](../../partneraccess/README.md).
The same package keeps human and instance-bound worker contexts separate; a
browser credential does not confer worker capabilities.

`ManagerWithAuthority` constructs a separate worker facade over the exact same
financial owners. It does not change the human manager or create grants. Human
reporting configuration is intentionally omitted because the worker uses only its
worker capabilities; this facade must not be attached to customer/operator HTTP. Service
account identity and current exact permissions still require owning verification.
Constructing native owners does not authenticate a request. Live session
admission belongs to the host's transport boundary, not these composition inputs.

`RecordStore`, `CheckoutRepository` and `Clock` expose the borrowed substrate and
exact owning dependencies needed by billing lifecycle composition. Do not replace
these with a second financial store or separately reconstructed clock/authority
instances. See [billing lifecycle recovery](../../billinglifecycle/README.md).

```go
runtime, err := partnerruntime.NewRuntime(database, config, dependencies)
if err != nil {
    return err
}
if err := runtime.Prepare(ctx); err != nil {
    return err
}
// Only now may the host attach admitted handlers and start configured workers.
```

## Storage and verification

Record kinds, fingerprints, encryption metadata, scopes and retained evidence
formats belong to their owning services. Persist the configured keys and those
contracts across restarts; changing storage identifiers requires a data migration.

Native tests use `GHATD_TEST_MONGO_URI` pointing to an isolated replica set and
create/drop separate fixture databases. They cover owner interoperability,
stable-key restart, wrong-key refusal, policy pauses and no implicit privileged
records. Run `go test ./external/partnermanager/runtime`.
