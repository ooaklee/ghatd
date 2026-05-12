# starter/v0 - Ejectable Lazy Composition Layer

`starter/v0` is a **minimal, ejectable composition layer** for GHATD. It
defines the top-level container types (`Config`, `Repositories`, `Services`,
`Handlers`, `Middleware`, `Stack`, `Cleanup`) and constructors that reflect how
a GHATD application is assembled at the `main` package level.

## Philosophy

**starter/v0 is NOT a replacement architecture.** It is a thin, lazy
composition layer over the existing modular GHATD packages
(`external/usermanager`, `external/accessmanager`, `external/billingmanager`,
etc.). It exists to:

- **Reserve the shape** - show new contributors what wiring looks like before
  they learn every package.
- **Eliminate boilerplate** - provide constructors for the common GHATD
  repository, service, handler, and middleware layers.
- **Enable ejection** - when the skeleton no longer fits, copy the files out
  of this package and modify them freely. There is zero framework lock-in.

## Types

| Type            | Purpose                                                  |
|-----------------|----------------------------------------------------------|
| `Config`        | Runtime parameters (port, environment, log level).       |
| `Repositories`  | Data-layer dependency container and Mongo repository wiring. |
| `Services`      | Business-logic dependency container and manager service wiring. |
| `Handlers`      | HTTP handler dependency container.                       |
| `Middleware`    | Access middleware suite container.                       |
| `Stack`         | Top-level composition aggregating all containers.        |
| `Cleanup`       | Graceful resource-release function.                      |
| `CleanupGroup`  | Aggregates multiple `Cleanup` functions into one.        |
| `RouteGroup`    | String enum identifying a standard API route group for `AttachDefaultRoutes`. |

## Constructors

| Function               | Purpose                                      |
|------------------------|----------------------------------------------|
| `NewRepositories`      | Builds package repositories from a core Mongo repository, with per-repository overrides. |
| `NewServices`          | Builds GHATD services from repositories and explicit app integrations such as Redis, email, audit, OAuth, policy, and payment providers. |
| `NewHandlers`          | Builds standard GHATD handlers with default error bundles and explicit override hooks. |
| `NewMiddleware`        | Builds the accessmanager middleware suite.   |
| `NewStack`             | Validates config and groups already-built layers. |
| `AttachDefaultRoutes`  | Attaches standard GHATD API routes from `Stack.Handlers` and `Stack.Middleware` to a router. |

## CleanupGroup

`CleanupGroup` aggregates multiple `Cleanup` functions into a single `Cleanup`.
It is useful when multiple resources (database connections, Redis clients,
temporary credential files, background goroutines) need independent teardown.

```go
var cleanupGroup starter.CleanupGroup
cleanupGroup.Add(mongoHandler.Close)
cleanupGroup.Add(func(ctx context.Context) error {
    return redisClient.Close()
})
cleanupGroup.Add(nil) // silently ignored

stack, _ := starter.NewStack(&starter.NewStackRequest{
    Config:   cfg,
    Cleanup:  cleanupGroup.Run, // assignable to Stack.Cleanup
})
```

- `Add(fns ...Cleanup)` appends non-nil cleanups in insertion order.
- `Run(ctx)` invokes every registered cleanup, collects all errors with
  `errors.Join`, and always runs every cleanup even when earlier ones fail.
- `CleanupGroup` implements the `Cleanup` signature via its `Run` method, so
  it can be assigned directly to `Stack.Cleanup`.

Starter creates GHATD components, but it does not create third-party clients.
Mongo handlers, Redis stores, email managers, OAuth providers, payment
providers, validators, and cleanup remain visible in the host application. The
service layer only requires accessmanager's ephemeral-store contract; the
middleware layer can additionally accept a `HardenedRateLimitStore` override
when hardened rate limiting uses a different store.

For a fuller GHATD host application server-command walkthrough, see
[`docs/getting-started/starter-v0-host-application-style.md`](../../../docs/getting-started/starter-v0-host-application-style.md).

`NewStack` intentionally accepts nil layer fields so teams can adopt starter/v0
incrementally. Treat nil layers as "not wired yet" and check them before use.

## Usage

