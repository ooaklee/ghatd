package streaker

// RecordStreakRequest holds everything needed to create a streak entry.
type RecordStreakRequest struct {
	StreakName string `json:"streak_name,omitempty"`
	StreakType string `json:"streak_type" validate:"required"`
	OwnerId    string `json:"owner_id" validate:"required"`
	TargetType string `json:"target_type" validate:"required"`
	TargetId   string `json:"target_id" validate:"required"`

	PeriodType StreakPeriodType `json:"period_type,omitempty"`
	PeriodKey  string           `json:"period_key,omitempty"`
	// PeriodTimezone is the IANA timezone used to derive daily, weekly, and monthly period keys.
	PeriodTimezone string `json:"period_timezone,omitempty"`
	OccurredAt     string `json:"occurred_at,omitempty"`

	CreatedByUserId string                 `json:"created_by_user_id" validate:"required"`
	Metadata        map[string]interface{} `json:"metadata,omitempty"`
}

// CreateRawStreakRequest is used by seeders or tests that need to persist
// a precomputed streak entry.
type CreateRawStreakRequest struct {
	Streak *Streak
}

// StreakStatsRequest holds shared filters used for streak stats.
type StreakStatsRequest struct {
	StreakType string           `json:"streak_type,omitempty" query:"streak_type"`
	OwnerId    string           `json:"owner_id,omitempty" query:"owner_id"`
	TargetType string           `json:"target_type,omitempty" query:"target_type"`
	TargetId   string           `json:"target_id,omitempty" query:"target_id"`
	PeriodType StreakPeriodType `json:"period_type,omitempty" query:"period_type"`
}

// GetLatestStreakRequest holds filters used to retrieve the latest entry.
type GetLatestStreakRequest struct {
	StreakStatsRequest
	PeriodKey string
}

// GetLongestStreakRequest holds filters used to retrieve the longest streak.
type GetLongestStreakRequest struct {
	StreakStatsRequest
}

// GetCurrentCountRequest holds filters used to retrieve the current count.
type GetCurrentCountRequest struct {
	StreakStatsRequest
}

// GetNumberOfStreaksRequest holds filters used to count streak entries.
type GetNumberOfStreaksRequest struct {
	StreakStatsRequest
}

// ListStreaksRequest holds filters used to list streak entries for history
// and board views.
type ListStreaksRequest struct {
	StreakStatsRequest

	PeriodKey     string `json:"period_key,omitempty" query:"period_key"`
	PeriodKeyFrom string `json:"period_key_from,omitempty" query:"period_key_from"`
	PeriodKeyTo   string `json:"period_key_to,omitempty" query:"period_key_to"`

	OccurredAtFrom string `json:"occurred_at_from,omitempty" query:"occurred_at_from"`
	OccurredAtTo   string `json:"occurred_at_to,omitempty" query:"occurred_at_to"`

	Page    int    `json:"page,omitempty" query:"page"`
	PerPage int    `json:"per_page,omitempty" query:"per_page"`
	Sort    string `json:"sort,omitempty" query:"sort"`
}
