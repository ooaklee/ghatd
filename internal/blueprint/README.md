# Blueprint

`internal/blueprint` is a small reference package for GHATD package structure. It is intentionally simple, but it should look and behave like production packages such as `external/group` and `external/streaker`.

Use it as a guide for:

- domain constants, errors, and reply error maps
- request, response, and model boundaries
- service-level validation and repository orchestration
- MongoDB repository setup with cached collection initialisation and retry behaviour
- package-owned migrations for collection indexes
- package-owned registries for reusable examples or extension points
- table tests that cover good and bad behaviour
- HTTP routes, handlers, and fender request mapping

## Repository Pattern

`Repository.GetBlueprintCollection` follows the standard GHATD MongoDB connection pattern:

1. lock collection initialisation
2. reuse the cached collection when it already exists
3. call `InitialiseClient`
4. resolve the configured database with `GetDatabase(ctx, "")`
5. cache `db.Collection(BlueprintCollection)`
6. retry transient initialisation failures up to the configured limit

This keeps host applications from repeating connection-management code in every feature package.

## Registry Pattern

`Registry` owns blueprint registrations by key. The service receives a registry, or creates an empty one by default, so host applications can choose between:

- the package-created empty registry
- a custom registry
- registrations added during application composition

Registrations normalise keys, names, and kinds before storage. Duplicate keys return a package error instead of silently replacing existing entries.

## Endpoint Pattern

Blueprint exposes a small v1 route set:

- `GET /api/v1/blueprints`
- `POST /api/v1/blueprints`
- `GET /api/v1/blueprints/{blueprintId}`

The list endpoint demonstrates query decoding through `query` tags. Create is an
administrator-session route; list and get-by-ID are authenticated-session routes.
Create and get-by-ID bind their actor from verified middleware context before
passing the request to the service. No update or delete HTTP route is registered.

## ActorID migration

Use this convention when copying the template into a new domain:

| Request | Previous caller field | Current caller field |
| --- | --- | --- |
| Create | `CreatedByUserID` | `ActorID` |
| Get by ID | `UserID` | `ActorID` |
| Update | `UpdatedByUserID` | `ActorID` |
| Delete | None | Required `ActorID` |

These are source changes to the reference package, not stored-schema changes.
The model's `CreatedByUserID` and `UpdatedByUserID` remain audit attribution;
`ID` still selects the record. Rename request literals and adapters, not stored
fields. Actor fields use `json:"-"` with no query/path tag: this query decoder
interprets `query:"-"` as a literal parameter, not an exclusion marker.

HTTP actor binding requires both an authenticated flag and a nonempty caller ID.
An ID-only context or anonymous placeholder is insufficient. All actor-bearing
service commands reject empty/padded IDs and disagreement with published caller
or cached-user context. Trusted in-process calls may supply an actor on a bare
context, but their integrating manager must establish permission first. An actor
string does not prove ownership or administrator access. Natural-key and list
queries remain actor-independent lower-domain operations; their integrating
manager or route middleware owns admission. See the
[request-identity guide](../../docs/how-to/request-identity.md).

## Service boundaries

Service entry checks reject nil contexts, absent command pointers, cancelled
contexts and nil/typed-nil repositories before invoking dependencies. List retains
its nil-request convention for an unfiltered query. A missing registry does not
disable CRUD; registry operations fail explicitly when their wiring is absent.

Selected-record and create/update results must match the expected identity;
create/update attribution must match the caller. Invalid wiring/results return
`ErrBlueprintUnavailable` (`BLP0-013`, HTTP 503), not record-not-found. Native
dependency errors remain intact for shared response mappings. A failed or invalid
write result is **not** evidence of rollback; the service never retries writes.

Updates copy the selected record before editing scalar fields. Nonempty strings
replace existing values; empty strings retain them. Supplied metadata replaces
the map. Nested metadata is read-only during a call, not deeply copied. List and
count receive separate scalar query copies and are independent reads, not a
transactional snapshot. These checks do not add ownership policy, concurrent
update protection, durable deletion attribution or Mongo matched-count checks.

Mapper and service logging avoids raw payloads and dependency diagnostics. The
composed HTTP tests verify these layers with recording adapters; this is not a
claim that every concrete persistence adapter has been hardened.

Error responses use `Handler.NewHTTPErrorResponse`, backed by the shared
[manifest writer](../../external/errormanifest/README.md#wrapped-errors-at-http-boundaries).
`responseManifests` supplies the same domain map and last-wins host overrides to
both error responses and the existing reply success factory. Expected failures
must be mapped; wrappers preserve those entries and all-mapped validation joins
retain every failure. Unknown joined causes cannot disappear behind a mapped
client error. Keep native causes for internal inspection, and never copy raw
diagnostics into manifest fields or response metadata.

## Migration Pattern

`internal/blueprint/migrations` contains the indexes owned by this package. A
host application can register `InitBlueprintIndexesUp` and
`InitBlueprintIndexesDown` from its `migrations/mongo` package, ensure the
`cmd/mongo-migrator` adapter blank-imports that host package, and apply pending
registrations with:

```sh
asdf exec go run main.go mongo-migrator up
```

The shared `down` action reverts every applied registered migration, not only
Blueprint indexes. See
[Managing MongoDB Migrations](../../docs/how-to/manage-mongodb-migrations.md)
for the reusable registration adapter and rollback precautions.

## Testing Pattern

Use named table-driven cases for related successful and failing behaviour. The
actor, mapper, handler and service suites cover verified and denied identities,
transport spoofing, selected-record failures, scalar-copy guarantees and native
error-map propagation. The single Mongo CRUD lifecycle is intentionally sequential:
each operation must observe the record created or updated in the prior step.

Set `GHATD_TEST_MONGO_URI` to a disposable Mongo instance to make that lifecycle
mandatory; an explicit unavailable URI fails rather than skipping. Without it,
the test may start optional local Mongo or skip when that runtime is unavailable.
The test uses and cleans up a unique database. Run:

```sh
asdf exec go test -race ./internal/blueprint/...
```

Remove unused layers when creating a smaller package. The goal is a clean package boundary, not a required file checklist.