```go
package main

import (
    "context"

    "github.com/ooaklee/ghatd/external/starter/v0"
)

func main() {
    cfg := starter.Config{
        Port:        8080,
        Environment: "local",
        LogLevel:    "debug",
    }
    if err := cfg.Validate(); err != nil {
        panic(err)
    }

    repositories, err := starter.NewRepositories(&starter.NewRepositoriesRequest{
        Core: coreRepository,
    })
    if err != nil {
        panic(err)
    }

    services, err := starter.NewServices(&starter.NewServicesRequest{
        Repositories:             repositories,
        EphemeralStore:            ephemeralStore,
        EmailManager:              emailManager,
        AccessTokenSecret:         accessTokenSecret,
        RefreshTokenSecret:        refreshTokenSecret,
        StaticPlaceholderUUID:     staticPlaceholderUUID,
        AuditService:              auditService, // optional; starter creates one when nil.
        AutoAdminEmailAddressRegex: adminEmailRegex,
        ValidPostTags:             nil, // nil uses GHATD defaults; []string{} disables them.
        PolicyConfig: &starter.PolicyConfig{
            BusinessEntityName:      "Example",
            BusinessEntityEmail:     "hello@example.test",
            BusinessEntityWebsite:   "https://example.test",
            LegalBusinessEntityName: "Example Ltd",
            GenerateStaticPolicies:  true,
        },
    })
    if err != nil {
        panic(err)
    }

    handlers, err := starter.NewHandlers(&starter.NewHandlersRequest{
        Services:                 services,
        Validator:                validator,
        Environment:              cfg.Environment,
        CookiePrefixAuthToken:    "auth",
        CookiePrefixRefreshToken: "refresh",
        CookieDomain:             "example.test",
    })
    if err != nil {
        panic(err)
    }

    middleware, err := starter.NewMiddleware(&starter.NewMiddlewareRequest{
        Services:    services,
        Environment: cfg.Environment,
    })
    if err != nil {
        panic(err)
    }

    stack, err := starter.NewStack(&starter.NewStackRequest{
        Config:       cfg,
        Repositories: repositories,
        Services:     services,
        Handlers:     handlers,
        Middleware:   middleware,
    })
    if err != nil {
        panic(err)
    }

    defer func() {
        if stack.Cleanup != nil {
            _ = stack.Cleanup(context.Background())
        }
    }()
}
```

## AttachDefaultRoutes

`AttachDefaultRoutes` attaches every standard GHATD API route group to a
`*router.Router` in a single call, using the handlers and middleware from a
`Stack`. It eliminates the per-package `AttachRoutes` boilerplate while
remaining fully ejectable.

```go
err := starter.AttachDefaultRoutes(&starter.AttachDefaultRoutesRequest{
    Router: httpRouter,
    Stack:  stack,
    Skip:   []starter.RouteGroup{starter.RouteGroupUserManager},
})
if err != nil {
    // handle validation error
}
```

### RouteGroup constants

| Constant                              | Routes attached                                      |
|---------------------------------------|------------------------------------------------------|
| `RouteGroupPricer`                    | `/api/v1/pricing/*`                                  |
| `RouteGroupPolicy`                    | `/api/v1/policies/*`                                 |
| `RouteGroupUser`                      | `/api/v2/users/*`                                    |
| `RouteGroupGroup`                     | `/api/v1/groups/*`                                   |
| `RouteGroupAccessManager`             | `/api/v1/ams/*`                                      |
| `RouteGroupUserManager`               | `/api/v1/ums/*`                                      |
| `RouteGroupContentManager`            | `/api/v1/cms/*`                                      |
| `RouteGroupBillingManager`            | `/api/v1/bms/*`                                      |

### Skip semantics

Groups listed in `Skip` are omitted entirely. A skipped group's handler may be
nil — validation only enforces non-nil handlers for non-skipped groups.
Middleware is validated based on the union of all remaining (non-skipped)
groups, so a shared middleware is still required when at least one non-skipped
group depends on it. If the remaining groups do not need access middleware
(for example, policy-only routing), `Stack.Middleware` may be nil. Unknown
`RouteGroup` values fail validation so typos do not silently attach routes.

### What AttachDefaultRoutes does NOT attach

- SPA routes (catch-all `/` handler) — these remain host-owned.
- Auth verify/CORS middleware — the host owns router bootstrap and middleware.
- Router bootstrap — the host creates the `*router.Router` and starts the HTTP server.

## Ejection

When the skeleton no longer serves your needs:

1. Copy `external/starter/v0/` into your own tree (e.g. `internal/app/`).
2. Update the package path.
3. Modify freely - add fields, remove types, inject concrete dependencies.

No part of the GHATD runtime depends on starter/v0. Removing the import is
always safe.
