---
id: adrs-adr003
title: 'ADR003: Separation of Billing System with Webhook-Based Provider Abstractio'
description: |
  Design and build a billing system with three separate packages with clear architectural boundaries and responsibilities.
---

## Decision

We will refactor the billing system into three clean architecture packages:

### `billing` Package
This package is responsible for billing data persistence and subscription management
- Manages subscription and billing event models
- Handles CRUD operations for subscriptions and billing events
- Zero dependencies on payment provider logic or webhook processing

### `paymentprovider` Package
This package is responsible for payment provider abstraction
- Defines a provider interface for webhook verification and payload parsing
- Keeps support for multiple providers (Stripe, Paddle, Lemon Squeezy, Ko-fi)
- Easy addition of new providers through common interface

### `billingmanager` Package
This package is responsible for billing orchestration and business logic management
- Combines the above packages and provides high-level APIs for webhook processing and subscription queries
- Integrates with existing systems (user service, audit service)

## Context

The initial billing implementation suffered from a monolithic structure and provider lock-in, limiting scalability and flexibility. The system relied on manual status checking and contained mixed functionalities—webhook verification, business logic, and data persistence—which made testing and maintenance complex.

The change aims to enable Multi-Provider Support, clearing boundaries between packages, and adopt a Webhook-First Design for a closer to real-time subscription updates.

### Architectural Boundaries

Here is the proposed boundaries

```
Application Layer       External Payment Provider (i.e, kofi, stripe, etc.)
         ↓                 ↙        
 billingmanager (Orchestration)
       ↙          ↘
paymentprovider   billing
(Webhooks)        (Data)
```

Each package will have its own models, errors, and configurations, ensuring testability and adherence to the single responsibility principle.

## Consequences

* Splitting the billing system into three packages will improve testability and make it easier for others to shape for their unique use cases.
* Allow users to extend the base offering to better suit their requirements without having to alter core logic.
  * Easy addition of new payment providers (Stripe, Paddle, Lemon Squeezy, Ko-fi, custom providers)
  * Support for webhook-first architecture with real-time subscription updates
  * Flexible user association supporting pre-registration purchases
* Configuration will now need to be split across three packages instead of one, though it's worthwhile noting that each package's configuration is focused on its specific concerns.
* Users of GHAT(d) will need to understand the separation of concerns and know which package to use for different scenarios.
* The `billingmanager` package provides optional integration with audit and user services, allowing flexibility in how billing events are logged and how users are resolved.

