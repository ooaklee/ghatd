---
id: adrs-adr010
title: 'ADR010: Optional Streaker and Reminder Manager Integrations'
# prettier-ignore
description: >
  Architecture Decision Record (ADR) for exposing optional reminder and streaker
  services through starter/v0 and User Manager while keeping product workflows
  owned by host applications.
date: 2026-05-22
status: accepted
---

## Status

Accepted.

## Context

GHATD provides reusable packages for reminder schedules and streak tracking.
Both packages are intentionally generic: Reminder stores notification schedule
intent, while Streaker records idempotent completions for a stable owner,
target, period, and period key.

Host applications often need these packages together with User Manager because
reminders and streaks are usually user-scoped features. At the same time, the
meaning of a reminder or streak is product-specific. One application might
record a streak for opening the app, another for completing a lesson, and
another for saving content. One application might send reminders through push,
another through email, and another through a background job that selects content
from a user-owned collection.

Starter/v0 should make the common integration path easy without turning
Reminder or Streaker into mandatory dependencies for every GHATD application.
User Manager should expose user-scoped APIs when those services are configured,
but applications that do not need reminders or streaks should still be able to
build and run the rest of the stack.

## Decision

Reminder and Streaker will remain optional services in starter/v0.

`NewRepositories` may build reminder and streaker repositories from the shared
Mongo repository. `NewServices` may build `Services.Reminder` and
`Services.Streaker` from those repositories. If either service cannot be built
because its repository is absent, starter still constructs the rest of the
service container.

When available, starter attaches Reminder and Streaker to
`Services.UserManager`. The User Manager route group can then expose
authenticated `/me` reminder and streak APIs, plus admin/service read APIs where
appropriate. The `/me` routes always scope operations to the authenticated
requester. Admin/service streak reads may accept a target `user_id` after the
caller has passed User Manager access checks.

User Manager treats a missing optional service as a disabled integration. Direct
calls to reminder or streak endpoints return a clear service-unavailable style
error instead of panicking. Host application side effects should be even softer:
they should log before attempting an optional record, skip when the service is
not configured, and warn without failing the primary product action when an
optional record fails.

Product-specific semantics remain owned by host applications. A host
application decides when a reminder is created, what schedule presets mean, when
a streak should be recorded, which target IDs are stable aggregate scopes, which
metadata should be stored, and which background job or notification transport
uses reminder schedules. Streaker does not decide whether a playback, save,
check-in, or task completion is valid; it only records the streak request it is
given.

Starter/v0 will not add a standalone Streaker route group. Streaker access is
available through User Manager when attached, or directly through host-owned
managers and handlers. Reminder keeps its existing package APIs and UMS
integration, with starter wiring available as the lazy path.

Package migrations remain host-owned. Starter may construct repositories and
services, but it does not run Mongo migrations or background schedulers. Hosts
register reminder and streaker indexes in their migration package and may
execute them through the shared command defined in
[ADR018](./adr018-shared-mongodb-migrator-command.md).

## Consequences

New GHATD applications can adopt reminders and streaks through the lazy starter
path with minimal wiring, while applications that do not need those features do
not pay an integration cost.

User-scoped reminder and streak APIs become first-class User Manager
integrations without making User Manager responsible for product-specific
workflow decisions.

Host applications have a clear failure policy. User-facing optional-service APIs
can report that the integration is disabled, while primary product workflows can
continue when optional streak or reminder side effects are unavailable.

The boundary keeps Streaker accurate for aggregate actions. Host applications
must choose stable target IDs for "once per period" goals and put
event-specific resource IDs in metadata; otherwise they will create separate
streak scopes per resource.

The lazy starter API gains more surface area through optional services and
override hooks. Documentation and tests need to stay explicit about nil-service
behaviour so future packages follow the same optional-integration pattern.
