---
id: adrs-adr009
title: 'ADR009: Public RateLimitOrActive Cookie Downgrade'
# prettier-ignore
description: Architecture Decision Record (ADR) for downgrading invalid cookie auth state to anonymous access on public-or-active middleware
date: 2026-05-21
status: accepted
---

## Status

Accepted.

## Context

Access Manager supports routes that may be used by either authenticated active
users or anonymous callers subject to rate limiting. These routes use
`RateLimitOrActiveJWTRequired`: authenticated requests receive user context,
while requests without credentials receive an anonymous placeholder user and
still pass through the route handler.

Some browser and crawler clients can send stale, empty, or otherwise invalid
auth cookies to a public-or-active route. That can happen after cookie deletion,
session expiry, failed refresh, restored browser state, or client-side replay of
old headers. In the previous behavior, the presence of an auth or refresh cookie
forced the middleware down the JWT validation path. If that validation or token
refresh failed, the middleware cleared cookies and returned an auth error.

That strict response is correct for protected routes, but it is surprising for
public-or-active routes: the same request would have succeeded as anonymous if
the client had not sent broken cookie state. The broken cookie therefore poisons
public content until the client accepts and applies the deletion response.

## Decision

`RateLimitOrActiveJWTRequired` will preserve authenticated behavior when valid
credentials are present:

1. Valid auth cookies still attach authenticated user context.
2. Expired access cookies with valid refresh cookies still attempt refresh.
3. Successful refreshes still commit replacement cookies only after retry
   validation succeeds.

Cookie authentication remains the credential mechanism for this middleware.
Unlike API-token-or-JWT middleware variants, `RateLimitOrActiveJWTRequired` does
not validate `X-Api-Token` headers.

When cookie auth is unusable on a public-or-active route, the middleware will
clear auth cookies and downgrade the request to the anonymous rate-limited flow
instead of returning an auth error:

1. Missing or empty cookie values are treated as unauthenticated for this
   middleware.
2. Missing refresh cookies on an otherwise cookie-authenticated request clear
   auth cookies and continue as anonymous.
3. Failed auth validation, failed refresh, or failed retry validation clear auth
   cookies and continue as anonymous.
4. The `Authorization` header set from cookie auth is removed before the
   anonymous retry so invalid cookie state cannot leak into the public flow.
5. The middleware emits safe structured logs for downgrades without recording
   token values.

If a request also supplied its own `Authorization` header, cookie-derived auth
continues to take precedence. A downgraded public-or-active request removes the
header before entering the anonymous rate-limited flow.

Protected middleware such as `JWTRequired`, `ActiveJWTRequired`, and
`ActiveValidApiTokenOrJWTRequired` remains strict. Broken auth state on protected
routes still clears cookies and returns an auth error.

## Consequences

Public-or-active endpoints become resilient to stale or empty auth cookie state.
A client with broken cookies receives the same public body and status code it
would have received with no cookies, while still receiving deletion headers for
the invalid state.

The downgrade does not grant private access. When auth validation fails, the
request proceeds only with the anonymous placeholder user produced by the
rate-limited flow.

The placeholder ID is deliberately non-empty, so downstream consumers cannot
use the presence of a requestor ID as proof of authentication. Consumers must
use `AcquireAuthenticatedFrom` when branching on authentication state and
`AcquireAuthenticatedUserIDFrom` before using a context-derived actor ID for a
database lookup, authorization decision, or attribution. A false or missing
authentication state fails closed and yields no authenticated user ID.

This rule applies to the request actor rather than every user ID handled during
the request. IDs loaded from persisted domain records, such as a content author
used for display-name enrichment, remain independent of the viewer's
authentication state.

Host applications can rely on public-or-active routes for public content without
letting stale auth cookies turn those routes into hard auth failures. They
should still use protected middleware for endpoints that require a real user.

Operationally, invalid cookie state is easier to observe because downgrades are
logged with reason and cookie-presence metadata, but without auth token values.
