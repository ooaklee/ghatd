---
id: adrs-adr007
title: 'ADR007: Add starter/v0 as an ejectable Lazy composition layer'
# prettier-ignore
description: >
  Introduce external/starter/v0 as a low-friction starter layer that composes
  existing modular GHATD packages, while keeping core packages independently
  usable and preparing host applications to migrate from hand-written
  bootstrap wiring to starter-owned wiring.
---

## Context

GHATD's core strength is modularity. Packages such as `external/repository`,
`external/accessmanager`, `external/usermanager`, `external/notifier`,
`external/billingmanager`, and `external/contentmanager` can be used directly
and composed by host applications.

That modularity is powerful, but it creates onboarding friction for new
projects. A host application currently needs to understand
and wire many concerns before it has a working server:

- MongoDB URI construction and repository handler configuration.
- Core repositories and services.
- Error manifest composition across package boundaries.
- Access middleware aliases for route attachment.
- Handler construction and route attachment.
- Provider setup for email, OAuth, billing, notifications, Redis, and MongoDB.
- Cleanup behaviour for long-lived clients and temporary credential files.

The immediate pain point was MongoDB URI construction. Both standard MongoDB
and Atlas connection strings were being assembled inline with `fmt.Sprintf`,
duplicating knowledge and making special-character handling easy to get wrong.

At the same time, we do not want a hidden framework container or a global
service locator. New users should be able to start lazily, then eject or
replace any layer when their project needs more control.

## Decision

We will introduce `external/starter/v0` as an ejectable Lazy composition layer.
The starter package is a convenience layer over existing GHATD packages, not a
replacement architecture.

The starter rollout will follow these rules:

1. Core packages remain independently usable.
2. Starter code composes existing packages; it does not reimplement domain
   behaviour.
3. Starter exposes the real components it creates through named containers
   such as `Repositories`, `Services`, `Handlers`, `Middleware`, and `Stack`.
4. Starter users can replace individual components or copy the starter wiring
   into their own project when they outgrow the default path.
5. New helpers live in the package that owns the concern. For example, MongoDB
   URI helpers live in `external/repository/helpers`, not in starter.
6. Tests for each rollout slice use table-driven SUCCESS/FAILURE cases where practical.
7. Documentation is updated alongside each implementation slice.

For this first rollout slice, we will add the foundation pieces that reduce
bootstrap noise without locking in the full starter API:

- `external/repository/helpers` gets MongoDB URI helpers:
  `GenerateMongoURI`, `GenerateGenericMongoURI`, and `GenerateAtlasMongoURI`.
- `cmd/mongo-migrator` uses the shared URI helper.
- `external/errormanifest/bundles` provides named cross-package error manifest
  bundles for common GHATD wiring. It is a subpackage to avoid import cycles
  with domain packages that already import `external/errormanifest`.
- `external/accessmanager/middleware` gets `Suite`, a named middleware
  container that maps existing access middleware methods to route attachment
  fields.
- `external/starter/v0` starts as a small, typed skeleton with `Config`,
  `Repositories`, `Services`, `Handlers`, `Middleware`, `Stack`, and `Cleanup`.
- Starter documentation is added under both the package and
  `docs/getting-started`.

After this foundation is reviewed and committed, the next phase will wire
starter-owned constructors for common GHATD repositories, services, handlers,
and middleware. Once that API is stable, the host application's server entry
point will be migrated to consume `external/starter/v0`.

The second rollout slice adds that wiring layer:

- `NewRepositories` builds the standard Mongo-backed repositories from a core
  `external/repository.MongoDbRepository`, while allowing per-repository
  overrides.
- `NewServices` builds the standard GHATD services and manager services from
  repositories plus explicit host-provided integrations for ephemeral storage,
  email, optional audit overrides, OAuth, policy, notifications, auth secrets,
  and payment providers.
- `NewHandlers` builds the standard handlers with default error bundles from
  `external/errormanifest/bundles`, while allowing callers to replace or clear
  those bundles.
- `NewMiddleware` wraps the accessmanager middleware suite instead of
  duplicating all middleware aliases inside starter. It can reuse the
  service-layer ephemeral store when that store supports hardened rate
  limiting, or accept an explicit middleware store override.
