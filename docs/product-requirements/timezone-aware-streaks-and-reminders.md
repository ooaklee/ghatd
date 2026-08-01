# Timezone-Aware Streaks and Reminders PRD

## Status

Implemented. The requirements below remain the behavioural contract for the
Streaker and Reminder packages.

## Problem

Streaks and reminders are user-facing time concepts. A user who opens an app at
00:31 in `Europe/London` expects today's daily streak to be available even
though it may still be the previous day in UTC. A user who asks for reminders at
09:00 in their local timezone expects scheduler queries to become due at that
local wall-clock time, not at 09:00 UTC.

GHATD stored UTC timestamps consistently, which remains the right storage rule.
The missing layer at the time of this proposal was explicit timezone-aware
derivation for:

- streak period keys such as daily, weekly, and monthly keys;
- reminder next-due timestamps derived from local wall-clock target times;
- first-class developer ergonomics so host applications do not each reimplement
  subtly different timezone logic.

## Goals

- Let streak callers provide an optional IANA timezone such as
  `Europe/London`, `America/New_York`, or `Pacific/Auckland`.
- Keep all persisted event timestamps in UTC while deriving streak period keys
  in the effective timezone.
- Persist the effective timezone used for each streak entry for auditability and
  debugging.
- Let reminder callers provide local wall-clock target times with an IANA
  timezone and have GHATD compute a UTC `next_due_at`.
- Make reminder scheduler lookups use `next_due_at <= due_before` instead of
  comparing raw local target-time strings.
- Preserve backward compatibility: missing timezone defaults to `UTC`; existing
  `BuildPeriodKey` behavior remains UTC-based.
- Keep streak idempotency scoped to owner, streak type, target, period type, and
  period key. Timezone must not be added to the uniqueness contract.

## Non-Goals

- GHATD will not own a host application's user profile UI or preference storage.
- GHATD will not decide how notifications are dispatched. It will provide due
  reminders and execution records for host workers.
- GHATD will not add reminder recurrence presets. Presets remain host-level
  product decisions that expand into one or more reminder declarations.
- This PRD does not require a full queueing/locking worker. A host worker task
  can build on `next_due_at` and reminder execution records separately.

## Requirements

### Effective Timezone Resolution

- All timezone inputs must be IANA timezone names accepted by Go's
  `time.LoadLocation`.
- Empty timezone input resolves to `UTC`.
- Invalid non-empty timezone input must return a typed validation error.
- GHATD packages should embed Go timezone data so minimal containers do not
  depend on host OS tzdata availability.
- Host applications should resolve the effective timezone in this order:
  persisted user preference, request/client timezone, then `UTC`.

### Streaker

- Add `period_timezone` to `RecordStreakRequest`.
- Add `period_timezone` to `Streak`.
- Add a timezone-aware helper that derives period keys from a timestamp, period
  type, optional custom key, and effective timezone.
- Keep `BuildPeriodKey` as a UTC-compatible helper for existing callers.
- Daily keys remain `YYYY-MM-DD`, weekly keys remain ISO `YYYY-wWW`, and monthly
  keys remain `YYYY-MM`.
- Daily, weekly, and monthly keys must be derived after converting
  `occurred_at` to the effective timezone.
- Custom period keys still require a caller-supplied `period_key`.
- Duplicate detection must continue to use the existing scope plus period key
  so retrying from different clients remains idempotent.
- Existing stats and list filters remain period-key based. Host applications
  that show a local week should calculate local period-key boundaries with the
  same helper.

### Reminder

- Treat `target_time` as a local wall-clock time for recurring reminder
  declarations. The launch format is `HH:MM`.
- Keep `timezone` on the reminder declaration and normalise it to the effective
  IANA timezone.
- Add `next_due_at`, stored as a UTC timestamp string, to reminder declarations.
- On create, compute `next_due_at` from `target_time`, `timezone`, and the
  current time.
- On update, recompute `next_due_at` whenever `target_time` or `timezone`
  changes.
- Scheduler lookup must filter active reminders by `next_due_at <= due_before`
  and sort by `next_due_at`.
- Existing reminder execution records keep using `scheduled_for` as the UTC due
  timestamp that a worker attempted.
- A future worker can advance `next_due_at` after a successful or skipped send;
  that worker design should account for multi-worker claiming separately.

### Host Application Integration

- Browser clients should send the browser IANA timezone on app-facing requests,
  for example through an `X-Timezone` header and/or request payload.
- Host services should pass the resolved timezone to streak recording calls.
- Host services should calculate streak summaries using the same timezone that
  was used for recording the user's current local period.
- App-shell check-in endpoints should be safe to call repeatedly. GHATD
  idempotency must prevent double-counting within one local period.
- Reminder management surfaces should pass the selected timezone when creating
  reminder schedules.

## Acceptance Criteria

- A daily streak recorded at `2026-05-22T23:31:00Z` with
  `period_timezone=Europe/London` stores `period_key=2026-05-23`.
- The same timestamp with `period_timezone=America/New_York` stores
  `period_key=2026-05-22`.
- Repeating the same daily streak action in the same local period returns the
  existing entry rather than creating a duplicate.
- Consecutive daily streaks across daylight-saving transitions continue to
  increment.
- A reminder with `target_time=09:00` and `timezone=Europe/London` stores the
  next UTC due timestamp corresponding to the next 09:00 London occurrence.
- Due reminder queries use `next_due_at`, not `target_time`.
- Missing timezone inputs resolve to `UTC`.
- Invalid non-empty timezone inputs return a typed validation error.

## Rollout Notes

- Existing streak records do not need migration; absent `period_timezone` means
  the old UTC behavior.
- Existing reminder records should be backfilled with `timezone=UTC` and a
  computed `next_due_at` before scheduler workers rely exclusively on
  `next_due_at`.
- Host applications should update clients before relying on local-day summaries.
  If clients omit timezone, launch behavior remains UTC-based rather than
  failing requests.
