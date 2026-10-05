# Pricer

Pricer is the **source of truth for the pricing catalogue**. It defines plans, feature entitlements, monetary costs, and provider references that describe what your product costs and what end users get at each tier. The `pricer` package provides a full CRUD API for managing these records, and is designed to be composed into your application or exposed through the Billing Manager Service (BMS) for read-only access by frontend and other client applications.

## ActorID migration

The eight mutation commands now use `ActorID`, replacing their previous `UserID`
caller field: create/update/publish/archive/delete a plan, and create/update/delete
a feature. This is a breaking Go source change; update struct literals and adapters.
Stored `CreatedByID`, `UpdatedByID`, `PublishedByID` and `DeletedByID` are attribution
history, not request actors, and are unchanged.

HTTP mappers bind `ActorID` only from explicitly authenticated shared context.
An anonymous placeholder or an ID without the authenticated flag is insufficient.
The actor is excluded from JSON and has no query/path tag. Selected plan/feature
IDs remain route-owned. Custom authentication adapters must verify credentials
before publishing context; see [request identity](../../docs/how-to/request-identity.md).

All `/api/v1/pricing` routes still require the configured administrator middleware
and registry policy. An actor ID does **not** grant access. Trusted in-process
callers must authenticate and authorize their workflow before invoking Pricer;
the service rejects an explicit actor that contradicts supplied identity context.
Public Billing Manager catalogue projections are unaffected.

