# starter/v0 - Ejectable Lazy Composition Layer

`starter/v0` is a **minimal, ejectable composition layer** for GHATD. It
defines the top-level container types (`Config`, `Repositories`, `Services`,
`Handlers`, `Middleware`, `Stack`, `Cleanup`) and constructors that reflect how
a GHATD application is assembled at the `main` package level.

## Philosophy

**starter/v0 is NOT a replacement architecture.** It is a thin, lazy
composition layer over the existing modular GHATD packages
(`external/usermanager`, `external/accessmanager`, `external/billingmanager`,
`external/reminder`, `external/streaker`, etc.). It exists to:

- **Reserve the shape** - show new contributors what wiring looks like before
  they learn every package.
- **Eliminate boilerplate** - provide constructors for the common GHATD
  repository, service, handler, and middleware layers.
- **Enable ejection** - when the skeleton no longer fits, copy the files out
  of this package and modify them freely. There is zero framework lock-in.

## Types

| Type            | Purpose                                                  |
|-----------------|----------------------------------------------------------|
| `Config`        | Runtime parameters (port, environment, log level).       |
| `Repositories`  | Data-layer dependency container and Mongo repository wiring. |
| `Services`      | Business-logic dependency container and manager service wiring. |
| `Handlers`      | HTTP handler dependency container.                       |
| `Middleware`    | Access middleware suite container.                       |
| `Stack`         | Top-level composition aggregating all containers.        |
| `Cleanup`       | Graceful resource-release function.                      |
| `CleanupGroup`  | Aggregates multiple `Cleanup` functions into one.        |
| `RouteGroup`    | String enum identifying a standard API route group for `AttachDefaultRoutes`. |

## Constructors

| Function               | Purpose                                      |
|------------------------|----------------------------------------------|
| `NewRepositories`      | Builds package repositories from a core Mongo repository, with per-repository overrides. |
| `NewServices`          | Builds GHATD services from repositories plus explicit app integrations such as Redis, email, audit, OAuth, policy, notifications, and payment providers. Includes reminder and streaker services. |
| `NewHandlers`          | Builds standard GHATD handlers with default error bundles and explicit override hooks. |
| `NewMiddleware`        | Builds the accessmanager middleware suite.   |
| `NewStack`             | Validates config and groups already-built layers. |
| `AttachDefaultRoutes`  | Attaches standard GHATD API routes from `Stack.Handlers` and `Stack.Middleware` to a router. |

## CleanupGroup

`CleanupGroup` aggregates multiple `Cleanup` functions into a single `Cleanup`.
It is useful when multiple resources (database connections, Redis clients,
temporary credential files, background goroutines) need independent teardown.

```go
var cleanupGroup starter.CleanupGroup
cleanupGroup.Add(mongoHandler.Close)
cleanupGroup.Add(func(ctx context.Context) error {
    return redisClient.Close()
})
cleanupGroup.Add(nil) // silently ignored

stack, _ := starter.NewStack(&starter.NewStackRequest{
    Config:   cfg,
    Cleanup:  cleanupGroup.Run, // assignable to Stack.Cleanup
})
```

- `Add(fns ...Cleanup)` appends non-nil cleanups in insertion order.
- `Run(ctx)` invokes every registered cleanup, collects all errors with
  `errors.Join`, and always runs every cleanup even when earlier ones fail.
- `CleanupGroup` implements the `Cleanup` signature via its `Run` method, so
  it can be assigned directly to `Stack.Cleanup`.

Starter creates GHATD components, but it does not own third-party choices.
Mongo handlers, Redis stores, email managers, OAuth providers, payment
providers, validators, and cleanup remain visible in the host application.
Package-owned helpers such as `repository.NewMongoRuntime`,
`ephemeral.NewRedisRuntime`, `emailprovider.NewSparkPostClient`,
`emailmanager.NewStandardEmailManager`, `spa.NewBootstrap`, and
`router.AttachDefaultAuthVerifyRoute` can reduce repeated setup without moving that
ownership into `starter/v0`. `router.NewAuthVerifyHandler` remains available
for custom auth verify endpoint paths. The service layer only requires accessmanager's
ephemeral-store contract; the middleware layer can additionally accept a
`HardenedRateLimitStore` override when hardened rate limiting uses a different
store.

