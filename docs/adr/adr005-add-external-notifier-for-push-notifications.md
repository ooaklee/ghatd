---
id: adrs-adr005
title: 'ADR005: Introduce external/notifier for push notification delivery'
# prettier-ignore
description: >
  Add a new external/notifier package that owns notification device/address
  storage, preferences, and channel delivery (Web Push and FCM). UMS exposes
  the API surface behind authentication and admin middleware.
---

## Decision

A new `external/notifier` package was created to own everything related to
push notification delivery: registering devices, storing their push tokens
or endpoints, managing per-user preferences, and orchestrating actual sends
through channel-specific sender adapters.

UserManager (UMS) depends on `notifier.Service` as an optional dependency
(wired via `WithNotifierService()`) and exposes the HTTP API with proper
authentication and admin access control.

V1 supports Web Push end-to-end (browser subscription through to delivery)
and adds FCM registration hooks without introducing Firebase app configuration.

## Discussion

### Why a separate package?

Notifications touch four concerns — device registration, preference storage,
channel delivery, and admin sends. Keeping them in a single package with a
clear `Service` boundary:

- Makes the dependency direction obvious (UMS depends on notifier).
- Keeps the data model self-contained (notification preferences are stored
  in notifier-owned collections, not on the UniversalUser document).
- Allows the senders and preferences to evolve without touching core user records.

### Why address deduplication?

A user might sign out and another user sign in on the same browser. If the
browser's Push subscription creates a new address record each time, stale
addresses accumulate and the same browser receives notifications for multiple
users.

The solution: hash the channel + identity (endpoint for Web Push, token for FCM)
into a `channel + address_hash` unique index. When the same subscription
registers again, the existing document is updated with the new user ID.

### Why sanitised output for clients?

The full `NotificationAddress` contains the Push API endpoint and encryption
keys (or the FCM token). If these leak in a client response, an attacker
could push arbitrary notifications to the user.

The `Sanitise()` method creates a `NotificationAddressSummary` that only
includes non-sensitive fields (ID, channel, status, device info, timestamps).
The full addresses are only used internally by `NotifyUser`.

### Why FCM is plumbed but disabled?

The plan calls for FCM support, but Firebase project configuration is not
available yet. Rather than block the entire feature, the FCM sender adapter
and mobile registration hooks are built but gated behind the `Enabled` flag.
When Firebase credentials arrive, flipping the flag is all that is needed.

## Consequences

- All notification preferences move from `UniversalUser` to a dedicated
  `notification_preferences` collection.
- The notifier package adds a new dependency: `github.com/nikoksr/notify`
  and its webpush/FCM service sub-packages.
- Server deployments without push notification support can leave the
  notifier unwired; UMS returns HTTP 503 for notification endpoints rather
  than failing to start.
- Mobile push registration requires a real `NotificationTokenProvider`
  (e.g. `firebase_messaging`), which is not included in this phase.
