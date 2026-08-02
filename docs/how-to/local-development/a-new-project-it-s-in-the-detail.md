# Getting Started With A New Project: It's in the Details


With [**`Details`**](../../about-details.md), you can transform your ideas and hobbies into real, tangible products in no time! These building blocks are designed to help you demo and even ship your creations faster than ever.

## Prerequisites

- The repository's pinned Go toolchain installed with `asdf`.
- The [GHATD CLI](../../../README.md#using-the-cli-experimental), either built locally or run from source.
- For `web-vite` details, Node.js and the package manager used by the detail.

## Steps

- Choose one or more compatible Detail repositories. See [About Details](../../about-details.md) for the supported types and repository formats.
  
- Generate a new web app based on your chosen **detail(s)** (`GHAT(D)` gives you the option to merge multiple details into one web app (feature still in alpha)). In the terminal, run:


```shell
ghatdcli new -n [PROJECT_NAME] -m [PROJECT_MODULE_PATH] -w [COMMA_SEPARATED_LINK_TO_DETAIL(S)] -o [DESTINATION_DIR]
```

> Remember to replace the placeholders in the command above! 
>
> - It is possible to ignore `[DESTINATION_DIR]` if you want the new app to be generated in the same folder `ghatdcli` is being used in.
> - Detail links can use `owner/repo`, `github.com/owner/repo`, `https://github.com/owner/repo`, or SSH-style GitHub sources.

- Run your new web app:

```shell
cd [PROJECT_NAME]
asdf exec go mod tidy
asdf exec go run main.go start-server
```

- When a generated app includes a `web-vite` detail, build the SPA before running a production-style Go binary:

```shell
cd [PROJECT_NAME]
npm install
npm run build
asdf exec go run main.go start-server
```

- Review the generated MongoDB migration scaffold:

```text
cmd/mongo-migrator/migrator.go
migrations/mongo/template.go
```

The adapter blank-imports the generated application's migration package and
uses GHATD's shared runner. Create and apply a migration from the generated
repository root with:

```sh
asdf exec go run main.go mongo-migrator new add-initial-indexes
asdf exec go run main.go mongo-migrator up
```

Do not use `mongo-migrator down` casually: it currently reverts all applied
registered migrations. See [Managing MongoDB Migrations](../manage-mongodb-migrations.md)
for registration, configuration, rollback, and troubleshooting guidance.

> For the best developer experience we recommend using [`reflex`](https://github.com/cespare/reflex).

## Additional context