- `NewStack` validates `Config` and groups already-built layers. It does not
  force all layers to be present, so projects can adopt starter/v0
  incrementally. Nil layers mean "not wired yet" and must be checked before
  use.

The starter constructors use request structs. This keeps the API readable as
the dependency list grows and gives host applications named escape hatches for
custom repositories, configs, error maps, and provider registries.

### Phase 3 — Route Attachment Helper (AttachDefaultRoutes)

The third rollout slice adds `AttachDefaultRoutes`, an optional helper that
attaches every standard GHATD API route group in a single call:

- `RouteGroup` is a typed string enum with constants for each package:
  `pricer`, `policy`, `user`, `group`, `accessmanager`, `usermanager`,
  `contentmanager`, `billingmanager`.
- `AttachDefaultRoutesRequest` holds a `*router.Router`, `*Stack`, and
  `Skip []RouteGroup`.
- `AttachDefaultRoutes` calls each non-skipped package's `AttachRoutes`
  function with the corresponding handler from `Stack.Handlers` and
  middleware from `Stack.Middleware.AccessManager`.
- Validation enforces non-nil handlers for non-skipped groups and non-nil
  middleware for every middleware function required by at least one
  non-skipped group. Skipped groups may have nil handlers, and a remaining
  policy-only/default subset may omit access middleware entirely.
- Unknown `RouteGroup` values fail validation instead of being silently ignored.
- SPA routes, auth verify/CORS middleware, and router bootstrap remain
  host-owned — `AttachDefaultRoutes` attaches only API routes.

This preserves ejectability: if the default wiring no longer fits, replace
the single `AttachDefaultRoutes` call with per-package `AttachRoutes` calls
or copy the function body.

## Consequences

New GHATD projects gain a Lazy path that makes the first server easier to
assemble without removing the composed and fully custom paths.

The starter package creates a clear place for common application wiring, which
should reduce copy-paste setup in host projects and lower cognitive load for
new contributors.

Core packages remain the source of truth for behaviour. This keeps package
ownership clear and avoids a monolithic starter package that becomes a second
implementation of GHATD.

The error bundle helpers intentionally live in `external/errormanifest/bundles`
rather than `external/errormanifest` to avoid import cycles. Application-level
composition code can import bundles, while domain packages can continue to
import only the composer.

The access middleware suite improves route wiring readability, but it adds one
more public API that must remain aligned with route package expectations.
Changes to access middleware names or semantics should update the suite and
its table tests together.

MongoDB URI helpers improve safety by URL-encoding credentials and query
parameters. Atlas URIs with an empty `appName` omit the empty query parameter
instead of producing `appName=`, which is a small behavioural improvement over
the previous inline string formatting.

Starter/v0 is intentionally versioned in its import path. If the starter API
needs a breaking redesign later, a future `external/starter/v1` can be added
without forcing immediate changes on projects using v0.

The next phases must be careful not to hide app-specific decisions too deeply.
Provider choices, secrets, environment behaviour, and cleanup should remain
visible and replaceable from the host application's server setup.

The `CleanupGroup` helper (added in a follow-up) provides a lightweight way to
aggregate multiple `Cleanup` functions, one per resource, without hand-rolling
error collection. It remains consistent with the "cleanup lives in the
host application" rule, because the host still owns the individual `Cleanup`
functions; `CleanupGroup` is only a composition helper that runs them all and
joins errors. Its `Run` method satisfies the `Cleanup` type so it can be
assigned directly to `Stack.Cleanup`.

HTTP server graceful shutdown lives in `external/http/server` as an
ejectable lifecycle helper (`StartServerWith`) and is not a
`starter/v0` service concern.

The second rollout preserves that boundary. Starter creates GHATD-owned
components, but it still expects the host application to create and own Mongo
handlers, Redis clients, email providers, OAuth providers, validators, payment
provider clients, and cleanup behaviour.

The initial host application migration follows that shape: it keeps database,
cache, email provider, payment provider, OAuth provider, push notification
credential cleanup, router setup, and server shutdown in its own server entry
point, while delegating GHATD repositories, services, handlers, error bundles,
and access middleware aliases to `external/starter/v0`.
