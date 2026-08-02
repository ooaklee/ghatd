# MongoDB Migrator

The `external/migrator/mongo` package provides GHATD's reusable Cobra command
for creating, applying, and reverting MongoDB migrations. It owns connection
settings and execution safety while leaving migration registration and the
template in the host application.

## Ownership Boundary

| Concern | Owner |
|---|---|
| Cobra `new`, `up`, and `down` actions | `external/migrator/mongo` |
| MongoDB URI construction, connection, ping, timeouts, and cleanup | `external/migrator/mongo` |
| Migration-history collection and isolated runner | `external/migrator/mongo` |
| Migration registrations and their order | Host application |
| `migrations/mongo/template.go` and generated migration files | Host application |
| Command registration on the root Cobra command | Host application |

This boundary keeps migrations application-specific without duplicating the
operational runner in every GHATD host.

## Quick Start

Keep a thin command adapter in the host. The blank import is required because
`mongo-migrate` registrations run from migration-package `init` functions.

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

Attach the adapter to the host's Cobra root:

```go
rootCmd.AddCommand(migrator.NewCommand())
```

The migration directory must contain `template.go`. Create migrations from the
repository root so the default relative path resolves correctly:

```sh
asdf exec go run main.go mongo-migrator new create-users
```

The command copies the template to a UTC timestamped file such as
`migrations/mongo/20260802143000_create-users.go`. It does not connect to
MongoDB.

Implement and register the migration in that host-owned file:

```go
package migrations

import (
    "context"

    migrate "github.com/xakep666/mongo-migrate"
    "go.mongodb.org/mongo-driver/v2/mongo"
)

func init() {
    if err := migrate.Register(
        func(ctx context.Context, db *mongo.Database) error {
            // Apply the migration.
            return nil
        },
        func(ctx context.Context, db *mongo.Database) error {
            // Revert the migration.
            return nil
        },
    ); err != nil {
        panic(err)
    }
}
```

Apply all pending registrations:

```sh
asdf exec go run main.go mongo-migrator up
```

## Command Behaviour

| Action | Database connection | Behaviour |
|---|---|---|
| `new <name>` | Never | Copies `template.go` to a new UTC timestamped Go file. |
| `up` | Only when migrations are registered | Applies all available pending migrations. |
| `down` | Only when migrations are registered | Reverts all applied migrations. |

`down` is intentionally broad: the shared command does not currently support a
single-step rollback, target version, status, plan, or dry-run action. Review
every registered down function, confirm backups, and test rollback in a safe
environment before using it against important data.

If no migrations are registered, `up` and `down` complete successfully without
opening a MongoDB connection. Settings are still loaded and validated before
the no-op is detected.

The command returns errors through Cobra and never terminates the host process
itself. The host root decides how to log the error and which exit code to use.

### Migration name rules

Names must:

- contain at most 128 characters;
- start and end with a lowercase letter or number;
- contain only lowercase letters, numbers, single hyphens, or single
  underscores between segments.

Examples of valid names are `create-users`, `add_v2_indexes`, and `seed-2026`.
Spaces, uppercase letters, path separators, dots, repeated separators, and
empty names are rejected. Creation uses exclusive file access, so a file with
the same timestamp and name is never overwritten. A partially written file is
removed if copying or closing fails.

The command does not create the migration directory or template. A missing
directory or `template.go` returns an error.

## Settings

`NewCommand()` loads settings from the environment when an action runs.
Existing `MONGO_DB_*` variables remain compatible, and migration-specific
settings are optional.

| Environment variable | Default | Purpose |
|---|---|---|
| `ENVIRONMENT` | `local` | Host environment label. |
| `MONGO_DB_USERNAME` | `mongoadmin` | MongoDB username. |
| `MONGO_DB_PASSWORD` | `secret` | MongoDB password for local defaults. |
| `MONGO_DB_HOST` | `127.0.0.1:27027` | MongoDB host and port, or Atlas host. |
| `MONGO_DB_NAME` | `local` | Database containing application data and migration history. |
| `MONGO_DB_CONNECTION_POOL` | `5` | Maximum migration client pool size; must be positive. |
| `MONGO_DB_ATLAS` | `false` | Generate an Atlas SRV URI when true. |
| `MONGO_DB_APP_NAME` | empty | Optional MongoDB application name. |
| `MONGO_MIGRATION_DIRECTORY` | `./migrations/mongo` | Host migration and template directory. |
| `MONGO_MIGRATION_COLLECTION` | `migrations` | MongoDB migration-history collection. |
| `MONGO_MIGRATION_TIMEOUT` | `60s` | Connection, ping, and migration deadline; must be positive. |
| `MONGO_DISCONNECT_TIMEOUT` | `10s` | Client cleanup deadline; must be positive. |

