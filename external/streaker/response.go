package streaker

// RecordStreakResponse returns the created streak entry.
type RecordStreakResponse struct {
	Streak *Streak `json:"streak"`
}

// GetLongestStreakResponse returns the entry that produced the highest count.
type GetLongestStreakResponse struct {
	Streak       *Streak `json:"streak,omitempty"`
	LongestCount int     `json:"longest_count"`
}

// GetCurrentCountResponse returns the current count for a streak scope.
type GetCurrentCountResponse struct {
	Streak       *Streak `json:"streak,omitempty"`
	CurrentCount int     `json:"current_count"`
}

// GetNumberOfStreaksResponse returns the number of entries matching a streak scope.
type GetNumberOfStreaksResponse struct {
	Total int64 `json:"total"`
}
