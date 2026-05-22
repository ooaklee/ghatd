# Reminder Package

The `external/reminder` package stores user reminder declarations and tracks
scheduler or notification execution attempts. It is intentionally generic: a
reminder can point at any platform target by using `target_type` and
`target_id`, such as a course, task, checklist, lesson, or onboarding action.

## Quick Start

Think of a reminder as two related records:

1. **Declaration**: what the user asked to be reminded about.
2. **Execution**: what happened when a scheduler or worker tried to process it.

Declarations live in the `reminders` collection. Execution attempts live in the
`reminder_executions` collection. Keeping them separate means the current
reminder stays small and easy to update, while the system can still keep a
history of sends, failures, skips, and notification references.

All stored timestamps should be UTC. The optional `timezone` field records the
user's local timezone so clients and admin tools can display the intended local
time without changing the persisted UTC value.

## Package Structure

```text
reminder/
|-- const.go          # Status values, collection names, and error keys
|-- model.go          # Reminder and ReminderExecution data models
|-- service.go        # Business logic and validation
|-- repository.go     # MongoDB persistence
|-- request.go        # API request types
|-- response.go       # API response types
|-- errors.go         # Sentinel errors
|-- errormap.go       # HTTP error code mapping
|-- service_test.go   # Service tests with fakes
|-- repository_test.go
`-- migrations/
    `-- indexes_reminders.go
```

## Model

`Reminder` is the declaration. Its most important fields are:

| Field | Purpose |
|---|---|
| `user_id` | The user who owns the reminder. |
| `target_type` | The kind of platform object the reminder belongs to. |
| `target_id` | The specific platform object ID, when there is one. |
| `title` | The short reminder label. |
| `description` | Optional user-facing detail. |
| `target_time` | The UTC time the reminder is due. |
| `timezone` | Optional user timezone for display and local-time reasoning. |
| `status` | `active`, `disabled`, `completed`, or `deleted`. |
| `task_data` | Optional structured payload for product-specific context. |

`ReminderExecution` records one scheduler or notification attempt. It stores
the reminder ID, user ID, target scope, scheduled time, execution status,
attempt number, optional notification reference, error text, and metadata.

## Basic Usage

```go
repo := reminder.NewRepository(mongoStore)
service := reminder.NewService(repo)

created, err := service.CreateReminder(ctx, &reminder.CreateReminderRequest{
    UserID:      userID,
    TargetType:  "course",
    TargetId:    "course-123",
    Title:       "Finish today's lesson",
    Description: "Keep your streak moving.",
    TargetTime:  "2026-05-15T10:00:00Z",
    Timezone:    "Europe/London",
})
if err != nil {
    return err
}

active, err := service.GetActiveRemindersForTargetTypeByUserID(ctx, &reminder.GetActiveRemindersForTargetTypeByUserIDRequest{
    UserID:     userID,
    TargetType: "course",
    TargetId:   "course-123",
})
```

When using `external/starter/v0`, `starter.NewRepositories` creates the
reminder repository and `starter.NewServices` creates `Services.Reminder`.
Starter also attaches that service to `Services.UserManager` by default, so
the UMS reminder endpoints are enabled when the User Manager route group is
attached. Pass `starter.NewServicesRequest.ReminderService` only when UMS
should use a custom reminder implementation.

Use `GetRemindersForTargetTypeByUserID` when a product screen needs all
reminders for a target. Use `GetActiveRemindersForTargetTypeByUserID` when a
scheduler or UI only needs enabled reminders.

## Listing and Admin Scope

The package exposes one `ListReminders` method for both user and admin views.
Passing `UserID` filters the list to one user. Leaving `UserID` empty returns
all matching reminders, which should only be done by trusted callers such as
User Manager after it has verified the requester is an admin.

Supported list filters:

- `user_id`
- `status`
- `target_type`
- `target_id`
- `page`
- `per_page`

## Execution Tracking

A scheduler can query due reminders, send notifications through another
service, then record the result. By default, due lookup is global. Pass
`UserID` to retrieve one user's due reminders, or `UserIDs` to retrieve due
reminders for a set of users.

```go
due, err := service.GetDueReminders(ctx, &reminder.GetDueRemindersRequest{
    DueBefore: "2026-05-15T10:05:00Z",
    UserIDs:   []string{"user-123", "user-456"},
    Limit:     100,
})
if err != nil {
    return err
}

for _, item := range due.Reminders {
    _, err = service.RecordReminderExecution(ctx, &reminder.RecordReminderExecutionRequest{
        ReminderId:      item.Id,
        UserID:          item.UserID,
        TargetType:      item.TargetType,
        TargetId:        item.TargetId,
        ScheduledFor:    item.TargetTime,
        Status:          reminder.ReminderExecutionStatusSent,
        Attempt:         1,
        NotificationRef: "notifier-message-id",
    })
    if err != nil {
        return err
    }
}
```

The reminder package does not run the scheduler loop itself. Host applications
or UMS integrations can decide how often to poll due reminders and which
notification provider to call.

## User Manager Integration

When wired into UMS, reminders are exposed under `/api/v1/ums`:

| Endpoint | Purpose |
|---|---|
| `GET /me/reminders` | List reminders for the authenticated user. |
| `POST /me/reminders` | Create a reminder for the authenticated user. |
| `GET /me/reminders/{reminderID}` | Get one owned reminder. |
| `PATCH /me/reminders/{reminderID}` | Update one owned reminder. |
| `DELETE /me/reminders/{reminderID}` | Delete one owned reminder. |
| `POST /me/reminders/{reminderID}/disable` | Disable one owned reminder. |
| `GET /reminders` | Admin/service list endpoint with optional `user_id`. |
| `GET /reminders/stats` | Admin/service aggregate stats. Supports optional `user_id` and `user_ids`. |
| `GET /reminders/due` | Due reminder lookup for schedulers. Supports optional `user_id`, `user_ids`, `due_before`, and `limit`; if neither `user_id` nor `user_ids` is provided, returns due reminders for everyone. |

UMS uses a single list service method for both `/me/reminders` and
`/reminders`. If the requesting user is not an admin, UMS locks the filter to
their own user ID. Admin users may omit `user_id` to list across users or pass
`user_id` to inspect one user's reminders.

## Migrations

Use the package migration helpers from the consuming application's Mongo
migrations:

```go
reminderMigrations.InitRemindersIndexesUp(db)
reminderMigrations.InitRemindersIndexesDown(db)
```

The indexes cover user/status lists, user target lookups, due reminder polling,
created-at sorting, and execution history lookups.

## Error Codes

| Code | Meaning | HTTP |
|---|---|---|
| REM0-001 | User ID is required | 400 |
| REM0-002 | Title is required | 400 |
| REM0-003 | Target time is required | 400 |
| REM0-004 | Target time is invalid | 400 |
| REM0-005 | Reminder status is invalid | 400 |
| REM0-006 | Reminder or execution was not found | 404 |
| REM0-007 | Database operation failed | 500 |
| REM0-008 | Reminder ID is required | 400 |
| REM0-009 | Reminder nano ID is required | 400 |
| REM0-010 | User cannot access the reminder | 403 |
| REM0-011 | Reminder status transition is invalid | 400 |
| REM0-012 | Pagination parameter is invalid | 400 |
| REM0-013 | Target type is required | 400 |
| REM0-014 | Execution status is invalid | 400 |
