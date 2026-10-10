# Group

The `group` package provides a robust and flexible system for managing user groups, teams, organisations, and other collections. It is built with a "universal model" approach, allowing a single data structure to represent various types of groups with support for hierarchical nesting.

This guide will walk you through the architecture, setup, and common use cases of the `group` package.

## Package Structure

```
group/
├── config.go                     # GroupConfig and hierarchy tree configuration
├── const.go                      # Group types, statuses, roles, visibility constants
├── direct_membership.go          # Bounded current active direct membership
├── direct_membership_test.go     # Owning membership and admission cases
├── direct_membership_mongo_test.go # Explicit native bounded-query contracts
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

## Architecture

The package follows a standard layered architecture common throughout this project:

1.  **Routes (`routes.go`)**: Defines the HTTP API endpoints (e.g., `POST /api/v1/groups`, `GET /api/v1/groups/{id}`). It maps URLs to specific handlers.
2.  **Handler (`handler.go`)**: Receives HTTP requests, parses them, and validates input. It acts as the bridge between the transport layer (HTTP) and the business logic.
3.  **Fender (`fender.go`)**: An authorisation layer that checks if the authenticated user has the necessary permissions to perform an action on a group.
4.  **Service (`service.go`)**: Contains the core business logic. It orchestrates operations like creating, updating, and retrieving groups, and it interacts with the repository.
5.  **Repository (`repository.go`)**: The data access layer. It is responsible for all database operations (Create, Read, Update, Delete) for the `groups` collection in MongoDB.
6.  **Model (`model.go`)**: Defines the `UniversalGroup` data structure and its associated helper methods. This is the core entity of the package.

## Active direct membership capability

`Service.GetActiveDirectGroupIDs(ctx, userID)` is an in-process owning read for
active, nondeleted groups where the selected user is an owner or an accepted
direct `USER` member. Pending invitations (including legacy `invited_at`),
non-user references and inherited descendant administration do not qualify.
Accepted membership has a literally empty invitation state and no pending invite.
Unknown or whitespace-only invitation states do not qualify.
The existing generic access map is not a substitute for this capability.

The optional `DirectMembershipRepository` port supplies bounded current
reference rows. `Repository.GetGroupsByReferencedUserIDBounded` uses a native
`limit+1` probe and the existing owner/member selector; the service applies
membership rules. Its 10,000-reference budget applies before filtering, and an
extra row refuses truncation. Successful empty reads are explicit; malformed or
duplicate evidence, unavailable dependencies, late read failure and cancellation
discard all output. `ErrDirectMembershipUnavailable` retains operational causes;
`ErrDirectMembershipCapacity` requires an explicit owning projection/review,
never a partial or fabricated empty result. Existing unbounded reference reads
keep their selector and sort behavior. This adds no schema or index migration.

This method registers no HTTP route and supplies neither caller authorization
nor financial cohort approval. Hosts authenticate the selected target and
independently enforce privileged rate-bearing cohort admission; ordinary group
creation/joining or administrator role inheritance cannot grant a higher rate.
The result is current evidence from one group query, not historical membership
or an atomic snapshot with a separate policy/grant domain. Do not expose another
customer's group identifiers directly in customer JSON.

Native bounded membership tests use `GHATD_TEST_MONGO_URI`, create independent
fixture databases and drop only those fixtures. They do not use a memongo
fallback and still run with `-short` when that explicit native URI is set.

## Running Tests

### Unit & Service Tests

No external dependencies required:

```sh
asdf exec go test ./external/group -count=1
```

### Integration Tests

Integration tests require a running MongoDB instance. By default they use an embedded [memongo](https://github.com/benweissmann/memongo) server. If memongo is unavailable (e.g. on ARM or in CI without the binary cache), set `GROUP_IT_MONGO_URI` to point at a real MongoDB instance.

**Start a local MongoDB instance with Docker:**

```sh
docker run --rm -p 47027:27017 mongo:7
```

**Run integration tests against it:**

```sh
export GROUP_IT_MONGO_URI=mongodb://localhost:47027
asdf exec go test ./external/group -count=1
```

**Run a specific integration test:**

```sh
export GROUP_IT_MONGO_URI=mongodb://localhost:47027
asdf exec go test ./external/group -run TestIntegration_GroupService_GetGroupsByUserID_WithSampleDataset -v -count=1
```

**Skip integration tests:**

```sh
asdf exec go test ./external/group -short -count=1
```

### Integration Test Caveats

- `TestIntegration_GroupRepository_FullLifecycle` always uses an in-memory memongo server and skips if memongo cannot start.
- `TestIntegration_GroupService_GetGroupsByUserID_WithSampleDataset` tries memongo first; if unavailable it falls back to `GROUP_IT_MONGO_URI`. If neither is available the test is skipped.
- The `GROUP_IT_MONGO_URI` instance does **not** need to be empty — each test run creates a randomly named database (via `memongo.RandomDatabase()`) and cleans up after itself.

## MongoDB Setup & Migrations

The `group` package relies on a MongoDB collection named `groups`. To ensure optimal query performance, a set of database indexes must be created. There are two migration scripts you will need to run.

### Migration 1 — Core Indexes

The first migration script, located at `external/group/migrations/indexes_groups.go`, creates the core indexes for the `groups` collection. These cover all the common query patterns and include:

-   **Unique Indexes**: To enforce uniqueness on `_id` and `_nano_id`.
-   **Standard Indexes**: On individual fields like `name`, `type`, `status`, and `metadata.created_at` for efficient filtering and sorting.
-   **Compound Indexes**: For common multi-field queries, such as filtering by type and status simultaneously.
-   **Ownership Index**: To quickly find groups by their owner using the `owner_id` field.
-   **Member Indexes**: To efficiently find groups a given user or sub-group belongs to (`members.id`, `members.id` + `members.type`).
-   **Visibility Index**: On `settings.visibility` for fast visibility-based filtering.
-   **Text Index**: On `name` to support text search capabilities.

The migration exposes two functions:

-   `InitGroupsIndexesUp(db *mongo.Database) error`: Creates all core indexes on the `groups` collection.
-   `InitGroupsIndexesDown(db *mongo.Database) error`: Drops all indexes created by the `Up` function.

### Migration 2 — Lineage Index

The second migration script, located at `external/group/migrations/indexes_groups_lineage.go`, adds a dedicated index to support efficient hierarchy traversal (lineage and descendants queries). This migration should be run after the first one.

-   `InitGroupsLineageIndexUp(db *mongo.Database) error`: Creates the `lineage` array index.
-   `InitGroupsLineageIndexDown(db *mongo.Database) error`: Drops the `lineage` index.

### Running Migrations

Register the provided functions from the host application's
`migrations/mongo` package. The helpers use `func(*mongo.Database) error`, so
adapt them to the context-aware `mongo-migrate` registration signature:

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
    if err := register(groupmigrations.InitGroupsIndexesUp, groupmigrations.InitGroupsIndexesDown); err != nil {
        panic(err)
    }
    if err := register(groupmigrations.InitGroupsLineageIndexUp, groupmigrations.InitGroupsLineageIndexDown); err != nil {
        panic(err)
    }
}
```

