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
	OccurredAt string           `json:"occurred_at,omitempty"`

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
	StreakType string
	OwnerId    string
	TargetType string
	TargetId   string
	PeriodType StreakPeriodType
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
