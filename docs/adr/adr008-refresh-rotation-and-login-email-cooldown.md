---
id: adrs-adr008
title: 'ADR008: Refresh Rotation and Login Email Cooldown'
# prettier-ignore
description: Architecture Decision Record (ADR) for tolerating near-concurrent refresh-token rotation and suppressing duplicate login email sends
date: 2026-05-21
status: accepted
---

## Status

Accepted.

## Context

Access Manager uses short-lived access tokens and longer-lived refresh tokens
stored in `HttpOnly` cookies. When an access token expires, middleware or a
client refresh call can rotate the refresh token and issue a new token pair.

Refresh-token rotation is intentionally one-time use. That improves replay
resistance, but browser applications can naturally create near-concurrent
refresh attempts: multiple tabs resume at once, a service worker and foreground
request both discover expiry, or several protected requests leave the browser
while the access-token cookie is stale.

Without coordination, the first refresh request consumes the old refresh token
and later requests fail even though they are part of the same user-visible
session recovery. That can sign users out after ordinary idle/resume behavior.

Passwordless login has a related user-experience and quota problem. Login
initiation returns an accepted response before the user verifies the email. If
a client keeps the submit action available, repeated clicks can send repeated
emails for the same user and return URL. This is noisy for users and wasteful
for email providers.

## Decision

Access Manager will make refresh-token rotation tolerant of near-concurrent
duplicates by adding a Redis-backed lock and replay result:

1. A refresh request validates the refresh cookie and derives the user ID plus
   refresh-token UUID.
2. The service checks for a short-lived cached rotation result for that
   user/token pair.
3. If no result exists, the request attempts to acquire a short-lived rotation
   lock.
4. The lock winner consumes the old refresh token, creates the replacement
   token pair, stores the new auth state, and stores a short-lived replay
   result.
5. A duplicate request that does not get the lock waits briefly for the replay
   result and returns it when available.
6. If no replay result appears before the wait expires, the duplicate request
   fails and the client should resolve the session through the normal `/me`
   probe or login flow.

Middleware that refreshes during protected-route handling will update the
request with the replacement access token and retry validation before writing
replacement cookies. Refreshed cookies are committed only when retry
validation succeeds.

Access Manager will also suppress duplicate login email sends for active users
within a short cooldown window. The cooldown key is scoped by user ID,
dashboard flag, and a hash of the requested return URL. If the cooldown is
already active, the service returns without sending another email. If token
setup or email delivery fails before the email is accepted, the cooldown is
released so a later retry can send a new email.

Host applications should still keep their login forms disciplined: disable the
submit button while the login request is in flight, treat any successful `2xx`
login-initiation response as "check your email", and keep the form locked once
the request is accepted.

## Consequences

Users are less likely to be signed out by ordinary idle/resume behavior or by
multiple requests discovering token expiry at the same time.

Refresh-token one-time-use semantics remain intact. Only the lock winner
consumes the old refresh token; duplicates receive a short-lived replay of the
winner's replacement token pair.

The design adds a small Redis dependency surface: `SETNX` locks, replay
payload storage, and cooldown keys. Ephemeral store implementations must
provide the same semantics if they replace the Redis store.

The replay window and wait timeout are intentionally short. They reduce common
browser races without making consumed refresh tokens broadly reusable.

Login email quota usage is protected at the server boundary, while clients
remain responsible for presenting a stable check-email transition.

Cooldown scoping by hashed return URL avoids storing raw return paths in Redis
keys while still letting legitimately different login contexts send their own
email.
