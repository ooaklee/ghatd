# Pricer

Pricer is the **source of truth for the pricing catalog**. It defines plans, feature entitlements, monetary costs, and provider references that describe what your product costs and what end users get at each tier. The `pricer` package provides a full CRUD API for managing these records, and is designed to be composed into your application or exposed through the Billing Manager Service (BMS) for read-only access by your frontend or Companion app.

## Core Packages Overview

The pricer package is self-contained, but integrates with other packages in the ecosystem:

| Package | Purpose | Role with Pricer |
|---|---|---|
| `pricer` | Source of truth for pricing plans, features, costs, and provider refs. | Core catalog management |
| `billingmanager` | Exposes read-only pricing endpoints for consumer-facing apps. | External consumers (Companion app) |

## Data Model Overview

### Price Plan

A price plan is a named tier or product offering. It has a lifecycle, one or more costs, optional feature references, and optional provider refs.

| Field | Type | Description |
|---|---|---|
| `id` | `string` | Unique identifier (UUID v4) |
| `slug` | `string` | URL-safe kebab-case identifier |
| `name` | `string` | Display name |
| `description` | `string` | Optional plan description |
| `status` | `string` | Lifecycle state (see [Lifecycle](#lifecycle)) |
| `features` | `[]PlanFeatureRef` | Ordered feature catalog references |
| `costs` | `[]PriceCost` | Monetary costs attached to this plan |
| `provider_refs` | `[]PriceProviderRef` | Links to provider-side identifiers |
| `metadata` | `map[string]interface{}` | Optional project-specific data |
| `published_at` / `published_by_id` | `string` | Publication metadata |
| `created_at` / `created_by_id` | `string` | Creation metadata |
| `updated_at` / `updated_by_id` | `string` | Update metadata |
| `deleted_at` / `deleted_by_id` | `string` | Soft delete tracking |

### Price Feature

A price feature is a reusable catalog item describing a capability or entitlement that can be included in plans.

| Field | Type | Description |
|---|---|---|
| `id` | `string` | Unique identifier (UUID v4) |
| `slug` | `string` | URL-safe kebab-case identifier |
| `name` | `string` | Display name |
| `description` | `string` | Optional description |
| `type` | `string` | See [Feature Types](#feature-types) |
| `unit` | `string` | See [Feature Units](#feature-units) |
| `sort_order` | `int` | Ordering for plan comparison displays |
| `metadata` | `map[string]interface{}` | Optional project-specific data |

### Plan Feature Reference

Links a feature catalog item to a plan, with plan-specific overrides.

| Field | Type | Description |
|---|---|---|
| `feature_id` | `string` | References `PriceFeature.id` |
| `feature_slug` | `string` | References `PriceFeature.slug` |
| `label` | `string` | Override display name for this plan |
| `included` | `bool` | Whether the feature is included |
| `quantity` | `int64` | Included quantity (for metered features) |
| `unit` | `string` | Optional unit override |

### Price Cost

A monetary cost for a plan, with billing cadence and optional trial/setup fees.

| Field | Type | Description |
|---|---|---|
| `amount` | `int64` | Cost in lowest currency unit (e.g. cents) |
| `currency` | `string` | ISO 4217 three-letter code (e.g. `USD`) |
| `billing_cadence` | `string` | See [Billing Cadences](#billing-cadences) |
| `trial_period_days` | `int` | Optional trial days before charge |
| `setup_fee_amount` | `int64` | Optional one-off setup fee in lowest currency unit |
| `provider_refs` | `[]PriceProviderRef` | Optional provider-side price identifiers |
| `metadata` | `map[string]interface{}` | Optional project-specific data |

### Price Provider Reference

Links internal plan or cost records to provider-side identifiers.

| Field | Type | Description |
|---|---|---|
| `provider` | `string` | Payment provider (see [Providers](#providers)) |
| `provider_id` | `string` | General provider identifier |
| `provider_product_id` | `string` | Provider-side product ID |
| `provider_price_id` | `string` | Provider-side price ID |

### Enums

#### Plan Statuses

| Value | Description |
|---|---|
| `draft` | Not yet published |
| `published` | Live and visible to consumers |
| `archived` | No longer active |

#### Feature Types

| Value | Description |
|---|---|
| `boolean` | On/off toggle (included or not) |
| `quantity` | Metered or limited numeric value |
| `text` | Free-text description |

#### Feature Units

| Value |
|---|
| `feature` |
| `seat` |
| `member` |
| `project` |
| `request` |
| `gb` |

#### Billing Cadences

| Value | Description |
|---|---|
| `one_time` | Single payment |
| `week` | Weekly recurrence |
| `month` | Monthly recurrence |
| `year` | Yearly recurrence |

#### Providers

| Value | Description |
|---|---|
| `manual` | Manually managed |
| `stripe` | Stripe |
| `paddle` | Paddle |
| `kofi` | Ko-fi |

## Lifecycle

A price plan moves through three lifecycle states:

```
 ┌────────┐    publish    ┌───────────┐    archive    ┌──────────┐
 │ Draft  │ ────────────► │ Published │ ────────────► │ Archived │
 └────────┘               └───────────┘               └──────────┘
                                  │                         │
                                  │ soft delete             │ soft delete
                                  ▼                         ▼
                            ┌──────────┐              ┌──────────┐
                            │ (marked  │              │ (marked  │
                            │ deleted) │              │ deleted) │
                            └──────────┘              └──────────┘
```

- **Draft**: Plan is being created or edited. Not visible to consumers.
- **Published**: Plan is live and appears in BMS read endpoints. Publishing requires at least one valid cost and one provider reference.
- **Archived**: Plan is no longer active. Soft delete additionally sets `deleted_at`/`deleted_by_id` and moves the plan to `archived` status.

Features do not have a formal lifecycle — they are always available once created, and are removed via soft delete.

## BMS Read Endpoints for Companion App

The billing manager service exposes **read-only** pricing endpoints designed for frontend and Companion app consumption. These endpoints require no authentication.

### GET `/api/v1/bms/pricing/plans`

Returns a paginated list of published price plans.

#### Query Parameters

| Parameter | Type | Description |
|---|---|---|
| `order` | `string` | Sort order: `created_at_asc`, `created_at_desc` (default), `updated_at_asc`, `updated_at_desc`, `published_at_asc`, `published_at_desc`, `name_asc`, `name_desc`, `slug_asc`, `slug_desc` |
| `page` | `int` | Page number (default: `1`) |
| `per_page` | `int` | Results per page (default: `25`) |
| `meta` | `bool` | Include pagination metadata in response |
| `include_features` | `bool` | Include `features` array in each plan |
| `include_costs` | `bool` | Include `costs` array in each plan |
| `include_providers` | `bool` | Include `provider_refs` array in each plan |
| `with_status` | `string` | Comma-separated statuses (e.g. `published`) |
| `with_billing_cadence` | `string` | Comma-separated cadences (e.g. `month,year`) |
| `with_currency` | `string` | Comma-separated ISO 4217 codes (e.g. `USD`) |
| `slugs` | `string` | Comma-separated slugs to filter by |
| `created_by_ids` | `string` | Comma-separated creator user IDs |
| `published_by_ids` | `string` | Comma-separated publisher user IDs |
| `created_at_from` | `string` | ISO 8601 date or Unix timestamp |
| `created_at_to` | `string` | ISO 8601 date or Unix timestamp |
| `published_at_from` | `string` | ISO 8601 date or Unix timestamp |
| `published_at_to` | `string` | ISO 8601 date or Unix timestamp |
| `deleted_at_from` | `string` | ISO 8601 date or Unix timestamp |
| `deleted_at_to` | `string` | ISO 8601 date or Unix timestamp |
| `is_deleted` | `bool` | Only show deleted plans |
| `is_not_deleted` | `bool` | Only show non-deleted plans |
| `is_published` | `bool` | Only show published plans |
| `is_not_published` | `bool` | Only show non-published plans |

#### Example Request

```
GET /api/v1/bms/pricing/plans?with_status=published&include_costs=true&include_features=true&per_page=10&page=1
```

#### Example Response

```json
{
    "data": [
        {
            "id": "123e4567-e89b-12d3-a456-426614174000",
            "slug": "pro",
            "name": "Pro",
            "description": "For growing teams",
            "status": "published",
            "features": [
                {
                    "feature_id": "223e4567-e89b-12d3-a456-426614174001",
                    "feature_slug": "projects",
                    "label": "Projects",
                    "included": true,
                    "quantity": 10
                },
                {
                    "feature_id": "323e4567-e89b-12d3-a456-426614174001",
                    "feature_slug": "team-members",
                    "label": "Team Members",
                    "included": true,
                    "quantity": 5
                }
            ],
            "costs": [
                {
                    "id": "423e4567-e89b-12d3-a456-426614174001",
                    "amount": 2900,
                    "currency": "USD",
                    "billing_cadence": "month",
                    "provider_refs": [
                        {
                            "provider": "stripe",
                            "provider_price_id": "price_stripe_pro_monthly"
                        }
                    ]
                },
                {
                    "id": "523e4567-e89b-12d3-a456-426614174001",
                    "amount": 29900,
                    "currency": "USD",
                    "billing_cadence": "year",
                    "trial_period_days": 14,
                    "provider_refs": [
                        {
                            "provider": "stripe",
                            "provider_price_id": "price_stripe_pro_yearly"
                        }
                    ]
                }
            ],
            "metadata": {
                "display_order": 2
            },
            "published_at": "2025-01-15T10:00:00.000000000Z",
            "published_by_id": "admin-user-id",
            "created_at": "2025-01-10T08:00:00.000000000Z",
            "updated_at": "2025-01-15T10:00:00.000000000Z"
        }
    ]
}
```

> **Note:** By default, `features`, `costs`, and `provider_refs` arrays are excluded from the response for listing queries. Pass `include_features=true`, `include_costs=true`, or `include_providers=true` to include them. When querying a single plan by slug, these are always included.

#### Example Meta Response (with `meta=true`)

```json
{
    "data": [...],
    "meta": {
        "resources_per_page": 10,
        "total_resources": 3,
        "total_pages": 1,
        "page": 1
    }
}
```

### GET `/api/v1/bms/pricing/plans/{slug}`

Returns a single published price plan by its slug.

#### Example Request

```
GET /api/v1/bms/pricing/plans/pro
```

#### Example Response

```json
{
    "data": {
        "id": "123e4567-e89b-12d3-a456-426614174000",
        "slug": "pro",
        "name": "Pro",
        "description": "For growing teams",
        "status": "published",
        "features": [
            {
                "feature_id": "223e4567-e89b-12d3-a456-426614174001",
                "feature_slug": "projects",
                "label": "Projects",
                "included": true,
                "quantity": 10
            }
        ],
        "costs": [
            {
                "id": "423e4567-e89b-12d3-a456-426614174001",
                "amount": 2900,
                "currency": "USD",
                "billing_cadence": "month"
            }
        ],
        "provider_refs": [
            {
                "provider": "stripe",
                "provider_product_id": "prod_stripe_pro"
            }
        ],
        "metadata": {},
        "published_at": "2025-01-15T10:00:00.000000000Z",
        "published_by_id": "admin-user-id",
        "created_at": "2025-01-10T08:00:00.000000000Z"
    }
}
```

> **Note:** Single plan queries by slug always include `features`, `costs`, and `provider_refs` by default.

### GET `/api/v1/bms/pricing/features`

Returns a paginated list of published feature catalog items.

#### Query Parameters

| Parameter | Type | Description |
|---|---|---|
| `order` | `string` | Same sort options as plans |
| `page` | `int` | Page number (default: `1`) |
| `per_page` | `int` | Results per page (default: `25`) |
| `meta` | `bool` | Include pagination metadata |
| `with_types` | `string` | Comma-separated feature types (e.g. `boolean,quantity`) |
| `with_units` | `string` | Comma-separated feature units (e.g. `seat,gb`) |
| `slugs` | `string` | Comma-separated slugs |
| `created_by_ids` | `string` | Comma-separated creator IDs |
| `published_by_ids` | `string` | Comma-separated publisher IDs |
| `created_at_from` / `created_at_to` | `string` | Date range filters |
| `published_at_from` / `published_at_to` | `string` | Date range filters |
| `deleted_at_from` / `deleted_at_to` | `string` | Date range filters |
| `is_deleted` / `is_not_deleted` | `bool` | Deletion status filters |
| `is_published` / `is_not_published` | `bool` | Publication status filters |

#### Example Request

```
GET /api/v1/bms/pricing/features?with_types=boolean,quantity&per_page=25&page=1
```

#### Example Response

```json
{
    "data": [
        {
            "id": "223e4567-e89b-12d3-a456-426614174001",
            "slug": "projects",
            "name": "Projects",
            "description": "Number of active projects",
            "type": "quantity",
            "unit": "project",
            "sort_order": 1,
            "metadata": {
                "category": "collaboration"
            },
            "created_at": "2025-01-08T14:00:00.000000000Z"
        },
        {
            "id": "323e4567-e89b-12d3-a456-426614174001",
            "slug": "team-members",
            "name": "Team Members",
            "description": "Number of team seats",
            "type": "quantity",
            "unit": "seat",
            "sort_order": 2,
            "created_at": "2025-01-08T14:30:00.000000000Z"
        }
    ]
}
```

## Quick Start: Setting Up Pricer

### 1. Import the Package

```go
import "github.com/ooaklee/ghatd/external/pricer"

// Assume coreRepository is an existing MongoDbStore implementation
```

### 2. Create the Repository and Service

```go
import (
    "context"
    "github.com/ooaklee/ghatd/external/pricer"
)

// Create repository
pricerRepository := pricer.NewRepository(coreRepository)

// Create service with business logic and validation
pricerService := pricer.NewService(pricerRepository)
```

### 3. Create Feature Catalog Items

```go
ctx := context.Background()

feature, err := pricerService.CreateFeature(ctx, &pricer.CreateFeatureRequest{
    UserID:      "admin-user-id",
    Name:        "Projects",
    Description: "Number of active projects",
    Type:        pricer.PriceFeatureTypeQuantity,
    Unit:        pricer.PriceFeatureUnitProject,
    SortOrder:   1,
    PublishNow:  true,
})
```

### 4. Create a Price Plan

```go
plan, err := pricerService.CreatePricePlan(ctx, &pricer.CreatePricePlanRequest{
    UserID:      "admin-user-id",
    Name:        "Pro",
    Description: "For growing teams",
    Features: []pricer.PlanFeatureRef{
        {
            FeatureID: feature.ID,
            Label:     "Projects",
            Included:  true,
            Quantity:  10,
        },
    },
    Costs: []pricer.PriceCost{
        {
            Amount:         2900,
            Currency:       "USD",
            BillingCadence: pricer.PriceBillingCadenceMonthly,
        },
    },
    ProviderRefs: []pricer.PriceProviderRef{
        {
            Provider:         pricer.PriceProviderStripe,
            ProviderPriceID:  "price_stripe_pro_monthly",
        },
    },
    PublishNow: true,
})
```

### 5. Expose via BMS for Companion App

```go
import "github.com/ooaklee/ghatd/external/billingmanager"

billingmanagerService.WithPricerService(pricerService)
```

The BMS will automatically expose the pricing read endpoints at:
- `GET /api/v1/bms/pricing/plans`
- `GET /api/v1/bms/pricing/plans/{slug}`
- `GET /api/v1/bms/pricing/features`

## High-level Overview

### Architecture

```
┌─────────────────────┐     ┌─────────────────────────┐
│   Admin Console     │     │    Companion App / FE    │
│   (Write Access)    │     │    (Read-Only Access)    │
└─────────┬───────────┘     └───────────┬─────────────┘
          │                             │
          │ /api/v1/pricing/*           │ /api/v1/bms/pricing/*
          │ (CRUD endpoints)            │ (BMS read endpoints)
          │                             │
          ▼                             ▼
┌─────────────────────────────────────────────────────────┐
│                    billingmanager                        │
│  ┌──────────────────────────────────────────────────┐   │
│  │              pricer.Service                       │   │
│  │  • Validate plan/feature payloads                │   │
│  │  • Enforce publishing rules                      │   │
│  │  • Pagination and filtering                      │   │
│  └────────────────────┬─────────────────────────────┘   │
│                       │                                  │
│  ┌────────────────────┴─────────────────────────────┐   │
│  │              pricer.Repository                     │   │
│  │  • MongoDB persistence                            │   │
│  │  • Query filter builders                          │   │
│  │  • Soft delete support                            │   │
│  └──────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────┘
                         │
                         ▼
              ┌──────────────────┐
              │     MongoDB      │
              │  pricing_plans   │
              │  pricing_features│
              └──────────────────┘
```

### Request Flow

```
Client Request
      │
      ▼
request mapping (fender.go)
      │  extract IDs from URI
      │  decode body / query params
      │  acquire user ID from context
      │  validate struct tags
      ▼
service layer (service.go)
      │  validate business rules
      │  normalise slugs and dates
      │  enforce publish constraints
      ▼
repository layer (repository.go)
      │  build MongoDB filter
      │  execute query / mutation
      ▼
response (response.go)
```

## Migration Setup

The pricer package includes MongoDB index migrations for the `pricing_plans` and `pricing_features` collections:

- Unique index on `pricing_plans.slug`
- Index on `pricing_plans.status` for filtering
- Index on `pricing_plans.created_at` for sorting
- Index on `pricing_plans.published_at` for sorting
- Unique index on `pricing_features.slug`
- Index on `pricing_features.type` for filtering
- Index on `pricing_features.created_at` for sorting

Register these migrations during server bootstrap using the `migrate.Register` pattern:

```go
import (
    pricerMigrations "github.com/ooaklee/ghatd/external/pricer/migrations"
    migrate "github.com/xakep666/mongo-migrate"
)

migrate.Register(
    pricerMigrations.InitPricingIndexesUp,
    pricerMigrations.InitPricingIndexesDown,
)
```

A seed migration is also provided with example feature catalog entries and a starter plan. See `external/pricer/migrations/seed_pricing.go` for details.

## Potential Future Improvements

Here's a list of areas for improvement in future iterations of `pricer`. These suggestions are not prioritised.

### Catalog Features
- [ ] Plan versioning / change history
- [ ] Plan comparison matrix endpoint
- [ ] Plan tags and categories
- [ ] Custom feature groups for plan display

### Pricing Flexibility
- [ ] Usage-based / metered pricing
- [ ] Tiered pricing (volume discounts)
- [ ] Per-unit add-on pricing
- [ ] Coupon and discount support
- [ ] Multi-currency cost entries per plan

### Integrations
- [ ] Stripe product/price sync (bidirectional)
- [ ] Paddle product/price sync
- [ ] Provider webhook reconciliation

### Developer Experience
- [ ] Admin console UI for plan management
- [ ] Plan preview before publish
- [ ] Bulk feature import/export
- [ ] Migration helpers for existing pricing data

### Queries & Performance
- [ ] Caching for BMS read endpoints
- [ ] Full-text plan/feature search
- [ ] Plan family / related plan recommendations
