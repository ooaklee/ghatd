// Package reminder manages user reminder declarations and execution tracking.
package reminder

// ReminderStatus represents the status of a reminder.
type ReminderStatus string

// ReminderExecutionStatus represents the status of one reminder delivery attempt.
type ReminderExecutionStatus string

const (
	// ReminderStatusActive means the reminder is enabled and eligible for scheduling.
	ReminderStatusActive ReminderStatus = "active"
	// ReminderStatusDisabled means the reminder is retained but excluded from scheduling.
	ReminderStatusDisabled ReminderStatus = "disabled"
	// ReminderStatusCompleted means the reminder has been fulfilled.
	ReminderStatusCompleted ReminderStatus = "completed"
	// ReminderStatusDeleted means the reminder has been removed from active use.
	ReminderStatusDeleted ReminderStatus = "deleted"
)

const (
	// ReminderExecutionStatusPending means an execution record has been created but not attempted.
	ReminderExecutionStatusPending ReminderExecutionStatus = "pending"
	// ReminderExecutionStatusProcessing means a scheduler or worker has started the attempt.
	ReminderExecutionStatusProcessing ReminderExecutionStatus = "processing"
	// ReminderExecutionStatusSent means the notification attempt was delivered or handed to the sender.
	ReminderExecutionStatusSent ReminderExecutionStatus = "sent"
	// ReminderExecutionStatusFailed means the attempt failed and the error should be inspected.
	ReminderExecutionStatusFailed ReminderExecutionStatus = "failed"
	// ReminderExecutionStatusSkipped means the attempt was intentionally not sent.
	ReminderExecutionStatusSkipped ReminderExecutionStatus = "skipped"
)

const (
	// ReminderCollection is the mongo collection name for reminder entries.
	ReminderCollection string = "reminders"

	// ReminderExecutionsCollection is the mongo collection name for reminder execution tracking.
	ReminderExecutionsCollection string = "reminder_executions"
)

const (
	// ErrKeyUserIDIsRequired is returned when a reminder operation has no user ID.
	ErrKeyUserIDIsRequired = "ReminderUserIDIsRequired"
	// ErrKeyTargetTypeIsRequired is returned when a target-type lookup is missing its target type.
	ErrKeyTargetTypeIsRequired = "ReminderTargetTypeIsRequired"
	// ErrKeyTitleIsRequired is returned when a reminder declaration has no title.
	ErrKeyTitleIsRequired = "ReminderTitleIsRequired"
	// ErrKeyTargetTimeIsRequired is returned when a reminder has no scheduled UTC target time.
	ErrKeyTargetTimeIsRequired = "ReminderTargetTimeIsRequired"
	// ErrKeyInvalidTargetTime is returned when a reminder target time cannot be accepted.
	ErrKeyInvalidTargetTime = "ReminderInvalidTargetTime"
	// ErrKeyInvalidStatus is returned when a reminder status is not supported.
	ErrKeyInvalidStatus = "ReminderInvalidStatus"
	// ErrKeyResourceNotFound is returned when a reminder or execution record cannot be found.
	ErrKeyResourceNotFound = "ReminderResourceNotFound"
	// ErrKeyDatabaseError is returned when persistence fails unexpectedly.
	ErrKeyDatabaseError = "ReminderDatabaseError"
	// ErrKeyIdIsRequired is returned when an ID-scoped operation has no reminder ID.
	ErrKeyIdIsRequired = "ReminderIdIsRequired"
	// ErrKeyNanoIdIsRequired is returned when a raw reminder record has no nano ID.
	ErrKeyNanoIdIsRequired = "ReminderNanoIdIsRequired"
	// ErrKeyNotAuthorized is returned when a user tries to access another user's reminder.
	ErrKeyNotAuthorized = "ReminderNotAuthorized"
	// ErrKeyInvalidReminderStatus is returned when a reminder status transition is not allowed.
	ErrKeyInvalidReminderStatus = "ReminderInvalidStatusTransition"
	// ErrKeyInvalidExecutionStatus is returned when an execution status is not supported.
	ErrKeyInvalidExecutionStatus = "ReminderInvalidExecutionStatus"
	// ErrKeyInvalidPaginationParameter is returned when pagination parameters are invalid.
	ErrKeyInvalidPaginationParameter = "ReminderInvalidPaginationParameter"
)