Ensure the host's `cmd/mongo-migrator` adapter blank-imports that migration
package, then apply both registrations in order:

```sh
asdf exec go run main.go mongo-migrator up
```

The shared `down` action reverts all applied registered migrations. It is not a
lineage-only rollback. See
[Managing MongoDB Migrations](../../docs/how-to/manage-mongodb-migrations.md)
before using it.

## API Endpoints

Group routes sit under the `/api/v1/groups` prefix. `AttachRoutes` applies the
provided `AdminOnlyMiddleware` to this surface; without it, the caller is
responsible for equivalent protection. The list below covers the full surface.

**Group CRUD**
-   `POST /api/v1/groups`: Create a new group.
-   `GET /api/v1/groups`: Retrieve a paginated, filterable list of groups. Supports `prefix_name=true` to return child group names prefixed with their root name.
-   `GET /api/v1/groups/{groupID}`: Get a single group by its ID. Supports `prefix_name=true`.
-   `GET /api/v1/groups/nano/{groupNanoID}`: Get a single group by its NanoID. Supports `prefix_name=true`.
-   `PATCH /api/v1/groups/{groupID}`: Update a group's details.
-   `DELETE /api/v1/groups/{groupID}`: Delete a group.

**Hierarchy**
-   `GET /api/v1/groups/{groupID}/lineage`: Get the full lineage chain (ancestors) for a group. Supports optional `as_user_id` and `prefix_name` query params.
-   `GET /api/v1/groups/{groupID}/descendants`: Get all descendants of a group, grouped by depth level. Supports `as_user_id` and `prefix_name`.

