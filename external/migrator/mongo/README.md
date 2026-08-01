# MongoDB Migrator

This package provides the shared MongoDB migration command for GHAT(D) host
applications. Connection settings, migration execution, and new-file creation
remain in GHAT(D); each host only owns its migration registrations and template.

## Host wiring

Keep a small command adapter in the host so its migration package is registered
when the application starts:

```go
package migrator

import (
    mongomigrator "github.com/ooaklee/ghatd/external/migrator/mongo"
    _ "github.com/example/host/migrations/mongo"
    "github.com/spf13/cobra"
)

func NewCommand() *cobra.Command {
    return mongomigrator.NewCommand()
}
```

If the host keeps migrations directly in `./migrations` rather than the
default `./migrations/mongo`, override only that host-owned path while retaining
environment-loaded database settings:

```go
func NewCommand() *cobra.Command {
    return mongomigrator.NewCommand(
        mongomigrator.WithMigrationDirectory("./migrations"),
    )
}
```

Add that command to the host's Cobra root, then run:

```bash
go run . mongo-migrator new create-users
go run . mongo-migrator up
go run . mongo-migrator down
```

`new` copies `template.go` from the configured migration directory without
connecting to MongoDB. Migration names may contain lowercase letters, numbers,
hyphens, and underscores. Existing files are never overwritten.

`up` and `down` apply migrations registered through
`github.com/xakep666/mongo-migrate`. If none are registered, the command
completes successfully without connecting to MongoDB. Each execution with
registered migrations uses its own migration runner, so the shared package does
not replace the dependency's process-global database or collection
configuration.

## Settings

`NewCommand()` loads settings from the environment. Existing `MONGO_DB_*`
variables remain supported. The migrator adds these optional settings:

| Environment variable | Default | Purpose |
|---|---|---|
| `MONGO_MIGRATION_DIRECTORY` | `./migrations/mongo` | Host migration and template directory |
| `MONGO_MIGRATION_COLLECTION` | `migrations` | MongoDB migration-history collection |
| `MONGO_MIGRATION_TIMEOUT` | `60s` | Connection, ping, and migration deadline |
| `MONGO_DISCONNECT_TIMEOUT` | `10s` | Client cleanup deadline |

Hosts that already load the complete configuration in code can pass it
directly. `WithSettings` replaces environment loading, so do not pass an empty
or partial `Settings` value:

```go
settings, err := mongomigrator.NewSettings()
if err != nil {
    return err
}

command := mongomigrator.NewCommand(mongomigrator.WithSettings(*settings))
```
