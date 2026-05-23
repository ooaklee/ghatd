---
id: adrs-adr012
title: 'ADR012: Client Platform and Timezone Request Context'
# prettier-ignore
description: >
  Architecture Decision Record (ADR) for treating client platform and timezone
  headers as explicit GHATD integration context without making every platform
  behave like a browser navigation surface.
date: 2026-05-23
status: accepted
---

## Status

Accepted.

## Context

GHATD host applications can be used from multiple client surfaces: browser
apps, native mobile apps, browser extensions, command-line tools, and other API
clients. These clients often need to send lightweight request context that is
not authentication state. Two examples are:

- `X-Platform`, which identifies the client surface that made the request.
- `X-Timezone`, which identifies the client's current IANA timezone.

Before this decision, GHATD defined `X-Platform` as
`WebPlatformHttpRequestHeader`. CORS allowed the header name, and Access Manager
used a non-empty `X-Platform` value as a signal that logout should redirect the
browser back to `/`. That was acceptable while the only intended value was the
legacy browser app value `web`.

Timezone-aware packages such as Streaker and Reminder now make client timezone
context more important. Host applications may use request timezone context when
recording streaks, building summaries, or creating reminders. At the same time,
native and extension clients may send `X-Platform` on every request. Treating
any non-empty platform value as a browser navigation request would make those
clients receive redirects where they expect normal API responses.

## Decision

GHATD will treat client platform and timezone as explicit request-context
headers.

`common.WebPlatformHttpRequestHeader` remains the canonical header constant for
the existing public API contract, and its value remains `X-Platform`. GHATD will
also define explicit platform value constants:

- `web`
- `mobile`
- `browser-extension`

The `web` value identifies browser app surfaces for Access Manager logout
redirects. HTMX requests keep their existing redirect behaviour. Other platform
values, including mobile, browser extension, and unknown API clients, receive
API responses rather than browser navigation redirects.

GHATD will define `common.TimezoneHttpRequestHeader` as `X-Timezone` and allow
it through the shared CORS middleware. `X-Timezone` should contain an IANA
timezone such as `Europe/London`. GHATD will not use this header for
authentication or authorization. Host applications can choose whether to pass
the value into packages such as Streaker and Reminder.

## Consequences

Browser logout behaviour remains backward compatible for clients that send
`X-Platform: web`.

Native, extension, and API clients can safely send platform context without
being treated as browser navigation flows.

Timezone-aware integrations have a shared header name that CORS permits by
default. Host applications still own precedence rules, such as preferring a
persisted user timezone over a request header.

The platform values are now part of the public integration contract. New client
surfaces should either reuse one of the defined values when the semantics match
or introduce a new explicit value with tests for any handler behaviour that
branches on platform.
