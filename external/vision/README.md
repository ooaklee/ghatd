# Vision

`internal/vision` is a small reference package for GHATD package structure. It is intentionally simple, but it should look and behave like production packages such as `external/group` and `external/streaker`.

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

`Repository.GetVisionCollection` follows the standard GHATD MongoDB connection pattern:

1. lock collection initialisation
2. reuse the cached collection when it already exists
3. call `InitialiseClient`
4. resolve the configured database with `GetDatabase(ctx, "")`
5. cache `db.Collection(VisionCollection)`
6. retry transient initialisation failures up to the configured limit

This keeps host applications from repeating connection-management code in every feature package.

## Registry Pattern

`Registry` owns vision registrations by key. The service receives a registry, or creates an empty one by default, so host applications can choose between:

- the package-created empty registry
- a custom registry
- registrations added during application composition

Registrations normalise keys, names, and kinds before storage. Duplicate keys return a package error instead of silently replacing existing entries.

## Endpoint Pattern

Vision exposes a small v1 route set:

- `GET /api/v1/visions`
- `POST /api/v1/visions`
- `GET /api/v1/visions/{visionID}`

The list endpoint demonstrates query decoding through `query` tags. The authenticated get-by-ID endpoint demonstrates pulling the requestor ID from middleware-populated context in fender code before passing the request to the service.

## Migration Pattern

`internal/vision/migrations` contains the indexes owned by this package. Host applications can import and register these migration functions as part of their application migration setup.

## Testing Pattern

The tests are table-driven and cover both successful and failing behaviour. When adding new package behaviour, prefer adding focused table cases before adding broad integration tests.

Remove unused layers when creating a smaller package. The goal is a clean package boundary, not a required file checklist.
