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

### How the "Period" System Works (aka How to keep your streak alive)

Think of the `Period` fields like keeping a Snapchat or Duolingo streak alive. A streak isn't just "you clicked a button"—it's "you clicked a button *today*." The period fields tell the system what the rules are for your streak.

- **`PeriodType` (The Rule):** How often do you need to do the thing?
  - `daily`: You have to do it once a day.
  - `weekly`: You have to do it once a week.
  - `monthly`: You have to do it once a month.
  - `custom`: You make up the rules! (Like beating specific levels in a game).

- **`PeriodKey` (The Bucket):** Which exact day, week, or level did you just finish?
  - daily: `2026-05-08` (Today's bucket)
  - weekly: `2026-w19` (This week's bucket)
  - custom: `level-4`, `boss-fight-3` (A specific challenge bucket)

- **`OccurredAt` (The Exact Time):** The exact second you actually did the task. For daily, weekly, or monthly streaks, the code will look at this exact time and automatically figure out what your `PeriodKey` (Bucket) should be. 

Because of this system, you can build streaks for *anything*. A "daily app open" streak. A "weekly math homework" streak. A custom "completed a game world" streak. 

#### Examples of how it behaves:

**1. You can't double-dip (Anti-cheat)**
If you are on a daily streak, and you do the task 5 times on Tuesday, your streak only goes up by 1. The system sees you already filled the "Tuesday" bucket and ignores the extra attempts. Your streak count stays exactly the same.

**2. Keeping it alive (Consecutive)**
If you log in on Monday, your streak is 1. If you log in on Tuesday, your streak becomes 2! 

**3. Dropping the ball (Gaps)**
If you log in on Monday (streak = 1), skip Tuesday, and log in on Wednesday... oh no! Your streak resets back to 1. But don't worry, the system keeps a hidden memory of your past streak so you never lose your history.

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

Stats requests require `PeriodType`, because current, longest, and total counts are only meaningful inside a specific rhythm. For example, a user's daily app streak and weekly app streak can both be valid, but they answer different product questions.

```go
current, err := service.GetCurrentCountByStreakTypeAndUserID(ctx, &streaker.GetCurrentCountRequest{
    StreakStatsRequest: streaker.StreakStatsRequest{
        StreakType:  "app-streak",
        OwnerId:     userID,
        PeriodType:  streaker.StreakPeriodTypeDaily,
    },
})

longest, err := service.GetLongestStreakByStreakTypeAndUserID(ctx, &streaker.GetLongestStreakRequest{
    StreakStatsRequest: streaker.StreakStatsRequest{
        StreakType:  "app-streak",
        OwnerId:     userID,
        PeriodType:  streaker.StreakPeriodTypeDaily,
    },
})

total, err := service.GetNumberOfStreaksByStreakTypeAndUserID(ctx, &streaker.GetNumberOfStreaksRequest{
    StreakStatsRequest: streaker.StreakStatsRequest{
        StreakType:  "app-streak",
        OwnerId:     userID,
        PeriodType:  streaker.StreakPeriodTypeDaily,
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