## Constructor Flow

For telemetry, start one host-owned `observability.Runtime` before this flow,
construct instrumented clients and pass their dependencies into Starter. Wrap
the finished router once with `otelhttp.Wrap`; Starter does not install an SDK,
exporter settings, Mongo monitors, Redis hooks, or an HTTP telemetry boundary
automatically. Drain the service and run its dependency cleanup before runtime
shutdown. Follow [Add Service Observability](../../../docs/how-to/add-service-observability.md)
for the complete sequence and existing-service checklist.

The common lazy path is:

1. Build third-party and app-specific dependencies in `main`.
2. Call `starter.NewRepositories` with a core Mongo repository.
3. Call `starter.NewServices` with repositories plus explicit Redis/email/OAuth/policy/notification/payment inputs.
4. Call `starter.NewHandlers` with services and a validator.
5. Call `starter.NewMiddleware` with services.
6. Group the results with `starter.NewStack`.
7. (Optional) Call `starter.AttachDefaultRoutes` with the stack and router to
   attach every standard GHATD API route group in one call, or attach routes
   individually per package.

For a fuller GHATD host application server-command walkthrough, see
[starter/v0 Host Application Setup](docs/host-application-style.md).

`NewStack` intentionally accepts nil layer fields so teams can adopt starter/v0
incrementally. Treat nil layers as "not wired yet" and check them before use.

## Payment Checkout and Customer Portal Capabilities

After loading settings, hosts can use the
[shared Stripe settings helper](../../paymentprovider/helpers/README.md) to
apply frontend/environment validation with `Configure` and append the enabled
provider. Low-level construction below remains available when the host owns
those checks directly.

`NewServices` registers each `PaymentProviders` entry in one shared provider
registry. Billing Manager uses that same registry for webhook processing and,
when providers implement the optional capabilities, authenticated checkout or
hosted customer-portal session creation. Hosts do not need a second provider
list or provider-specific session handlers.

The host configures trusted browser return destinations on the same provider
instance that it supplies to Starter. A non-empty `ReturnURL` implicitly opts
that provider into checkout; `NewServices` validates any provider-owned
checkout validation capability before returning:

```go
stripeProvider, err := paymentprovider.NewStripeProvider(&paymentprovider.Config{
    ProviderName:                  "stripe",
    WebhookSecret:                 "<stripe-webhook-secret>",
    APIKey:                        "<stripe-secret-key>",
    PublishableKey:                "<stripe-publishable-key>",
    ReturnURL:                     "https://app.example.test/app/plan?checkout=pending&session_id={CHECKOUT_SESSION_ID}",
    CustomerPortalReturnURL:       "https://app.example.test/settings/billing",
    CustomerPortalConfigurationID: "bpc_example", // Optional.
})
if err != nil {
    return err
}

serviceRequest.PaymentProviders = append(serviceRequest.PaymentProviders, stripeProvider)
services, err := starter.NewServices(serviceRequest)
if err != nil {
    return err
}
```

`starter.AttachDefaultRoutes` exposes the resulting authenticated endpoints at
`POST /api/v1/bms/billings/{providerName}/checkout` and
`POST /api/v1/bms/billings/{providerName}/portal`. Provider credentials,
return destinations, catalogue migrations, CORS, and frontend rendering remain
host-owned. Billing Manager discovers checkout and portal capabilities from
the same registered instance, so no post-construction provider registration
is needed. A custom webhook-only registry remains valid; custom composition can
supply separate optional capability registries directly to Billing Manager.

An empty provider-owned `ReturnURL` remains valid for webhook-only and provider
API-sync deployments. For Stripe checkout, configure the secret API key,
browser publishable key, and an absolute HTTP(S) return URL together so startup
fails before routes begin serving.

`CustomerPortalReturnURL` is an independent opt-in. Starter validates the
provider's optional portal configuration, including a provider-specific
configuration identifier when supplied and the provider's hosted-session URL
allowlist capability. The portal request accepts no customer or return URL;
Billing Manager derives the authenticated user's provider customer from
recurring subscription state and returns a fresh, non-cacheable session:

