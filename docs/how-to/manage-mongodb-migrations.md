# Managing MongoDB Migrations

This guide shows how a GHATD host application creates, registers, applies, and
reverts MongoDB migrations with the shared `external/migrator/mongo` command.

For the command API, settings table, and compatibility notes, see the
[MongoDB Migrator package guide](../../external/migrator/mongo/README.md).

## 1. Add the Host-Owned Scaffold

Keep these paths in the host application:

```text
cmd/mongo-migrator/migrator.go
migrations/mongo/template.go
main.go
```

The command adapter must blank-import the host migration package before it
returns the shared command:

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

Register that command once on the root Cobra command:

```go
rootCmd.AddCommand(migrator.NewCommand())
```

Applications created by `ghatdcli new` already contain this scaffold with
host-owned imports rewritten to their module path.

## 2. Create a Migration

Run the generator from the repository root:

```sh
asdf exec go run main.go mongo-migrator new add-group-indexes
```

The name must contain only lowercase letters, numbers, single hyphens, and
single underscores, with a maximum length of 128 characters. The command copies
`migrations/mongo/template.go` into a new UTC timestamped file and never opens
a database connection.

If the host uses another directory, set `MONGO_MIGRATION_DIRECTORY` or construct
the shared command with `WithMigrationDirectory`. Relative paths resolve from
the process working directory.

## 3. Register Up and Down Functions

GHATD packages expose migration helpers, but the host decides which migrations
belong to the application and in which registration order.

For helpers that accept `*mongo.Database`, adapt them to the context-aware
`mongo-migrate` signature:

```go
package migrations

import (
    "context"

    groupmigrations "github.com/ooaklee/ghatd/external/group/migrations"
    migrate "github.com/xakep666/mongo-migrate"
    "go.mongodb.org/mongo-driver/v2/mongo"
)

func init() {
    register := func(up, down func(*mongo.Database) error) error {
        return migrate.Register(
            func(_ context.Context, db *mongo.Database) error { return up(db) },
            func(_ context.Context, db *mongo.Database) error { return down(db) },
        )
    }

    if err := register(
        groupmigrations.InitGroupsIndexesUp,
        groupmigrations.InitGroupsIndexesDown,
    ); err != nil {
        panic(err)
    }
    if err := register(
        groupmigrations.InitGroupsLineageIndexUp,
        groupmigrations.InitGroupsLineageIndexDown,
    ); err != nil {
        panic(err)
    }
}
```

For a custom migration, put the database operations directly inside the two
functions passed to `migrate.Register`. Make each up function safe to retry
where practical and ensure its down function reverses only the resources that
migration owns.

Registration happens during package initialisation. Do not also register the
same migration from server startup or another imported package.

## 4. Configure the Database

The shared command retains the existing `MONGO_DB_*` environment contract. A
typical deployment supplies at least:

```sh
export MONGO_DB_USERNAME="<mongo-username>"
export MONGO_DB_PASSWORD="<mongo-password>"
export MONGO_DB_HOST="<mongo-host>"
export MONGO_DB_NAME="<mongo-database>"
```

Atlas deployments also set `MONGO_DB_ATLAS=true` and may set
`MONGO_DB_APP_NAME`. Optional migration controls include:

```sh
export MONGO_MIGRATION_COLLECTION="migrations"
export MONGO_MIGRATION_TIMEOUT="60s"
export MONGO_DISCONNECT_TIMEOUT="10s"
```

Keep real credentials in the deployment's secret manager rather than source
control or shell-history examples.

## 5. Apply Pending Migrations

Run migrations as an explicit deployment or maintenance step before starting
application instances that depend on the new schema or seed data:

```sh
asdf exec go run main.go mongo-migrator up
```

`up` applies every pending registered migration. The history collection
records which versions have run. When no migrations are registered, the
command reports success without opening a MongoDB connection.

## 6. Revert Migrations Carefully

```sh
asdf exec go run main.go mongo-migrator down
```

The current `down` action reverts all applied registered migrations. There is
no single-step, target-version, status, plan, or dry-run command. Before using
it against important data:

1. inspect every registered down function;
2. confirm a current backup and restore procedure;
3. test the complete rollback against a disposable database; and
4. stop application writes when a migration requires exclusive access.

Prefer a forward corrective migration when reversing all history would be too
destructive.

## Troubleshooting

### The command succeeds but no indexes or data change

Confirm the command adapter blank-imports the host's `migrations/mongo` package
and that the expected files call `migrate.Register`. An empty registry is a
successful no-op by design.

### `new` cannot find `template.go`

Run from the repository root, check `MONGO_MIGRATION_DIRECTORY`, and ensure the
configured directory contains `template.go`. The command does not create the
directory or template automatically.

### A migration file already exists

The generator uses a UTC timestamp with one-second precision and exclusive file
creation. Wait for the next second or choose another valid name; the existing
file will not be overwritten.

### Connection or ping times out

Verify the `MONGO_DB_*` values, network access, Atlas mode, and the configured
`MONGO_MIGRATION_TIMEOUT`. The same deadline covers connection, ping, and
migration work.

### An older host no longer compiles

Replace imports from `cmd/mongo-migrator/settings` with
`external/migrator/mongo`. The old command constants are now
`DefaultMigrationDirectory` and `DefaultMigrationCollection` in the shared
package. Existing generated applications must adopt the thin adapter and
migration scaffold manually.

## Validation

Test migration up and down functions against an isolated MongoDB instance, then
run the shared command tests:

```sh
asdf exec go test ./external/migrator/mongo/... ./cmd/mongo-migrator/...
```

When changing generated-app scaffolding, also run:

```sh
asdf exec go test ./cli/cmd/...
```
