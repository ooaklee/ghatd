package reminder

import (
	"strings"
	"time"
	_ "time/tzdata"

	"github.com/ooaklee/ghatd/external/common"
	"github.com/ooaklee/ghatd/external/toolbox"
)

// defaultReminderTimezone defines the fallback timezone for reminders when none is provided or determined.
const defaultReminderTimezone = "UTC"

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
	NextDueAt       string                 `json:"next_due_at,omitempty" bson:"next_due_at,omitempty"`
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

// ParseReminderTime validates that a reminder target time value is present and supported.
func ParseReminderTime(value string) (bool, error) {
	_, err := parseReminderTargetTime(strings.TrimSpace(value))
	return err == nil, err
}

// FormatTime returns the provided time value or the current UTC timestamp when empty.
func FormatTime(value string) string {
	if value == "" {
		return toolbox.TimeNowUTC()
	}
	return value
}

// NormaliseReminderTimezone returns the package default timezone when none is provided.
func NormaliseReminderTimezone(value string) (string, *time.Location, error) {
	timezone := strings.TrimSpace(value)
	if timezone == "" {
		timezone = defaultReminderTimezone
	}

	location, err := time.LoadLocation(timezone)
	if err != nil {
		return "", nil, ErrInvalidTimezone
	}

	return timezone, location, nil
}

// BuildNextDueAt calculates the next UTC due timestamp for a reminder target time.
func BuildNextDueAt(targetTime string, timezone string, now time.Time) (string, error) {
	parsed, err := parseReminderTargetTime(strings.TrimSpace(targetTime))
	if err != nil {
		return "", err
	}

	if !parsed.localWallClock {
		return parsed.absoluteTime.UTC().Format(common.RFC3339NanoUTC), nil
	}

	_, location, err := NormaliseReminderTimezone(timezone)
	if err != nil {
		return "", err
	}

	nowUTC := now.UTC()
	localNow := nowUTC.In(location)
	nextLocal := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), parsed.hour, parsed.minute, parsed.second, 0, location)
	if !nextLocal.After(localNow) {
		nextLocal = nextLocal.AddDate(0, 0, 1)
	}

	return nextLocal.UTC().Format(common.RFC3339NanoUTC), nil
}

type parsedReminderTargetTime struct {
	localWallClock bool
	hour           int
	minute         int
	second         int
	absoluteTime   time.Time
}

func parseReminderTargetTime(value string) (parsedReminderTargetTime, error) {
	if value == "" {
		return parsedReminderTargetTime{}, ErrTargetTimeIsRequired
	}

	for _, layout := range []string{"15:04", "15:04:05"} {
		parsed, err := time.Parse(layout, value)
		if err == nil {
			return parsedReminderTargetTime{
				localWallClock: true,
				hour:           parsed.Hour(),
				minute:         parsed.Minute(),
				second:         parsed.Second(),
			}, nil
		}
	}

	for _, layout := range []string{common.RFC3339NanoUTC, time.RFC3339Nano, time.RFC3339} {
		parsed, err := time.Parse(layout, value)
		if err == nil {
			return parsedReminderTargetTime{absoluteTime: parsed.UTC()}, nil
		}
	}

	return parsedReminderTargetTime{}, ErrInvalidTargetTime
}
