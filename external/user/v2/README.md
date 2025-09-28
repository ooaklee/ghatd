# Migration Guide - User -> Universal User (BETA) Model 

## Overview

The Universal User Model (User v2) was designed to be more flexible and applicable to various projects compared to the earlier user model.

## Differences

Several changes have been made to this model compared to the previous one, including but not limited to

### 1. **Utilising Dependency Injection**

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

### 2. **Configurable Status System**

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

### 3. **Flexible Field Structure**

Made fields optional to allow the User model to represent various types of users required for different projects.

```go
type UniversalUser struct {
    PersonalInfo *PersonalInfo `json:"personal_info,omitempty"`
    Extensions   map[string]interface{} `json:"extensions,omitempty"`
}
```

### 4. **Extensible**

Provided method to make new Universal User model more composable for various types of projects by leveraging "extensions" 

```go
user.SetExtension("department", "Engineering")
user.SetExtension("preferences", map[string]interface{}{
    "theme": "dark",
    "language": "en",
})
```

## Migration Strategies

### Strategy 1: Gradual Migration (Recommended)

#### Phase 1: Add Universal Model Alongside Current Model
1. Keep your existing `User` model
2. Add the new `UniversalUser` model
3. Use migration functions to convert between them

```go
// Convert existing user to universal
universalUser := MigrateFromLegacyUser(existingUser, factory)

// Convert back when needed
legacyUser := MigrateToLegacyUser(universalUser)
```

#### Phase 2: Start Using Universal Model for New Features
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

#### Phase 3: Full Migration
Replace all `*User` references with `*UniversalUser`

### Strategy 2: Direct Replacement (High Risk, High Reward)

Replace the current model entirely:

1. **Backup your data**
2. **Update all references**
3. **Test thoroughly**

## Configuration Examples for Different Projects

### Web Application
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

### Microservice
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

### API Service
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

## Integration with Your Repository

### Update Repository Interface
```go
type UniversalUserRepository interface {
    CreateUser(ctx context.Context, user *UniversalUser) (*UniversalUser, error)
    GetUserByID(ctx context.Context, id string) (*UniversalUser, error)
    GetUserByEmail(ctx context.Context, email string) (*UniversalUser, error)
    UpdateUser(ctx context.Context, user *UniversalUser) (*UniversalUser, error)
    DeleteUser(ctx context.Context, id string) error
}
```

### Migration-Aware Repository
```go
type HybridRepository struct {
    legacyRepo *Repository
    universalRepo UniversalUserRepository
    factory *UserFactory
}

func (r *HybridRepository) GetUser(ctx context.Context, id string) (*UniversalUser, error) {
    // Try new repository first
    if user, err := r.universalRepo.GetUserByID(ctx, id); err == nil {
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

## Testing Benefits

### Simplified Mocking
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

## Backward Compatibility

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
