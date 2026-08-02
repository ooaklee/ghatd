---
id: adrs-adr018
title: 'ADR018: Share MongoDB Migrator Execution While Keeping Registrations Host-Owned'
# prettier-ignore
description: Architecture Decision Record for moving MongoDB migration command execution into a reusable package while retaining host-owned registrations and templates
date: 2026-08-02
status: accepted
---

## Status

Accepted.

## Context

GHATD host applications need a repeatable way to create and execute MongoDB
migrations. The original `cmd/mongo-migrator` combined several concerns:

- application-specific blank imports and registration order;
- environment settings;
- MongoDB URI construction and connection lifecycle;
- process-global `mongo-migrate` database configuration;
- command parsing; and
- copying the host's migration template.

Copying that command into generated applications duplicated operational code
and made fixes to timeouts, cleanup, driver integration, and file safety
host-specific. Keeping everything inside GHATD's root command instead would
make a reusable package discover application migrations implicitly and would
erase the host's ownership of schema and seed-data history.

The CLI generator also needs a usable migration scaffold in every generated
application. Host command and migration import paths must follow the generated
module, while the reusable runner should remain a GHATD dependency.

## Decision

We will move reusable MongoDB migration execution into
`external/migrator/mongo` and keep `cmd/mongo-migrator` as a thin host adapter.

The shared package will own:

- Cobra `new`, `up`, and `down` subcommands;
- environment-backed `Settings` and complete settings injection;
- optional migration-directory override;
- MongoDB URI generation, connection, ping, timeouts, pool size, and cleanup;
- an isolated `mongo-migrate` runner per database action; and
- safe timestamped file creation from the host template.

The host application will own:

- the `migrations/mongo` package and template;
- blank-importing that package from its adapter;
- choosing and ordering registrations; and
- attaching the adapter to its root command.

`new` will never connect to MongoDB. `up` and `down` will avoid a connection
when the registration snapshot is empty. Database actions will still load and
validate settings before deciding that an empty registry is a no-op.

Migration names will be limited to safe lowercase alphanumeric segments joined
by single hyphens or underscores, with a maximum length of 128 characters.
Creation will use an exclusive file open, will never overwrite an existing
timestamped file, and will remove a partially written file after copy or close
failure.

Database actions will create a local `mongo-migrate` runner from the registered
migration snapshot rather than changing the library's process-global database
or history-collection configuration. The registration mechanism remains
library-global because host migration packages register through
`migrate.Register` during initialisation.

The command will return errors through Cobra instead of terminating the host
process. Disconnect failures will be joined with action failures so cleanup
errors remain visible.

`ghatdcli new` will copy the host command and migration scaffold, rewrite the
host-owned imports to the generated module, and retain the shared migrator
package import. Existing generated applications will not be rewritten
automatically.

## Alternatives Considered

We considered keeping the complete runner in `cmd/mongo-migrator`. That would
avoid a new public package but would continue to duplicate operational fixes in
generated hosts and would make direct reuse awkward.

We considered making GHATD discover and register all package migrations
automatically. That would reduce host wiring but could apply indexes, seeds, or
destructive down functions for packages the application did not intend to own.
Explicit host registration keeps the database history reviewable.

We considered retaining the process-global `mongo-migrate` database and
collection setters. They are simple for a single command but make isolation and
parallel tests harder and can leak configuration between callers in one
process.

We considered connecting before every action, including `new` and empty
registries. File generation does not need a database, and an empty generated
application should be able to run its command without requiring MongoDB.

We considered adding status, dry-run, target-version, and single-step rollback
actions in the same change. Those require additional public semantics and test
coverage. The first shared command preserves the existing all-available `up`
and `down` behaviour.

## Consequences

Host applications share one hardened execution path while retaining complete
control over migration content and order.

The new package is a public extension point. Changes to command names, settings,
defaults, option ordering, error categories, migration-name validation, or
all-available execution semantics require compatibility review and package
documentation updates.

`down` remains intentionally broad and can revert every applied registered
migration. Operators must review rollback functions and backups; applications
that need one-step or target-version rollback require a future command API or a
separate operational tool.

The removed `cmd/mongo-migrator/settings` import path and removed command-level
constants can break direct consumers. They must move to
`external/migrator/mongo.Settings`, `NewSettings`,
`DefaultMigrationDirectory`, and `DefaultMigrationCollection`.

`WithSettings` accepts a complete value rather than merging defaults, and
command options apply in order. A host that combines `WithSettings` with
`WithMigrationDirectory` must pass the directory option last.

An empty registry is indistinguishable from a host that forgot its blank import
unless the host validates expected registrations separately. Documentation and
tests must keep the blank-import requirement prominent.

Generated applications now depend on the presence of the base migration
scaffold during generation. Their command and migration paths are host-owned,
but the shared runner remains versioned with GHATD.

## Regression and Rollout Invariants

Future changes must preserve these boundaries unless a superseding ADR says
otherwise:

- host migrations are never discovered or registered implicitly;
- `new` performs no database connection;
- empty `up` and `down` registries perform no database connection;
- existing files are never overwritten and failed copies leave no partial
  migration file;
- registered migrations execute through an isolated runner and configured
  history collection;
- migration and disconnect deadlines remain independent;
- cleanup failures remain observable alongside action failures;
- Cobra receives errors instead of the shared package exiting the process; and
- generated applications rewrite only host-owned imports while retaining the
  shared package dependency.

Package, adapter, settings, CLI-generation, and full repository tests must cover
these invariants whenever the migrator boundary changes.
