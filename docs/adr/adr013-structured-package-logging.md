---
id: adrs-adr013
title: 'ADR013: Structured GHATD Package Logging'
# prettier-ignore
description: >
  Architecture Decision Record (ADR) for adding consistent GHATD package
  attribution, operation fields, and sensitive-data rules to framework logs.
date: 2026-05-27
status: proposed
---

## Status

Proposed.

## Context

GHATD packages run inside host applications and usually reuse the host request
logger. That gives every package access to host fields such as `component`,
`environment`, and `correlation-id`, but it also means GHATD package logs can be
hard to distinguish from host application logs.

This became painful during a notification dispatch failure where the only
visible log line was the host request middleware summary for a `500` response.
The summary included request metadata and a correlation ID, but not the GHATD
package, delivery channel, sender path, target user, or concrete package error.

GHATD also has several package layers that need consistent diagnostics:
handlers and fenders, services, repository helpers, third-party providers,
background cleanup flows, and starter composition helpers. These logs must be
useful in production without exposing secrets or payload values.

## Decision

GHATD package code will acquire loggers from `context.Context` and add stable
framework attribution fields before writing package logs.

The canonical helpers are:

```go
logger := logger.AcquirePackageFrom(ctx, "external/notifier")
logger := logger.AcquireOperationFrom(ctx, "external/notifier", "notify-user")
```

These helpers preserve the host logger and add:

- `source=ghatd`
- `ghatd-package=<package-path>`
- `operation=<operation-name>` when the operation is known

They also apply `zap.AddStacktrace(zap.DPanicLevel)` consistently, so package
call sites do not need to repeat that option.

Package authors should use the narrowest logger that matches the current flow:

- Use `AcquireOperationFrom` for service methods, repository operations,
  handler/fender mappings, third-party provider calls, and background jobs.
- Use `AcquirePackageFrom` when the operation is already clear from the log
  message or when a helper is shared by multiple operations.
- Use the request context for HTTP handlers and middleware.
- Use an explicit context for startup, shutdown, cleanup, and CLI flows.
- Leave pure value helpers unlogged unless they are called from a logged flow.
- Name the acquired local logger `logger`. Do not introduce aliases such as
  `log`, `loggr`, `logr`, `structuredLog`, or `zapLogger` for GHATD package
  logging.

The log level convention is:

- `Debug`: routine branch decisions, counts, pagination, cache hits, and
  non-essential success details.
- `Info`: lifecycle boundaries, completed work, dispatch summaries, and
  expected high-level state transitions.
- `Warn`: recoverable or expected failures such as validation misses, skipped
  senders, missing optional dependencies, or no-op cleanup paths.
- `Error`: failed dependencies, persistence failures, third-party call failures,
  and errors returned to callers.

GHATD logs must not include secrets or raw payload values. Do not log raw
tokens, cookies, authorization headers, passwords, private keys, webhook
signatures, push endpoints, FCM tokens, request bodies, provider payloads, or
arbitrary notification data values. Prefer IDs, counts, booleans, data keys,
channel names, provider names, event IDs, status enums, hashes already stored
by GHATD, and redacted lengths.

When an existing log needs request or model context, package code should pass
the value through `logger.SafeValue` or `logger.SafeAny`, or use an equivalent
package-specific helper. These helpers reduce structs to operational fields and
summarise maps and slices without serialising raw payload values.

Host application fields remain intact. For example, a log can contain both
`component=<host-component>` and `source=ghatd`; the GHATD fields identify the
framework package while the host fields identify the running application.

## Consequences

GHATD package logs become filterable by `source=ghatd` and by
`ghatd-package`, while still retaining request correlation from the host logger.

Operational failures can now identify the package and operation responsible for
the error. Notification sends, repository helper failures, payment provider
verification, SPA fallback behavior, and cleanup flows can be debugged without
guessing which layer emitted a log line.

The convention adds more debug-level events. Host applications should keep
production log levels at `info` or higher unless debugging a specific issue.

Package authors must avoid raw request and payload logging. Existing or future
logs that need request context should log selected safe fields rather than
serialising entire request structs or payloads.

Applications that query logs by field names should add filters for
`source=ghatd`, `ghatd-package`, and `operation`.

## Rollout

The initial GHATD rollout introduces the package helpers and migrates existing
package logger acquisition to include GHATD attribution. It also adds targeted
logs for previously quiet package paths such as HTTP server lifecycle, SPA
fallbacks, repository connection helpers, blueprint, cleanup, and CLI clone
flows. HTTP handlers and context-bearing service methods should emit at least a
debug-level operation boundary so host applications can trace a request through
GHATD even when the failure happens in a downstream dependency.

After this ADR is accepted and the GHATD changes are released, host
applications should update to the released GHATD version, verify that request
middleware still propagates the logger through context, and review log queries
or dashboards for the new fields.
