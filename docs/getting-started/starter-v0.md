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
| `Repositories` | struct       | Data-layer dependency container.                   |
| `Services`   | struct         | Business-logic dependency container.               |
| `Handlers`   | struct         | HTTP handler dependency container.                 |
| `Middleware` | struct         | Accessmanager middleware suite container.          |

## Constructor Flow

The common Lazy path is:

1. Build third-party and app-specific dependencies in `main`.
2. Call `starter.NewRepositories` with a core Mongo repository.
3. Call `starter.NewServices` with repositories plus explicit Redis/email/OAuth/policy/payment inputs.
4. Call `starter.NewHandlers` with services and a validator.
5. Call `starter.NewMiddleware` with services.
6. Group the results with `starter.NewStack`.

The starter package intentionally does not create Redis clients, email
providers, OAuth providers, payment provider clients, validators, or cleanup
logic. Those remain visible and replaceable in the host application.

## Escape Hatches

Each constructor accepts a request struct so projects can override only the
piece they need:

- `NewRepositoriesRequest` accepts per-repository overrides.
- `NewServicesRequest` accepts custom policy stores, group/user config,
  audit services, notifier senders, OAuth services, payment registries, payment
  providers, and post tag configuration. `ValidPostTags: nil` uses GHATD
  defaults, while `ValidPostTags: []string{}` intentionally disables them.
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
- [Architecture Decision Records](../adr/)
