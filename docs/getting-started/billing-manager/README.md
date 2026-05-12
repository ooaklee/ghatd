# Billing Manager

The recommended billing functionality comes in three independent, composable packages: `paymentprovider`, `billing`, and `billingmanager`. For most application features, you should use the high-level `billingmanager` package, which handles webhook processing, subscription management, billing event tracking, and **read-only pricing catalogue endpoints** — all with integrated audit logging.

The billing manager can also expose the pricer package's read-only pricing endpoints through `WithPricerService()`, making plans and features available to frontend and client applications at `/api/v1/bms/pricing/plans`, `/api/v1/bms/pricing/plans/{slug}`, and `/api/v1/bms/pricing/features`.

## Core Packages Overview

Here's an overview of the core packages:

| Package | Purpose | Recommended Use Case | Examples |
|---|---|---|---|
| `paymentprovider` | Abstracts payment provider webhook verification and payload normalisation (e.g., Stripe, Lemon Squeezy). | Building custom webhook handlers or testing provider integrations. | [`paymentprovider/examples`](../../../external/paymentprovider/examples/examples.go) |
| `billing` | Manages subscription and billing event data persistence with a repository pattern. | Direct database operations or building custom billing workflows. | [`billing/examples`](../../../external/billing/examples/examples.go) |
| `billingmanager` | Orchestrates `paymentprovider` and `billing` with high-level API methods for webhook processing. | Building application features (Standard)—provides the full workflow and audit logging. | [`billingmanager/examples`](../../../external/billingmanager/examples/examples.go) |

### Usage Overview