```http
POST /api/v1/bms/billings/stripe/portal
Accept: application/json
```

```json
{
  "data": {
    "id": "bps_example",
    "url": "https://billing.stripe.com/p/session_example"
  }
}
```

One-time purchases are not eligible for the hosted subscription-management
portal. Stored subscription update or cancellation links remain separate,
provider-specific read-model values rather than substitutes for an on-demand
session. Multiple distinct provider customers in the selected lifecycle tier
fail closed instead of opening an arbitrary billing account.

## Reminder and Streaker

`NewRepositories` creates `Reminder` and `Streaker` repositories from the core
Mongo repository unless the caller supplies overrides. `NewServices` then
creates `Services.Reminder` and `Services.Streaker`.

Reminder and streaker are optional starter integrations. If their repositories
are available, `NewServices` creates `Services.Reminder` and
`Services.Streaker`; if they are absent, the rest of the stack can still be
constructed.

The starter-created reminder and streaker services are attached to
`Services.UserManager` by default. When `AttachDefaultRoutes` includes
`RouteGroupUserManager`, UMS exposes reminder and streak endpoints backed by
those services. To attach a different implementation to UMS, pass
`NewServicesRequest.ReminderService` or `NewServicesRequest.StreakService`.

Direct calls through `Services.UserManager` use `ActorID` for the verified
caller, separately from a target-user filter. Existing host adapters using
User Manager's former caller fields must follow the
[request-identity migration](../../../docs/how-to/request-identity.md).
Starter's access middleware publishes the authentication flag and caller ID;
custom replacements must preserve that verified-context contract.

`streaker` does not have a standalone starter route group in v0. Host
applications still own product-specific streak workflows, schedulers, and
custom API routes. Those workflows can call `Services.Streaker` directly or
use the UMS streak endpoints for authenticated user-scoped access.

## Usage

### Shared voting services

`NewRepositories` creates `Repositories.Voter` using the managed Mongo
repository (or a supplied override). `NewServices` requires that repository and
constructs one `Services.Voter`, shared by Vision and the conversation voting
service injected into `Services.UserManager`. Custom/ejected compositions must
provide the equivalent service wiring; no database client belongs in a voting
handler or manager.

