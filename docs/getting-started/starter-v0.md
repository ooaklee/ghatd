# starter/v0

> An ejectable, lazy composition layer over modular GHATD packages.

## Overview

`external/starter/v0` provides the container types and constructors that
describe how a GHATD application is wired together at the top level. It is
**not** a framework, nor does it replace the existing package architecture.
Instead it reserves the shape so that:

- New contributors can understand the application structure at a glance.
- The `main` package can build repositories, services, handlers, middleware,
  and a final `Stack` without dozens of ad-hoc variables.
- Teams that outgrow the skeleton can copy (eject) the code and customise it
  without any dependency on this package.

## Types

| Symbol       | Kind           | Description                                        |
|--------------|----------------|----------------------------------------------------|
| `Config`     | struct         | Runtime parameters (`Port`, `Environment`, `LogLevel`). |
| `Stack`      | struct         | Top-level composition of every application layer.  |
| `Cleanup`    | func type      | `func(context.Context) error` for graceful teardown.|
| `CleanupGroup` | struct      | Aggregates multiple `Cleanup` functions into one, with `Add` and `Run(ctx)`.|
| `Repositories` | struct       | Data-layer dependency container.                   |
| `Services`   | struct         | Business-logic dependency container.               |
| `Handlers`   | struct         | HTTP handler dependency container.                 |
| `Middleware` | struct         | Accessmanager middleware suite container.          |
| `RouteGroup` | string type    | Typed enum identifying a standard API route group for `AttachDefaultRoutes`. |

## Constructor Flow

The common Lazy path is:

1. Build third-party and app-specific dependencies in `main`.
2. Call `starter.NewRepositories` with a core Mongo repository.
3. Call `starter.NewServices` with repositories plus explicit Redis/email/OAuth/policy/notification/payment inputs.
4. Call `starter.NewHandlers` with services and a validator.
5. Call `starter.NewMiddleware` with services.
6. Group the results with `starter.NewStack`.
7. (Optional) Call `starter.AttachDefaultRoutes` with the stack and router to
   attach every standard GHATD API route group in one call, or attach routes
   individually per package.

The starter package intentionally does not create Redis clients, email
providers, OAuth providers, payment provider clients, validators, or resource
cleanup functions. Those remain visible and replaceable in the host application.
For the lazy path, package-owned helpers such as `repository.NewMongoRuntime`,
`ephemeral.NewRedisRuntime`, `emailprovider.NewSparkPostClient`,
`emailmanager.NewStandardEmailManager`, `spa.NewBootstrap`, and
`router.AttachDefaultAuthVerifyRoute` can reduce repeated host-application
setup without moving that ownership into `starter/v0`. Use
`router.NewAuthVerifyHandler` directly when a project needs custom auth verify
endpoint paths.

`NewRepositories` includes the reminder and streaker Mongo repositories, and
`NewServices` exposes them as `Services.Reminder` and `Services.Streaker` when
those repositories are available. Both services are optional: host applications
may omit them without preventing the rest of the starter stack from running.

When present, starter attaches reminder and streaker to
`Services.UserManager`. The User Manager route group then exposes UMS reminder
and streak endpoints. Streaker still has no standalone starter route group in
v0; host applications own product-specific streak workflows and may call
`Services.Streaker` directly from custom managers, handlers, or jobs.

For a fuller server-command example that mirrors a GHATD host application
setup, see [starter/v0 Host Application Setup](starter-v0-host-application-style.md).

## AttachDefaultRoutes

`AttachDefaultRoutes` is an **optional, ejectable** helper that attaches every
standard GHATD API route group (`/api/v1/pricing`, `/api/v2/users`,
`/api/v1/groups`, `/api/v1/ams`, `/api/v1/ums`, `/api/v1/cms`, `/api/v1/bms`,
`/api/v1/policies`) to a `*router.Router` using the handlers and middleware
from a `Stack`. It does **not** attach SPA routes — those remain the host's
responsibility.

