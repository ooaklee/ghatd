# User v2 (Universal User Model)

## Overview

The Universal User Model is designed for reuse across different projects. It features dependency injection for testability, configurable status transitions, optional fields, and extension points for project-specific data.

## Table of Contents

- [Key Features](#key-features)
- [Architecture](#architecture)
- [MongoDB Setup](#mongodb-setup)
- [API Endpoints](#api-endpoints)
- [Configuration Examples](#configuration-examples)
- [Testing](#testing)

## Key Features

### 1. **Dependency Injection**
All dependencies are injected, making testing easier and the model more flexible.

```go
type IDGenerator interface {
    GenerateUUID() string
    GenerateNanoID() string
}

type TimeProvider interface {
    Now() time.Time
    NowUTC() string
}

type StringUtils interface {
    ToTitleCase(s string) string
    ToLowerCase(s string) string
    ToUpperCase(s string) string
    InSlice(item string, slice []string) bool
}
```

### 2. **Version Tracking**
Every user has an internal `version` field (not exposed in JSON) to track which model version they were created/updated with:

```go
// Version field is stored in database but not in JSON responses
Version int `json:"-" bson:"version" db:"version"`

// Version is automatically set to 2 on create/update
user.SetInitialState() // Sets version to 2
user.EnsureVersion()    // Normalises a stored record to version 2
```

### 3. **Configurable Status System**
Define your own status transitions and validation rules. Each config also carries a `Type` identifier so the service can look up and serve the right preset at runtime:

```go
config := &UserConfig{
    Type:          "web_app",
    DefaultStatus: "PROVISIONED",
    StatusTransitions: map[string][]string{
        "ACTIVE":       {"PROVISIONED", "DEACTIVATED"},
        "SUSPENDED":    {"ACTIVE"},
        "DEACTIVATED":  {"ACTIVE", "SUSPENDED"},
        "UNSUSPEND":    {"SUSPENDED"},
        "EMAIL_CHANGE": {"PROVISIONED", "ACTIVE"},
    },
}
```

### 4. **Flexible Structure**
Optional fields allow the model to represent various user types:

```go
type UniversalUser struct {
    ID     string `json:"id" bson:"_id"`
    Email  string `json:"email"`
    Status string `json:"status"`
    Version int   `json:"-" bson:"version"` // Internal only
    
    // Optional fields
    NanoID       string                 `json:"nano_id,omitempty"`
    PersonalInfo *PersonalInfo          `json:"personal_info,omitempty"`
    Roles        []string               `json:"roles"`
    Verification *VerificationStatus    `json:"verification,omitempty"`
    Metadata     *UserMetadata          `json:"metadata"`
    Extensions   map[string]interface{} `json:"extensions,omitempty"`
}
```

### 5. **Multiple Identifier Support**
Support both UUID and NanoID:

```go
user.GenerateNewUUID()    // Primary ID
user.GenerateNewNanoID()  // Alternative short ID
```

### 6. **Extension Fields**
Add project-specific data without modifying the core model:

```go
user.SetExtension("department", "Engineering")
user.SetExtension("preferences", map[string]interface{}{
    "theme": "dark",
    "language": "en",
})

dept, exists := user.GetExtension("department")
```

## Architecture

### Layer Structure

```
┌─────────────────────────────────────────────────────────┐
│                    HTTP Layer (routes.go)               │
│  - Route definitions                                    │
│  - Middleware integration                               │
│  - AttachRoutes function                                │
└──────────────────────┬──────────────────────────────────┘
                       │
┌──────────────────────▼──────────────────────────────────┐
│                  Handler Layer (handler.go)             │
│  - HTTP request/response handling                       │
│  - 23 endpoint handlers                                 │
│  - Error response formatting                            │
└──────────────────────┬──────────────────────────────────┘
                       │
┌──────────────────────▼──────────────────────────────────┐
│            Request Mapping Layer (fender.go)            │
│  - URI variable extraction                              │
│  - Query parameter parsing                              │
│  - Request body decoding                                │
│  - Validation integration                               │
└──────────────────────┬──────────────────────────────────┘
                       │
┌──────────────────────▼──────────────────────────────────┐
│              Service Layer (service.go)                 │
│  - Business logic                                       │
│  - Auto-admin detection                                 │
│  - Change detection                                     │
│  - Audit logging                                        │
│  - Version management (sets version to 2)               │
└──────────────────────┬──────────────────────────────────┘
                       │
┌──────────────────────▼──────────────────────────────────┐
│            Repository Layer (repository.go)             │
│  - MongoDB data access                                  │
│  - Query building                                       │
│  - Filtering, sorting, pagination                       │
└──────────────────────┬──────────────────────────────────┘
                       │
┌──────────────────────▼──────────────────────────────────┐
│                  Model Layer (model.go)                 │
│  - UniversalUser struct with version field              │
│  - Business logic methods                               │
│  - Validation                                           │
└─────────────────────────────────────────────────────────┘
```

## MongoDB Setup

### Prerequisites

- MongoDB 4.2 or higher
- mongo-migrate library: `github.com/xakep666/mongo-migrate`
- Go mongo driver: `go.mongodb.org/mongo-driver/v2/mongo`

### Installing Dependencies

```bash
asdf exec go get github.com/xakep666/mongo-migrate
asdf exec go get go.mongodb.org/mongo-driver/v2/mongo
```

### Running Migrations

Register the user index helpers from the host application's
`migrations/mongo` package:

```go
package migrations

import (
    "context"

    usermigrations "github.com/ooaklee/ghatd/external/user/v2/migrations"
    migrate "github.com/xakep666/mongo-migrate"
    "go.mongodb.org/mongo-driver/v2/mongo"
)

func init() {
    if err := migrate.Register(
        func(_ context.Context, db *mongo.Database) error {
            return usermigrations.InitUsersIndexesUp(db)
        },
        func(_ context.Context, db *mongo.Database) error {
            return usermigrations.InitUsersIndexesDown(db)
        },
    ); err != nil {
        panic(err)
    }
}
```

Ensure the host's `cmd/mongo-migrator` adapter blank-imports its migration
package, then apply every pending registration:

```sh
asdf exec go run main.go mongo-migrator up
```

The shared `down` action reverts all applied registered migrations, not only
the user indexes. See
[Managing MongoDB Migrations](../../../docs/how-to/manage-mongodb-migrations.md)
for command wiring, settings, and rollback precautions.

### User Indexes

The following indexes are created for the `users` collection:

| Index Name | Fields | Type | Purpose | Example Query |
|------------|--------|------|---------|---------------|
| `idx_users_email` | `email` | Unique | Fast email lookups and prevent duplicates | `db.users.find({email: "user@example.com"})` |
| `idx_users_nano_id` | `_nano_id` | Unique, Sparse | Alternative identifier lookups | `db.users.find({_nano_id: "abc123"})` |
| `idx_users_status` | `status` | Standard | Filter users by status | `db.users.find({status: "ACTIVE"})` |
| `idx_users_roles` | `roles` | Standard | Role-based queries | `db.users.find({roles: "ADMIN"})` |
| `idx_users_status_created_at` | `status`, `metadata.created_at` | Compound | Efficient filtered sorting | `db.users.find({status: "ACTIVE"}).sort({created_at: -1})` |
| `idx_users_email_verified` | `verification.email_verified` | Standard | Filter unverified users | `db.users.find({"verification.email_verified": false})` |
| `idx_users_created_at` | `metadata.created_at` | Standard (Descending) | Sort by registration date | `db.users.find().sort({"metadata.created_at": -1})` |
| `idx_users_updated_at` | `metadata.updated_at` | Standard (Descending) | Sort by last update | `db.users.find().sort({"metadata.updated_at": -1})` |
| `idx_users_last_login_at` | `metadata.last_login_at` | Standard (Descending) | Activity tracking | `db.users.find().sort({"metadata.last_login_at": -1})` |
| `idx_users_activated_at` | `metadata.activated_at` | Standard (Descending) | Filter activated users | `db.users.find({"metadata.activated_at": {$exists: true}})` |
| `idx_users_status_changed_at` | `metadata.status_changed_at` | Standard (Descending) | Status change tracking | `db.users.find().sort({"metadata.status_changed_at": -1})` |
| `idx_users_email_verified_at` | `verification.email_verified_at` | Standard (Descending) | Verification tracking | `db.users.find().sort({"verification.email_verified_at": -1})` |

### Index Details

**Unique Indexes:**
- `email` - Ensures no duplicate email addresses
- `_nano_id` - Ensures no duplicate nano IDs (sparse index, only for users with nano IDs)

**Compound Index:**
- `status` + `created_at` - Optimizes queries that filter by status and sort by creation date

**Timestamp Indexes:**
All timestamp fields in the `metadata` object are indexed for efficient sorting and filtering:
- `created_at` - Registration date
- `updated_at` - Last modification
- `last_login_at` - Activity tracking (any login)
- `activated_at` - Activation tracking
- `status_changed_at` - Status change history
- `email_verified_at` - Verification history

> **Note:** `last_fresh_login_at` is also stored on the metadata object to distinguish full credential logins from session refreshes. It is not currently indexed.

### Verifying Indexes

After running migrations, verify the indexes were created:

```javascript
// Connect to MongoDB
use your_database_name

// Check user indexes
db.users.getIndexes()

// Check index usage stats
db.users.aggregate([
    { $indexStats: {} }
])

```

### Dropping Indexes (Rollback)

The migration system includes a down function to remove indexes:

```go
// Rollback user indexes
if err := userMigration.InitUsersIndexesDown(db); err != nil {
    log.Printf("Failed to drop user indexes: %v", err)
}
```

The package-level down function is the supported rollback API. Host migration
tooling may wrap it in an explicit rollback command where required.

### Performance Considerations

**Index Memory Usage:**
- Unique indexes on `email` and `_nano_id` are essential for data integrity
- Timestamp indexes improve sorting performance significantly
- Compound `status` + `created_at` index optimizes the most common query pattern

**Query Optimization:**
```javascript
// Efficient: Uses idx_users_status_created_at compound index
db.users.find({ status: "ACTIVE" }).sort({ "metadata.created_at": -1 })

// Efficient: Uses idx_users_email unique index
db.users.find({ email: "user@example.com" })

// Efficient: Uses idx_users_roles index
db.users.find({ roles: "ADMIN" })

// Efficient: Uses idx_users_email_verified index
db.users.find({ "verification.email_verified": false })
```

**Monitoring Index Usage:**
```javascript
// Check which indexes are being used most
db.users.aggregate([
    { $indexStats: {} },
    { $sort: { "accesses.ops": -1 } }
])

// Identify unused indexes (consider removing if ops < 100)
db.users.aggregate([
    { $indexStats: {} },
    { $match: { "accesses.ops": { $lt: 100 } } }
])
```

## API Endpoints

### Base Path
All v2 endpoints are under: `/api/v2/users`

### CRUD Operations

| Method | Endpoint | Description | Admin Only |
|--------|----------|-------------|------------|
| POST | `/api/v2/users` | Create new user | ✓ |
| GET | `/api/v2/users` | List users with filters | ✓ |
| GET | `/api/v2/users/{userID}` | Get user by ID | ✓ |
| PATCH | `/api/v2/users/{userID}` | Update user | ✓ |
| DELETE | `/api/v2/users/{userID}` | Delete user | ✓ |

### Alternative Lookups

| Method | Endpoint | Description | Admin Only |
|--------|----------|-------------|------------|
| GET | `/api/v2/users/nano/{nanoID}` | Get user by nano ID | ✓ |
| GET | `/api/v2/users/email/{email}` | Get user by email | ✓ |

### Profile Management

| Method | Endpoint | Description | Admin Only |
|--------|----------|-------------|------------|
| GET | `/api/v2/users/{userID}/profile` | Get full profile | ✓ |
| GET | `/api/v2/users/{userID}/micro` | Get micro profile | ✓ |
| PATCH | `/api/v2/users/{userID}/personal-info` | Update personal info | ✓ |

### Status & Role Management

| Method | Endpoint | Description | Admin Only |
|--------|----------|-------------|------------|
| PATCH | `/api/v2/users/{userID}/status` | Update user status | ✓ |
| POST | `/api/v2/users/{userID}/roles` | Add role | ✓ |
| DELETE | `/api/v2/users/{userID}/roles` | Remove role | ✓ |

### Verification

| Method | Endpoint | Description | Admin Only |
|--------|----------|-------------|------------|
| POST | `/api/v2/users/{userID}/verify/email` | Verify email | ✓ |
| POST | `/api/v2/users/{userID}/unverify/email` | Unverify email | ✓ |
| POST | `/api/v2/users/{userID}/verify/phone` | Verify phone | ✓ |

### Extensions

| Method | Endpoint | Description | Admin Only |
|--------|----------|-------------|------------|
| POST | `/api/v2/users/{userID}/extensions` | Set extension field | ✓ |
| GET | `/api/v2/users/{userID}/extensions/{extensionKey}` | Get extension field | ✓ |

### Stats

| Method | Endpoint | Description | Admin Only |
|--------|----------|-------------|------------|
| GET | `/api/v2/users/stats` | Get user platform stats | ✓ |
| GET | `/api/v2/users/stats?with_email_regex=@example\.com$` | Stats scoped to email domain | ✓ |

### Configuration

| Method | Endpoint | Description | Admin Only |
|--------|----------|-------------|------------|
| GET | `/api/v2/users/configs` | List supported user config presets | ✓ |

### Advanced Queries

| Method | Endpoint | Description | Admin Only |
|--------|----------|-------------|------------|
| GET | `/api/v2/users?with_roles=ADMIN,USER` | List users filtered by roles | ✓ |
| GET | `/api/v2/users?with_status=ACTIVE` | List users filtered by status | ✓ |
| GET | `/api/v2/users?with_extension_key=x&with_extension_value=y` | List users filtered by extension key/value | ✓ |

### Bulk Operations

| Method | Endpoint | Description | Admin Only |
|--------|----------|-------------|------------|
| POST | `/api/v2/users/bulk/status` | Bulk update statuses | ✓ |

### Utilities

| Method | Endpoint | Description | Admin Only |
|--------|----------|-------------|------------|
| GET | `/api/v2/users/{userID}/validate` | Validate user | ✓ |
| POST | `/api/v2/users/{userID}/recordings/login` | Record login | ✓ |

### Response Shapes

#### `/stats` — `UserStats`

```json
{
  "total": 1042,
  "by_status": {
    "provisioned": 120,
    "active": 850,
    "suspended": 12,
    "deactivated": 40,
    "locked_out": 8,
    "recovery": 12
  }
}
```

The `total` field is the sum of all status counts. Pass `?with_email_regex=` to scope results to a subset of users matched by email pattern.

#### `/configs` — `[]UserConfigCapabilities`

Returns the list of built-in config presets the service was initialised with. Each entry describes exactly what that preset allows:

```json
[
  {
    "default_status": "PROVISIONED",
    "supported_status_transitions": {
      "ACTIVE": ["PROVISIONED", "DEACTIVATED"],
      "SUSPENDED": ["ACTIVE"],
      "DEACTIVATED": ["ACTIVE", "SUSPENDED"]
    },
    "required_fields": ["email", "first_name", "last_name"],
    "valid_roles": ["ADMIN", "USER"],
    "email_verification_required": true,
    "supports_multiple_identifiers": false,
    "supported_statuses": ["ACTIVE", "DEACTIVATED", "PROVISIONED", "SUSPENDED"]
  }
]
```

`supported_statuses` is derived automatically from all keys and values in `supported_status_transitions`, so you never have to keep a separate list in sync.

## Configuration Examples

Four built-in presets are available out of the box. Each is identified by a `Type` string so the service can resolve the right config at runtime:

| Type constant | Value | When to use |
|---|---|---|
| `UserConfigTypeDefault` | `"default"` | General purpose; a safe starting point |
| `UserConfigTypeWebApp` | `"web_app"` | Consumer web applications |
| `UserConfigTypeAPIService` | `"api_service"` | Machine-to-machine API clients |
| `UserConfigTypeMicroservice` | `"microservice"` | Internal services with minimal lifecycle |
| `UserConfigTypeCustom` | `"custom"` | Any config without an explicit type set |

You can pass one primary config to `NewService` and register additional presets with `WithConfigs`. This lets callers request a specific config type at create-time:

```go
service := NewService(repo, auditSvc, DefaultUserConfig(), idGen, timeProv, strUtils, adminRegex).
    WithConfigs(WebAppUserConfig(), APIServiceUserConfig(), MicroserviceUserConfig())
```

To get all four built-in presets at once, use the helper:

```go
for _, cfg := range BuiltInUserConfigs() {
    fmt.Println(cfg.Type)
}
// default, web_app, api_service, microservice
```

### Default

```go
func DefaultUserConfig() *UserConfig {
    return &UserConfig{
        Type:          "default",
        DefaultStatus: "PROVISIONED",
        StatusTransitions: map[string][]string{
            "ACTIVE":       {"PROVISIONED"},
            "DEACTIVATED":  {"PROVISIONED", "ACTIVE", "LOCKED_OUT", "RECOVERY", "SUSPENDED"},
            "SUSPENDED":    {"ACTIVE"},
            "EMAIL_CHANGE": {"PROVISIONED", "ACTIVE"},
            "LOCKED_OUT":   {"ACTIVE"},
            "RECOVERY":     {"ACTIVE"},
        },
        RequiredFields:            []string{"email", "first_name", "last_name"},
        DefaultRole:               "USER",
        ValidRoles:                []string{"ADMIN", "USER"},
        EmailVerificationRequired: true,
        MultipleIdentifiers:       true,
    }
}
```

### Web Application

```go
func WebAppUserConfig() *UserConfig {
    return &UserConfig{
        Type:          "web_app",
        DefaultStatus: "PROVISIONED",
        StatusTransitions: map[string][]string{
            "ACTIVE":       {"PROVISIONED", "DEACTIVATED"},
            "SUSPENDED":    {"ACTIVE"},
            "DEACTIVATED":  {"ACTIVE", "SUSPENDED"},
            "UNSUSPEND":    {"SUSPENDED"},
            "EMAIL_CHANGE": {"PROVISIONED", "ACTIVE"},
        },
        RequiredFields:            []string{"email", "first_name", "last_name"},
        DefaultRole:               "USER",
        ValidRoles:                []string{"ADMIN", "USER"},
        EmailVerificationRequired: true,
        MultipleIdentifiers:       false,
    }
}
```

### API Service

```go
func APIServiceUserConfig() *UserConfig {
    return &UserConfig{
        Type:          "api_service",
        DefaultStatus: "ACTIVE",
        StatusTransitions: map[string][]string{
            "ACTIVE":       {"PROVISIONED", "DEACTIVATED"},
            "SUSPENDED":    {"ACTIVE"},
            "DEACTIVATED":  {"ACTIVE", "SUSPENDED"},
            "EMAIL_CHANGE": {"PROVISIONED", "ACTIVE"},
        },
        RequiredFields:            []string{"email"},
        ValidRoles:                []string{"SERVICE", "CLIENT", "ADMIN"},
        EmailVerificationRequired: false,
        MultipleIdentifiers:       true,
    }
}
```

### Microservice

```go
func MicroserviceUserConfig() *UserConfig {
    return &UserConfig{
        Type:          "microservice",
        DefaultStatus: "ACTIVE",
        StatusTransitions: map[string][]string{
            "ACTIVE":       {},
            "DEACTIVATED":  {"ACTIVE"},
            "EMAIL_CHANGE": {"DEACTIVATED", "ACTIVE"},
        },
        RequiredFields:            []string{"email"},
        ValidRoles:                []string{}, // Allow any roles
        EmailVerificationRequired: false,
        MultipleIdentifiers:       true,
    }
}
```

## Testing

### Dependency Injection Benefits

```go
func TestUniversalUser_UpdateStatus(t *testing.T) {
    // Mock dependencies
    mockTime := &MockTimeProvider{
        fixedTime: "2025-01-01T00:00:00Z",
    }
    mockStrings := &MockStringUtils{}
    mockIDGen := &MockIDGenerator{
        uuids: []string{"test-uuid-123"},
    }
    
    config := &UserConfig{
        DefaultStatus: "PROVISIONED",
        StatusTransitions: map[string][]string{
            "SUSPENDED": {"ACTIVE"},
        },
    }
    
    // Create user with mocked dependencies
    user := NewUniversalUser(config, mockIDGen, mockTime, mockStrings)
    user.Status = "ACTIVE"
    user.Version = 2
    
    // Test status transition
    updatedUser, err := user.UpdateStatus("SUSPENDED")
    
    assert.NoError(t, err)
    assert.Equal(t, "SUSPENDED", updatedUser.Status)
    assert.Equal(t, "2025-01-01T00:00:00Z", updatedUser.Metadata.StatusChangedAt)
    assert.Equal(t, 2, updatedUser.Version)
}

func TestUniversalUser_VersionIsSet(t *testing.T) {
    user := NewUniversalUser(DefaultUserConfig(), mockIDGen, mockTime, mockStrings)
    user.SetInitialState()
    
    assert.Equal(t, 2, user.Version, "Version should be set to 2")
}

func TestUniversalUser_EnsureVersion(t *testing.T) {
    user := &UniversalUser{Version: 0} // Simulate an unversioned stored record
    user.SetDependencies(DefaultUserConfig(), mockIDGen, mockTime, mockStrings)
    
    user.EnsureVersion()
    
    assert.Equal(t, 2, user.Version, "Version should be updated to 2")
}
```

### Integration Testing

```go
func TestService_CreateUser_SetsVersion(t *testing.T) {
    // Setup
    service := setupTestService()
    
    // Create user
    response, err := service.CreateUser(ctx, &CreateUserRequest{
        Email: "test@example.com",
    })
    
    require.NoError(t, err)
    assert.Equal(t, 2, response.User.Version)
}

func TestService_UpdateUser_EnsuresVersion(t *testing.T) {
    // Setup
    service := setupTestService()
    
    // Create a stored record with a missing model version
    user := createUnversionedUserInDB(t)
    assert.NotEqual(t, 2, user.Version)
    
    // Update via v2 service
    response, err := service.UpdateUser(ctx, &UpdateUserRequest{
        ID:        user.ID,
        FirstName: "Updated",
    })
    
    require.NoError(t, err)
    assert.Equal(t, 2, response.User.Version, "Version should be set to 2 after update")
}
```

## Best Practices

1. **Always use dependency injection** - Makes testing easier
2. **Configure status transitions** - Define valid state changes upfront
3. **Leverage extensions** - Add project-specific fields without modifying core model
4. **Test thoroughly** - Use mocked dependencies for unit tests
5. **Audit logging** - All mutations are logged automatically
6. **Validate early** - Use `user.Validate()` before database operations

## Error Handling

All errors use the error manifest system:

```go
// Service layer errors
ErrKeyUserNotFound            = "UserNotFound"           // USV2-001
ErrKeyEmailAlreadyExists      = "UserEmailAlreadyExists" // USV2-002
ErrKeyValidationFailed        = "UserValidationFailed"   // USV2-003

// Model validation errors
ErrKeyUserConfigNotSet        = "UserConfigNotSet"       // USV2-009
ErrKeyUserInvalidTargetStatus = "UserInvalidTargetStatus" // USV2-010
```

Errors are automatically mapped to appropriate HTTP status codes and responses.

## Monitoring & Metrics

Track these metrics for your v2 implementation:

- **Users created** (with version 2)
- **Stored model-version distribution**
- **API endpoint usage** (which v2 endpoints are most used)
- **Status transitions** (track most common transitions)
- **Extension field usage** (which extensions are most popular)

Example query to inspect stored model versions:
```javascript
db.users.aggregate([
    {
        $group: {
            _id: "$version",
            count: { $sum: 1 }
        }
    },
    {
        $project: {
            version: { $ifNull: ["$_id", "unversioned"] },
            count: 1,
            percentage: {
                $multiply: [
                    { $divide: ["$count", { $literal: totalUsers }] },
                    100
                ]
            }
        }
    }
])
```

## Support

For issues or questions:
- Review the [model.go](model.go) for implementation details
- Check [service.go](service.go) for business logic
- See [handler.go](handler.go) for HTTP endpoints
- Refer to [routes.go](routes.go) for route configuration
- See the default implementations in [utils.go](utils.go).
