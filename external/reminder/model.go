package reminder

import (
	"strings"

	"github.com/ooaklee/ghatd/external/toolbox"
)

// Reminder represents a user reminder with scheduling and status information.
type Reminder struct {
	Id              string                 `json:"id" bson:"_id"`
	NanoId          string                 `json:"nano_id" bson:"_nano_id"`
	UserID          string                 `json:"user_id" bson:"user_id"`
	TargetType      string                 `json:"target_type,omitempty" bson:"target_type,omitempty"`
	TargetId        string                 `json:"target_id,omitempty" bson:"target_id,omitempty"`
	Title           string                 `json:"title" bson:"title"`
	Description     string                 `json:"description,omitempty" bson:"description,omitempty"`
	TargetTime      string                 `json:"target_time" bson:"target_time"`
	Timezone        string                 `json:"timezone,omitempty" bson:"timezone,omitempty"`
	Status          ReminderStatus         `json:"status" bson:"status"`
	TaskData        map[string]interface{} `json:"task_data,omitempty" bson:"task_data,omitempty"`
	CreatedAt       string                 `json:"created_at" bson:"created_at"`
	CreatedByUserID string                 `json:"created_by_user_id,omitempty" bson:"created_by_user_id,omitempty"`
	UpdatedAt       string                 `json:"updated_at,omitempty" bson:"updated_at,omitempty"`
	UpdatedByUserID string                 `json:"updated_by_user_id,omitempty" bson:"updated_by_user_id,omitempty"`
}

// ReminderExecution records one scheduler or notification attempt for a reminder.
type ReminderExecution struct {
	Id              string                  `json:"id" bson:"_id"`
	NanoId          string                  `json:"nano_id" bson:"_nano_id"`
	ReminderId      string                  `json:"reminder_id" bson:"reminder_id"`
	UserID          string                  `json:"user_id" bson:"user_id"`
	TargetType      string                  `json:"target_type,omitempty" bson:"target_type,omitempty"`
	TargetId        string                  `json:"target_id,omitempty" bson:"target_id,omitempty"`
	ScheduledFor    string                  `json:"scheduled_for" bson:"scheduled_for"`
	ExecutedAt      string                  `json:"executed_at,omitempty" bson:"executed_at,omitempty"`
	Status          ReminderExecutionStatus `json:"status" bson:"status"`
	Attempt         int                     `json:"attempt" bson:"attempt"`
	NotificationRef string                  `json:"notification_ref,omitempty" bson:"notification_ref,omitempty"`
	Error           string                  `json:"error,omitempty" bson:"error,omitempty"`
	Metadata        map[string]interface{}  `json:"metadata,omitempty" bson:"metadata,omitempty"`
	CreatedAt       string                  `json:"created_at" bson:"created_at"`
	CreatedByUserID string                  `json:"created_by_user_id,omitempty" bson:"created_by_user_id,omitempty"`
	UpdatedAt       string                  `json:"updated_at,omitempty" bson:"updated_at,omitempty"`
	UpdatedByUserID string                  `json:"updated_by_user_id,omitempty" bson:"updated_by_user_id,omitempty"`
}

// GenerateId assigns a platform UUID to the reminder declaration.
func (r *Reminder) GenerateId() *Reminder {
	r.Id = toolbox.GenerateUuidV4()
	return r
}

// GenerateNanoId assigns a short public identifier to the reminder declaration.
func (r *Reminder) GenerateNanoId() *Reminder {
	r.NanoId = toolbox.GenerateNanoId()
	return r
}

// SetCreatedAtTimeToNow stamps the reminder declaration with the current UTC time.
func (r *Reminder) SetCreatedAtTimeToNow() *Reminder {
	r.CreatedAt = toolbox.TimeNowUTC()
	return r
}

// SetUpdatedAtTimeToNow stamps the reminder declaration update time with the current UTC time.
func (r *Reminder) SetUpdatedAtTimeToNow() *Reminder {
	r.UpdatedAt = toolbox.TimeNowUTC()
	return r
}

// GenerateId assigns a platform UUID to the reminder execution record.
func (r *ReminderExecution) GenerateId() *ReminderExecution {
	r.Id = toolbox.GenerateUuidV4()
	return r
}

// GenerateNanoId assigns a short public identifier to the reminder execution record.
func (r *ReminderExecution) GenerateNanoId() *ReminderExecution {
	r.NanoId = toolbox.GenerateNanoId()
	return r
}

// SetCreatedAtTimeToNow stamps the execution record with the current UTC time.
func (r *ReminderExecution) SetCreatedAtTimeToNow() *ReminderExecution {
	r.CreatedAt = toolbox.TimeNowUTC()
	return r
}

// SetUpdatedAtTimeToNow stamps the execution record update time with the current UTC time.
func (r *ReminderExecution) SetUpdatedAtTimeToNow() *ReminderExecution {
	r.UpdatedAt = toolbox.TimeNowUTC()
	return r
}

// ValidateAndNormaliseStatus returns a supported reminder status or the active default.
func ValidateAndNormaliseStatus(status ReminderStatus) (ReminderStatus, error) {
	normalised := ReminderStatus(strings.TrimSpace(strings.ToLower(string(status))))
	switch normalised {
	case "", ReminderStatusActive:
		return ReminderStatusActive, nil
	case ReminderStatusDisabled:
		return ReminderStatusDisabled, nil
	case ReminderStatusCompleted:
		return ReminderStatusCompleted, nil
	case ReminderStatusDeleted:
		return ReminderStatusDeleted, nil
	default:
		return "", ErrInvalidStatus
	}
}

// ValidateAndNormaliseExecutionStatus returns a supported execution status or the pending default.
func ValidateAndNormaliseExecutionStatus(status ReminderExecutionStatus) (ReminderExecutionStatus, error) {
	normalised := ReminderExecutionStatus(strings.TrimSpace(strings.ToLower(string(status))))
	switch normalised {
	case "", ReminderExecutionStatusPending:
		return ReminderExecutionStatusPending, nil
	case ReminderExecutionStatusProcessing:
		return ReminderExecutionStatusProcessing, nil
	case ReminderExecutionStatusSent:
		return ReminderExecutionStatusSent, nil
	case ReminderExecutionStatusFailed:
		return ReminderExecutionStatusFailed, nil
	case ReminderExecutionStatusSkipped:
		return ReminderExecutionStatusSkipped, nil
	default:
		return "", ErrInvalidExecutionStatus
	}
}

// ParseReminderTime validates that a reminder target time value is present.
func ParseReminderTime(value string) (bool, error) {
	if strings.TrimSpace(value) == "" {
		return false, ErrTargetTimeIsRequired
	}
	return true, nil
}

// FormatTime returns the provided time value or the current UTC timestamp when empty.
func FormatTime(value string) string {
	if value == "" {
		return toolbox.TimeNowUTC()
	}
	return value
}
