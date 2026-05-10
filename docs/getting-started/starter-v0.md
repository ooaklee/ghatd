# starter/v0

> An ejectable, lazy composition layer over modular GHATD packages.

## Overview

`external/starter/v0` provides the container types that describe how a GHATD
application is wired together at the top level. It is **not** a framework,
nor does it replace the existing package architecture. Instead it reserves
the shape so that:

- New contributors can understand the application structure at a glance.
- The `main` package has a single `Stack` value to populate instead of a
  dozen ad-hoc variables.
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
| `Middleware` | struct         | HTTP middleware constructor container.             |

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
