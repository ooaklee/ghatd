# starter/v0 - Ejectable Lazy Composition Layer

`starter/v0` is a **minimal, ejectable composition skeleton** for GHATD. It
defines the top-level container types (`Config`, `Repositories`, `Services`,
`Handlers`, `Middleware`, `Stack`, `Cleanup`) that reflect how a GHATD
application is assembled at the `main` package level.

## Philosophy

**starter/v0 is NOT a replacement architecture.** It is a thin, lazy
composition layer over the existing modular GHATD packages
(`external/usermanager`, `external/accessmanager`, `external/billingmanager`,
etc.). It exists to:

- **Reserve the shape** - show new contributors what a wiring looks like
  before they learn every package.
- **Eliminate boilerplate** - provide a single `Stack` value that can be
  populated in `main`.
- **Enable ejection** - when the skeleton no longer fits, copy the files out
  of this package and modify them freely. There is zero framework lock-in.

## Types

| Type            | Purpose                                                  |
|-----------------|----------------------------------------------------------|
| `Config`        | Runtime parameters (port, environment, log level).       |
| `Repositories`  | Data-layer dependency container.                         |
| `Services`      | Business-logic dependency container.                     |
| `Handlers`      | HTTP handler dependency container.                       |
| `Middleware`    | HTTP middleware constructor container.                   |
| `Stack`         | Top-level composition aggregating all containers.        |
| `Cleanup`       | Graceful resource-release function.                      |

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

    stack := starter.Stack{
        Config: cfg,
        // Populate remaining fields when wiring is ready.
    }
    defer func() {
        if stack.Cleanup != nil {
            _ = stack.Cleanup(context.Background())
        }
    }()
}
```

## Ejection

When the skeleton no longer serves your needs:

1. Copy `external/starter/v0/` into your own tree (e.g. `internal/app/`).
2. Update the package path.
3. Modify freely - add fields, remove types, inject concrete dependencies.

No part of the GHATD runtime depends on starter/v0. Removing the import is
always safe.