**Member Management**
-   `GET /api/v1/groups/{groupID}/members`: List all members of a group.
-   `POST /api/v1/groups/{groupID}/members`: Add a member to a group.
-   `DELETE /api/v1/groups/{groupID}/members/{memberID}`: Remove a member from a group.
-   `PUT /api/v1/groups/{groupID}/members/{memberID}/role`: Update a member's role within a group.

**Invitations**
-   `GET /api/v1/groups/invitations/{memberID}`: List groups awaiting an invitation response from a member.
-   `POST|DELETE /api/v1/groups/{groupID}/invitations`: Invite or uninvite a user.
-   `POST /api/v1/groups/{groupID}/invitations/accept`: Accept an invitation.
-   `POST /api/v1/groups/{groupID}/invitations/reject`: Reject an invitation.

**Ownership**
-   `PUT /api/v1/groups/{groupID}/owner`: Update the owner of a group.

**Lifecycle**
-   `POST /api/v1/groups/{groupID}/archive`: Archive a group.
-   `POST /api/v1/groups/{groupID}/restore`: Restore an archived group.

**Stats, Config & Utilities**
-   `GET /api/v1/groups/stats`: Retrieve aggregate statistics across all groups.
-   `GET /api/v1/groups/{groupID}/stats`: Retrieve statistics for a specific group.
-   `GET /api/v1/groups/configs`: Retrieve the current group configuration capabilities (valid types, statuses, roles, nesting rules, etc.).
-   `GET /api/v1/groups/validate-name`: Validate and preview a group name before creating it.
-   `POST /api/v1/groups/repairs/members`: Repair groups with invalid member states (useful for recovering from data inconsistencies).
-   `GET /api/v1/groups/users/{userID}`: Get all groups a specific user belongs to. Supports `include_descendants` and `prefix_name`.

**Email-domain Automation**
-   `POST /api/v1/groups/{groupID}/auto-join/enable`: Enable automatic joining for an email domain.
-   `POST /api/v1/groups/{groupID}/auto-join/disable`: Disable automatic joining.
-   `POST /api/v1/groups/{groupID}/auto-invite/enable`: Enable automatic invitations for an email domain.
-   `POST /api/v1/groups/{groupID}/auto-invite/disable`: Disable automatic invitations.

## Name Prefixing (`prefix_name`)

If your hierarchy has children that share suspiciously similar names (because life is chaos), use `prefix_name=true`.

-   Child groups are returned as `RootGroup/ChildGroup`.
-   `raw_name` remains the original, un-prefixed value.
-   This is a response formatting feature; it does not mutate stored group names.

Example:

-   Stored name: `science`
-   Stored raw name: `Science`
-   With `prefix_name=true` under root `Hogwarts`: name becomes `hogwarts/science`, raw name stays `Science`.

## Using the Model (`UniversalGroup`)

The `UniversalGroup` model is the heart of the package. It is designed to be used both within the service and potentially in other parts of your application.

### Creating a New Group

Always use the `NewUniversalGroup` constructor to ensure all dependencies are injected correctly.

