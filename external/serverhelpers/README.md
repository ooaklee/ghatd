# Server composition helpers

Use this package family to assemble owning GHATD services from resolved typed
configuration. Keep `server.go` as the explicit composition root: acquire
resources, register cleanup, choose policy, attach routes, validate, then start.
Helpers do not load environment variables, select host brands or grant authority.

| Package | Shared responsibility |
| --- | --- |
| [`access`](access/README.md) | Transactional policy runtime and current-session management routes |
| [`http`](http/README.md) | HTTP telemetry/logging boundary, exact-origin CORS and local email review |
| [`providers`](providers/README.md) | Optional payment and OAuth provider composition |
| [`publication`](publication/README.md) | Public article HTML, sitemap eligibility and IndexNow runtime composition |
| [`partners`](partners/README.md) | Partner runtime, authenticated HTTP assembly, worker drain and bounded commands |

Use existing owner helpers directly when they already supply the integration:
`emailmanager/helper`, `middleware/helper`, `repository/helpers`, `oauth/helper`
and `notifier.NewStandardSenders`. This namespace composes owners; it does not
replace their business rules or persistence contracts. Application configuration,
identity admission, consent, presentation and product rules remain in the host.

## Resource lifecycle

`Lifecycle` records resources acquired during startup and releases them in
reverse order. Register each resource immediately after acquisition, together
with drain functions for workers that borrow it. On startup failure, call
`Close` to roll back the resources already acquired. Call it on normal shutdown
as well; repeated or concurrent calls do not repeat callbacks.

```go
life := serverhelpers.NewLifecycle(serverhelpers.LifecycleConfig{
    Timeout: 15 * time.Second,
    Telemetry: telemetryShutdown,
})
// After a store has been acquired and its worker started:
if err := life.AddResource(store.Close, worker.Drain); err != nil {
    return err
}
// On startup failure or normal shutdown:
err := life.Close(ctx)
```

Each callback receives a fresh timeout derived from the supplied context's
values, even when that context has been cancelled. Drains run before their
resource is closed. If a drain fails or exhausts its budget, the dependent
resource is left open; other resources are still cleaned up. Telemetry is
attempted last with a separate budget, including when a callback panics. A panic
continues after telemetry shutdown. Returned errors use fixed phase messages and
omit callback error text while preserving causes through `errors.Is`/`errors.As`.
After a panic, a later `Close` reports interrupted cleanup rather than success. Callbacks must honor cancellation; the lifecycle does
not abandon goroutines to force a timeout. Register only during startup, before
`Close` begins. A nil context is treated as `context.Background()`.

The runnable `examples/serverhelpers` package demonstrates resolved typed
configuration, an offline handler and explicit cleanup. Child packages provide
specific integrations; the lifecycle is not a service container.
