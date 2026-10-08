# Billing

The `billing` package (`external/billing`) gives you the tools for core subscription and billing event management, with MongoDB persistence. It acts as the data layer for your billing system, handling how you store, retrieve, and manage subscriptions and billing events, using optimised indexes for better performance.

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
Create subscriptions with just an email address, allowing for pre-registration purchases:
- Users can purchase before signing up.
- Automatic association when a user creates an account.
- Tools for managing orphan subscriptions.

### 2. **Optimised MongoDB Indexes**
Comes with purpose-built indexes for efficient queries:
- Standard user/email lookups.
- Partial indexes for orphaned subscriptions.
- Unique constraints to prevent duplicates.
- Time-based sorting and filtering.

### 3. **Dual Repository Pattern**
A flexible data access layer with two implementations:
- **MongoDbStore**: Production-ready MongoDB persistence.
- **InMemoryRepositoryStore**: For testing and development.

### 4. **Comprehensive Event Tracking**
Records every billing action:
- Payment events.
- Subscription changes.
- Status updates.
- A full audit trail.

## Architecture

### Service Layer

The billing service holds the business logic and orchestration:

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

1. **`billing_subscriptions`** - Stores subscription records.
2. **`billing_events`** - Holds the billing event history.

## MongoDB Setup

### Prerequisites

- MongoDB 4.2 or higher.
- `mongo-migrate` library: `github.com/xakep666/mongo-migrate`.
- Go mongo driver: `go.mongodb.org/mongo-driver/v2/mongo`.

### Installing Dependencies

```bash
asdf exec go get github.com/xakep666/mongo-migrate
asdf exec go get go.mongodb.org/mongo-driver/v2/mongo
```

### Running Migrations

Register the billing index helpers from the host application's
`migrations/mongo` package:

```go
package migrations

import (
    "context"

    billingmigrations "github.com/ooaklee/ghatd/external/billing/migrations"
    migrate "github.com/xakep666/mongo-migrate"
    "go.mongodb.org/mongo-driver/v2/mongo"
)

func init() {
    if err := migrate.Register(
        func(_ context.Context, db *mongo.Database) error {
            return billingmigrations.InitBillingSubscriptionIndexesUp(db)
        },
        func(_ context.Context, db *mongo.Database) error {
            return billingmigrations.InitBillingSubscriptionIndexesDown(db)
        },
    ); err != nil {
        panic(err)
    }
    if err := migrate.Register(
        func(_ context.Context, db *mongo.Database) error {
            return billingmigrations.InitBillingEventsIndexesUp(db)
        },
        func(_ context.Context, db *mongo.Database) error {
            return billingmigrations.InitBillingEventsIndexesDown(db)
        },
    ); err != nil {
        panic(err)
    }
}
```

Ensure the host's command adapter blank-imports its migration package, then run:

```sh
asdf exec go run main.go mongo-migrator up
```

The shared command applies every pending registered migration. Its `down`
action reverts all applied registered migrations and does not support partial
rollback or status actions. See
[Managing MongoDB Migrations](../../docs/how-to/manage-mongodb-migrations.md)
for command wiring, configuration, and rollback precautions.

### Subscription Indexes

Four indexes are created for the `billing_subscriptions` collection:

| Index Name | Fields | Type | Purpose | Example Query |
|------------|--------|------|---------|---------------|
| `idx_subscriptions_user_id` | `user_id` | Standard | Fetch all subscriptions for a user. | `db.billing_subscriptions.find({user_id: "user-123"})` |
| `idx_subscriptions_email` | `email` | Standard | Query subscriptions by email. | `db.billing_subscriptions.find({email: "user@example.com"})` |
| `idx_subscriptions_integrator` | `integrator`, `integrator_subscription_id` | Unique Compound | Prevent duplicate subscriptions from the same provider. | Used internally by MongoDB for uniqueness. |
| `idx_subscriptions_created_at` | `created_at` | Standard (Descending) | Sort/filter by date. | `db.billing_subscriptions.find().sort({created_at: -1})` |

