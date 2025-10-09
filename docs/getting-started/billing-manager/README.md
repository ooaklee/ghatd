# Getting Started With Billing Manager

The recommended billing functionality comes in the form of three independent, composable packages: [**paymentprovider**](../../../external/holder/paymentprovider/), [**billing**](../../../external/holder/billing/), and [**billingmanager**](../../../external/holder/billingmanager/). For most application features, you should use the high-level `billingmanager` package, which handles webhook processing, subscription management, and billing event tracking with integrated audit logging.

## Core Packages Overview

See below an overview of the core packages mentioned above:

| Package | Purpose | Use Case (Recommended) | Examples Location |
|---------|---------|------------------------|-------------------|
| **`paymentprovider`** | Abstracts payment provider webhook verification and payload normalization (e.g., Stripe, Lemon Squeezy, Ko-fi). | Building custom webhook handlers or testing provider integrations. | [external/holder/paymentprovider/examples](../../../external/holder/paymentprovider/examples/examples.go) |
| **`billing`** | Manages subscription and billing event data persistence with repository pattern. | Direct database operations or building custom billing workflows. | [external/holder/billing/examples](../../../external/holder/billing/examples/examples.go) |
| **`billingmanager`** | Orchestrates paymentprovider and billing with high-level API methods for webhook processing. | Building application features (Standard)—provides the full workflow and audit logging. | [external/holder/billingmanager/examples](../../../external/holder/billingmanager/examples/examples.go) |

### Usage Overview

For a high-level overview of how this might fit into your GHAT(D) project, please [**visit this section**](#high-level-overview).

## Quick Start: Setup and Processing Webhooks

In the following section we'll demonstrate how to set up the `billingmanager` and process payment provider webhooks, which is the recommended way to use the system for standard application operations. For more examples [please check out the reference examples above](#core-packages-overview).

### 1. Import Packages and Configure

You'll need configuration for the `paymentprovider`, a `billing` service instance, and an [`audit` service](../../../external/audit/).

```go
import (
    "context"
    "net/http"
    "github.com/ooaklee/ghatd/external/holder/paymentprovider"
    "github.com/ooaklee/ghatd/external/holder/billing"
    "github.com/ooaklee/ghatd/external/holder/billingmanager"
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

You'll be able to use the high-level methods on the `billingmanager` to process incoming webhooks.

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

If you don't want to use your payment provider's webhook endpoints when running locally, you can leverage the `MockProvider` to simulate webhook payloads for testing.

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

> **Note on Environments:** You can also use the `LoggingProvider` wrapper to log webhook payloads for debugging without actually processing them in your billing system.

## Advanced Use Cases

While `billingmanager` is recommended, the packages can be used independently for specialized needs.

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

// Create billing events for audit trail
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
    // Parse provider-specific payload into normalized format
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

You can implement custom repositories for different databases while maintaining the same service layer.

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

See below high-level overviews of this billing solution (and its component packages) and a few examples of how it can be used in your GHAT(D) application for different use-cases.

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

For a more comprehensive setup that includes all billing manager endpoints with proper middleware, use the `AttachRoutes` function:

```go
import (
    "github.com/ooaklee/ghatd/external/holder/billingmanager"
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

**Authenticated User Routes:**
- `GET /api/v1/bms/billings/users/{userId}/events` - Get user's billing events
- `GET /api/v1/bms/users/{userId}/details/subscription` - Get user's subscription status
- `GET /api/v1/bms/users/{userId}/details/billing` - Get user's billing details

**Admin Only Routes:**
- `GET /api/v1/bms/admin/billings/users/{userId}/events` - Get any user's billing events
- `GET /api/v1/bms/admin/users/{userId}/details/subscription` - Get any user's subscription status
- `GET /api/v1/bms/admin/users/{userId}/details/billing` - Get any user's billing details

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
   ├──► Create new subscription (if first event)
   ├──► Update existing subscription (status, dates, etc.)
   │
5. Event Recording
   │
   ├──► Create billing event for audit trail
   │
6. Audit Logging (Optional)
   │
   └──► Log to audit service
```

### Subscription States

The billing system tracks various subscription states:

- **`active`** - Subscription is active and in good standing
- **`trialing`** - Subscription is in trial period
- **`past_due`** - Payment failed but subscription still active
- **`cancelled`** - Subscription has been cancelled
- **`paused`** - Subscription is temporarily paused
- **`expired`** - Subscription has expired
- **`incomplete`** - Subscription setup incomplete
- **`unpaid`** - Subscription unpaid

## Authorization & Security

### User Authorization

The `billingmanager` includes built-in authorization checks:

```go
// User querying their own subscription (allowed)
statusResp, err := manager.GetUserSubscriptionStatus(ctx, &billingmanager.GetUserSubscriptionStatusRequest{
    UserID:           "user-123",
    RequestingUserID: "user-123",
})

// Admin querying another user's subscription (requires admin role via UserService)
statusResp, err := manager.GetUserSubscriptionStatus(ctx, &billingmanager.GetUserSubscriptionStatusRequest{
    UserID:           "user-456",
    RequestingUserID: "admin-user-123", // Must be admin
})
```

### Webhook Security

Each provider has its own webhook verification mechanism:

- **Stripe**: HMAC-SHA256 signature verification
- **Lemon Squeezy**: HMAC-SHA256 signature verification
- **Ko-fi**: Verification token validation

The `paymentprovider` package handles all verification automatically before processing webhooks.

## Testing Strategies

### Unit Testing with Mocks

```go
import (
    "testing"
    "github.com/ooaklee/ghatd/external/holder/paymentprovider"
    "github.com/ooaklee/ghatd/external/holder/billing"
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
    
    // Use MongoDB test database
    mongoRepo := billing.NewRepository(mongoStore)
    billingService := billing.NewService(mongoRepo, mongoRepo)
    
    // Test full webhook flow
    // ...
}
```

## Potential Future Improvements

Below is a list of areas for improvement in future iterations of `billingmanager`, `billing`, and `paymentprovider`. Please note that these suggestions are not prioritised.

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
