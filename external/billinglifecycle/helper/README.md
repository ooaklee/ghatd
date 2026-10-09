# Billing lifecycle composition

`billinglifecyclehelper.NewRuntime(ctx, RuntimeConfig, Dependencies)` explicitly
builds the lifecycle scheduler, discovery, original outboxes, execution, completion
and resolver over prepared native owners. It uses a separate billing manager over
the supplied revenue/checkout owners, leaving human billing authority unchanged.
The helper accepts no host settings, secrets, account repository or product types.

Supply prepared `Records`, `Checkout`, `Revenue`, owning `Clock`, configured
`Providers` and a current worker `Authority` that implements the existing
execution/discovery ports plus trusted `Bind`. For example, the published
[partneraccess worker authority](../../partneraccess/README.md) satisfies these
ports. Its scopes/actor must match the explicit runtime configuration; do not
implement Bind by converting a human HTTP session into a worker invocation.

Construction clones scopes, validates bounds and applies the configured total
`PassTimeout` startup deadline. It checks current read/refresh authority even for
empty discovery, validates each provider registry's reviewed subscription/session
capabilities and probes both discovery lanes with limit 1. Pages and final current
read/refresh authority must validate. Unprepared history, missing/revoked grants,
unknown dependency errors or cancellation prevent activation. Registry capability
checks are local and startup discovery is a bounded owning read. These checks do
not prove provider availability, complete source coverage or older-writer drain.

Construction never prepares history, seeds policy/grants, creates accounts,
looks up remote provider evidence or starts a goroutine. Perform explicit owning
preparation before invoking it. No provider call or financial work happens in a
constructor or a retryable storage callback. Persistent identifiers, scope hashes,
receipt fingerprints and encrypted payload schemas remain the existing owners'.

The returned `Runtime.RunOnce(ctx)` binds the current instance's worker authority
for each explicit pass. `Interval()` and `PassTimeout()` expose validated host
scheduling bounds. The caller must apply that per-pass deadline and schedule one
sequential pass at a time, with cadence measured from completion. It also owns
cancellation, operational logging and borrower drain before closing storage.
A cancelled/timed-out pass does not prove rollback; recover through the original
owning execution/receipt protocol and current authority.

```go
runtime, err := billinglifecyclehelper.NewRuntime(ctx, policy,
    billinglifecyclehelper.Dependencies{
        Revenue: revenue, Records: records, Checkout: checkoutRepository,
        Clock: clock, Providers: providers, Authority: workerAuthority,
    })
// If construction succeeds, the host may explicitly schedule bounded RunOnce.
```

See [billinglifecycle](../README.md) for lane fences, original receipts and
preparation contracts. The helper's validation/zero-value tables use inert ports
and assert that invalid configuration never invokes authority. Native host
integration separately checks preparation, startup/restart, current grants,
service identity and no provider call for empty discovery. This is not deployment
or authenticated live-provider evidence.
