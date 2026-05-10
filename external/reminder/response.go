package reminder

// CreateReminderResponse returns the newly created reminder declaration.
type CreateReminderResponse struct {
	Reminder *Reminder `json:"reminder"`
}

// GetReminderByIDResponse returns one reminder declaration.
type GetReminderByIDResponse struct {
	Reminder *Reminder `json:"reminder"`
}

// ListRemindersResponse returns reminder declarations plus optional pagination metadata.
type ListRemindersResponse struct {
	Reminders []*Reminder            `json:"reminders"`
	Meta      map[string]interface{} `json:"-"`
}

// UpdateReminderByIDResponse returns the reminder declaration after an update.
type UpdateReminderByIDResponse struct {
	Reminder *Reminder `json:"reminder"`
}

// ReminderStats summarises reminder counts for admin overview surfaces.
type ReminderStats struct {
	TotalReminders  int64 `json:"total_reminders"`
	ActiveReminders int64 `json:"active_reminders"`
	DueReminders    int64 `json:"due_reminders"`
	CompletedCount  int64 `json:"completed_count"`
	DisabledCount   int64 `json:"disabled_count"`
}

// GetReminderStatsResponse returns aggregate reminder statistics.
type GetReminderStatsResponse struct {
	Stats *ReminderStats `json:"stats"`
}

// GetDueRemindersResponse returns reminders that should be considered for dispatch.
type GetDueRemindersResponse struct {
	Reminders []*Reminder `json:"reminders"`
}

// RecordReminderExecutionResponse returns the stored execution tracking record.
type RecordReminderExecutionResponse struct {
	Execution *ReminderExecution `json:"execution"`
}

// ListReminderExecutionsResponse returns execution tracking records plus optional pagination metadata.
type ListReminderExecutionsResponse struct {
	Executions []*ReminderExecution   `json:"executions"`
	Meta       map[string]interface{} `json:"meta,omitempty"`
}