```go
import (
    "github.com/ooaklee/ghatd/external/group"
    "github.com/ooaklee/ghatd/external/toolbox"
)

// Setup dependencies (typically done once)
config := group.DefaultGroupConfig()
idGen := toolbox.NewIDGenerator()
timeProvider := toolbox.NewTimeProvider()
stringUtils := toolbox.NewStringUtils()

// Create a new group
g := group.NewUniversalGroup(config, idGen, timeProvider, stringUtils)
g.Name = "My Awesome Team"
g.Type = group.GroupTypeTeam
g.SetInitialState() // Sets ID, NanoID, default status, and timestamps

// The group is now ready to be saved to the database.
if err := g.Validate(); err != nil {
    // Handle validation error
}
```

### Modifying an Existing Group

When you retrieve a group from the database, you must re-inject its dependencies to use its methods safely.

```go
// Assume 'existingGroup' is loaded from MongoDB
var existingGroup group.UniversalGroup
// ... unmarshal from BSON ...

// Set dependencies before using methods
existingGroup.SetDependencies(config, idGen, timeProvider, stringUtils)

// Now you can safely use methods
existingGroup.AddMember("user-123", group.MemberTypeUser, group.MemberRoleAdmin)
existingGroup.SetUpdatedAtNow()
```

### Key Model Features

-   **Hierarchical Nesting**: Add a group as a member of another group by using `MemberTypeGroup`. The model supports configurable nesting trees (e.g. `ORGANISATION` → `DEPARTMENT` → `TEAM` → `SQUAD`) and prevents circular references.

-   **Lineage Tracking**: The `Lineage` field on `UniversalGroup` is a root-first ordered array of ancestor IDs (e.g. `["rootID", "parentID"]`). This makes hierarchy traversal efficient without needing recursive queries.

-   **Flexible Membership**: Members can be users (`MemberTypeUser`) or other groups (`MemberTypeGroup`). Each member is assigned a role. The available roles are:
    -   `MemberRoleOwner`
    -   `MemberRoleAdmin`
    -   `MemberRoleMember`
    -   `MemberRoleModerator`
    -   `MemberRoleGuest`
    -   `MemberRoleHead`
    -   `MemberRoleLead`
    -   `MemberRoleCoordinator`
    -   `MemberRoleSuperUser`

    You can also restrict which roles are valid per group type by configuring `TypeToRoleOverrides` in your `GroupConfig`.

-   **Ownership at the Root**: The `OwnerID` field lives directly on `UniversalGroup` — there is no separate leadership structure. This simplifies queries and keeps ownership easy to reason about.

-   **Custom Data**: Use `SetExtension(key, value)` to store any project-specific data on a group without polluting the core model.

-   **Status Control**: Use `UpdateStatus(newStatus)` to safely transition between states. Valid transitions are defined in `GroupConfig`. The built-in statuses are:
    -   `GroupStatusActive`
    -   `GroupStatusInactive`
    -   `GroupStatusArchived`
    -   `GroupStatusSuspended`
    -   `GroupStatusProvisioned`

-   **Configuration**: Use `DefaultGroupConfig()` to get a sensible starting point, or build a custom `GroupConfig` to define your own valid types, status transitions, nesting rules, and role overrides. The full set of built-in group types is:
    -   `GroupTypeTeam`
    -   `GroupTypeSquad`
    -   `GroupTypeTribe`
    -   `GroupTypeDepartment`
    -   `GroupTypeOrganisation`
    -   `GroupTypeCompany`
    -   `GroupTypeProject`
    -   `GroupTypeCommunity`
    -   `GroupTypeFamily`
    -   `GroupTypeFriends`
    -   `GroupTypeCustom`

-   **Visibility**: Control who can see a group using `GroupSettings.Visibility`:
    -   `VisibilityPublic` — visible to everyone.
    -   `VisibilityPrivate` — visible to invited members only.
    -   `VisibilityInternal` — visible to members and others within the same hierarchy.

-   **Rich Metadata**: The model includes dedicated structures for `DisplayInfo` (description, icon, avatar, email, website), `Settings` (visibility, max members, approval requirements), and `Integrations` (e.g., Slack).