The credential defaults are intended only for the repository's local
development stack. Supply deployment credentials through the host's normal
secret-management mechanism.

For `new`, only the migration directory is required. Database actions also
require a non-empty database name and migration collection plus positive pool
and timeout values. MongoDB URI generation validates the remaining connection
settings.

## Configuration Options

Override only the host-owned migration path while retaining environment-loaded
database settings:

```go
command := mongomigrator.NewCommand(
    mongomigrator.WithMigrationDirectory("./migrations"),
)
```

Hosts that already load a complete configuration can bypass environment
loading:

```go
settings, err := mongomigrator.NewSettings()
if err != nil {
    return err
}

command := mongomigrator.NewCommand(
    mongomigrator.WithSettings(*settings),
)
```

`WithSettings` replaces the loader; it does not merge a partial value with
defaults. Command options are applied in order. To override the directory on a
complete settings value, pass `WithSettings` first and
`WithMigrationDirectory` second.

## Execution and Failure Semantics

Each database action:

1. snapshots the registered migrations;
2. builds the MongoDB URI with `external/repository/helpers`;
3. connects with the configured timeout and maximum pool size;
4. pings MongoDB before running anything;
5. constructs an isolated `mongo-migrate` runner for the configured database,
   registrations, and history collection;
6. applies or reverts all available migrations within the migration timeout;
   and
7. disconnects with an independent cleanup timeout.

The isolated runner avoids replacing `mongo-migrate`'s process-global database
or history-collection configuration. Registration remains library-global, so
hosts should register each migration once through their migration package.

A disconnect failure is joined with any action failure so cleanup errors are
not lost. Error messages identify settings, URI, connection, ping, execution,
and disconnect stages without logging credentials.

## Generated Applications

`ghatdcli new` copies these host-owned files into a generated application:

- `cmd/mongo-migrator/`;
- `migrations/mongo/template.go`; and
- the root command registration in `main.go`.

The generator rewrites command and migration imports to the generated module,
while the adapter continues to import GHATD's shared
`external/migrator/mongo` implementation. A newly generated application has no
active migration registrations, so `up` and `down` are safe no-ops until the
host adds migration files.

Existing generated applications are not rewritten automatically. Adopt the
adapter and scaffold manually when upgrading an older host.

## Compatibility Notes for Older Hosts

- `cmd/mongo-migrator/settings` was removed. Import
  `external/migrator/mongo` and use `Settings` or `NewSettings` there.
- `cmd/mongo-migrator.MongoMigrationDirectory` and
  `MongoMigrationCollection` were replaced by
  `mongo.DefaultMigrationDirectory` and `mongo.DefaultMigrationCollection`.
- Direct consumers that need command options should construct
  `external/migrator/mongo.NewCommand`; the root `cmd/mongo-migrator` adapter
  intentionally exposes only the repository's standard configuration.
- The old command opened MongoDB even for `new`. The shared command creates
  files offline and connects only for registered `up` or `down` work.
- Migration files and registration order remain host-owned. Moving the runner
  does not discover package migrations automatically.

See [Managing MongoDB Migrations](../../../docs/how-to/manage-mongodb-migrations.md)
for the cross-package workflow and [ADR018](../../../docs/adr/adr018-shared-mongodb-migrator-command.md)
for the architectural decision.

## Testing

Run the focused package and adapter tests:

```sh
asdf exec go test ./external/migrator/mongo/... ./cmd/mongo-migrator/...
```

Run CLI generation tests when changing the scaffold or import rewriting:

```sh
asdf exec go test ./cli/cmd/...
```
