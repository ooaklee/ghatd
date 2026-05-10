package reminder

// CreateReminderRequest contains the declaration fields needed to create a reminder.
type CreateReminderRequest struct {
	UserID      string
	TargetType  string                 `json:"target_type,omitempty"`
	TargetId    string                 `json:"target_id,omitempty"`
	Title       string                 `json:"title"`
	Description string                 `json:"description,omitempty"`
	TargetTime  string                 `json:"target_time"`
	Timezone    string                 `json:"timezone,omitempty"`
	Status      ReminderStatus         `json:"status,omitempty"`
	TaskData    map[string]interface{} `json:"task_data,omitempty"`
}

// GetReminderByIDRequest identifies a reminder declaration by ID.
type GetReminderByIDRequest struct {
	UserID string
	Id     string
}

// ListRemindersRequest filters reminder declarations for user and admin views.
type ListRemindersRequest struct {
	UserID     string
	Status     string `query:"status"`
	TargetType string `query:"target_type"`
	TargetId   string `query:"target_id"`
	Page       int    `query:"page"`
	PerPage    int    `query:"per_page"`
}

// UpdateReminderByIDRequest contains optional changes for one reminder declaration.
type UpdateReminderByIDRequest struct {
	UserID      string
	Id          string
	TargetType  *string                `json:"target_type,omitempty"`
	TargetId    *string                `json:"target_id,omitempty"`
	Title       *string                `json:"title,omitempty"`
	Description *string                `json:"description,omitempty"`
	TargetTime  *string                `json:"target_time,omitempty"`
	Timezone    *string                `json:"timezone,omitempty"`
	Status      *ReminderStatus        `json:"status,omitempty"`
	TaskData    map[string]interface{} `json:"task_data,omitempty"`
}

// DeleteReminderByIDRequest identifies a user-owned reminder to delete.
type DeleteReminderByIDRequest struct {
	UserID string
	Id     string
}

// DisableReminderByIDRequest identifies a user-owned reminder to disable.
type DisableReminderByIDRequest struct {
	UserID string
	Id     string
}

// GetDueRemindersRequest filters reminders that are ready for scheduler processing.
type GetDueRemindersRequest struct {
	DueBefore string         `query:"due_before"`
	Status    ReminderStatus `query:"status"`
	UserID    string         `query:"user_id"`
	UserIDs   []string       `query:"user_ids"`
	Limit     int64          `query:"limit"`
}

// GetReminderStatsRequest filters reminder stats calculations.
type GetReminderStatsRequest struct {
	UserID  string   `query:"user_id"`
	UserIDs []string `query:"user_ids"`
}

// GetRemindersForTargetTypeByUserIDRequest fetches a user's reminders for a platform target type.
type GetRemindersForTargetTypeByUserIDRequest struct {
	UserID     string
	TargetType string
	TargetId   string
	Page       int
	PerPage    int
}

// GetActiveRemindersForTargetTypeByUserIDRequest fetches active reminders for a user's target type.
type GetActiveRemindersForTargetTypeByUserIDRequest struct {
	UserID     string
	TargetType string
	TargetId   string
	Page       int
	PerPage    int
}

// RecordReminderExecutionRequest contains the outcome of one scheduler or notification attempt.
type RecordReminderExecutionRequest struct {
	ReminderId      string                  `json:"reminder_id"`
	UserID          string                  `json:"user_id"`
	TargetType      string                  `json:"target_type,omitempty"`
	TargetId        string                  `json:"target_id,omitempty"`
	ScheduledFor    string                  `json:"scheduled_for"`
	ExecutedAt      string                  `json:"executed_at,omitempty"`
	Status          ReminderExecutionStatus `json:"status,omitempty"`
	Attempt         int                     `json:"attempt,omitempty"`
	NotificationRef string                  `json:"notification_ref,omitempty"`
	Error           string                  `json:"error,omitempty"`
	Metadata        map[string]interface{}  `json:"metadata,omitempty"`
	CreatedByUserID string                  `json:"created_by_user_id,omitempty"`
}

// ListReminderExecutionsRequest filters reminder execution records.
type ListReminderExecutionsRequest struct {
	ReminderId string                  `query:"reminder_id"`
	UserID     string                  `query:"user_id"`
	TargetType string                  `query:"target_type"`
	TargetId   string                  `query:"target_id"`
	Status     ReminderExecutionStatus `query:"status"`
	Page       int                     `query:"page"`
	PerPage    int                     `query:"per_page"`
}
