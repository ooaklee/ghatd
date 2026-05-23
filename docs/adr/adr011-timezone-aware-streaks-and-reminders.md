---
id: adrs-adr011
title: 'ADR011: Timezone-Aware Streaks and Reminders'
# prettier-ignore
description: >
  Architecture Decision Record (ADR) for deriving user-facing streak periods
  and reminder due times from IANA timezones while keeping persisted timestamps
  in UTC.
date: 2026-05-23
status: accepted
---

## Status

Accepted.

## Context

Streaks and reminders are experienced in a user's local time. A daily streak
should turn over at the user's local midnight, and a reminder set for 09:00
should become due at 09:00 in the user's timezone. UTC remains the right storage
format for timestamps, but UTC alone is not enough to derive user-facing period
keys or scheduler due times.

Before this decision, Streaker derived daily, weekly, and monthly period keys in
UTC. Reminder stored a `timezone` value for display context, but scheduler due
queries compared raw `target_time` strings. Host applications would therefore
have needed to reimplement timezone-aware logic around GHATD, increasing the
risk of inconsistent streak boundaries and reminder dispatch behaviour.

## Decision

GHATD will keep persisted event timestamps and due timestamps in UTC, but it
will derive user-facing time concepts from IANA timezone names.

Streaker accepts an optional `period_timezone` on `RecordStreakRequest`. Empty
timezone values resolve to `UTC`; invalid non-empty values return a typed
validation error. Streaker derives daily, weekly, and monthly `period_key`
values after converting `occurred_at` into the effective timezone. The effective
timezone is stored on each streak entry as `period_timezone` for auditability.
`BuildPeriodKey` keeps its UTC-compatible behaviour, and
`BuildPeriodKeyForTimezone` provides the timezone-aware helper for callers.

`period_timezone` is not part of the streak uniqueness contract. Idempotency
continues to use owner, streak type, target, period type, and period key. This
keeps retries from different clients safe and prevents duplicate rows when a
host application resolves the same local period through different request
paths.

Reminder treats `target_time` as the local wall-clock time for recurring
reminder declarations. The initial supported wall-clock format is `HH:MM`, with
legacy absolute timestamp parsing retained for compatibility. Reminder
normalises the IANA `timezone`, computes `next_due_at` in UTC, and uses
`next_due_at` for due scheduler lookups. Reminder execution records continue to
store the attempted UTC scheduled time in `scheduled_for`.

GHATD packages embed Go timezone data so consuming applications running in
minimal containers can resolve IANA timezones without relying on OS tzdata.

Host applications remain responsible for choosing the effective timezone source
of truth. The preferred order is persisted user preference, request/client
timezone, then `UTC`.

## Consequences

Users get streak boundaries and reminder due times that match their local day
and local wall-clock expectations.

Host applications can use shared GHATD helpers instead of writing their own
timezone conversion code around Streaker and Reminder.

Existing callers remain compatible. Missing timezone input preserves UTC
behaviour, existing `BuildPeriodKey` calls stay UTC-based, and legacy reminder
absolute target times can still be parsed.

Reminder scheduler workers should poll by `next_due_at`. Existing reminder data
that lacks `next_due_at` should be backfilled before a production worker relies
exclusively on timezone-aware due polling.

Embedding timezone data slightly increases binary size, but it removes a common
deployment footgun for minimal images.