Full `PricePlan` and `Feature` replacements are in-process capabilities. HTTP
updates reject non-null replacement objects; use the declared editable fields
in the [HTTP update contract](#http-update-contract).
For a trusted replacement, `ID`, when supplied, must equal the replacement's ID;
an omitted `ID` selects the nonempty replacement ID. Stored nano-ID, creation,
publication and deletion history override replacement values. Update attribution
comes from the current actor. A newly published plan with no publication history
still receives its normal publication metadata; use the publish operation to
explicitly publish again.

Mutation entry checks reject nil context/request, cancellation and invalid
repository wiring. Missing or inconsistent selected-record read results return
`ErrPricerUnavailable` (`PRC0-27`, 503), while original dependency errors retain
their shared reply mappings and host overrides. No database schema, route,
response envelope or existing `PRC0-15` actor-error code changes are required.

Service normalization copies plan/feature scalar fields and plan cost entries
before modifying them. Arbitrary nested metadata is not deep-cloned and must
remain read-only in custom adapters. This change does not add transactions,
revision checks or result-bearing repository writes; concurrent lifecycle edits
and uncertain write outcomes still require separate handling.

## Core Packages Overview

The pricer package is self-contained, but integrates with other packages in the ecosystem:

| Package | Purpose | Role with Pricer |
|---|---|---|
| `pricer` | Source of truth for pricing plans, features, costs, and provider refs. | Core catalogue management |
| `billingmanager` | Exposes read-only pricing endpoints for consumer-facing apps. | External client applications |

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
| `features` | `[]PlanFeatureRef` | Ordered feature catalogue references |
| `costs` | `[]PriceCost` | Monetary costs attached to this plan |
| `discounts` | `[]PriceDiscount` | Optional typed discount rules (amount or percent) |
| `payment_terms` | `PricePaymentTerms` | Optional payment terms (collection method, due days) |
| `provider_refs` | `[]PriceProviderRef` | Links to provider-side identifiers |
| `display_order` | `int` | Optional visual sort position for public listings |
| `metadata` | `map[string]interface{}` | Optional project-specific data |
| `published_at` / `published_by_id` | `string` | Publication metadata |
| `created_at` / `created_by_id` | `string` | Creation metadata |
| `updated_at` / `updated_by_id` | `string` | Update metadata |
| `deleted_at` / `deleted_by_id` | `string` | Soft delete tracking |

### Price Feature

A price feature is a reusable catalogue item describing a capability or entitlement that can be included in plans.

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

Links a feature catalogue item to a plan, with plan-specific overrides.

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
| `id` | `string` | Stable cost UUID; assigned on create when missing and retained when editing that cost |
| `amount` | `int64` | Cost in lowest currency unit (e.g. cents) |
| `currency` | `string` | ISO 4217 three-letter code (e.g. `USD`) |
| `billing_cadence` | `string` | See [Billing Cadences](#billing-cadences) |
| `trial_period_days` | `int` | Optional trial days before charge |
| `setup_fee_amount` | `int64` | Optional one-off setup fee in lowest currency unit |
| `provider_refs` | `[]PriceProviderRef` | Optional provider-side price identifiers |
| `metadata` | `map[string]interface{}` | Optional project-specific data |

### Price Discount

A typed discount rule that can be attached to a plan. Discounts can reduce a fixed amount or a percentage of the cost.

| Field | Type | Description |
|---|---|---|
| `label` | `string` | Admin-facing display label |
| `type` | `string` | Discount type: `amount` or `percent` (see [Discount Types](#discount-types)) |
| `amount` | `int64` | Fixed discount in lowest currency unit (for amount discounts) |
| `currency` | `string` | ISO 4217 currency code (required for amount discounts) |
| `percent_bps` | `int64` | Percentage discount in basis points, 0–10000 (for percent discounts) |
| `starts_at` | `string` | Optional date the discount becomes active |
| `ends_at` | `string` | Optional date the discount stops being active |
| `provider_refs` | `[]PriceProviderRef` | Optional provider-side coupon or promotion identifiers |
| `metadata` | `map[string]interface{}` | Optional project-specific data |

### Price Payment Terms

Defines how and when payment should be collected for a plan.

| Field | Type | Description |
|---|---|---|
| `label` | `string` | Admin-facing display label (e.g. "Net 30") |
| `due_days` | `int` | Number of days before payment is due |
| `collection_method` | `string` | See [Collection Methods](#collection-methods) |
| `notes` | `string` | Admin-facing payment instructions or context |
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

#### Discount Types

| Value | Description |
|---|---|
| `amount` | Fixed monetary discount (requires `amount` and `currency`) |
| `percent` | Percentage discount in basis points (requires `percent_bps`, 0–10000) |

#### Collection Methods

| Value | Description |
|---|---|
| `manual` | Payment is collected manually (offline) |
| `automatic` | Payment is collected automatically via the payment provider |
| `invoice` | Payment is invoice-based with specified due terms |

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

### Bind, save and publish a Stripe catalogue

Drafts allow incomplete provider configuration so an operator can save work in
progress. A provider-neutral draft cannot publish until it has a valid cost
and provider association. The shared publish validator accepts valid
associations with supported providers; a plan-level Stripe reference alone
does not make a plan compatible with Stripe Checkout.

For a Stripe offer:

1. Create a **draft** and record its returned plan and cost UUIDs.
2. Provision and independently retrieve-verify the Product and each Price in
   the intended Stripe account and test/live mode. Match the approved amount,
   currency and cadence. IDs from another account or mode are not portable.
3. Record the verified Product on the plan's Stripe reference and a distinct
   Price on each purchasable Stripe cost's reference, preserving its cost UUID.
   A plan-level Product is useful for catalogue correlation, but GHATD does
   not enforce Product-to-Price correspondence. Checkout selects **cost-level**
   Price references, not a plan-level Price ID.
4. Save with the [editable-field HTTP contract](#http-update-contract), wait
   for success, then verify the complete stored draft through a fresh admin
   `GET /api/v1/pricing/plans/{id}` with `include_features=true`,
   `include_costs=true` and `include_providers=true`. Stop if Save fails:
   Publish reads persisted state, not unsaved form edits.
5. Use `POST /api/v1/pricing/plans/{id}/publish`, then verify the returned
   publication state and a fresh public
   `GET /api/v1/bms/pricing/plans/{slug}` through the configured Billing Manager.
   Scheduled publication is not public until its effective time.
6. Verify Checkout and the signed-webhook-derived billing read model. Catalogue
   publication is not evidence of a successful payment or granted access; see
   [Billing Manager fulfilment](../billingmanager/README.md#checkout-ownership-and-fulfilment).

Stripe publication rejects missing or duplicate cost-level Price IDs,
nonpositive Stripe costs, setup fees, one-time trials, recurring trials over
730 days, discounts and custom payment terms. The Price must resolve to exactly
one eligible published cost across the catalogue for Checkout. Operator checks
and [live provider verification](../paymentprovider/helpers/README.md#catalogue-and-operator-verification)
are separate from this local publication validation.

Saving a changed provider reference on an **already-published** plan changes
the live mapping when the save succeeds; there is no later staging/Publish gate.
Coordinate edits, retain the previous active Price and mapping for rollback,
and verify fresh admin/public reads before using the replacement. Pricer does
not add revision preconditions or atomic concurrent-edit protection.

## HTTP update contract

All admin writes retain the configured authentication, administrator middleware
and route policy. `PUT` uses top-level editable fields, not a GET response
wrapped in `price_plan` or `feature`:

| Endpoint | Ordinary editable fields |
| --- | --- |
| `PUT /api/v1/pricing/plans/{id}` | `slug`, `name`, `description`, `features`, `costs`, `discounts`, `payment_terms`, `provider_refs`, `metadata`, `display_order` |
| `PUT /api/v1/pricing/features/{id}` | `slug`, `name`, `description`, `type`, `unit`, `sort_order`, `metadata` |

The plan request also accepts `status`, but prefer the dedicated `/publish` and
`/archive` actions for explicit lifecycle intent. Creating a draft with
`POST /api/v1/pricing/plans` is a different request and may include
`status: "draft"` and `publish_now: false`.

Send changed fields or an allowlisted editable projection. Do not copy
top-level identity, nano-ID, actor IDs, timestamps or attribution/publication
history from GET: the selected ID belongs to the route, the actor comes from
verified context, and stored history belongs to the service. Nested cost IDs
are editable-model references, not top-level audit fields, and must be retained
for existing costs.

For example, binding an existing single-cost draft uses this shape; replace all
placeholders with verified values and use the offer's actual reviewed terms:

```http
PUT /api/v1/pricing/plans/{id}
Content-Type: application/json
```

```json
{
  "provider_refs": [
    {
      "provider": "stripe",
      "provider_product_id": "prod_<verified product>"
    }
  ],
  "costs": [
    {
      "id": "<existing cost UUID>",
      "amount": 2900,
      "currency": "USD",
      "billing_cadence": "month",
      "trial_period_days": 0,
      "setup_fee_amount": 0,
      "provider_refs": [
        {
          "provider": "stripe",
          "provider_product_id": "prod_<verified product>",
          "provider_price_id": "price_<verified price>"
        }
      ]
    }
  ]
}
```

Feature edits have the same top-level shape, for example
`{ "name": "Updated feature name", "sort_order": 0 }`, not a `feature` wrapper.

- Omitted fields leave their stored values unchanged. Preserve intentional
  zero/false values when building the editable projection.
- Sending an array replaces it, rather than merging entries. For `costs`, send
  **every** cost that must remain, with its stable ID, terms, metadata and
  existing provider refs; the single-cost example is not a multi-cost patch.
- Explicit `[]` clears `features`, `costs`, `discounts` or `provider_refs`.
  Confirm removals with a fresh GET; a published plan must remain valid.
- `payment_terms: null` currently does **not** clear existing terms: the updater
  treats it as absent. Do not assume a successful response removed them. Resolve
  this limitation before publishing a Stripe plan that already has terms;
  trusted in-process replacement is not an HTTP escape hatch.

### Update and publication errors

These are the default public mappings; hosts may compose explicit overrides.
Payload errors have several possible causes, not just replacement wrappers.

| Code | Meaning | Action |
| --- | --- | --- |
| `PRC0-01` (400) | Invalid plan payload, including a non-null `price_plan` wrapper | Check the plan editable fields and retry Save before Publish |
| `PRC0-02` (400) | Invalid feature payload, including a non-null `feature` wrapper | Send the feature editable fields, not its complete read response |
| `PRC0-18` (400) | Publication has no cost | Save a valid reviewed cost and verify the stored draft |
| `PRC0-19` (400) | Publication has no valid provider association | Save verified provider refs and verify the stored draft |
| `PRC0-25` (400) | Stripe terms or references cannot be represented by shared Checkout | Correct cost-level Price refs and unsupported terms; a plan-only Stripe ref is insufficient |

## BMS Read Endpoints for Client integration

The billing manager service exposes **read-only** pricing endpoints designed for frontend and other client applications. These endpoints require no authentication.

### GET `/api/v1/bms/pricing/plans`

Returns a paginated list of published price plans.

#### Query Parameters

| Parameter | Type | Description |
|---|---|---|
| `order` | `string` | Sort order: `created_at_asc`, `created_at_desc` (default), `updated_at_asc`, `updated_at_desc`, `published_at_asc`, `published_at_desc`, `name_asc`, `name_desc`, `slug_asc`, `slug_desc`, `display_order_asc`, `display_order_desc` |
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
            "display_order": 2,
            "metadata": {},
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

The `data` array is abbreviated below; `[...]` is not literal response JSON.

```text
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

Returns a paginated list of published feature catalogue items.

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

### GET `/api/v1/pricing/validate-slug`

Validates a proposed slug for a pricing resource (plan or feature) without persisting anything. Returns the normalised slug, availability, and a human-readable hint.

#### Query Parameters

| Parameter | Type | Description |
|---|---|---|
| `name` | `string` | Display name used to derive a slug when `slug` is not provided |
| `slug` | `string` | Explicit slug candidate to validate |
| `resource_type` | `string` | Pricing resource type: `plan`, `feature`, or `plan_feature_ref` (default: `plan`) |
| `exclude_id` | `string` | Existing resource ID to ignore during edit validation |

#### Example Request

```
GET /api/v1/pricing/validate-slug?name=Pro%20Plan&resource_type=plan
```

#### Example Response

```json
{
    "data": {
        "raw_name": "Pro Plan",
        "raw_slug": "",
        "slug": "pro-plan",
        "resource_type": "plan",
        "adjusted": true,
        "available": true,
        "hint": "pro-plan will be the stored plan slug, adjusted to comply with slug rules."
    }
}
```

If the slug is already taken:

```json
{
    "data": {
        "slug": "pro-plan",
        "resource_type": "plan",
        "adjusted": false,
        "available": false,
        "existing_id": "existing-plan-uuid",
        "hint": "The slug \"pro-plan\" is already used by another plan."
    }
}
```

For `plan_feature_ref`, the endpoint checks whether a feature with the given slug exists. `available` is `true` only when a matching feature is found, making it useful for real-time validation when adding feature references to a plan.

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

### 3. Create Feature Catalogue Items

```go
ctx := context.Background()

feature, err := pricerService.CreateFeature(ctx, &pricer.CreateFeatureRequest{
    ActorID:     verifiedAdministratorID,
    Name:        "Projects",
    Description: "Number of active projects",
    Type:        pricer.PriceFeatureTypeQuantity,
    Unit:        pricer.PriceFeatureUnitProject,
    SortOrder:   1,
    PublishNow:  true,
})
```

Check `err` before accessing `feature.Feature` in the next step.

### 4. Create a Price Plan

Create a provider-neutral draft first. The example terms are illustrative;
choose the approved offer before provisioning provider objects. The service
assigns missing cost UUIDs and returns them with the saved plan.

```go
displayOrder := 2
plan, err := pricerService.CreatePricePlan(ctx, &pricer.CreatePricePlanRequest{
    ActorID:     verifiedAdministratorID,
    Name:        "Pro",
    Description: "For growing teams",
    Status:      pricer.PricePlanStatusDraft,
    Features: []pricer.PlanFeatureRef{
        {
            FeatureID: feature.Feature.ID,
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
    DisplayOrder: &displayOrder,
    PublishNow:   false,
})
```

Check `err` before using `plan.PricePlan` or continuing to the next step. Bind
verified provider references, save and publish using the
[catalogue workflow](#bind-save-and-publish-a-stripe-catalogue); publishing this
unbound draft immediately would fail provider validation. Do not add discounts
or custom payment terms to a plan intended for the current shared Stripe
Checkout flow.

### 5. Expose via BMS for Client integration

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
│   Admin Console     │     │  Client App / Frontend  │
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
      │  bind verified ActorID from context
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

Register each migration pair from its own dated host migration file. The
`mongo-migrate` package derives the migration version from the caller's
filename, so do not register several pairs through one shared helper or source
file. For example, a host file named
`migrations/20260101120000_pricing_indexes.go` can contain:

```go
package migrations

import (
    "context"

    pricerMigrations "github.com/ooaklee/ghatd/external/pricer/migrations"
    migrate "github.com/xakep666/mongo-migrate"
    "go.mongodb.org/mongo-driver/v2/mongo"
)

func init() {
    migrate.MustRegister(
        func(_ context.Context, db *mongo.Database) error {
            return pricerMigrations.InitPricingIndexesUp(db)
        },
        func(_ context.Context, db *mongo.Database) error {
            return pricerMigrations.InitPricingIndexesDown(db)
        },
    )
}
```

Create later dated files for the provider-neutral seed and, when upgrading an
already-applied seed, `InitPricingSeedProviderNeutralReconcileUp`/`Down`. Test
fixtures and their Stripe-fixture reconciliation must each have separate dated
host files too. Import the host migration package from its migration command so
the `init` registrations run before the shared command. Apply all pending
migrations with:

```sh
asdf exec go run main.go mongo-migrator up
```

Register `InitTestPlansSeedUp` only behind two independent, fail-closed gates:
an explicit local/test environment and a dedicated fixture opt-in that defaults
to disabled. It publishes fake Stripe Product and Price identifiers for
deterministic E2E verification with a fake checkout provider. They are not
accepted by the Stripe API and must never be registered in staging, production,
or a host process configured to use a real Stripe checkout provider.

The reconciliation migration gives hosts that already applied an older version
of the three-plan fixture a new forward migration. It requires the deterministic
IDs, slugs, and ownership markers of the base plans, checks the complete
catalogue for conflicting cost and Price IDs, and will not create fixtures in an
empty or operator-owned catalogue. Fresh E2E databases receive the same target
data directly from `InitTestPlansSeedUp`, so reconciliation is an idempotent
no-op there.

The shared `down` action reverts every applied registered migration, so review
the seed and index down functions before rollback. See
[Managing MongoDB Migrations](../../docs/how-to/manage-mongodb-migrations.md).

Seed migrations are also provided:

- `external/pricer/migrations/seed_pricing.go` inserts a provider-neutral starter feature catalogue and starter plan.
- `external/pricer/migrations/seed_pricing_reconcile.go` safely removes obsolete trial fields from a previously applied owned Starter seed.
- `external/pricer/migrations/seed_test_plans.go` inserts comparison plans (`free`, `pro`, `enterprise`) plus three provider-behaviour fixtures for local pricing-card and checkout verification.
- `external/pricer/migrations/seed_test_stripe_plans_reconcile.go` safely brings an already-seeded local/test database to the current provider-behaviour fixture version.

The provider-behaviour fixtures deliberately separate commercial intent by
plan so clients do not have to distinguish two identical cadence labels:

| Fixture | Costs | Trial |
| --- | --- | --- |
| `stripe-one-time-test` | one-time | none |
| `stripe-recurring-test` | weekly, monthly, yearly | none |
| `stripe-recurring-trial-test` | weekly, monthly, yearly | 7, 14, and 30 days respectively |

Every cost has a deterministic ID and a globally unique placeholder Stripe
Price ID. Monthly is stored first on recurring plans so clients that display a
single primary cost have a stable default. These placeholders belong only to
the fake-provider fixtures. Do not register the fake fixture seed in a process
using a real Stripe provider, including Stripe test mode. Author a separate,
reviewed host catalogue with verified provider objects, starting as drafts through
the [catalogue workflow](#bind-save-and-publish-a-stripe-catalogue) instead.

## Local E2E Testing

The pricer migration test suite includes golden-card, checkout-matrix,
reconciliation, and rollback coverage under `external/pricer/migrations`.

- `TestE2E_PricingCardsGolden` exercises the public pricing HTTP endpoints and compares the response projection to `external/pricer/migrations/testdata/pricing_cards.golden.json`.
- `TestE2E_PricingStripeCheckoutMatrix` locks the exact cost IDs, Price IDs, cadences, and trial terms in the seven-case provider fixture matrix.
- `TestE2E_BillingManagerCheckoutFromStripeSeed` sends every matrix Price through Billing Manager with a fake checkout provider and verifies payment/subscription mode, trusted catalogue metadata, and trial propagation without making provider API calls.
- `TestE2E_TestStripePlansReconciliationIsIdempotentAndReversible` covers both fresh and already-seeded database paths.
- `TestE2E_PricingTestPlansSeedDownRollsBackCleanly` verifies that the pricing-card test seed rolls back cleanly without deleting the starter seed.

By default these tests try to start [memongo](https://github.com/benweissmann/memongo). On ARM Macs, or anywhere memongo cannot download a local `mongod`, set `PRICER_E2E_MONGO_URI` to a real MongoDB instance.

### Start local Mongo with Docker

The repo includes a dedicated compose file for this under `ghatd/docker/compose/docker-compose.test.yaml`:

```sh
docker compose -f docker/compose/docker-compose.test.yaml up -d
```

This exposes MongoDB on `mongodb://localhost:47027`.

### Run the pricer E2E tests locally

```sh
export PRICER_E2E_MONGO_URI=mongodb://localhost:47027
asdf exec go test -count=1 -v -run 'TestE2E_(Pricing|BillingManagerCheckout|TestStripe)' ./external/pricer/migrations/...
```

### Update the pricing-card golden fixture

If you intentionally change the pricing-card seed data or the card projection, regenerate the golden fixture with:

```sh
export PRICER_E2E_MONGO_URI=mongodb://localhost:47027
asdf exec go test -count=1 -run TestE2E_PricingCardsGolden ./external/pricer/migrations/... -update
```

Then rerun the same test without `-update` to confirm the new fixture matches runtime output.

### Test caveats

- The `PRICER_E2E_MONGO_URI` database does not need to be empty. Each run creates a random database name and drops it during cleanup.
- The pricing-card golden test requests `include_costs=true`, `include_features=true`, and `include_providers=true` on the single-plan endpoint so the response matches what a frontend pricing-card view needs.
- To skip the E2E tests in fast local runs, use `asdf exec go test -short ./external/pricer/...`.

## Potential Future Improvements

Here's a list of areas for improvement in future iterations of `pricer`. These suggestions are not prioritised.

### Catalogue Features
- [ ] Plan versioning / change history
- [ ] Plan comparison matrix endpoint
- [ ] Plan tags and categories
- [ ] Custom feature groups for plan display

### Pricing Flexibility
- [ ] Usage-based / metered pricing
- [ ] Tiered pricing (volume discounts)
- [ ] Per-unit add-on pricing
- [ ] Multi-currency cost entries per plan

### Integrations
- [ ] Stripe product/price sync (bidirectional)
- [ ] Paddle product/price sync
- [ ] Provider webhook reconciliation
- [ ] Kofi membership/sponsorship sync

### Developer Experience
- [ ] Admin console UI for plan management
- [ ] Plan preview before publish
- [ ] Bulk feature import/export
- [ ] Migration helpers for existing pricing data

### Queries & Performance
- [ ] Caching for BMS read endpoints
- [ ] Full-text plan/feature search
- [ ] Plan family / related plan recommendations
