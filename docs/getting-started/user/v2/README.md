
# User v2 (Universal User Model)

## Overview

The Universal User Model (User v2) was designed to be more flexible and applicable to various projects compared to the earlier user model. It features dependency injection for testability, configurable status transitions, optional fields, and extension points for project-specific data.

## Table of Contents

- [Key Features](#key-features)
- [Architecture](#architecture)
- [MongoDB Setup](#mongodb-setup)
- [API Endpoints](#api-endpoints)
- [Migration Guide](#migration-guide)
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
    InSlice(item string, slice []string) bool
}
```

### 2. **Version Tracking**
Every user has an internal `version` field (not exposed in JSON) to track which model version they were created/migrated with. This makes it easy to identify users that need migration:

```go
// Version field is stored in database but not in JSON responses
Version int `json:"-" bson:"version" db:"version"`

// Version is automatically set to 2 on create/update
user.SetInitialState() // Sets version to 2
user.EnsureVersion()    // Ensures version is 2 (for migrations)
```

**Migration Detection Query:**
```javascript
// MongoDB: Find all v1 users that need migration
db.users.find({ $or: [ { version: { $exists: false } }, { version: { $ne: 2 } } ] })
```

### 3. **Configurable Status System**
Define your own status transitions and validation rules:

```go
config := &UserConfig{
    DefaultStatus: "PROVISIONED",
    StatusTransitions: map[string][]string{
        "ACTIVE":      {"PROVISIONED"},
        "SUSPENDED":   {"ACTIVE"},
        "DEACTIVATED": {"PROVISIONED", "ACTIVE", "SUSPENDED"},
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
- Go mongo driver: `go.mongodb.org/mongo-driver/mongo`

### Installing Dependencies

```bash
go get github.com/xakep666/mongo-migrate
go get go.mongodb.org/mongo-driver/mongo
```

### Running Migrations

Create a migration runner to set up indexes for the users collection:

```go
package main

import (
    "context"
    "log"
    "os"
    
    userMigration "github.com/ooaklee/ghatd/external/user/v2/migrations"
    migrate "github.com/xakep666/mongo-migrate"
    "go.mongodb.org/mongo-driver/mongo"
    "go.mongodb.org/mongo-driver/mongo/options"
)

func main() {
    // Connect to MongoDB
    mongoURI := os.Getenv("MONGODB_URI")
    if mongoURI == "" {
        mongoURI = "mongodb://localhost:27017"
    }
    
    client, err := mongo.Connect(context.Background(), 
        options.Client().ApplyURI(mongoURI))
    if err != nil {
        log.Fatalf("Failed to connect to MongoDB: %v", err)
    }
    defer func() {
        if err := client.Disconnect(context.Background()); err != nil {
            log.Printf("Error disconnecting: %v", err)
        }
    }()
    
    // Select database
    db := client.Database("your_database_name")
    
    // Initialize migration system
    migrate.SetDatabase(db)
    
    // Register user v2 indexes migration
    migrate.Register(
        userMigration.InitUsersIndexesUp,
        userMigration.InitUsersIndexesDown,
    )
    
    // Run migrations
    log.Println("Running user v2 indexes migrations...")
    if err := migrate.Up(migrate.AllAvailable); err != nil {
        log.Fatalf("Migration failed: %v", err)
    }
    
    log.Println("✓ Migrations completed successfully")
}
```

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
- `last_login_at` - Activity tracking
- `activated_at` - Activation tracking
- `status_changed_at` - Status change history
- `email_verified_at` - Verification history

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

// Example: Find users needing migration (no version or version != 2)
db.users.find({
    $or: [
        { version: { $exists: false } },
        { version: { $ne: 2 } }
    ]
})
```

### Dropping Indexes (Rollback)

The migration system includes a down function to remove indexes:

```go
// Rollback user indexes
if err := userMigration.InitUsersIndexesDown(db); err != nil {
    log.Printf("Failed to drop user indexes: %v", err)
}
```

Or use the migration CLI:

```bash
# Rollback last migration set
go run cmd/mongo-migrator/migrator.go down 1

# Rollback all migrations
go run cmd/mongo-migrator/migrator.go down all
```

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

### Advanced Queries

| Method | Endpoint | Description | Admin Only |
|--------|----------|-------------|------------|
| GET | `/api/v2/users/by-roles?roles=ADMIN,USER` | Get users by roles | ✓ |
| GET | `/api/v2/users/by-status?status=ACTIVE` | Get users by status | ✓ |
| GET | `/api/v2/users/search/extensions?key=x&value=y` | Search by extension | ✓ |

### Bulk Operations

| Method | Endpoint | Description | Admin Only |
|--------|----------|-------------|------------|
| POST | `/api/v2/users/bulk/status` | Bulk update statuses | ✓ |

### Utilities

| Method | Endpoint | Description | Admin Only |
|--------|----------|-------------|------------|
| GET | `/api/v2/users/{userID}/validate` | Validate user | ✓ |
| POST | `/api/v2/users/{userID}/recordings/login` | Record login | ✓ |

## Migration Guide

### Understanding the Version Field

The v2 model includes an internal `version` field that helps track which model version a user was created with:

- **Version 2**: Users created or updated in v2 system
- **Version missing/1**: Users that need migration from v1

This field is:
- **NOT exposed in JSON responses** (`json:"-"`)
- **Stored in the database** (`bson:"version"`, `db:"version"`)
- **Automatically set to 2** when creating or updating users in v2

### Identifying Users That Need Migration

```javascript
// MongoDB: Count users needing migration
db.users.countDocuments({ 
    $or: [ 
        { version: { $exists: false } }, 
        { version: { $ne: 2 } } 
    ] 
})

// MongoDB: Find all v1 users
db.users.find({ 
    $or: [ 
        { version: { $exists: false } }, 
        { version: { $ne: 2 } } 
    ] 
}).limit(10)
```

### Strategy 1: Gradual Migration (Recommended)

#### Phase 1: Dual Model Support

Keep both models running simultaneously:

```go
// Initialize v2 system
userFactory := NewUserFactory(WebAppUserConfig())
v2Service := NewService(v2Repository, auditService, config, ...)
v2Handler := NewHandler(v2Service, validator)

// Attach v2 routes
v2.AttachRoutes(&v2.AttachRoutesRequest{
    Router:              router,
    Handler:             v2Handler,
    AdminOnlyMiddleware: adminMiddleware,
})

// Keep v1 routes active
v1.AttachRoutes(&v1.AttachRoutesRequest{
    Router:              router,
    Handler:             v1Handler,
    AdminOnlyMiddleware: adminMiddleware,
})
```

#### Phase 2: Migrate Users on Access

When a v1 user is accessed, automatically migrate and update:

```go
func (s *Service) GetUserByID(ctx context.Context, req *GetUserByIDRequest) (*GetUserByIDResponse, error) {
    user, err := s.UserRepository.GetUserByID(ctx, req.ID)
    if err != nil {
        return nil, err
    }

    // Check if user needs migration
    if user.Version != 2 {
        // User is from v1, migrate them
        user.EnsureVersion() // Sets version to 2
        
        // Update in database
        user, err = s.UserRepository.UpdateUser(ctx, user)
        if err != nil {
            log.Error("failed to update user version", zap.Error(err))
            // Don't fail the request, just log
        }
    }

    user.SetDependencies(s.Config, s.IDGenerator, s.TimeProvider, s.StringUtils)
    return &GetUserByIDResponse{User: user}, nil
}
```

#### Phase 3: Batch Migration Script

Create a migration command to update remaining users:

```go
func MigrateUsersToV2(ctx context.Context, repo Repository) error {
    // Find all users without version 2
    filter := bson.M{
        "$or": []bson.M{
            {"version": bson.M{"$exists": false}},
            {"version": bson.M{"$ne": 2}},
        },
    }

    collection := repo.Store.Database().Collection("users")
    
    // Update all in one operation
    result, err := collection.UpdateMany(ctx, filter, bson.M{
        "$set": bson.M{
            "version": 2,
            "metadata.updated_at": time.Now().UTC().Format(time.RFC3339),
        },
    })

    if err != nil {
        return fmt.Errorf("migration failed: %w", err)
    }

    log.Info("migration completed", 
        zap.Int64("migrated_count", result.ModifiedCount))
    
    return nil
}
```

#### Phase 4: Monitor Migration Progress

```javascript
// Check migration progress
db.users.aggregate([
    {
        $group: {
            _id: "$version",
            count: { $sum: 1 }
        }
    },
    {
        $project: {
            version: { $ifNull: ["$_id", "missing/v1"] },
            count: 1
        }
    }
])

// Expected output after full migration:
// { version: 2, count: 10523 }
```

#### Phase 5: Remove v1 Routes

Once all users have `version: 2`, remove v1 routes and code.

### Strategy 2: One-Time Batch Migration

For smaller systems or during maintenance windows:

```go
func BatchMigrateAllUsers(ctx context.Context) error {
    // 1. Backup database
    if err := BackupDatabase(); err != nil {
        return err
    }

    // 2. Update all users to version 2
    if err := MigrateUsersToV2(ctx, repo); err != nil {
        return err
    }

    // 3. Verify migration
    unmigrated, err := repo.CountUnmigratedUsers(ctx)
    if err != nil {
        return err
    }

    if unmigrated > 0 {
        return fmt.Errorf("migration incomplete: %d users remaining", unmigrated)
    }

    // 4. Switch to v2 routes only
    // Deploy new version with only v2 routes

    return nil
}
```

### Differences from v1

| Feature | v1 | v2 |
|---------|----|----|
| **Version Tracking** | None | Internal `version` field |
| **Dependencies** | Hardcoded | Injected (testable) |
| **Status Transitions** | Fixed | Configurable via `UserConfig` |
| **Field Structure** | All required | Optional with `omitempty` |
| **Extensions** | None | `map[string]interface{}` |
| **Multiple IDs** | UUID only | UUID + NanoID support |
| **Verification** | Basic boolean | Structured with timestamps |
| **Metadata** | Fixed fields | Extensible with custom timestamps |
| **API Endpoints** | 7 endpoints | 23 endpoints |
| **Bulk Operations** | None | Bulk status updates |
| **Search** | Basic | Advanced (by roles, status, extensions) |
| **Auto-admin** | Manual | Regex-based automatic detection |

## Configuration Examples

### Web Application
```go
func WebAppUserConfig() *UserConfig {
    return &UserConfig{
        DefaultStatus: "PROVISIONED",
        StatusTransitions: map[string][]string{
            "ACTIVE":      {"PROVISIONED"},
            "SUSPENDED":   {"ACTIVE"},
            "DEACTIVATED": {"PROVISIONED", "ACTIVE", "SUSPENDED"},
        },
        RequiredFields: []string{"email", "first_name", "last_name"},
        ValidRoles:     []string{"ADMIN", "USER", "MODERATOR"},
        EmailVerificationRequired: true,
        MultipleIdentifiers: true,
    }
}
```

### Microservice
```go
func MicroserviceUserConfig() *UserConfig {
    return &UserConfig{
        DefaultStatus: "ACTIVE",
        StatusTransitions: map[string][]string{
            "ACTIVE":   {},
            "INACTIVE": {"ACTIVE"},
        },
        RequiredFields: []string{"email"},
        ValidRoles:     []string{}, // Allow any roles
        EmailVerificationRequired: false,
        MultipleIdentifiers: false,
    }
}
```

### API Service
```go
func APIServiceUserConfig() *UserConfig {
    return &UserConfig{
        DefaultStatus: "ACTIVE",
        StatusTransitions: map[string][]string{
            "ACTIVE":    {"PROVISIONED"},
            "SUSPENDED": {"ACTIVE"},
            "DISABLED":  {"ACTIVE", "SUSPENDED"},
        },
        RequiredFields: []string{"email"},
        ValidRoles:     []string{"SERVICE", "CLIENT", "ADMIN"},
        MultipleIdentifiers: true,
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
    user := &UniversalUser{Version: 0} // Simulating v1 user
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
    
    // Create v1 user (version 0 or missing)
    user := createLegacyUserInDB(t)
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

## Backward Compatibility

The v2 model maintains backward compatibility with v1:

```go
// Legacy methods still work
user.GetUserId()        // Returns user.ID
user.GetUserStatus()    // Returns user.Status
user.GetUserEmail()     // Returns user.Email
user.IsAdmin()          // Returns user.HasRole("ADMIN")

// Profile generation unchanged
profile := user.GetAsProfile()          // Returns *user.UserProfile
microProfile := user.GetAsMicroProfile() // Returns *user.UserMicroProfile
```

## Best Practices

1. **Always use dependency injection** - Makes testing easier
2. **Configure status transitions** - Define valid state changes upfront
3. **Leverage extensions** - Add project-specific fields without modifying core model
4. **Monitor version field** - Track migration progress
5. **Use batch migration** - Migrate users in batches during low-traffic periods
6. **Test thoroughly** - Use mocked dependencies for unit tests
7. **Audit logging** - All mutations are logged automatically
8. **Validate early** - Use `user.Validate()` before database operations

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
- **Users migrated** (version updated from <2 to 2)
- **Migration progress** (% of users with version 2)
- **API endpoint usage** (which v2 endpoints are most used)
- **Status transitions** (track most common transitions)
- **Extension field usage** (which extensions are most popular)

Example query to track migration:
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
            version: { $ifNull: ["$_id", "v1"] },
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
- Review the [model.go](../../external/user/v2/model.go) for implementation details
- Check [service.go](../../external/user/v2/service.go) for business logic
- See [handler.go](../../external/user/v2/handler.go) for HTTP endpoints
- Refer to [routes.go](../../external/user/v2/routes.go) for route configuration 


#### 1. **Utilising Dependency Injection**

The decision to use dependency injection makes testing easier with the new model. See how  the generation of the user's ID now uses an injected dependency.

> see the default dependencies [here](.utils.go).

```go
type IDGenerator interface {
    GenerateUUID() string
    GenerateNanoID() string
}

user.idGenerator.GenerateUUID()
user.stringUtils.InSlice("ADMIN", user.Roles)
```

#### 2. **Configurable Status System**

To improve the compatibility of the `UniversalUser` with a broader range of projects and user types, we have moved away from a fixed status system to a more flexible approach using `UserConfig`. This new system allows for customisable configurations that defines valid statuses, roles, and more etc. different user types have available to them.

A simple example is provided below:

```go
type UserConfig struct {
    DefaultStatus     string
    StatusTransitions map[string][]string
    ValidRoles []string
}

config := &UserConfig{
    StatusTransitions: map[string][]string{
        "ACTIVE": {"PROVISIONED"},
        "SUSPENDED": {"ACTIVE"},
 },
}
```

#### 3. **Flexible Field Structure**

Made fields optional to allow the User model to represent various types of users required for different projects.

```go
type UniversalUser struct {
    PersonalInfo *PersonalInfo `json:"personal_info,omitempty"`
    Extensions   map[string]interface{} `json:"extensions,omitempty"`
}
```

#### 4. **Extensible**

Provided method to make new Universal User model more composable for various types of projects by leveraging "extensions" 

```go
user.SetExtension("department", "Engineering")
user.SetExtension("preferences", map[string]interface{}{
    "theme": "dark",
    "language": "en",
})
```

### Migration Strategies

#### Strategy 1: Gradual Migration (Recommended)

###### Phase 1: Add Universal Model Alongside Current Model
1. Keep your existing `User` model
2. Add the new `UniversalUser` model
3. Use migration functions to convert between them

```go
// Convert existing user to universal
universalUser := MigrateFromLegacyUser(existingUser, factory)

// Convert back when needed
legacyUser := MigrateToLegacyUser(universalUser)
```

###### Phase 2: Start Using Universal Model for New Features
```go
// New user creation
factory := NewUserFactory(WebAppUserConfig())
newUser := factory.CreateUser("user@example.com")

// Existing user operations
existingUser := getExistingUser() // Returns *User
universal := MigrateFromLegacyUser(existingUser, factory)
universal.AddRole("PREMIUM")
updated := MigrateToLegacyUser(universal)
```

###### Phase 3: Full Migration
Replace all `*User` references with `*UniversalUser`

#### Strategy 2: Direct Replacement (High Risk, High Reward)

Replace the current model entirely:

1. **Backup your data**
2. **Update all references**
3. **Test thoroughly**

### Configuration Examples for Different Projects

#### Web Application
```go
config := &UserConfig{
    DefaultStatus: "PROVISIONED",
    StatusTransitions: map[string][]string{
        "ACTIVE":      {"PROVISIONED"},
        "SUSPENDED":   {"ACTIVE"},
        "DEACTIVATED": {"ACTIVE", "SUSPENDED"},
 },
    RequiredFields: []string{"email", "first_name", "last_name"},
    ValidRoles:     []string{"ADMIN", "USER", "MODERATOR"},
    EmailVerificationRequired: true,
}
```

#### Microservice
```go
config := &UserConfig{
    DefaultStatus: "ACTIVE",
    StatusTransitions: map[string][]string{
        "ACTIVE":   {},
        "INACTIVE": {"ACTIVE"},
 },
    RequiredFields: []string{"email"},
    ValidRoles:     []string{}, // Allow any roles
    EmailVerificationRequired: false,
}
```

#### API Service
```go
config := &UserConfig{
    DefaultStatus: "ACTIVE",
    StatusTransitions: map[string][]string{
        "ACTIVE":    {"PROVISIONED"},
        "SUSPENDED": {"ACTIVE"},
        "DISABLED":  {"ACTIVE", "SUSPENDED"},
 },
    RequiredFields: []string{"email"},
    ValidRoles:     []string{"SERVICE", "CLIENT", "ADMIN"},
    MultipleIdentifiers: true, // Both UUID and NanoID
}
```

### Integration with Your Repository

#### Update Repository Interface
```go
type UniversalUserRepository interface {
    CreateUser(ctx context.Context, user *UniversalUser) (*UniversalUser, error)
    GetUserByID(ctx context.Context, id string) (*UniversalUser, error)
    GetUserByEmail(ctx context.Context, email string) (*UniversalUser, error)
    UpdateUser(ctx context.Context, user *UniversalUser) (*UniversalUser, error)
    DeleteUser(ctx context.Context, id string) error
}
```

#### Migration-Aware Repository
```go
type HybridRepository struct {
    legacyRepo *Repository
    universalRepo UniversalUserRepository
    factory *UserFactory
}

func (r *HybridRepository) GetUser(ctx context.Context, id string) (*UniversalUser, error) {
    // Try new repository first
    if user, err := r.universalRepo.GetUserByID(ctx, id); err == nil || user.version == 2 {
        return user, nil
 }
    
    // Fallback to legacy repository
    legacyUser, err := r.legacyRepo.GetUserByID(ctx, id)
    if err != nil {
        return nil, err
 }
    
    // Convert and return
    return MigrateFromLegacyUser(legacyUser, r.factory), nil
}
```

### Testing Benefits

#### Simplified Mocking
```go
func TestUniversalUser_UpdateStatus(t *testing.T) {
    mockTime := &MockTimeProvider{fixedTime: "2025-01-01T00:00:00Z"}
    mockStrings := &MockStringUtils{}
    
    config := &UserConfig{
        StatusTransitions: map[string][]string{"SUSPENDED": {"ACTIVE"}},
 }
    
    user := NewUniversalUser(config, nil, mockTime, mockStrings)
    user.Status = "ACTIVE"
    
    updatedUser, err := user.UpdateStatus("SUSPENDED")
    assert.NoError(t, err)
    assert.Equal(t, "SUSPENDED", updatedUser.Status)
    assert.Equal(t, "2025-01-01T00:00:00Z", updatedUser.Metadata.StatusChangedAt)
}
```

### Backward Compatibility

The universal model maintains backward compatibility:

```go
// Legacy methods still work
user.GetUserId()        // Returns user.ID
user.GetUserStatus()    // Returns user.Status
user.IsAdmin()          // Returns user.HasRole("ADMIN")

// Profile generation unchanged
profile := user.GetAsProfile()
microProfile := user.GetAsMicroProfile()
```
