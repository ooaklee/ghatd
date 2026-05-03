# group

Universal group management package supporting hierarchical groupings (organisations, departments, teams, squads, etc.) with member management, ownership, visibility controls, and descendant traversal.

## Package Structure

```
group/
├── config.go                     # GroupConfig and hierarchy tree configuration
├── const.go                      # Group types, statuses, roles, visibility constants
├── errormap.go                   # HTTP error code mappings
├── fender.go                     # HTTP request mappers (fender layer)
├── handler.go                    # HTTP handlers
├── model.go                      # Core domain models (UniversalGroup, Member, etc.)
├── repository.go                 # MongoDB repository implementation
├── request.go                    # Service request types
├── response.go                   # Service response types
├── routes.go                     # Route registration
├── service.go                    # Business logic
├── utils.groupfactory.go         # Group construction helpers
├── utils.toolbox.go              # Shared utilities
├── examples/
│   └── basic_usage.go
└── migrations/
    ├── indexes_groups.go
    └── indexes_groups_lineage.go
```

## Running Tests

### Unit & Service Tests

No external dependencies required:

```sh
go test ./external/group -count=1
```

### Integration Tests

Integration tests require a running MongoDB instance. By default they use an embedded [memongo](https://github.com/benweissmann/memongo) server. If memongo is unavailable (e.g. on ARM or in CI without the binary cache), set `GROUP_IT_MONGO_URI` to point at a real MongoDB instance.

**Start a local MongoDB instance with Docker:**

```sh
docker run --rm -p 47027:27017 mongo:4.2.21-bionic
```

**Run all integration tests:**

```sh
export GROUP_IT_MONGO_URI=mongodb://localhost:47027
go test ./external/group -count=1
```

**Run a specific integration test:**

```sh
export GROUP_IT_MONGO_URI=mongodb://localhost:47027
go test ./external/group -run TestIntegration_GroupService_GetGroupsByUserID_WithSampleDataset -v -count=1
```

**Skip integration tests:**

```sh
go test ./external/group -short -count=1
```

### Integration Test Caveats

- `TestIntegration_GroupRepository_FullLifecycle` always uses an in-memory memongo server and skips if memongo cannot start.
- `TestIntegration_GroupService_GetGroupsByUserID_WithSampleDataset` tries memongo first; if unavailable it falls back to `GROUP_IT_MONGO_URI`. If neither is available the test is skipped.
- The `GROUP_IT_MONGO_URI` instance does **not** need to be empty — each test run creates a randomly named database (via `memongo.RandomDatabase()`) and cleans up after itself.
