# About Details

**Details** are "building blocks" that reduce cognitive load and make getting started easier. A **`detail`** is a boilerplate application slice that can function within a GHATD host application and, where practical, on its own.

GHATD currently supports these detail types:

| Type | Purpose |
|------|---------|
| `api` | Adds Go API routes, handlers, services, repositories, and tests into the generated host application. |
| `web` | Adds a Go web detail with GHATD-managed embedded assets and route wiring. |
| `web-vite` | Adds a Vite SPA under the generated app's `web/` directory and wires it through GHATD's SPA server support. |

## Running Details Independently

Details should be able to function independently, allowing users to work on them without considering other components. To get started, find the **detail** you need and clone it to your local machine. Depending on the type of detail you choose, as specified in the `ghatd-conf.yaml`, you should be able to run the equivalent of:

```shell
asdf exec go run [DETAIL_TYPE].go
```

For `web-vite` details, install the frontend dependencies and run the package's Vite scripts from the detail root.

`web-vite` details do not copy a Go web adapter into `internal/web`. That path is reserved for the older Go-template style web details; SPA assets belong under the generated app's root `web/` directory.

## Installing Details

By using the `ghatdcli new` command, it will handle cloning referenced **detail** boilerplate, configuring dependencies, and merging details into a consolidated GHATD host application. GitHub detail sources can use `owner/repo`, `github.com/owner/repo`, `https://github.com/owner/repo`, or SSH-style GitHub sources.

The generated host also receives its own `cmd/mongo-migrator` adapter and
`migrations/mongo/template.go`. The generator rewrites host-owned command and
migration imports to the new module while retaining GHATD's shared migrator
dependency. Existing generated applications are not updated automatically; see
[Managing MongoDB Migrations](./how-to/manage-mongodb-migrations.md) when
adopting the scaffold manually.

When the GitHub CLI is available, GHATD prefers it for cloning so private repositories can use the developer's existing `gh auth login` session. It falls back to `git clone` for non-GitHub sources or when the GitHub CLI is unavailable.
