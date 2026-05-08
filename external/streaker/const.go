package streaker

// StreakPeriodType controls how entries are grouped into streak periods.
type StreakPeriodType string

const (
	// StreakPeriodTypeDaily represents one streak period per UTC day.
	StreakPeriodTypeDaily StreakPeriodType = "daily"

	// StreakPeriodTypeWeekly represents one streak period per UTC ISO week.
	StreakPeriodTypeWeekly StreakPeriodType = "weekly"

	// StreakPeriodTypeMonthly represents one streak period per UTC month.
	StreakPeriodTypeMonthly StreakPeriodType = "monthly"

	// StreakPeriodTypeCustom allows callers to provide their own period key.
	StreakPeriodTypeCustom StreakPeriodType = "custom"
)

const (
	// StreakCollection is the mongo collection name for streak entries.
	StreakCollection string = "streaks"
)

const (
	// Error Keys
	ErrKeyStreakTypeIsRequired       = "StreakTypeIsRequired"
	ErrKeyOwnerIdIsRequired          = "StreakOwnerIdIsRequired"
	ErrKeyTargetIdIsRequired         = "StreakTargetIdIsRequired"
	ErrKeyTargetTypeIsRequired       = "StreakTargetTypeIsRequired"
	ErrKeyCreatedByUserIdIsRequired  = "StreakCreatedByUserIdIsRequired"
	ErrKeyInvalidPeriodType          = "StreakInvalidPeriodType"
	ErrKeyInvalidOccurredAt          = "StreakInvalidOccurredAt"
	ErrKeyPeriodKeyIsRequired        = "StreakPeriodKeyIsRequired"
	ErrKeyResourceNotFound           = "StreakResourceNotFound"
	ErrKeyResourceConflict           = "StreakResourceConflict"
	ErrKeyDatabaseError              = "StreakDatabaseError"
	ErrKeyIdIsRequired               = "StreakIdIsRequired"
	ErrKeyNanoIdIsRequired           = "StreakNanoIdIsRequired"
	ErrKeyCurrentCountCannotBeZero   = "StreakCurrentCountCannotBeZero"
	ErrKeyInvalidCurrentCount        = "StreakInvalidCurrentCount"
	ErrKeyPreviousEntryMustBeRelated = "StreakPreviousEntryMustBeRelated"
)
