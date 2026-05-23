package reminder

import "errors"

// Sentinel errors for the reminder package.
//
// These values describe the specific failure modes that reminder services,
// repositories, and API adapters can return. Callers can use errors.Is()
// against them, and ReminderErrorMap turns them into consistent HTTP
// responses at the handler layer.
var (
	// ErrUserIDIsRequired means a request that must be scoped to a user did not include one.
	ErrUserIDIsRequired = errors.New(ErrKeyUserIDIsRequired)
	// ErrTargetTypeIsRequired means a target-type lookup did not include a target type.
	ErrTargetTypeIsRequired = errors.New(ErrKeyTargetTypeIsRequired)
	// ErrTitleIsRequired means a reminder declaration did not include a title.
	ErrTitleIsRequired = errors.New(ErrKeyTitleIsRequired)
	// ErrTargetTimeIsRequired means a reminder or execution did not include its scheduled time.
	ErrTargetTimeIsRequired = errors.New(ErrKeyTargetTimeIsRequired)
	// ErrInvalidTargetTime means a reminder target time could not be accepted.
	ErrInvalidTargetTime = errors.New(ErrKeyInvalidTargetTime)
	// ErrInvalidTimezone means a reminder timezone could not be accepted.
	ErrInvalidTimezone = errors.New(ErrKeyInvalidTimezone)
	// ErrInvalidStatus means the reminder status is not supported.
	ErrInvalidStatus = errors.New(ErrKeyInvalidStatus)
	// ErrResourceNotFound means the requested reminder or execution record could not be found.
	ErrResourceNotFound = errors.New(ErrKeyResourceNotFound)
	// ErrDatabaseError means persistence failed unexpectedly.
	ErrDatabaseError = errors.New(ErrKeyDatabaseError)
	// ErrIdIsRequired means an ID-scoped operation did not include a reminder ID.
	ErrIdIsRequired = errors.New(ErrKeyIdIsRequired)
	// ErrNanoIdIsRequired means a raw reminder record did not include a nano ID.
	ErrNanoIdIsRequired = errors.New(ErrKeyNanoIdIsRequired)
	// ErrNotAuthorized means the requesting user does not own the reminder.
	ErrNotAuthorized = errors.New(ErrKeyNotAuthorized)
	// ErrInvalidReminderStatus means the requested reminder status transition is not allowed.
	ErrInvalidReminderStatus = errors.New(ErrKeyInvalidReminderStatus)
	// ErrInvalidExecutionStatus means the reminder execution status is not supported.
	ErrInvalidExecutionStatus = errors.New(ErrKeyInvalidExecutionStatus)
	// ErrInvalidPaginationParameter means pagination input could not be accepted.
	ErrInvalidPaginationParameter = errors.New(ErrKeyInvalidPaginationParameter)
)