Before either consumer writes votes, register and run
`voter.EnsureIndexes(ctx, database)` through the host's explicit migration path
against that same database. Starter does not run the migration automatically.
Shared storage replaces embedded Vision voter arrays and supplies the contact
voting store; it provides no legacy backfill or dual-write fallback. Review
the [Vision voting upgrade guide](../../voter/README.md#upgrading-existing-vision-voting)
before changing a dependency pin, especially if existing votes must be retained.

Constructing the service does not expose private HTTP endpoints. See the
[conversation route opt-in](#optional-private-conversation-voting) below.

### Basic composition

```go
package main

import (
    "context"

    "github.com/ooaklee/ghatd/external/starter/v0"
)

func main() {
    cfg := starter.Config{
        Port:        8080,
        Environment: "local",
        LogLevel:    "debug",
    }
    if err := cfg.Validate(); err != nil {
        panic(err)
    }

    repositories, err := starter.NewRepositories(&starter.NewRepositoriesRequest{
        Core: coreRepository,
    })
    if err != nil {
        panic(err)
    }

    services, err := starter.NewServices(&starter.NewServicesRequest{
        Repositories:             repositories,
        EphemeralStore:            ephemeralStore,
        EmailManager:              emailManager,
        AccessTokenSecret:         accessTokenSecret,
        RefreshTokenSecret:        refreshTokenSecret,
        StaticPlaceholderUUID:     staticPlaceholderUUID,
        AuditService:              auditService, // optional; starter creates one when nil.
        AutoAdminEmailAddressRegex: adminEmailRegex,
        ValidPostTags:             nil, // nil uses GHATD defaults; []string{} disables them.
        PolicyConfig: &starter.PolicyConfig{
            BusinessEntityName:      "Example",
            BusinessEntityEmail:     "hello@example.test",
            BusinessEntityWebsite:   "https://example.test",
            LegalBusinessEntityName: "Example Ltd",
            GenerateStaticPolicies:  true,
        },
    })
    if err != nil {
        panic(err)
    }

    handlers, err := starter.NewHandlers(&starter.NewHandlersRequest{
        Services:                 services,
        Validator:                validator,
        Environment:              cfg.Environment,
        CookiePrefixAuthToken:    "auth",
        CookiePrefixRefreshToken: "refresh",
        CookieDomain:             "example.test",
    })
    if err != nil {
        panic(err)
    }

    middleware, err := starter.NewMiddleware(&starter.NewMiddlewareRequest{
        Services:    services,
        Environment: cfg.Environment,
    })
    if err != nil {
        panic(err)
    }

    stack, err := starter.NewStack(&starter.NewStackRequest{
        Config:       cfg,
        Repositories: repositories,
        Services:     services,
        Handlers:     handlers,
        Middleware:   middleware,
    })
    if err != nil {
        panic(err)
    }

    defer func() {
        if stack.Cleanup != nil {
            _ = stack.Cleanup(context.Background())
        }
    }()
}
```

## AttachDefaultRoutes

`AttachDefaultRoutes` attaches every standard GHATD API route group to a
`*router.Router` in a single call, using the handlers and middleware from a
`Stack`. It eliminates the per-package `AttachRoutes` boilerplate while
remaining fully ejectable.

```go
err := starter.AttachDefaultRoutes(&starter.AttachDefaultRoutesRequest{
    Router: httpRouter,
    Stack:  stack,
    Skip:   []starter.RouteGroup{starter.RouteGroupUserManager},
})
if err != nil {
    // handle validation error
}
```

### Optional display-handle API

`AttachDefaultRoutesRequest.EnableUserHandles` defaults to false. After applying
the explicit [user handle migration](../../user/v2/README.md#display-handles),
set it to true to attach session-only GET/PATCH `/api/v1/ums/me/handle` and POST
`/api/v1/ums/me/handle/validate`. Configure a route-policy evaluator before
attachment: PATCH declares `RevisionRequired`, and attachment returns an error
if the registry is incomplete. Skipping `RouteGroupUserManager` also omits these
routes. The existing active-session middleware is forwarded; API tokens do not
qualify. Hosts still own CSRF/origin policy, authenticated rate limits and CORS.

Per-type `UserConfig.GenerateHandle` is independent of route exposure and is
also false by default. Starter neither applies the migration nor backfills
existing accounts. See the [User Manager contract](../../usermanager/README.md#self-service-display-handles)
for payloads, ETags, error codes and compatibility requirements.

### Optional private conversation voting

Set `AttachDefaultRoutesRequest.EnableCommsVoting` to true only after applying
the shared voter index migration:

```go
err := starter.AttachDefaultRoutes(&starter.AttachDefaultRoutesRequest{
    Router: httpRouter,
    Stack: stack,
    EnableCommsVoting: true,
})
if err != nil {
    return err
}
```

The flag defaults to false. Skipping `RouteGroupUserManager` also omits these
five voting route definitions. They are owned by User Manager and require its
administrator-session middleware plus a live administrator recheck on every
read/set/remove. Mutation routes always apply `X-Comms-Expected-Owner`; API-token
admission is not substituted. Missing custom capabilities fail closed.

For custom composition use `WithCommsVotingService` and
`WithAdministratorAuthorizer` on User Manager. History/metadata owner wrapping
via `usermanager.RequireCommsConversationRoutes` is a separate composition step.
The starter injects its existing `Services.Contacter`, configured with the shared
voter service, into User Manager. User lookup delegates to
`user/v2.Service.GetUsersByIDs`; UMS retains its private participant projection.
Use native UMS route attachment as the single handler registration for these
paths. The [UMS service contracts](../../usermanager/README.md#conversation-service-contracts)
describe composition, and the [voting guide](../../usermanager/README.md#private-conversation-voting)
defines routes, policy operation keys, response codes and payloads.

### RouteGroup constants

| Constant                              | Routes attached                                      |
|---------------------------------------|------------------------------------------------------|
| `RouteGroupPricer`                    | `/api/v1/pricing/*`                                  |
| `RouteGroupPolicy`                    | `/api/v1/policies/*`                                 |
| `RouteGroupUser`                      | `/api/v2/users/*`                                    |
| `RouteGroupGroup`                     | `/api/v1/groups/*`                                   |
| `RouteGroupAccessManager`             | `/api/v1/ams/*`                                      |
| `RouteGroupUserManager`               | `/api/v1/ums/*`, including reminder and streak endpoints |
| `RouteGroupContentManager`            | `/api/v1/cms/*`                                      |
| `RouteGroupBillingManager`            | `/api/v1/bms/*`                                      |

### Skip semantics

Groups listed in `Skip` are omitted entirely. A skipped group's handler may be
nil — validation only enforces non-nil handlers for non-skipped groups.
Middleware is validated based on the union of all remaining (non-skipped)
groups, so a shared middleware is still required when at least one non-skipped
group depends on it. If the remaining groups do not need access middleware
(for example, policy-only routing), `Stack.Middleware` may be nil. Unknown
`RouteGroup` values fail validation so typos do not silently attach routes.

### What AttachDefaultRoutes does NOT attach

- SPA routes (catch-all `/` handler) — these remain host-owned.
- Auth verify/CORS middleware — package helpers exist, but the host owns when
  they are attached and how middleware is ordered.
- Router bootstrap — the host creates the `*router.Router` and starts the HTTP server.

## Escape Hatches

Each constructor accepts a request struct so projects can override only the
piece they need:

- `NewRepositoriesRequest` accepts per-repository overrides.
- `NewServicesRequest` accepts custom policy stores, group/user config,
  audit services, notifier senders, OAuth services, payment registries, payment
  providers, custom UMS reminder/streak service overrides, and post tag
  configuration. `ValidPostTags: nil` uses GHATD defaults, while
  `ValidPostTags: []string{}` intentionally disables them.
- `NewHandlersRequest` accepts `HandlerErrorMaps`; `nil` uses starter defaults,
  while an empty slice intentionally clears a bundle.
- `NewMiddlewareRequest` accepts custom error maps, rate-limit tuning, and an
  optional `HardenedRateLimitStore` override for middleware-specific storage.

`NewStack` accepts nil layer fields so projects can adopt starter/v0
incrementally. Nil means "not wired yet"; check a layer before dereferencing it.

## Config Validation

`Config.Validate()` uses simple built-in validation so starter/v0 does not
introduce hidden global state or a validation framework dependency.

| Field      | Rule                                  |
|------------|---------------------------------------|
| `Port`     | Required, 1-65535                     |
| `Environment` | Required, `local`/`development`/`staging`/`production` |
| `LogLevel` | Required, `debug`/`info`/`warn`/`error`       |

## Ejection

When the skeleton no longer serves your needs:

1. Copy `external/starter/v0/` into your own tree (e.g. `internal/app/`).
2. Update the package path.
3. Modify freely - add fields, remove types, inject concrete dependencies.

No part of the GHATD runtime depends on starter/v0. Removing the import is
always safe.

## Related

- [Host application setup](docs/host-application-style.md)
- [Managing MongoDB migrations](../../../docs/how-to/manage-mongodb-migrations.md)
- [MongoDB migrator](../../migrator/mongo/README.md)
- [ADR007: Add starter/v0 as an ejectable Lazy composition layer](../../../docs/adr/adr007-starter-v0-lazy-composition-layer.md)
- [ADR017: Colocate package documentation](../../../docs/adr/adr017-colocate-package-documentation.md)


## Google and Apple sign-in

Pass secure providers through `NewServicesRequest.OAuthServices`, apply
`user/v2/migrations.InitUsersOAuthIndexesUp` through the host migrator, and set
`NewHandlersRequest.OAuthOrigin` to the trusted frontend origin. Before serving,
opt native apps in with `handlers.AccessManager.ConfigureMobileOAuth` and check
its returned error; an empty redirect allowlist disables native handoff.
Starter does not load provider secrets, register callbacks or apply migrations.

See the cross-package [adoption guide](../../../docs/how-to/add-google-apple-sign-in.md),
[compile-checked composition example](../../../examples/oauth/README.md),
[provider reference](../../oauth/README.md) and
[access-manager routes](../../accessmanager/README.md).
