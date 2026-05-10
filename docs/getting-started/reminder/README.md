# Reminder

The `reminder` package lets GHATD applications save scheduled tasks for users
and track the attempts made to process them. It pairs naturally with
`notifier`: reminders decide what is due, and notifier delivers the push
message.

## When To Use It

Use reminders when a product needs to ask a user to come back to a specific
task, target, or workflow at a chosen time.

Examples:

- A course app reminds a user to finish today's lesson.
- A habit app reminds a user to complete a daily check-in.
- A team tool reminds a user to review one task or checklist.
- An admin overview needs to see reminder volume by time and status.

## Data Model

The package uses two MongoDB collections.

| Collection | Purpose |
|---|---|
| `reminders` | Stores the reminder declaration: owner, target, title, due time, timezone, status, and task data. |
| `reminder_executions` | Stores processing history: scheduled time, sent/failed/skipped status, attempt number, notification reference, error, and metadata. |

This split keeps reminder configuration separate from delivery history. A user
can update or disable a reminder without losing the record of past attempts.

Each reminder can include:

- `user_id`: the owner of the reminder.
- `target_type`: the kind of thing the reminder relates to.
- `target_id`: the concrete target object, if applicable.
- `target_time`: the UTC due time.
- `timezone`: the user's timezone for local display and admin analysis.
- `status`: `active`, `disabled`, `completed`, or `deleted`.
- `task_data`: optional structured data for the host application.

## Basic Setup

```go
import (
    "github.com/ooaklee/ghatd/external/reminder"
    reminderMigrations "github.com/ooaklee/ghatd/external/reminder/migrations"
)

repo := reminder.NewRepository(mongoStore)
reminderService := reminder.NewService(repo)

if err := reminderMigrations.InitRemindersIndexesUp(db); err != nil {
    return err
}
```

## Creating a Reminder

```go
created, err := reminderService.CreateReminder(ctx, &reminder.CreateReminderRequest{
    UserID:      userID,
    TargetType:  "lesson",
    TargetId:    "lesson-123",
    Title:       "Finish your lesson",
    Description: "You planned to complete this today.",
    TargetTime:  "2026-05-15T18:30:00Z",
    Timezone:    "Europe/London",
    TaskData: map[string]interface{}{
        "url": "/lessons/lesson-123",
    },
})
if err != nil {
    return err
}

_ = created.Reminder.Id
```

Store `target_time` in UTC. Keep `timezone` as the user's local timezone so the
front end and admin tools can show the intended local time.

## Target-Based Lookups

Use target lookups when a screen wants to show reminders attached to the thing
the user is already looking at.

```go
reminders, err := reminderService.GetRemindersForTargetTypeByUserID(ctx, &reminder.GetRemindersForTargetTypeByUserIDRequest{
    UserID:     userID,
    TargetType: "lesson",
    TargetId:   "lesson-123",
    Page:       1,
    PerPage:    25,
})
```

For scheduling or active-only UI states:

```go
active, err := reminderService.GetActiveRemindersForTargetTypeByUserID(ctx, &reminder.GetActiveRemindersForTargetTypeByUserIDRequest{
    UserID:     userID,
    TargetType: "lesson",
    TargetId:   "lesson-123",
})
```

## UMS Endpoints

When `ReminderService` is wired into `usermanager.Service`, UMS exposes user
and admin endpoints.

Authenticated user endpoints:

| Endpoint | Purpose |
|---|---|
| `GET /api/v1/ums/me/reminders` | List the authenticated user's reminders. |
| `POST /api/v1/ums/me/reminders` | Create a reminder for the authenticated user. |
| `GET /api/v1/ums/me/reminders/{reminderID}` | Get one reminder owned by the user. |
| `PATCH /api/v1/ums/me/reminders/{reminderID}` | Update one reminder owned by the user. |
| `DELETE /api/v1/ums/me/reminders/{reminderID}` | Delete one reminder owned by the user. |
| `POST /api/v1/ums/me/reminders/{reminderID}/disable` | Disable one reminder owned by the user. |

Admin/service endpoints:

| Endpoint | Purpose |
|---|---|
| `GET /api/v1/ums/reminders` | List reminders across users, optionally filtered by `user_id`. |
| `GET /api/v1/ums/reminders/stats` | Get reminder totals for admin overview pages. Supports optional `user_id` and `user_ids`. |
| `GET /api/v1/ums/reminders/due` | Fetch reminders ready for scheduler processing. Supports optional `user_id`, `user_ids`, `due_before`, and `limit`; if neither `user_id` nor `user_ids` is provided, it returns due reminders for everyone. |

Both list routes call the same UMS service method. UMS checks the requesting
user. Non-admin users are locked to their own `UserID`; admins can omit
`user_id` for an all-user list or provide it to inspect one user.

Supported list query parameters:

- `user_id` for admin/service filtering.
- `status`
- `target_type`
- `target_id`
- `page`
- `per_page`

## Scheduler Flow

The reminder package does not own a long-running scheduler. A host application
or worker can poll and process due reminders. At the reminder package level,
`GetDueReminders` can be global, scoped to one user with `UserID`, or scoped to
many users with `UserIDs`.

```go
due, err := reminderService.GetDueReminders(ctx, &reminder.GetDueRemindersRequest{
    DueBefore: "2026-05-15T18:35:00Z",
    UserIDs:   []string{"user-123", "user-456"},
    Limit:     100,
})
if err != nil {
    return err
}

for _, item := range due.Reminders {
    // Send through notifier, email, or another delivery system.

    _, err = reminderService.RecordReminderExecution(ctx, &reminder.RecordReminderExecutionRequest{
        ReminderId:      item.Id,
        UserID:          item.UserID,
        TargetType:      item.TargetType,
        TargetId:        item.TargetId,
        ScheduledFor:    item.TargetTime,
        Status:          reminder.ReminderExecutionStatusSent,
        Attempt:         1,
        NotificationRef: "notification-id",
    })
    if err != nil {
        return err
    }
}
```

Use `ReminderExecutionStatusFailed` with the `Error` field when delivery fails.
That gives admin tooling a source of truth for retry and diagnostics without
mutating the original reminder declaration.