### Billing Events Indexes

Four indexes are created for the `billing_events` collection:

| Index Name | Fields | Type | Purpose | Example Query |
|------------|--------|------|---------|---------------|
| `idx_billing_events_user_id` | `user_id` | Standard | Fetch event history for a user. | `db.billing_events.find({user_id: "user-123"})` |
| `idx_billing_events_email` | `email` | Standard | Query events by email. | `db.billing_events.find({email: "user@example.com"})` |
| `idx_billing_events_subscription_id` | `integrator_subscription_id` | Standard | Get all events for a subscription. | `db.billing_events.find({integrator_subscription_id: "sub-123"})` |
| `idx_billing_events_created_at` | `created_at` | Standard (Descending) | Sort/filter by date. | `db.billing_events.find().sort({created_at: -1})` |

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

Host migration tooling may wrap these down functions when an explicit rollback
command is required.

## Email-Based Subscriptions

### Overview

The billing package supports subscriptions without a user ID, enabling pre-registration purchases. This allows users to buy subscriptions before creating an account.

### Core Concept

**Subscription States:**

1. **Orphaned**: `UserID=""`, `Email="user@example.com"` (pre-registration).
2. **Associated**: `UserID="user-123"`, `Email="user@example.com"` (after signup).

### Flow Diagram

```
┌─────────────────────────────────────────────────────────────┐
│  1. Pre-Registration Purchase                               │
│     User purchases without an account                       │
│     Webhook creates a subscription with email only          │
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
│     User creates an account with the same email             │
│     `accessmanager` calls `AssociateSubscriptionsWithUser`  │
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
→ Users purchase early-bird subscriptions
→ Platform launches
→ Users sign up
→ Subscriptions are automatically associated
```

**2. Gift Subscriptions**
```
Alice buys a subscription for bob@example.com
→ Bob doesn't have an account yet
→ Subscription is stored with the email
→ Bob signs up weeks later
→ Gets the subscription automatically
```

**3. Corporate Bulk Purchases**
```
A company admin buys 10 licences
→ Provides a list of employee emails
→ Subscriptions are created for each email
→ Employees sign up at their convenience
→ Each gets their subscription
```

### Service Methods

#### Query by Email

Retrieve subscriptions before a user account exists:

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
// When a user signs up or when you want to manually associate
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
- Emails are automatically converted to lowercase.
- This ensures consistent matching between purchase and signup.
- No manual normalisation is required.

**2. Monitoring**
- Set up alerts for subscriptions orphaned for more than 30 days.
- Track the conversion rate from orphaned to associated.
- Monitor by provider and date range.

**3. User Experience**
- Show a "You have a pending subscription" message on the signup page if the email matches an orphaned subscription.
- Send reminder emails to users with orphaned subscriptions.
- Provide clear instructions for claiming subscriptions.

**4. Administrative Tools**
- Build an admin dashboard showing orphaned subscriptions.
- Allow manual association via the UI.
- Export orphaned subscriptions for analysis.

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
        UserID:                   "user-123",     // Can be empty for pre-registration
        Email:                    "user@example.com",
        PlanName:                 "Pro Plan",
        Status:                   billing.StatusActive,
        Amount:                   2999,           // In cents
        Currency:                 "USD",
        BillingInterval:          "month",
        NextBillingDate:          nextMonth,
        TrialEndsAt:              nil,
    })
```

#### GetSubscriptions

Query subscriptions with filters:

```go
response, err := billingService.GetSubscriptions(ctx, 
    &billing.GetSubscriptionsRequest{
        ForUserIDs:    []string{"user-123", "user-456"}, // Filter by users
        Statuses:      []string{"active", "trialing"},   // Filter by status
        IntegratorName: "stripe",                         // Filter by one provider
        PerPage:       25,                                 // Pagination
        Page:          1,                                  // Page number
        Order:         "created_at_desc",                  // Sort order
    })

