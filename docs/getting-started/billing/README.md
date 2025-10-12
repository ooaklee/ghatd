# Billing Package Documentation

## Overview

The `billing` package (`external/billing`) provides core subscription and billing event management with MongoDB persistence. It forms the data layer of the billing system, handling storage, retrieval, and management of subscriptions and billing events with optimised indexes for performance.

## Table of Contents

- [Key Features](#key-features)
- [Architecture](#architecture)
- [MongoDB Setup](#mongodb-setup)
- [Email-Based Subscriptions](#email-based-subscriptions)
- [Service Methods](#service-methods)
- [Repository Implementation](#repository-implementation)
- [Best Practices](#best-practices)

## Key Features

### 1. **Email-Based Subscriptions**
Subscriptions can be created with just an email address, enabling pre-registration purchases:
- Users can purchase before signing up
- Automatic association when user creates account
- Orphan subscription management tools

### 2. **Optimised MongoDB Indexes**
Purpose-built indexes for efficient queries:
- Standard user/email lookups
- Partial indexes for orphaned subscriptions
- Unique constraints preventing duplicates
- Time-based sorting and filtering

### 3. **Dual Repository Pattern**
Flexible data access with two implementations:
- **MongoDbStore**: Production-ready MongoDB persistence
- **InMemoryRepositoryStore**: Testing and development

### 4. **Comprehensive Event Tracking**
Every billing action is recorded:
- Payment events
- Subscription changes
- Status updates
- Full audit trail

## Architecture

### Service Layer

The billing service provides business logic and orchestration:

```
Application
     │
     ▼
┌─────────────────────────────────────┐
│     Billing Service                 │
│  - Business logic                   │
│  - Validation                       │
│  - Email standardisation            │
│  - Error handling                   │
└──────────────┬──────────────────────┘
               │
               ▼
┌─────────────────────────────────────┐
│     Repository Interface            │
│  - Abstract data access             │
└──────────────┬──────────────────────┘
               │
       ┌───────┴────────┐
       ▼                ▼
┌──────────────┐  ┌────────────────────┐
│  MongoDB     │  │  In-Memory         │
│  Repository  │  │  Repository        │
└──────────────┘  └────────────────────┘
       │
       ▼
┌──────────────┐
│  MongoDB     │
│  Database    │
└──────────────┘
```

### Collections

The billing package uses two MongoDB collections:

1. **`billing_subscriptions`** - Subscription records
2. **`billing_events`** - Billing event history

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

Create a migration runner to set up indexes:

```go
package main

import (
    "context"
    "log"
    "os"
    
    billingMigration "github.com/ooaklee/ghatd/external/billing/migrations"
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
    
    // Initialise migration system
    migrate.SetDatabase(db)
    
    // Register billing indexes migrations
    migrate.Register(
        billingMigration.InitBillingSubscriptionIndexesUp,
        billingMigration.InitBillingSubscriptionIndexesDown,
    )
    migrate.Register(
        billingMigration.InitBillingEventsIndexesUp,
        billingMigration.InitBillingEventsIndexesDown,
    )
    
    // Run migrations
    log.Println("Running billing indexes migrations...")
    if err := migrate.Up(migrate.AllAvailable); err != nil {
        log.Fatalf("Migration failed: %v", err)
    }
    
    log.Println("✓ Migrations completed successfully")
}
```

### Migration Commands

```bash
# Run all migrations
go run cmd/mongo-migrator/migrator.go up

# Rollback one migration
go run cmd/mongo-migrator/migrator.go down 1

# Rollback all migrations
go run cmd/mongo-migrator/migrator.go down all

# Check migration status
go run cmd/mongo-migrator/migrator.go status
```

### Subscription Indexes

Five indexes are created for the `billing_subscriptions` collection:

| Index Name | Fields | Type | Purpose | Example Query |
|------------|--------|------|---------|---------------|
| `idx_subscriptions_user_id` | `user_id` | Standard | Fetch all subscriptions for a user | `db.billing_subscriptions.find({user_id: "user-123"})` |
| `idx_subscriptions_email` | `email` | Standard | Query subscriptions by email | `db.billing_subscriptions.find({email: "user@example.com"})` |
| `idx_subscriptions_email_no_user` | `email` | Partial | Find orphaned subscriptions (no user ID) | `db.billing_subscriptions.find({email: "user@example.com", user_id: {$in: ["", null]}})` |
| `idx_subscriptions_integrator` | `integrator`, `integrator_subscription_id` | Unique Compound | Prevent duplicate subscriptions from same provider | Used internally by MongoDB for uniqueness |
| `idx_subscriptions_created_at` | `created_at` | Standard (Descending) | Sort/filter by date | `db.billing_subscriptions.find().sort({created_at: -1})` |

**Partial Index Details:**

The `idx_subscriptions_email_no_user` index uses a partial filter expression to only index subscriptions without a user ID:

```javascript
{
  $or: [
    { user_id: "" },
    { user_id: { $exists: false } },
    { user_id: null }
  ]
}
```

This optimises queries for orphaned subscriptions while keeping the index size minimal.

### Billing Events Indexes

Four indexes are created for the `billing_events` collection:

| Index Name | Fields | Type | Purpose | Example Query |
|------------|--------|------|---------|---------------|
| `idx_billing_events_user_id` | `user_id` | Standard | Fetch event history for a user | `db.billing_events.find({user_id: "user-123"})` |
| `idx_billing_events_email` | `email` | Standard | Query events by email | `db.billing_events.find({email: "user@example.com"})` |
| `idx_billing_events_subscription_id` | `integrator_subscription_id` | Standard | Get all events for a subscription | `db.billing_events.find({integrator_subscription_id: "sub-123"})` |
| `idx_billing_events_created_at` | `created_at` | Standard (Descending) | Sort/filter by date | `db.billing_events.find().sort({created_at: -1})` |

### Verifying Indexes

After running migrations, verify the indexes were created:

```javascript
// Connect to MongoDB
use your_database_name

// Check subscription indexes
db.billing_subscriptions.getIndexes()

// Check billing events indexes
db.billing_events.getIndexes()

// Check index usage stats
db.billing_subscriptions.aggregate([
    { $indexStats: {} }
])
```

### Dropping Indexes (Rollback)

The migration system includes down functions to remove indexes:

```go
// Rollback subscription indexes
if err := billingMigration.InitBillingSubscriptionIndexesDown(db); err != nil {
    log.Printf("Failed to drop subscription indexes: %v", err)
}

// Rollback events indexes
if err := billingMigration.InitBillingEventsIndexesDown(db); err != nil {
    log.Printf("Failed to drop events indexes: %v", err)
}
```

Or use the migration CLI:

```bash
# Rollback last migration set
go run cmd/mongo-migrator/migrator.go down 1
```

## Email-Based Subscriptions

### Overview

The billing package supports subscriptions without a user ID, enabling pre-registration purchases. This allows users to purchase subscriptions before creating an account.

### Core Concept

**Subscription States:**

1. **Orphaned**: `UserID=""`, `Email="user@example.com"` (pre-registration)
2. **Associated**: `UserID="user-123"`, `Email="user@example.com"` (after signup)

### Flow Diagram

```
┌─────────────────────────────────────────────────────────────┐
│  1. Pre-Registration Purchase                               │
│     User purchases without account                          │
│     Webhook creates subscription with email only            │
└────────────────────────┬────────────────────────────────────┘
                         │
                         ▼
┌─────────────────────────────────────────────────────────────┐
│  MongoDB: billing_subscriptions                             │
│  {                                                          │
│    "id": "sub-123",                                         │
│    "user_id": "",              ← Empty                      │
│    "email": "user@example.com", ← Has email                 │
│    "status": "active",                                      │
│    ...                                                      │
│  }                                                          │
└────────────────────────┬────────────────────────────────────┘
                         │
                         ▼
┌─────────────────────────────────────────────────────────────┐
│  2. User Signs Up                                           │
│     User creates account with same email                    │
│     accessmanager calls AssociateSubscriptionsWithUser      │
└────────────────────────┬────────────────────────────────────┘
                         │
                         ▼
┌─────────────────────────────────────────────────────────────┐
│  MongoDB: billing_subscriptions                             │
│  {                                                          │
│    "id": "sub-123",                                         │
│    "user_id": "user-456",      ← Now has user ID            │
│    "email": "user@example.com",                             │
│    "status": "active",                                      │
│    ...                                                      │
│  }                                                          │
└─────────────────────────────────────────────────────────────┘
```

### Use Cases

**1. Pre-Launch Sales**
```
Marketing campaign before platform launch
→ Users purchase early bird subscriptions
→ Platform launches
→ Users sign up
→ Subscriptions automatically associated
```

**2. Gift Subscriptions**
```
Alice buys subscription for bob@example.com
→ Bob doesn't have account yet
→ Subscription stored with email
→ Bob signs up weeks later
→ Gets subscription automatically
```

**3. Corporate Bulk Purchases**
```
Company admin buys 10 licenses
→ Provides list of employee emails
→ Subscriptions created for each email
→ Employees sign up at their convenience
→ Each gets their subscription
```

### Service Methods

#### Query by Email

Retrieve subscriptions before user account exists:

```go
// Get all subscriptions for an email (regardless of user_id)
response, err := billingService.GetSubscriptionsByEmail(ctx, 
    &billing.GetSubscriptionsByEmailRequest{
        Email: "user@example.com",
    })

if err != nil {
    log.Printf("Error: %v", err)
    return
}

for _, sub := range response.Subscriptions {
    if sub.UserID == "" {
        log.Printf("Orphaned subscription: %s", sub.ID)
    } else {
        log.Printf("Associated subscription: %s (UserID: %s)", sub.ID, sub.UserID)
    }
}
```

#### Get Billing Events by Email

Retrieve billing history for an email:

```go
response, err := billingService.GetBillingEventsByEmail(ctx, 
    &billing.GetBillingEventsByEmailRequest{
        Email:      "user@example.com",
        EventTypes: []string{"payment.succeeded", "payment.failed"}, // Optional
        Limit:      50,                                               // Optional
    })

if err != nil {
    log.Printf("Error: %v", err)
    return
}

for _, event := range response.Events {
    log.Printf("[%s] %s - %s", 
        event.EventType, 
        event.EventTime.Format("2006-01-02 15:04:05"),
        event.Status)
}
```

#### Associate Subscriptions

Manually link email-based subscriptions to a user:

```go
// When user signs up or when you want to manually associate
result, err := billingService.AssociateSubscriptionsWithUser(ctx, 
    &billing.AssociateSubscriptionsWithUserRequest{
        UserID: "user-123",
        Email:  "user@example.com",
    })

if err != nil {
    log.Printf("Association failed: %v", err)
    return
}

log.Printf("Associated %d subscriptions with user %s", 
    result.AssociatedCount, result.UserID)
```

**Note:** The `accessmanager` package automatically calls this method when a user creates an account, so manual calls are typically only needed for administrative operations.

### Orphan Management

#### Find Unassociated Subscriptions

Monitor subscriptions that haven't been claimed:

```go
// Get all orphaned subscriptions
orphans, err := billingService.GetUnassociatedSubscriptions(ctx, 
    &billing.GetUnassociatedSubscriptionsRequest{
        // All filters are optional
        IntegratorName: "stripe",           // Filter by provider
        Email:          "user@example.com", // Filter by specific email
        CreatedAtFrom:  "2025-01-01",       // Filter by date range
        Limit:          100,                // Limit results (default: 100)
    })

if err != nil {
    log.Printf("Error: %v", err)
    return
}

log.Printf("Found %d orphaned subscriptions", len(orphans.Subscriptions))

for _, sub := range orphans.Subscriptions {
    daysSinceCreated := time.Since(sub.CreatedAt).Hours() / 24
    log.Printf("Subscription %s (email: %s) orphaned for %.0f days", 
        sub.ID, sub.Email, daysSinceCreated)
}
```

#### Update Subscription User ID

Manually associate a specific subscription:

```go
// Associate a specific subscription with a user
updated, err := billingService.UpdateSubscriptionUserID(ctx, 
    &billing.UpdateSubscriptionUserIDRequest{
        SubscriptionID: "sub-123",
        UserID:         "user-456",
    })

if err != nil {
    log.Printf("Update failed: %v", err)
    return
}

log.Printf("Updated subscription %s", updated.Subscription.ID)
```

### Monitoring Queries

**Count orphaned subscriptions:**

```javascript
db.billing_subscriptions.countDocuments({
    $or: [
        { user_id: "" },
        { user_id: { $exists: false } },
        { user_id: null }
    ]
})
```

**Find orphaned subscriptions older than 30 days:**

```javascript
db.billing_subscriptions.find({
    $or: [
        { user_id: "" },
        { user_id: { $exists: false } },
        { user_id: null }
    ],
    created_at: { 
        $lt: new Date(Date.now() - 30 * 24 * 60 * 60 * 1000) 
    }
}).sort({ created_at: -1 })
```

**Group orphaned subscriptions by provider:**

```javascript
db.billing_subscriptions.aggregate([
    {
        $match: {
            $or: [
                { user_id: "" },
                { user_id: { $exists: false } },
                { user_id: null }
            ]
        }
    },
    {
        $group: {
            _id: "$integrator",
            count: { $sum: 1 },
            total_value: { $sum: "$amount" }
        }
    }
])
```

### Best Practices

**1. Email Normalisation**
- Emails are automatically converted to lowercase
- Ensures consistent matching between purchase and signup
- No manual normalisation required

**2. Monitoring**
- Set up alerts for subscriptions orphaned > 30 days
- Track conversion rate from orphaned to associated
- Monitor by provider and date range

**3. User Experience**
- Show "You have a pending subscription" message on signup page if email matches orphaned subscription
- Send reminder emails to users with orphaned subscriptions
- Provide clear instructions for claiming subscriptions

**4. Administrative Tools**
- Build admin dashboard showing orphaned subscriptions
- Allow manual association via UI
- Export orphaned subscriptions for analysis

## Service Methods

The billing service provides methods for subscription and event management.

### Subscription Management

#### CreateSubscription

Create a new subscription:

```go
response, err := billingService.CreateSubscription(ctx, 
    &billing.CreateSubscriptionRequest{
        IntegratorSubscriptionID: "stripe_sub_abc123",
        Integrator:               "stripe",
        UserID:                   "user-123",     // Can be empty for pre-reg
        Email:                    "user@example.com",
        PlanName:                 "Pro Plan",
        Status:                   billing.StatusActive,
        Amount:                   2999,           // In cents
        Currency:                 "USD",
        BillingPeriod:            "month",
        NextBillingDate:          nextMonth,
        CancelAtPeriodEnd:        false,
        TrialEndDate:             nil,
    })
```

#### GetSubscriptions

Query subscriptions with filters:

```go
response, err := billingService.GetSubscriptions(ctx, 
    &billing.GetSubscriptionsRequest{
        ForUserIDs:  []string{"user-123", "user-456"},  // Filter by users
        Statuses:    []string{"active", "trialing"},    // Filter by status
        Integrators: []string{"stripe", "lemonsqueezy"}, // Filter by provider
        PerPage:     25,                                 // Pagination
        Page:        1,                                  // Page number
        Order:       "created_at_desc",                  // Sort order
    })

for _, sub := range response.Subscriptions {
    log.Printf("Subscription %s: %s (%s)", 
        sub.ID, sub.PlanName, sub.Status)
}
```

#### UpdateSubscription

Update subscription details:

```go
response, err := billingService.UpdateSubscription(ctx, 
    &billing.UpdateSubscriptionRequest{
        SubscriptionID:    "sub-123",
        Status:            "cancelled",
        CancelAtPeriodEnd: true,
        UpdatedAt:         time.Now(),
    })
```

### Billing Event Management

#### CreateBillingEvent

Record a billing event:

```go
response, err := billingService.CreateBillingEvent(ctx, 
    &billing.CreateBillingEventRequest{
        SubscriptionID:           "sub-123",
        IntegratorEventID:        "evt_stripe_456",
        IntegratorSubscriptionID: "stripe_sub_abc123",
        Integrator:               "stripe",
        UserID:                   "user-123",
        Email:                    "user@example.com",
        EventType:                "payment.succeeded",
        EventTime:                time.Now(),
        Amount:                   2999,
        Currency:                 "USD",
        PlanName:                 "Pro Plan",
        Status:                   billing.EventStatusProcessed,
        Description:              "Monthly subscription payment",
    })
```

#### GetBillingEvents

Query billing events:

```go
response, err := billingService.GetBillingEvents(ctx, 
    &billing.GetBillingEventsRequest{
        ForUserIDs:      []string{"user-123"},
        SubscriptionIDs: []string{"sub-123"},
        EventTypes:      []string{"payment.succeeded"},
        PerPage:         50,
        Page:            1,
        Order:           "created_at_desc",
    })

for _, event := range response.Events {
    log.Printf("[%s] %s: $%.2f", 
        event.EventTime.Format("2006-01-02"),
        event.EventType,
        float64(event.Amount)/100)
}
```

## Repository Implementation

### MongoDB Repository

Production-ready MongoDB implementation:

```go
import (
    "github.com/ooaklee/ghatd/external/billing"
    "github.com/ooaklee/ghatd/external/repository"
)

// Initialise MongoDB store
mongoStore := &repository.MongoDbStore{
    Database: mongoDatabase,
}

// Create repository
billingRepo := billing.NewRepository(mongoStore)

// Create service
billingService := billing.NewService(billingRepo, billingRepo)
```

### In-Memory Repository

For testing and development:

```go
import (
    "github.com/ooaklee/ghatd/external/billing"
)

// Initialise in-memory store
store := &billing.InMemoryRepositoryStore{
    Subscriptions: make(map[string]*billing.Subscription),
    Events:        make(map[string]*billing.BillingEvent),
}

// Create repository
inMemoryRepo := billing.NewInMemoryRepository(store)

// Create service
billingService := billing.NewService(inMemoryRepo, inMemoryRepo)
```

### Testing Example

```go
func TestAssociateSubscriptions(t *testing.T) {
    // Setup in-memory repository
    store := &billing.InMemoryRepositoryStore{
        Subscriptions: make(map[string]*billing.Subscription),
    }
    repo := billing.NewInMemoryRepository(store)
    service := billing.NewService(repo, repo)
    
    ctx := context.Background()
    
    // Create orphaned subscription
    createResp, err := service.CreateSubscription(ctx, 
        &billing.CreateSubscriptionRequest{
            IntegratorSubscriptionID: "test-sub-123",
            Integrator:               "stripe",
            UserID:                   "", // Orphaned
            Email:                    "test@example.com",
            Status:                   "active",
            Amount:                   2999,
            Currency:                 "USD",
            PlanName:                 "Test Plan",
        })
    assert.NoError(t, err)
    assert.Empty(t, createResp.Subscription.UserID)
    
    // Associate with user
    associateResp, err := service.AssociateSubscriptionsWithUser(ctx, 
        &billing.AssociateSubscriptionsWithUserRequest{
            UserID: "user-456",
            Email:  "test@example.com",
        })
    assert.NoError(t, err)
    assert.Equal(t, 1, associateResp.AssociatedCount)
    
    // Verify association
    getResp, err := service.GetSubscriptions(ctx, 
        &billing.GetSubscriptionsRequest{
            ForUserIDs: []string{"user-456"},
        })
    assert.NoError(t, err)
    assert.Len(t, getResp.Subscriptions, 1)
    assert.Equal(t, "user-456", getResp.Subscriptions[0].UserID)
}
```

## Best Practices

### 1. Index Management

**Always run migrations before deployment:**

```bash
# In your CI/CD pipeline
go run cmd/mongo-migrator/migrator.go up
```

**Monitor index usage:**

```javascript
// Check which indexes are being used
db.billing_subscriptions.aggregate([
    { $indexStats: {} }
])

// Look for indexes with low usage
db.billing_subscriptions.aggregate([
    { $indexStats: {} },
    { 
        $match: { 
            "accesses.ops": { $lt: 100 } 
        } 
    }
])
```

**Rebuild indexes periodically:**

```javascript
// Rebuild all indexes (do during maintenance window)
db.billing_subscriptions.reIndex()
db.billing_events.reIndex()
```

### 2. Email Standardisation

Emails are automatically normalised to lowercase:

```go
// These all match the same subscription
billingService.GetSubscriptionsByEmail(ctx, &billing.GetSubscriptionsByEmailRequest{
    Email: "User@Example.COM",  // Normalised to "user@example.com"
})
```

No manual normalisation required - the service handles it.

### 3. Context Usage

Always pass context for cancellation and timeouts:

```go
// With timeout
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()

response, err := billingService.GetSubscriptions(ctx, req)

// With cancellation
ctx, cancel := context.WithCancel(context.Background())
go func() {
    <-shutdownSignal
    cancel()
}()

response, err := billingService.ProcessWebhook(ctx, webhookData)
```

### 4. Orphan Monitoring

Set up automated monitoring:

```go
// Daily cron job to report orphaned subscriptions
func reportOrphanedSubscriptions() {
    ctx := context.Background()
    
    // Get orphans older than 30 days
    thirtyDaysAgo := time.Now().AddDate(0, 0, -30)
    
    response, err := billingService.GetUnassociatedSubscriptions(ctx, 
        &billing.GetUnassociatedSubscriptionsRequest{
            CreatedAtFrom: thirtyDaysAgo.Format("2006-01-02"),
            Limit:         1000,
        })
    
    if err != nil {
        log.Printf("Error fetching orphans: %v", err)
        return
    }
    
    if len(response.Subscriptions) > 0 {
        // Send alert
        sendAlert(fmt.Sprintf(
            "Found %d orphaned subscriptions older than 30 days",
            len(response.Subscriptions)))
    }
}
```

## Further Reading

- [Billing Manager Documentation](../billing-manager/README.md) - High-level billing system overview
- [Email-Based Subscriptions](../billing-manager/EMAIL_BASED_SUBSCRIPTIONS.md) - Detailed pre-registration flow
- [Service Implementation](../../../external/billing/service.go) - Complete service code
- [Repository Implementation](../../../external/billing/repository.go) - MongoDB implementation details
- [Migration Files](../../../external/billing/migrations/) - Index migration code

## Support

For issues or questions:
- Review service methods in `external/billing/service.go`
- Check repository implementation in `external/billing/repository.go`
- See migration files in `external/billing/migrations/`
- Refer to examples in `external/billing/examples/examples.go`
