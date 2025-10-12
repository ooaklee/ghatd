---
id: adrs-adr004
title: 'ADR004: Email-Based Subscriptions for Pre-Registration Purchases'
description: |
  Support subscriptions identified by email address only, enabling users to purchase before creating an account on the platform.
---

## Decision

We will enable **Email-Based Subscriptions and Billing Events** to support purchases made before a user creates an account on the platform.

This involves three main changes:

1.  **Data Model Update**: Both the `Subscription` and `BillingEvent` models support a **nullable `UserID`** field, while the `Email` field is required. A subscription or billing event with an empty `UserID` represents a valid pre-registration purchase.

2.  **Association Strategy**: Both subscriptions and billing events are associated with users via two mechanisms:
    * **Automatic Association**: The **Access Manager** orchestrates the linking of any unassociated subscriptions and billing events (identified by email) to the `UserID` immediately after a new user account is created.
    * **Manual Association**: Admin APIs allow for manual linking for edge cases like corporate purchases or email mismatches.

3.  **Direct Email-Based Queries**: All billing operations (subscriptions and events) use **direct email field queries** rather than multi-step lookups, enabling:
    * Single-query association operations (improved performance)
    * Support for one-off payments (donations, shop orders, commissions) without requiring subscriptions
    * Simplified architecture with billing events as truly isolated entities


## Context

Traditional subscription models require an existing user account for a purchase, creating **technical friction** and **business limitations**.

* **Conversion Friction**: Requiring account creation first can reduce conversion rates and complicate sales flows like **gift subscriptions** or **pre-launch sales**.
* **Webhook Failures**: Webhooks from payment providers fail if the system cannot find a matching user ID at the time of purchase, risking data loss.
* **One-Off Payment Support**: Payment providers like Ko-fi support multiple payment types (donations, shop orders, commissions) that don't involve subscriptions, but still need to be tracked and associated with users.
* **Architectural Goal**: Maintain the **clean separation of concerns** between the billing system (focused on payment data) and the user management system (focused on account lifecycle).

This decision supports the modern e-commerce practice of **"buy now, register later"** while ensuring data integrity and comprehensive payment tracking across all payment types.


## Consequences

* **Positive Consequences**
    * **Improved User Experience**: Users can purchase immediately, reducing friction and supporting flexible flows like pre-launch sales.
    * **No Webhook Failures**: Webhooks succeed even when the user hasn't registered, as both subscriptions and billing events are stored with only the email.
    * **Automatic Association**: Manual intervention for linking purchases is rarely needed, as the system handles it automatically upon user signup for both subscriptions and billing events.
    * **Universal Payment Support**: All payment types (subscriptions, donations, shop orders, commissions) are tracked and associable using the same email-based mechanism.
    * **Performance Optimization**: Direct email-based queries eliminate multi-step lookups (50% query reduction), simplifying the architecture and improving response times.
    * **Data Independence**: Billing events exist as isolated entities, not requiring subscription records, which enables accurate tracking of all revenue sources.


* **Negative Consequences**
    * **Dual Query Support**: Systems must support queries by both `UserID` and `Email` to handle both associated and unassociated records.
    * **Edge Case Handling**: The system must monitor and manage **orphaned records** (subscriptions/events where the user never signs up) and handle cases of email mismatches.
    * **Webhook Handler Updates**: All payment provider webhook handlers must be updated to extract and store customer email when creating billing events.

## Architectural Integration

This feature integrates by leveraging the **manager-as-orchestrator** pattern established in GHAT(D).

```
Access Manager (Orchestration)
↓
Billing Service (Service Operations: AssociateSubscriptionsWithUser, AssociateBillingEventsWithUser)
↓
Billing Repository (Persistence: Direct Email Queries, Nullable UserID, Email Indexes)
```

The **Access Manager** coordinates the cross-domain operation, while the **Billing Service** provides two parallel service operations to perform association:
- `AssociateSubscriptionsWithUser` - Links subscriptions via email
- `AssociateBillingEventsWithUser` - Links billing events via email

Both operations use **direct email-based queries** for optimal performance.