for _, sub := range response.Subscriptions {
    log.Printf("Subscription %s: %s (%s)", 
        sub.ID, sub.PlanName, sub.Status)
}
```

#### UpdateSubscription

Update subscription details:

```go
cancelled := billing.StatusCancelled
response, err := billingService.UpdateSubscription(ctx,
    &billing.UpdateSubscriptionRequest{
        ID:     "sub-123",
        Status: &cancelled,
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
    })
```

#### GetBillingEvents

Query billing events:

```go
response, err := billingService.GetBillingEvents(ctx, 
    &billing.GetBillingEventsRequest{
        ForUserIDs:              []string{"user-123"},
        IntegratorSubscriptionID: "stripe_sub_abc123",
        EventTypes:              []string{"payment.succeeded"},
        PerPage:                 50,
        Page:                    1,
        Order:                   "created_at_desc",
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
asdf exec go run main.go mongo-migrator up
```

The host application must register the billing migrations as described in
[Running Migrations](#running-migrations). Keep rollback as an explicit,
reviewed operation: `mongo-migrator down` reverts every applied registered
migration rather than only the latest billing change.

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

- [Billing Manager Documentation](../billingmanager/README.md) - High-level billing system overview
- [Email-Based Subscriptions (ADR004)](../../docs/adr/adr004-email-based-subscriptions-for-pre-registration-purchases.md) - Pre-registration purchase flow
- [Service Implementation](service.go) - Complete service code
- [Repository Implementation](repository.go) - MongoDB implementation details
- [Migration Files](migrations/) - Index migration code

## Support

For issues or questions:
- Review service methods in `external/billing/service.go`
- Check repository implementation in `external/billing/repository.go`
- See migration files in `external/billing/migrations/`
- Refer to examples in `external/billing/examples/examples.go`

## Verified revenue feed

`RevenueService` owns financial fact validation, provider/account/mode economic
identity, immutable fingerprints and durable transaction acceptance. Its
`RevenueRepository` atomically appends facts and delivery observations before
webhook acknowledgement. The additive encrypted implementation is
[`revenuestore`](revenuestore/README.md), using shared transaction-capable
repository helpers. There is no nontransactional or in-memory feed fallback.

`PaidMinor` is verified net paid line revenue after discounts and credits,
excluding tax; it is independent from the existing subscription access model,
individual billing-event gross amounts and catalogue commercial terms. Provider
price/customer references and the historical principal/plan/cost association
remain attached to accepted economic facts. Different webhook envelopes may
refer to one economic allocation; they add observations rather than duplicate
money. A changed payload under an existing economic identity conflicts.

Consumers read bounded pending facts and acknowledge only with a durable owning
acceptance or explicit no-entitlement/quarantine decision. A global cursor must
not skip earlier unresolved facts. `GetRevenueAcknowledgement` reads the
consumer's immutable owning receipt after a lost reply; its historical actor
never supplies current permission. An adapter without the optional receipt-read
capability returns unavailable. Quarantined delivery observations containing
no facts have a separate source-reconciliation contract; they cannot be cleared
with a fact acknowledgement. `ResolveQuarantinedRevenue` requires the original
fingerprint, authenticated recovered facts (or a reasoned no-revenue outcome)
and a server-bound actor. Facts and the immutable resolution commit together;
the original quarantine remains available. Lost acknowledgement is recovered by
reading/replaying the immutable resolution. Source digests and actor identity
are private JSON fields, retained explicitly in encrypted persistence.

This owning service does not authenticate HTTP callers or query a provider.
The billing manager supplies those boundaries and the provider evidence.
Applications must supply current scoped worker/operator authority, complete
historical payer/plan resolution, migrations and bounded reconciliation workers
before exposing or scheduling the feature.

Optional `RevenuePagingRepository` supports bounded full sweeps through pending
facts and source quarantines. `PendingRevenueFactsAfter` uses acceptance sequence;
`UnresolvedRevenueObservationsAfter` uses retained source ID. Advancing either
read position does not acknowledge or resolve anything. Earlier failures remain
pending and must be revisited on subsequent sweeps. The capability is explicit;
adapters without it return unavailable rather than silently truncating work.

### Immutable checkout and historical payer identity

`CheckoutService` stores the exact server-authorized provider request before a
checkout POST. The intent retains the paying account, plan/cost/provider price,
amount, currency, cadence, trial terms, return URL and request key. Personal
request fields are excluded from public JSON and explicitly retained in an
encrypted repository envelope. Use `revenuestore.Repository` after preparing
and probing the shared transaction-capable store.

`FindCheckoutIntent` recovers frozen parameters before consulting a mutable
catalogue or profile. `PrepareCheckout` conflicts when an existing key is reused
with different parameters. `AcknowledgeCheckout` atomically reserves a provider
session for one intent within provider/account/live scope. A lost POST response
leaves the intent unresolved; it is not evidence that submission failed.

`ResolveRevenueAssociation` first reads immutable subscription/price history.
For new history, it requires authenticated complete checkout-session/line-item
evidence and a matching pre-existing intent. Provider metadata supplies only the
opaque intent pointer; principal and plan/cost come from the owning record.
Session, customer, subscription, original price amount/cadence, currency, mode,
client reference and creation time must agree. Acknowledgement, session owner,
subscription principal and price association commit together. Renewals recover
this history without calling the provider or current catalogue again.

`CanSubmitCheckout` refuses acknowledged sessions and unresolved intents older
than 23 hours. Stripe may prune idempotency keys after 24 hours; an old ambiguous
attempt must be reconciled, rather than resubmitted under a possibly expired
key. Managers retrieve an acknowledged session instead of creating it again.
See [Stripe idempotency](https://docs.stripe.com/api/idempotent_requests),
[subscription-filtered sessions](https://docs.stripe.com/api/checkout/sessions/list)
and [complete original line items](https://docs.stripe.com/api/checkout/sessions/line_items).

Legacy subscriptions lacking a saved authorization, portal changes to an
unmapped provider price and contradictory evidence remain unassessable. There
is no automatic legacy backfill, amount-based mapping or operator override in
this service. Applications must provide an explicitly reviewed history recovery
workflow before claiming these cases supported. This service supplies billing
identity only; it neither determines partner entitlement nor issues payouts.

### Pre-payment checkout lifecycle ownership

For an acknowledged subscription checkout, `LookupCheckoutLifecycleEvidence`
uses the optional provider session-evidence capability to read the exact retained
session outside transactions. It checks the stored authorization, payer, original
price/amount/currency/cadence, mode and complete session evidence. Hosts enforce
current owning authority and durably retain this original evidence before calling
`CaptureCheckoutLifecycleEvidence`; recovery reuses those inputs, not a new lookup.
Missing acknowledgement or contradictory evidence cannot establish an anchor.

The optional `CheckoutLifecycleTx` stores an immutable per-intent receipt and the
first scope/subscription anchor. Exact capture retries return the original receipt
and timestamp. Later checkouts may retain separate receipts for the same payer
and provider customer but never replace the first anchor. Conflicting lifecycle
and paid checkout owners are rejected under the same checkout scope transaction.
`FindCheckoutLifecycleAnchor` verifies the first anchor, receipt and frozen intent.
Legacy repositories without this optional extension remain compatible and report
unavailable when the new capture/read capability is requested.

All anchor fields are private JSON and explicitly encrypted by `revenuestore`.
Restore requires both anchor and receipt records with their original intents and
acknowledgements; no TTL or history rewrite is introduced. If a referenced intent,
anchor or receipt is missing, joined recovery fails with `ErrRevenueUnavailable`.
Restore the complete original owning records from a verified backup; do not delete
receipts, re-submit checkout, fabricate replacement history or label the result
inactive. Ordinary absence of any anchor remains `ErrRevenueNotFound`.
These additions use the
existing prepared record-store indexes. A completed checkout is neither a payment
nor fresh subscription status: this workflow creates no revenue fact, paid
conversion, commission or first-payment economic terms lock. The status workflow
below can use these anchors. Bounded discovery, durable host scheduling and trial
journey reporting still require separate integration.

### Scoped current subscription status

`PrepareSubscriptionStatusForCheckout` selects an immutable joined checkout
anchor before the first payment. `GetSubscriptionStatusForCheckout` reads the same
subscription head used by paid reporting. Checkout preparations use explicit
`checkout-lifecycle-v1` provenance and contain no fact ID. Payment preparations
retain their original canonical format and receipt IDs with empty source; old
persistence records lacking the new fields still decode unchanged. Mixed or
unknown source shapes are rejected. The private persistence codec retains the
new provenance fields in encrypted head/receipt envelopes with existing indexes.

Both sources must agree on provider/account/mode, subscription, paying principal
and provider customer. Paid status preparation also checks an existing checkout
anchor before the first status head, refusing contradictory payer proof. A later
valid payment can advance a checkout-backed head; the old original receipt remains
recoverable. Reading a checkout-backed active or trialing state never establishes
paid conversion or commission. Current authority, original-input retention and
freshness requirements below apply equally to both sources. Restores must retain
the corresponding immutable payment fact or complete checkout provenance; do not
rewrite old receipt identities or repair missing history with fresh observations.

`RevenueService` optionally uses `SubscriptionStatusRepository` on its **same**
revenue repository. `PrepareSubscriptionStatus` verifies an immutable accepted
payment fact, freezes its merchant/mode, paying principal, provider customer and
subscription, and reads the current head revision/fingerprint before provider
I/O. Missing historical customer identity is unassessable. Refund/dispute facts
cannot establish this binding; another payment cannot reassign an existing
subscription to a different principal or provider customer.

The billing manager authenticates the exact provider subscription and supplies
`VerifiedSubscriptionStatusEvidence` to `CaptureVerifiedSubscriptionStatus`.
Capture atomically retains an immutable receipt and replaces the head only at
the prepared revision/fingerprint. Late responses conflict instead of replacing
newer evidence. Current is defined by accepted revision, not provider timestamps.
The owning clock bounds request/observation times and rejects rollback for a new
capture. No raw response or browser-supplied evidence is retained.

An uncertain commit must replay the **same preparation and evidence**. The
original receipt is checked before later head changes or the current clock;
changed evidence conflicts. A known advanced-head conflict instead requires
fresh preparation and a fresh provider lookup. The trusted host must persist
the preparation and lookup evidence before capture for crash recovery; these
methods do not supply a durable refresh scheduler.

`GetSubscriptionStatusForFact` joins the head with its immutable receipt in one
snapshot and verifies the original billing provenance. Explicit host-approved
freshness is bounded to 1 second–24 hours, conservatively measured from request
start. Missing, corrupt and stale evidence never becomes an inactive zero;
`ErrSubscriptionStatusStale` identifies expired coverage. Reads perform no
provider I/O. Status input, receipts and internal scope exclude public JSON.

The eight recognized states remain distinct, including `trialing`, `paused`
and `incomplete_expired`; scheduled cancellation remains separate from status.
Unknown states fail closed. **Active status is not paid revenue or entitlement.**
Original payment facts remain immutable while refunds/disputes are separate
facts. Reporting must join those owning adjustments and retained relationships
to establish its paid cohort; subtracting the original payment's refund field,
counting allocations as people or using legacy access flags is insufficient.
The optional [partner reporting join](../partnermanager/README.md#paid-referral-source-and-status-evidence)
uses those owners explicitly. Refresh scheduling, coverage alerts and host
customer/admin experiences remain integration work.

### Confirmed payment revenue history

`RevenueService.GetPaymentRevenueHistory` uses the optional
`RevenueHistoryRepository` on its same configured revenue repository. Trusted
queries select 1–10 explicit provider/account/mode scopes and 1–10,000 owning payer
principals. They are private service inputs, not customer transport fields.
The repository reads the global sequence head, facts and original/resolution
receptions in one snapshot. The service validates canonical IDs/fingerprints,
contiguous acceptance sequences and every fact's original reception before
filtering. Missing, contradictory or future-accepted evidence fails; clock
rollback cannot certify facts accepted after the current owning clock.

The complete global budget is **10,000 facts plus reception/resolution receipts**,
independent of filters. Capacity returns `ErrRevenueHistoryTooLarge`, never a
truncated report. Persistent history eventually needs a reviewed budget or an
indexed projection with equivalent completeness proof. No new projection or
storage migration is introduced by this read capability.

Original PAYMENT rows remain immutable. Refund is the maximum verified
cumulative refunded amount across separate REFUND facts, not the sum of snapshot
values or adjustment `PaidMinor`. Identical cumulative evidence under distinct
delivery identities does not debit twice. Adjustments must resolve the exact
original scope, payer, customer, subscription, plan, currency and allocation.
Confirmed full-allocation dispute loss consumes the exposure remaining after
refunds. Temporary hold overlaps net revenue; it is not another debit. Terminal
won/lost evidence supersedes a hold for the same dispute identity; contradictory
terminals fail. Each result validates nonnegative, conserved current economics.
These calculations do not apply commission rates or create entitlement.

The private result carries confirmed original allocations, an owning classification
`AsOf`, source revision and acceptance sequence. `AsOf` is sampled after the
snapshot, not a database commit timestamp. It does not establish an atomic view
with attribution, commission or lifecycle owners, nor prove that every provider
delivery arrived. Quarantines lacking historical payer association cannot be
assigned to a relationship; their private scoped count belongs to operations.
All history/query/economic fields exclude public JSON. Reads perform no provider
I/O, revenue acceptance or attribution mutation.

## Bounded lifecycle source discovery

The owning `RevenueService.DiscoverLifecycleSources` optional capability reads
acknowledged subscription checkouts or immutable scoped subscription sources
through the same configured repository. Queries select one provider/account/mode
scope and one source kind, with a limit of 1–200. Continuation cursors are bound
to both selections; they are read positions, not authorization credentials.

The adapter reads one indexed bounded page and joins original records in the
same snapshot. Billing validates canonical accepted payments, frozen checkout
intent/acknowledgement/session reservations, first lifecycle anchor and receipt,
and any paid checkout owner with its original acknowledged intent before
returning the whole page. Missing joins,
contradictions, malformed ordering, cancellation and late failure withhold all
items. A binding without paid or anchored lifecycle evidence is not refreshable,
but still contributes to cursor progress so it cannot block later sources.
Returned candidates and pages are private in-process data, excluded from JSON.
`LifecycleDiscoveryPage.Validate` keeps canonical provenance and page-contract
checks in billing for composing managers. Continuation may advance past the last
visible candidate because binding-only rows count toward the raw cursor.

Discovery requires an owning schema-preparation record. Missing preparation
returns `ErrLifecycleDiscoveryUnprepared`, including when projections are empty.
The optional owning preparation operation described below establishes native
history coverage. This gated read does not enable a collector, expose a route or
establish current caller permission. The optional
[billing manager discovery boundary](../billingmanager/README.md#private-lifecycle-source-discovery)
checks current scope permission before lookup, selected payer/source permission
for each result and scope permission again before disclosure. Hosts still supply
the current instance-bound authority and collector orchestration.

`ReachedEnd` means the prepared projection's current page ended, not that all
provider subscriptions/events are known. Repeat full sweeps to reach hashed
identities inserted behind a saved position, and retain durable handoff before
advancing a cursor. Discovery performs no provider I/O, financial or status
writes, freshness reset, paid conversion or commission calculation.

### Explicit bounded preparation

`RevenueService.PrepareLifecycleDiscovery(ctx, scope, limit)` advances one
bounded native-history page per call through the same optional owning repository.
The limit is 1–200. Durable encrypted progress retains revision, phase, cursor,
source epoch and sweep counts across restarts. Phases scan acknowledged checkouts,
accepted payments, first lifecycle anchors and paid principal bindings, then
validate both resulting projection sets against original evidence. Continue
until `State.Phase == LifecyclePreparationComplete`; an intermediate result is
not readiness. `Scanned` includes all legacy partition and validation rows;
`Selected` counts selected original source rows, not distinct subscriptions or
payments. `CustomerlessPayments` counts selected canonical legacy payments that
remain financial-only. These private counts are not customer reporting metrics.

Preparation reconstructs only additive source projections. Original checkout
requests and payments are validated before scope/mode filtering; corrupt global
history cannot hide selected sources by changing those fields, and may block a
selected scope until remediated. Billing validates
frozen intent/session reservations, canonical payments and immutable checkout
ownership in the same transaction as each handoff and progress advance. It does
not rewrite original receipts, economic sequences or financial history, create
status/freshness, enroll customers, backfill attribution or accrue commissions.
Contradictory owners, malformed evidence and missing joins prevent readiness;
operators must investigate the original history rather than skip it silently.
Lost commit replies return uncertainty; the next call reads committed durable
progress before advancing, avoiding duplicate page counts. Completed retries
return the original prepared state without rewriting its marker.

**Upgrade precondition:** drain application instances running older source
writers before preparation. Current native acknowledgements, payment facts
(including customer-less payments), paid associations and lifecycle anchors
atomically advance the selected scope's source epoch. Completion deliberately
CAS-writes that same epoch with progress and the immutable schema marker, closing
write skew with a concurrent current writer. An epoch change during a sweep
resets its cursor/counts, preserves safe projections and returns `Restarted`;
continue a new sweep. Continuous writes can require repeated sweeps, so an
operator may need a controlled quiet period. The fence cannot observe writes
from older binaries or direct database modifications; neither is safe during
preparation. This prerequisite requires host deployment orchestration and is not
a framework-enforced drain.

Preparation scans the legacy global partitions through bounded indexed pages
once per explicit upgrade sweep; ordinary discovery queries only the scope's
prepared projection. Repeat discovery sweeps still remain necessary for later
current writes behind a cursor. Host migration orchestration, manager authority,
collector scheduling and deployment/restore qualification remain separate work.


### Private acknowledged-checkout preparation and receipt recovery

`CheckoutService.PrepareCheckoutLifecycle` validates the original frozen
subscription intent and both acknowledgement directions in one owning snapshot.
It returns a detached original input without provider I/O or writes. Retain it
durably before lookup. New preparation requires the optional
`CheckoutAcknowledgementTx` join; unsupported custom adapters return unavailable.
`CheckoutIntent.ValidateAcknowledgedSubscription` checks shape only, never
authority or current storage provenance.

`FindCheckoutLifecycleReceipt` reads the per-intent receipt, first immutable
anchor and original acknowledgements together. Only a missing receipt after
valid original joins is conclusive absence. Missing joined records are
unavailable; contradictory ownership or changed original input conflicts.
Later same-owner receipts still depend on the first anchor and its receipt.
Preparation/recovery does not reset original creation or anchoring times when
the current clock changes. Native lookup, capture and retained-anchor validation
also check the optional reverse-session join when the adapter supports it;
existing legacy ports remain compatible. Receipt formats and fingerprints stay
unchanged.

These are private owning stages, not an automatic lookup-and-capture operation.
Current manager/service-account authority, an encrypted host outbox retaining
original lookup evidence before capture, and exact-input uncertain-commit
recovery remain integration requirements. A native intent/acknowledgement alone
does not retain the provider response or a host job's uncertain disposition.
No financial fact, current status, commission or trial entitlement is created by
preparation or receipt reads.
