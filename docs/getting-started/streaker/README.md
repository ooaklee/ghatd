# Streaker

The `streaker` package tracks repeat completions for a user or owner against any target on the platform. It is intentionally generic: an app can record an "app-open" streak, an "affirmation-listened" streak, a "content-read" streak, or a future to-do completion streak without changing the package.

## Model

Each completion creates a `streaks` document for a scope:

- `streak_type`
- `owner_id`
- `target_type`
- `target_id`
- `period_type`
- `period_key`

The package computes `current_count` from the latest previous entry in the same scope. If the previous period is consecutive, the count increments. If there is a gap, the count resets to `1`. Each new entry stores a lightweight `previous` reference so consumers can trace how the count was calculated without extra queries.

Mongo indexes include a unique compound index across the scope and period fields. That makes `RecordStreak` idempotent for the same user, target, streak type, and period.

## Basic Usage

```go
repo := streaker.NewRepository(mongoStore)
service := streaker.NewService(repo)

recorded, err := service.RecordStreak(ctx, &streaker.RecordStreakRequest{
    StreakName:      "App Streak",
    StreakType:      "app-streak",
    OwnerId:         userID,
    TargetType:      "app",
    TargetId:        "platform",
    CreatedByUserId: userID,
})
if err != nil {
    return err
}

currentCount := recorded.Streak.CurrentCount
```

## Stats

```go
current, err := service.GetCurrentCountByStreakTypeAndUserID(ctx, &streaker.GetCurrentCountRequest{
    StreakStatsRequest: streaker.StreakStatsRequest{
        StreakType: "app-streak",
        OwnerId:    userID,
    },
})

longest, err := service.GetLongestStreakByStreakTypeAndUserID(ctx, &streaker.GetLongestStreakRequest{
    StreakStatsRequest: streaker.StreakStatsRequest{
        StreakType: "app-streak",
        OwnerId:    userID,
    },
})

total, err := service.GetNumberOfStreaksByStreakTypeAndUserID(ctx, &streaker.GetNumberOfStreaksRequest{
    StreakStatsRequest: streaker.StreakStatsRequest{
        StreakType: "app-streak",
        OwnerId:    userID,
    },
})
```

Add `TargetType` and `TargetId` to the stats request when the screen needs the count for a specific thing, such as one course, one affirmation pack, or one task list.

## Migrations

Use the package migration helpers in the consuming application's mongo migrations:

```go
streakerMigrations.InitStreaksIndexesUp(db)
streakerMigrations.InitStreaksIndexesDown(db)
```
