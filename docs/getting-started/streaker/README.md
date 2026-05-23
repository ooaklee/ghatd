# Streaker

`external/streaker` records repeat completions for a user or owner against a
stable target. It is intentionally generic: the package does not know whether
a completion means an app open, lesson completion, saved item, reading play, or
task check-off.

Use Streaker when a host application needs idempotent "completed this period"
tracking plus current count, best count, and history.

## Core Model

Each streak entry is unique per:

- `streak_type`
- `owner_id`
- `target_type`
- `target_id`
- `period_type`
- `period_key`

Calling `RecordStreak` more than once for the same scope and period returns the
existing entry. It does not increment the count again.

For `daily`, `weekly`, and `monthly` periods, Streaker derives `period_key`
from `occurred_at` in `period_timezone`. If no timezone is supplied, Streaker
uses `UTC` for backward compatibility. For `custom`, callers must provide
`period_key`.

`period_timezone` must be an IANA timezone such as `Europe/London` or
`America/New_York`. The effective timezone is stored on each entry for
debugging and auditability, but it is not part of the uniqueness contract.

## Setup

```go
import (
    "github.com/ooaklee/ghatd/external/streaker"
    streakerMigrations "github.com/ooaklee/ghatd/external/streaker/migrations"
)

repo := streaker.NewRepository(mongoStore)
streakService := streaker.NewService(repo)

if err := streakerMigrations.InitStreaksIndexesUp(db); err != nil {
    return err
}
```

With `external/starter/v0`, `NewRepositories` creates the streaker repository
when a core Mongo repository is supplied, and `NewServices` exposes
`Services.Streaker`. Streaker is optional: if it is omitted, the rest of the
starter stack can still run.

Starter attaches `Services.Streaker` to `Services.UserManager` when available,
which enables the UMS streak endpoints under `/api/v1/ums`.

## Recording Aggregate Actions

Use stable targets for aggregate daily actions. Put event-specific resource IDs
in metadata.

```go
res, err := streakService.RecordStreak(ctx, &streaker.RecordStreakRequest{
    StreakName:      "Daily saved item",
    StreakType:      "saved-items",
    OwnerId:         userID,
    TargetType:      "item",
    TargetId:        "daily-save",
    PeriodType:      streaker.StreakPeriodTypeDaily,
    PeriodTimezone:  "Europe/London",
    CreatedByUserId: userID,
    Metadata: map[string]interface{}{
        "item_id":  itemID,
        "platform": "web",
    },
})
if err != nil {
    return err
}

_ = res.Streak.CurrentCount
```

Avoid using a changing resource ID as `target_id` when the product goal is
"complete this once per period across any resource." A changing `target_id`
creates one streak scope per resource.

Call `RecordStreak` after the host application has accepted the user action as
valid. Streaker does not know whether a play, save, check-in, or task
completion should count; it only records the stable streak scope it receives.

## Consecutive Behaviour

When the latest previous entry is in the immediately preceding period,
`current_count` increments. If there is a gap, the new entry resets to `1`.

Examples for a daily streak:

| Action | Result |
|--------|--------|
| Record Monday | `current_count = 1` |
| Record Tuesday | `current_count = 2` |
| Record Tuesday again | existing Tuesday entry is returned |
| Skip Wednesday, record Thursday | `current_count = 1` |

The package stores a lightweight `previous` reference on new entries so callers
can inspect how a count was calculated without an extra query.

## Reading Stats

```go
current, err := streakService.GetCurrentCount(ctx, &streaker.GetCurrentCountRequest{
    StreakStatsRequest: streaker.StreakStatsRequest{
        StreakType: "saved-items",
        OwnerId:    userID,
        TargetType: "item",
        TargetId:   "daily-save",
        PeriodType: streaker.StreakPeriodTypeDaily,
    },
})

best, err := streakService.GetLongestStreak(ctx, &streaker.GetLongestStreakRequest{
    StreakStatsRequest: streaker.StreakStatsRequest{
        StreakType: "saved-items",
        OwnerId:    userID,
        TargetType: "item",
        TargetId:   "daily-save",
        PeriodType: streaker.StreakPeriodTypeDaily,
    },
})
```

`GetCurrentCount` returns the latest recorded count for the scope. If a host
application needs "active today" semantics, compare the response period with
the current local period. Use `BuildPeriodKeyForTimezone` when deriving local
summary windows.

## Listing History

Use `ListStreaks` for streak boards, calendars, and history views.

```go
history, err := streakService.ListStreaks(ctx, &streaker.ListStreaksRequest{
    StreakStatsRequest: streaker.StreakStatsRequest{
        StreakType: "saved-items",
        OwnerId:    userID,
        TargetType: "item",
        TargetId:   "daily-save",
        PeriodType: streaker.StreakPeriodTypeDaily,
    },
    PeriodKeyFrom: "2026-05-16",
    PeriodKeyTo:   "2026-05-22",
    Sort:          "asc",
    PerPage:       50,
})
```

For generated daily, weekly, and monthly keys, period-key ranges sort
lexicographically because the keys are stable and zero-padded.

## UMS Endpoints

When Streaker is attached to User Manager, authenticated users can access:

| Endpoint | Purpose |
|----------|---------|
| `GET /api/v1/ums/me/streaks` | List the authenticated user's streak history. |
| `POST /api/v1/ums/me/streaks/record` | Record a streak for the authenticated user. |
| `GET /api/v1/ums/me/streaks/current` | Get the authenticated user's current count. |
| `GET /api/v1/ums/me/streaks/longest` | Get the authenticated user's best count. |
| `GET /api/v1/ums/me/streaks/count` | Count streak entries for the authenticated user. |

Admin/service read endpoints are also available:

| Endpoint | Purpose |
|----------|---------|
| `GET /api/v1/ums/streaks` | List streak history, optionally filtered by `user_id`. |
| `GET /api/v1/ums/streaks/current` | Read current count for a target user and scope. |
| `GET /api/v1/ums/streaks/longest` | Read best count for a target user and scope. |
| `GET /api/v1/ums/streaks/count` | Count entries for a target user and scope. |

The `/me` routes always scope `owner_id` to the authenticated requester.
Admin/service routes may use `user_id` to inspect one target user after the
caller has passed User Manager's access checks.

## Optional Integration Pattern

For host application side effects:

1. Log before attempting to record a streak.
2. Skip if the service is not configured.
3. Warn and continue if recording fails.

For direct user-facing streak APIs, return a clear service-unavailable or
skipped response when Streaker is not attached. That keeps optional
integration behaviour visible without breaking unrelated application features.

## Package README

See [`external/streaker/README.md`](../../../external/streaker/README.md) for
the package-level reference.
