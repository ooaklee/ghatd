# GHATD Streaker

`external/streaker` records idempotent streak completions for host applications.
It is intentionally generic: the package does not know what an app check-in,
lesson completion, saved item, or playback means. Host applications define those
domain events and pass a stable streak scope into Streaker.

## Core Concepts

A streak entry is unique per:

- `streak_type`
- `owner_id`
- `target_type`
- `target_id`
- `period_type`
- `period_key`

Calling `RecordStreak` more than once for the same scope and period returns the
existing entry instead of incrementing the count again. When the previous entry
is in the immediately preceding period, the new entry increments
`current_count`; otherwise the count resets to `1`.

Supported period types are:

- `daily`
- `weekly`
- `monthly`
- `custom`

For `daily`, `weekly`, and `monthly`, Streaker derives `period_key` from
`occurred_at` in `period_timezone`. If `period_timezone` is omitted, Streaker
uses `UTC` for backward compatibility. For `custom`, callers must provide
`period_key`.

`period_timezone` must be an IANA timezone such as `Europe/London` or
`America/New_York`. Streaker stores the effective timezone on each entry so host
applications can audit which local boundary created the period key.

## Service Setup

```go
repo := streaker.NewRepository(coreRepository)
svc := streaker.NewService(repo)
```

When using `external/starter/v0`, the starter creates `Services.Streaker` when
`Repositories.Streaker` is configured. Streaker is optional: host applications
may omit the repository/service and keep the rest of GHATD running.

```go
services, err := starter.NewServices(&starter.NewServicesRequest{
    Repositories: repos,
    // other required starter fields...
})
if err != nil {
    return err
}

if services.Streaker != nil {
    // Attach to your host application manager or call it directly.
}
```

Starter also attaches Streaker to User Manager when available, enabling the UMS
streak endpoints.

## Recording A Streak

Use stable target IDs for aggregate action streaks. Put event-specific resource
IDs in `metadata`.

```go
res, err := svc.RecordStreak(ctx, &streaker.RecordStreakRequest{
    StreakName:      "Daily saved item",
    StreakType:      "saved-items",
    OwnerId:         "<user-id>",
    TargetType:      "item",
    TargetId:        "daily-save",
    PeriodType:      streaker.StreakPeriodTypeDaily,
    PeriodTimezone:  "Europe/London",
    CreatedByUserId: "<user-id>",
    Metadata: map[string]interface{}{
        "item_id":  "<item-id>",
        "platform": "web",
    },
})
```

Avoid using a changing resource ID as `target_id` when the product goal is
"do this once per day across any resource." A changing `target_id` creates a
separate counter per resource.

Host applications should call `RecordStreak` only after they have decided that
the user action is valid. For example, an app can record an explicit play-button
intent, save action, lesson completion, or check-in, but Streaker does not
validate playback readiness, content ownership, or product workflow rules.

## Reading Stats

```go
current, err := svc.GetCurrentCount(ctx, &streaker.GetCurrentCountRequest{
    StreakStatsRequest: streaker.StreakStatsRequest{
        StreakType: "saved-items",
        OwnerId:    "<user-id>",
        TargetType: "item",
        TargetId:   "daily-save",
        PeriodType: streaker.StreakPeriodTypeDaily,
    },
})

best, err := svc.GetLongestStreak(ctx, &streaker.GetLongestStreakRequest{
    StreakStatsRequest: streaker.StreakStatsRequest{
        StreakType: "saved-items",
        OwnerId:    "<user-id>",
        TargetType: "item",
        TargetId:   "daily-save",
        PeriodType: streaker.StreakPeriodTypeDaily,
    },
})
```

`GetCurrentCount` returns the latest recorded count for the scope. If a host
application needs "active today" semantics, compare the returned `period_key`
with the current local period and the immediately preceding period. Use
`BuildPeriodKeyForTimezone` to derive local period keys consistently with
recording.

## Listing History

Use `ListStreaks` for streak boards, calendars, and history views.

```go
history, err := svc.ListStreaks(ctx, &streaker.ListStreaksRequest{
    StreakStatsRequest: streaker.StreakStatsRequest{
        StreakType: "saved-items",
        OwnerId:    "<user-id>",
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

For `daily`, `weekly`, and `monthly` periods, period-key ranges are
lexicographically sortable because the generated keys are zero-padded.

## User Manager Endpoints

When Streaker is attached to User Manager, authenticated users can access:

- `GET /api/v1/ums/me/streaks`
- `POST /api/v1/ums/me/streaks/record`
- `GET /api/v1/ums/me/streaks/current`
- `GET /api/v1/ums/me/streaks/longest`
- `GET /api/v1/ums/me/streaks/count`

Admin/service routes are also available for read operations:

- `GET /api/v1/ums/streaks`
- `GET /api/v1/ums/streaks/current`
- `GET /api/v1/ums/streaks/longest`
- `GET /api/v1/ums/streaks/count`

The `/me` routes always scope `owner_id` to the authenticated requester.
Admin/service routes may pass `user_id` as a query parameter when querying a
target user.

## Optional Integration Guidance

For host application side effects, treat Streaker as optional:

- log before attempting to record a streak
- skip if the service is not configured
- warn and continue if recording fails

For direct user-facing streak APIs, return a clear service-unavailable response
when Streaker is not attached.