### Usage

```go
err := starter.AttachDefaultRoutes(&starter.AttachDefaultRoutesRequest{
    Router: httpRouter,
    Stack:  stack,
    Skip:   []starter.RouteGroup{starter.RouteGroupUserManager},
})
```

### RouteGroup constants

| Constant                              | Route prefix        |
|---------------------------------------|---------------------|
| `RouteGroupPricer`                    | `/api/v1/pricing`   |
| `RouteGroupPolicy`                    | `/api/v1/policies`  |
| `RouteGroupUser`                      | `/api/v2/users`     |
| `RouteGroupGroup`                     | `/api/v1/groups`    |
| `RouteGroupAccessManager`             | `/api/v1/ams`       |
| `RouteGroupUserManager`               | `/api/v1/ums`, including reminder and streak endpoints |
| `RouteGroupContentManager`            | `/api/v1/cms`       |
| `RouteGroupBillingManager`            | `/api/v1/bms`       |

### Skip semantics

- Skipped groups are not attached and their handler may be nil.
- Middleware validation considers only non-skipped groups. Shared middleware
  is still required when at least one non-skipped group depends on it.
- If the remaining groups do not need middleware, `Stack.Middleware` may be nil.
- Unknown `RouteGroup` values fail validation so typos do not silently attach routes.
- Skipping all groups is valid and attaches no routes.

### Ejection

`AttachDefaultRoutes` calls the same per-package `AttachRoutes` functions that
host applications call directly. If the default wiring no longer fits, replace
the single call with per-package calls or copy the function body.

## CleanupGroup

`CleanupGroup` aggregates multiple host-owned `Cleanup` functions (for example
database close, Redis close, temporary credential removal, or background
goroutine cancellation) into a single `Cleanup`.

- `Add(fns ...Cleanup)` appends non-nil cleanups; nil entries are ignored.
- `Run(ctx)` invokes every registered cleanup in insertion order. Every
  cleanup runs even when earlier ones fail. All errors are collected and joined
  with `errors.Join`.
- `Run` satisfies the `Cleanup` signature, so it can be passed directly as
  `Stack.Cleanup` via `cleanupGroup.Run`.

HTTP server graceful shutdown is handled by `external/http/server` (an
ejectable lifecycle helper with `StartServerWith`) and is not a
`starter/v0` service concern per se.

## Escape Hatches

Each constructor accepts a request struct so projects can override only the
piece they need:

- `NewRepositoriesRequest` accepts per-repository overrides.
- `NewServicesRequest` accepts custom policy stores, group/user config,
  audit services, notifier senders, OAuth services, payment registries, payment
  providers, custom UMS reminder/streak service overrides, and post tag
  configuration. `ValidPostTags: nil` uses GHATD defaults, while
  `ValidPostTags: []string{}` intentionally disables them.
- `NewHandlersRequest` accepts `HandlerErrorMaps`; `nil` uses starter defaults,
  while an empty slice intentionally clears a bundle.
- `NewMiddlewareRequest` accepts custom error maps, rate-limit tuning, and an
  optional `HardenedRateLimitStore` override for middleware-specific storage.

`NewStack` accepts nil layer fields so projects can adopt starter/v0
incrementally. Nil means "not wired yet"; check a layer before dereferencing it.

## Config Validation

`Config.Validate()` uses simple built-in validation so starter/v0 does not
introduce hidden global state or a validation framework dependency.

| Field      | Rule                                  |
|------------|---------------------------------------|
| `Port`     | Required, 1-65535                     |
| `Environment` | Required, `local`/`development`/`staging`/`production` |
| `LogLevel` | Required, `debug`/`info`/`warn`/`error`       |

## Related

- [Package README](../../external/starter/v0/README.md)
- [Host application setup](starter-v0-host-application-style.md)
- [Architecture Decision Records](../adr/)
