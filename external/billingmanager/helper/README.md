# Billing integration helpers

`billingmanagerhelper.CheckoutEvidence` dispatches native checkout evidence
lookups through an explicitly supplied `CheckoutProviderRegistry`. Construct it
with `NewCheckoutEvidence(registry)`; this performs no lookup. Missing/typed-nil
registries/providers or unsupported capabilities fail closed at call time. The
zero value is unavailable and is useful only for an explicitly disconnected
composition.

The provider is selected solely by `RevenueScope.Provider`. Both subscription
and retained-session methods forward the exact scope and identifier; neither
infers a provider from a catalogue, email address or current subscription.
`LookupRevenueCheckoutSessionEvidence` requires that exact reviewed capability,
withholds partial evidence on error and checks cancellation before/after lookup.
It never falls back to subscription lookup. The older subscription method
forwards the owning provider outcome for native verification unchanged.

The owning `billing.CheckoutService` still verifies returned evidence against its
frozen authorization and original intent. A forwarding adapter creates no payment
fact, history, grant or idempotency record. Do not expose provider evidence as a
browser DTO. Hosts supply credentials/provider configuration and resource lifetime.

```go
checkout, err := billing.NewCheckoutService(repository, clock,
    billingmanagerhelper.NewCheckoutEvidence(checkoutRegistry))
```

The named fixture tables test retained-scope/session forwarding, missing
capabilities, errors, cancellation and refusal to use subscription fallback.
These are adapter tests, not live authenticated-provider delivery evidence.
