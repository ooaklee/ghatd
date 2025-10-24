# Group Package Getting Started

The `group` package provides a robust and flexible system for managing user groups, teams, organisations, and other collections. It is built with a "universal model" approach, allowing a single data structure to represent various types of groups with support for hierarchical nesting.

This guide will walk you through the architecture, setup, and common use cases of the `group` package.

## Architecture

The package follows a standard layered architecture common throughout this project:

1.  **Routes (`routes.go`)**: Defines the HTTP API endpoints (e.g., `POST /v1/groups`, `GET /v1/groups/{id}`). It maps URLs to specific handlers.
2.  **Handler (`handler.go`)**: Receives HTTP requests, parses them, and validates input. It acts as the bridge between the transport layer (HTTP) and the business logic.
3.  **Fender (`fender.go`)**: An authorisation layer that checks if the authenticated user has the necessary permissions to perform an action on a group.
4.  **Service (`service.go`)**: Contains the core business logic. It orchestrates operations like creating, updating, and retrieving groups, and it interacts with the repository.
5.  **Repository (`repository.go`)**: The data access layer. It is responsible for all database operations (Create, Read, Update, Delete) for the `groups` collection in MongoDB.
6.  **Model (`model.go`)**: Defines the `UniversalGroup` data structure and its associated helper methods. This is the core entity of the package.

## MongoDB Setup & Migrations

The `group` package relies on a MongoDB collection named `groups`. To ensure optimal query performance, a set of database indexes must be created.

### Initial Indexes

The initial migration script, located at `external/group/migrations/indexes_groups.go`, creates 19 indexes to support the various query patterns used by the repository. These include:

-   **Unique Indexes**: To enforce uniqueness on `_id` and `_nano_id`.
-   **Standard Indexes**: On individual fields like `name`, `type`, `status`, and `metadata.created_at` for efficient filtering and sorting.
-   **Compound Indexes**: For common multi-field queries, such as filtering by type and status simultaneously.
-   **Member & Leadership Indexes**: To quickly find groups based on their members (`members.id`), owner (`leadership.owner_id`), or other leadership roles.
-   **Text Index**: A text index on `name` and `display_info.name_aliases` to enable full-text search capabilities.

### Running Migrations

To apply these indexes, you must integrate the provided migration functions into your application's startup process using a migration tool like `mongo-migrate`.

The migration provides two key functions:

-   `InitGroupsIndexesUp(db *mongo.Database) error`: Creates all 19 indexes on the `groups` collection.
-   `InitGroupsIndexesDown(db *mongo.Database) error`: Drops all indexes created by the `Up` function.

**Example Integration:**

```go
// In your main application setup
import (
    "github.com/xakep666/mongo-migrate"
    groupMigrations "github.com/ooaklee/ghatd/external/group/migrations"
)

func main() {
    // ... database connection setup ...
    db := client.Database("your_db_name")
    migrate.SetDatabase(db)

    // Add the group migrations
    migrate.Add(migrate.NewMigration(
        "initial_groups_indexes",
        groupMigrations.InitGroupsIndexesUp,
        groupMigrations.InitGroupsIndexesDown,
    ))

    // Apply migrations
    if err := migrate.Up(migrate.AllAvailable); err != nil {
        log.Fatalf("Migration failed: %v", err)
    }
}
```

## API Endpoints

The following are the primary API endpoints provided by the `group` service.

-   `POST /v1/groups`: Create a new group.
-   `GET /v1/groups`: Retrieve a paginated list of groups with filtering and sorting options.
-   `GET /v1/groups/{id}`: Get a single group by its ID.
-   `PUT /v1/groups/{id}`: Update a group's details.
-   `DELETE /v1/groups/{id}`: Delete a group.
-   `POST /v1/groups/{id}/members`: Add a member to a group.
-   `DELETE /v1/groups/{id}/members/{memberId}`: Remove a member from a group.

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

-   **Hierarchical Nesting**: Add a group as a member of another group by using `MemberTypeGroup`. The model prevents circular references.

-   **Flexible Membership**: Members can be users (`MemberTypeUser`) or other groups (`MemberTypeGroup`). Each member is assigned a role, such as:
    -   `MemberRoleOwner`
    -   `MemberRoleAdmin`
    -   `MemberRoleMember`
    -   `MemberRoleModerator`
    -   `MemberRoleGuest`

-   **Custom Data**: Use `SetExtension(key, value)` to store any project-specific data on a group.

-   **Status Control**: Use `UpdateStatus(newStatus)` to safely transition between states. The model supports several built-in statuses:
    -   `GroupStatusActive`
    -   `GroupStatusInactive`
    -   `GroupStatusArchived`
    -   `GroupStatusSuspended`
    -   `GroupStatusProvisioned`

-   **Configuration**: Create a `CustomGroupConfig` to define your own valid group types, status transitions, and required fields. The package includes many default types:
    -   `GroupTypeTeam`
    -   `GroupTypeDepartment`
    -   `GroupTypeOrganization`
    -   `GroupTypeProject`
    -   `GroupTypeCommunity`

-   **Rich Metadata**: The model includes dedicated structures for `DisplayInfo` (description, avatar), `Leadership` (owner, head), `Settings` (visibility, max members), and `Integrations` (e.g., Slack).