For a high-level overview of how this might fit into your project, please [**visit this section**](#high-level-overview).

## Quick Start: Setup and Processing Webhooks

This section shows how to set up the `billingmanager` and process payment provider webhooks. This is the recommended way to use the system for standard operations. For more examples, [check out the reference examples above](#core-packages-overview).

### 1. Import Packages and Configure

You'll need configuration for the `paymentprovider`, a `billing` service instance, and an `audit` service.

```go
import (
    "context"
    "net/http"
    "github.com/ooaklee/ghatd/external/paymentprovider"
    "github.com/ooaklee/ghatd/external/billing"
    "github.com/ooaklee/ghatd/external/billingmanager"
)

// Assume auditService and userService are initialised dependencies

// 1. Configure payment providers
stripeConfig := &paymentprovider.Config{
    ProviderName:  "stripe",
    WebhookSecret: "whsec_your_stripe_webhook_secret",
    APIKey:        "sk_test_your_stripe_api_key",
}

lemonSqueezyConfig := &paymentprovider.Config{
    ProviderName:  "lemonsqueezy",
    WebhookSecret: "your_lemonsqueezy_webhook_secret",
    APIKey:        "your_lemonsqueezy_api_key",
}

kofiConfig := &paymentprovider.Config{
    ProviderName:  "kofi",
    WebhookSecret: "your_kofi_verification_token",
}

// 2. Create payment providers
stripeProvider, _ := paymentprovider.NewStripeProvider(stripeConfig)
lemonSqueezyProvider, _ := paymentprovider.NewLemonSqueezyProvider(lemonSqueezyConfig)
kofiProvider, _ := paymentprovider.NewKofiProvider(kofiConfig)

// 3. Create provider registry
registry := paymentprovider.NewProviderRegistry()
registry.Register(stripeProvider)
registry.Register(lemonSqueezyProvider)
registry.Register(kofiProvider)

// 4. Create billing service (with MongoDB or in-memory repository)
repo := billing.NewInMemoryRepository(nil) // Or NewRepository(mongoStore)
billingService := billing.NewService(repo, repo)

// 5. Create billing manager (Orchestration layer)
manager := billingmanager.NewService(registry, billingService)
manager.WithAuditService(auditService)  // Optional: Enables audit logging
manager.WithUserService(userService)    // Optional: Enables email->user ID resolution
```

### 2. Process Webhook

You can use the high-level methods on the `billingmanager` to process incoming webhooks.

```go
// 6. Process a webhook from a payment provider
func handleWebhook(w http.ResponseWriter, r *http.Request) {
    ctx := r.Context()
    
    // Extract provider name from URL path: /api/v1/bms/billing/stripe
    providerName := extractProviderFromPath(r.URL.Path) // e.g., "stripe"
    
    err := manager.ProcessBillingProviderWebhooks(ctx, &billingmanager.ProcessBillingProviderWebhooksRequest{
        ProviderName: providerName,
        Request:      r,
    })
    
    if err != nil {
        // Handle error, e.g., errors.New(billingmanager.ErrKeyBillingManagerUnableToResolveUserId)
        http.Error(w, "Webhook processing failed", http.StatusBadRequest)
        return
    }
    
    w.WriteHeader(http.StatusOK)
}
```

### 3. Query Subscription Status

After webhooks are processed, you can query subscription and billing information.

```go
// Get user's subscription status
ctx := context.Background()
statusResp, err := manager.GetUserSubscriptionStatus(ctx, &billingmanager.GetUserSubscriptionStatusRequest{
    UserID:           "user-123",
    RequestingUserID: "user-123", // User querying their own subscription
})

if err != nil {
    // Handle error
}

status := statusResp.SubscriptionStatus
if status.HasSubscription {
    fmt.Printf("User has %s subscription\n", status.PlanName)
    fmt.Printf("Status: %s\n", status.Status)
    fmt.Printf("Provider: %s\n", status.Provider)
    fmt.Printf("Active: %v\n", status.IsActive)
    if status.NextBillingDate != nil {
        fmt.Printf("Next billing: %s\n", status.NextBillingDate)
    }
}

// Get billing event history
eventsResp, err := manager.GetUserBillingEvents(ctx, &billingmanager.GetUserBillingEventsRequest{
    UserID:           "user-123",
    RequestingUserID: "user-123",
    PerPage:          10,
    Page:             1,
    Order:            "created_at_desc",
})

for _, event := range eventsResp.Events {
    fmt.Printf("[%s] %s - $%.2f %s\n",
        event.EventTime.Format("2006-01-02"),
        event.Description,
        float64(event.Amount)/100,
        event.Currency)
}
```

### 4. Development Environment Setup

If you don't want to use your payment provider's webhook endpoints when running locally, you can use the `MockProvider` to simulate webhook payloads for testing.

```go
// Use mock provider for local development
mockProvider := paymentprovider.NewMockProvider("stripe")

// Set up test webhook payload
mockProvider.SetMockPayload(&paymentprovider.WebhookPayload{
    EventType:      paymentprovider.EventTypeSubscriptionCreated,
    EventID:        "evt_test_123",
    SubscriptionID: "sub_test_123",
    CustomerEmail:  "test@example.com",
    Status:         paymentprovider.SubscriptionStatusActive,
    PlanName:       "Pro Plan",
    Amount:         2999, // $29.99 in cents
    Currency:       "USD",
})

registry := paymentprovider.NewProviderRegistry()
registry.Register(mockProvider)

manager := billingmanager.NewService(
    registry,
    billingService,
)
```

> **Note on Environments:** You can also use the `LoggingProvider` wrapper to log webhook payloads for debugging without processing them in your billing system.

## Advanced Use Cases

While `billingmanager` is recommended, the packages can be used independently for specialised needs.

### Direct Billing Service Usage

You can use the `billing` service directly for custom workflows without the orchestration layer.

```go
// Use billing service alone for direct database operations
ctx := context.Background()

// Create a subscription manually
createResp, err := billingService.CreateSubscription(ctx, &billing.CreateSubscriptionRequest{
    IntegratorSubscriptionID: "stripe_sub_123",
    Integrator:               "stripe",
    UserID:                   "user-123",
    Email:                    "user@example.com",
    PlanName:                 "Pro Plan",
    Status:                   billing.StatusActive,
    Amount:                   2999,
    Currency:                 "USD",
})

// Query subscriptions with complex filters
subsResp, err := billingService.GetSubscriptions(ctx, &billing.GetSubscriptionsRequest{
    ForUserIDs:  []string{"user-123"},
    Statuses:    []string{billing.StatusActive, billing.StatusTrialing},
    PerPage:     25,
    Page:        1,
    Order:       "created_at_desc",
})

// Create billing events for an audit trail
eventResp, err := billingService.CreateBillingEvent(ctx, &billing.CreateBillingEventRequest{
    SubscriptionID:           "sub_123",
    IntegratorEventID:        "evt_stripe_456",
    IntegratorSubscriptionID: "stripe_sub_123",
    Integrator:               "stripe",
    UserID:                   "user-123",
    EventType:                "payment.succeeded",
    EventTime:                time.Now(),
    Amount:                   2999,
    Currency:                 "USD",
    PlanName:                 "Pro Plan",
    Status:                   billing.EventStatusProcessed,
})
```

### Custom Payment Provider

Adding a new provider (e.g., Paddle, PayPal) only requires implementing the `paymentprovider.Provider` interface.

```go
type MyCustomProvider struct {
    config *paymentprovider.Config
    name string
}

func (p *MyCustomProvider) VerifyWebhook(ctx context.Context, req *http.Request) error {
    // Custom webhook verification logic
    return nil
}

func (p *MyCustomProvider) ParsePayload(ctx context.Context, req *http.Request) (*paymentprovider.WebhookPayload, error) {
    // Parse provider-specific payload into a normalised format
    return &paymentprovider.WebhookPayload{
        EventType:      paymentprovider.EventTypePaymentSucceeded,
        SubscriptionID: "sub_from_provider",
        // ... map other fields
    }, nil
}

func (p *MyCustomProvider) Name() string {
    return "CUSTOM_PROVIDER"
}

func (p *MyCustomProvider) GetConfig() *paymentprovider.Config {
    return p.config
}

// Use it with the manager
provider := &MyCustomProvider{config: customConfig}
registry.Register(provider)
manager := billingmanager.NewService(registry, billingService)
```

### Custom Repository Implementation

You can implement custom repositories for different databases while keeping the same service layer.

```go
// Implement the repository interfaces for your database
type PostgresRepository struct {
    db *sql.DB
}

func (r *PostgresRepository) CreateSubscription(ctx context.Context, sub *billing.Subscription) (*billing.Subscription, error) {
    // PostgreSQL-specific implementation
    query := `INSERT INTO subscriptions (id, user_id, email, status, ...) VALUES ($1, $2, $3, $4, ...)`
    // Execute query and return subscription
    return sub, nil
}

func (r *PostgresRepository) GetSubscriptions(ctx context.Context, req *billing.GetSubscriptionsRequest) ([]billing.Subscription, error) {
    // PostgreSQL-specific query with filters and pagination
    return subscriptions, nil
}

// Implement all other repository methods...

// Use with billing service
postgresRepo := &PostgresRepository{db: db}
billingService := billing.NewService(postgresRepo, postgresRepo)
```

## High-level Overview

Here are some high-level overviews of this billing solution and its packages, with examples of how it can be used in your application for different use-cases.

### Usage Patterns

#### Pattern 1: Full Stack (Recommended for Applications)

```
Application Code
       │
       └──► billingmanager ──┬──► paymentprovider ──► Verify & Parse Webhooks
                             │
                             ├──► billing ──► Store Subscriptions & Events
                             │
                             └──► audit ──► Log Operations
```

#### Pattern 2: Direct Service (For Custom Workflows)

```
Application Code
       │
       └──► billing ──► Direct Database Operations
```

#### Pattern 3: Provider Only (For Testing/Integration)

```
Application Code
       │
       └──► paymentprovider ──► Verify Webhooks & Parse Payloads
```

### Environment Usage & Outputs Flow

```
┌──────────────────────────────────────────────────────────────┐
│                      Production                              │
│                                                              │
│  ┌─────────────┐         ┌──────────────┐                    │
│  │   Stripe    │────────►│billingmanager│                    │
│  │ LemonSqueezy│         └──────┬───────┘                    │
│  │   Ko-fi     │                │                            │
│  └─────────────┘                │                            │
│                   ┌─────────────┼──────────────┐             │
│                   │             │              │             │
│                   ▼             ▼              ▼             │
│         ┌──────────────┐  ┌──────────┐  ┌──────────┐         │
│         │   payment    │  │ billing  │  │  Audit   │         │
│         │   provider   │  │ Service  │  │ Service  │         │
│         └──────────────┘  └────┬─────┘  └────┬─────┘         │
│                                │             │               │
└────────────────────────────────┼─────────────┼───────────────┘
                                 │             │
                                 ▼             ▼
                       ┌──────────────┐  ┌──────────┐
                       │   MongoDB    │  │  Audit   │
                       │              │  │   Logs   │
                       └──────────────┘  └──────────┘

┌──────────────────────────────────────────────────────────────┐
│                      Local Development                       │
│                                                              │
│  ┌─────────────┐         ┌──────────────┐                    │
│  │    Mock     │────────►│billingmanager│                    │
│  │  Provider   │         └──────┬───────┘                    │
│  └─────────────┘                │                            │
│                   ┌─────────────┼──────────────┐             │
│                   │             │              │             │
│                   ▼             ▼              ▼             │
│         ┌──────────────┐  ┌──────────┐  ┌──────────┐         │
│         │   payment    │  │ billing  │  │  Audit   │         │
│         │   provider   │  │ Service  │  │ Service  │         │
│         └──────────────┘  └────┬─────┘  └────┬─────┘         │
│                                │             │               │
└────────────────────────────────┼─────────────┼───────────────┘
                                 │             │
                                 ▼             ▼
                       ┌──────────────┐  ┌──────────┐
                       │  In-Memory   │  │  Audit   │
                       │  Repository  │  │   Logs   │
                       └──────────────┘  └──────────┘
```

## Webhook Endpoint Design

The system expects webhooks at provider-specific endpoints. Here's the recommended URL pattern:

```
POST /api/v1/bms/billing/stripe
POST /api/v1/bms/billing/lemonsqueezy
POST /api/v1/bms/billing/kofi
```

### Example Router Setup

#### Manual Route Setup

If you want full control over your routing, you can manually set up the webhook endpoint:

```go
import (
    "github.com/gorilla/mux"
    "net/http"
)

func SetupBillingRoutes(r *mux.Router, manager *billingmanager.Service) {
    r.HandleFunc("/api/v1/bms/billing/{provider}", func(w http.ResponseWriter, r *http.Request) {
        vars := mux.Vars(r)
        providerName := vars["provider"]
        
        err := manager.ProcessBillingProviderWebhooks(r.Context(), &billingmanager.ProcessBillingProviderWebhooksRequest{
            ProviderName: providerName,
            Request:      r,
        })
        
        if err != nil {
            http.Error(w, "Webhook processing failed", http.StatusBadRequest)
            return
        }
        
        w.WriteHeader(http.StatusOK)
    }).Methods("POST")
}
```

#### Using AttachRoutes

For a more comprehensive setup that includes all billing manager endpoints with the correct middleware, use the `AttachRoutes` function:

```go
import (
    "github.com/ooaklee/ghatd/external/billingmanager"
    "github.com/ooaklee/ghatd/external/router"
    "github.com/gorilla/mux"
)

// set up router - httpRouter

// set up billing manager handler - billingHandler

// configure respective middleware - look at external/accessmanager/middleware

billingmanager.AttachRoutes(&billingmanager.AttachRoutesRequest{
    Router:                             httpRouter,
    Handler:                            billingHandler,
    AdminOnlyMiddleware:                adminMiddleware,
    ActiveValidApiTokenOrJWTMiddleware: authMiddleware,
})

```

This sets up the following routes automatically:

**Open Routes (No Authentication):**
- `POST /api/v1/bms/billings/{providerName}/webhooks` - Process payment provider webhooks
- `GET /api/v1/bms/pricing/plans` - List published price plans (when wired via `WithPricerService`)
- `GET /api/v1/bms/pricing/plans/{slug}` - Get a single published price plan by slug
- `GET /api/v1/bms/pricing/features` - List published feature catalogue items

**Authenticated User Routes:**
- `GET /api/v1/bms/billings/users/{userId}/events` - Get a user's billing events.
- `GET /api/v1/bms/users/{userId}/details/subscription` - Get a user's subscription status.
- `GET /api/v1/bms/users/{userId}/details/billing` - Get a user's billing details.

**Admin Only Routes:**
- `GET /api/v1/bms/admin/billings/users/{userId}/events` - Get any user's billing events.
- `GET /api/v1/bms/admin/users/{userId}/details/subscription` - Get any user's subscription status.
- `GET /api/v1/bms/admin/users/{userId}/details/billing` - Get any user's billing details.

## Subscription Lifecycle

Understanding the subscription lifecycle helps you work effectively with the billing system.

### Webhook Flow

```
1. Payment Provider Event
   │
   ├──► Webhook received at /api/v1/bms/billing/{provider}
   │
2. Verification & Parsing
   │
   ├──► paymentprovider.VerifyWebhook()
   ├──► paymentprovider.ParsePayload()
   │
3. User Resolution
   │
   ├──► Lookup existing subscription by integrator ID
   ├──► Or lookup user by email (via UserService)
   │
4. Subscription Management
   │
   ├──► Create a new subscription (if it's the first event)
   ├──► Update an existing subscription (status, dates, etc.)
   │
5. Event Recording
   │
   ├──► Create a billing event for the audit trail
   │
6. Audit Logging (Optional)
   │
   └──► Log to the audit service
```

### Subscription States

The billing system tracks various subscription states:

- **`active`** - The subscription is active and in good standing.
- **`trialing`** - The subscription is in a trial period.
- **`past_due`** - Payment has failed, but the subscription is still active.
- **`cancelled`** - The subscription has been cancelled.
- **`paused`** - The subscription is temporarily paused.
- **`expired`** - The subscription has expired.
- **`incomplete`** - The subscription setup is incomplete.
- **`unpaid`** - The subscription is unpaid.

## Authorisation & Security

### User Authorisation

The `billingmanager` includes built-in authorisation checks:

```go
// User querying their own subscription (allowed)
statusResp, err := manager.GetUserSubscriptionStatus(ctx, &billingmanager.GetUserSubscriptionStatusRequest{
    UserID:           "user-123",
    RequestingUserID: "user-123",
})

// Admin querying another user's subscription (requires admin role via UserService)
statusResp, err := manager.GetUserSubscriptionStatus(ctx, &billingmanager.GetUserSubscriptionStatusRequest{
    UserID:           "user-456",
    RequestingUserID: "admin-user-123", // Must be an admin
})
```

### Webhook Security

Each provider has its own webhook verification mechanism:

- **Stripe**: HMAC-SHA256 signature verification.
- **Lemon Squeezy**: HMAC-SHA256 signature verification.
- **Ko-fi**: Verification token validation.

The `paymentprovider` package handles all verification automatically before processing webhooks.

## Testing Strategies

### Unit Testing with Mocks

```go
import (
    "testing"
    "github.com/ooaklee/ghatd/external/paymentprovider"
    "github.com/ooaklee/ghatd/external/billing"
)

func TestWebhookProcessing(t *testing.T) {
    // Setup
    mockProvider := paymentprovider.NewMockProvider("stripe")
    mockProvider.SetMockPayload(&paymentprovider.WebhookPayload{
        EventType:      paymentprovider.EventTypePaymentSucceeded,
        SubscriptionID: "sub_test",
        CustomerEmail:  "test@example.com",
        Status:         paymentprovider.SubscriptionStatusActive,
        Amount:         2999,
        Currency:       "USD",
    })
    
    registry := paymentprovider.NewProviderRegistry()
    registry.Register(mockProvider)
    
    inMemoryRepo := billing.NewInMemoryRepository(nil)
    billingService := billing.NewService(inMemoryRepo, inMemoryRepo)
    
    manager := billingmanager.NewService(registry, billingService)
    
    // Execute
    req := httptest.NewRequest("POST", "/webhook", strings.NewReader("{}"))
    err := manager.ProcessBillingProviderWebhooks(context.Background(), &billingmanager.ProcessBillingProviderWebhooksRequest{
        ProviderName: "stripe",
        Request:      req,
    })
    
    // Assert
    if err != nil {
        t.Errorf("Expected no error, got %v", err)
    }
}
```

### Integration Testing

```go
func TestSubscriptionLifecycle(t *testing.T) {
    // Setup real providers with test API keys
    stripeConfig := &paymentprovider.Config{
        ProviderName:  "stripe",
        WebhookSecret: os.Getenv("STRIPE_TEST_WEBHOOK_SECRET"),
        APIKey:        os.Getenv("STRIPE_TEST_API_KEY"),
    }
    
    provider, _ := paymentprovider.NewStripeProvider(stripeConfig)
    
    // Use a MongoDB test database
    mongoRepo := billing.NewRepository(mongoStore)
    billingService := billing.NewService(mongoRepo, mongoRepo)
    
    // Test the full webhook flow
    // ...
}
```

## Potential Future Improvements

Here's a list of areas for improvement in future iterations of `billingmanager`, `billing`, and `paymentprovider`. Please note that these suggestions are not prioritised.

### Additional Providers
- [ ] Paddle provider
- [ ] PayPal provider
- [ ] Chargebee provider
- [ ] Recurly provider
- [ ] Braintree provider

### Advanced Features
- [ ] Subscription plan upgrades/downgrades
- [ ] Proration calculations
- [ ] Usage-based billing support
- [ ] Multi-currency support
- [ ] Tax calculation integration
- [ ] Invoice generation
- [ ] Payment retry logic
- [ ] Dunning management
- [ ] Subscription trial extensions
- [ ] Coupon/discount support
- [ ] Metered billing
- [ ] Subscription pausing/resuming

### Data & Analytics
- [ ] Revenue analytics
- [ ] Churn rate tracking
- [ ] MRR/ARR calculations
- [ ] Cohort analysis
- [ ] Subscription metrics dashboard
- [ ] Export functionality

### Testing
- [ ] Unit tests for billing service
- [ ] Unit tests for paymentprovider
- [ ] Unit tests for billingmanager
- [ ] Integration tests with real providers
- [ ] Webhook simulation tools
- [ ] Performance benchmarks

### Monitoring & Observability
- [ ] Webhook processing metrics
- [ ] Failed payment alerting
- [ ] Provider health monitoring
- [ ] Subscription status dashboard
- [ ] Audit trail query interface

### Developer Experience
- [ ] CLI tool for testing webhooks
- [ ] Provider migration utilities
- [ ] Data export/import tools
- [ ] Subscription reconciliation tools
- [ ] Webhook replay functionality
