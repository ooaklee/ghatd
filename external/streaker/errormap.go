package streaker

import (
	"net/http"

	"github.com/ooaklee/reply/v2"
)

// StreakErrorMap holds error keys, human-friendly messages, and response status codes.
var StreakErrorMap reply.ErrorManifest = reply.ErrorManifest{
	ErrStreakTypeIsRequired: {
		Title:      "Missing Streak Type",
		StatusCode: http.StatusBadRequest,
		Code:       "STR0-001",
		Detail:     "Please provide a streak type",
	},
	ErrOwnerIdIsRequired: {
		Title:      "Missing Owner",
		StatusCode: http.StatusBadRequest,
		Code:       "STR0-002",
		Detail:     "Please provide the streak owner",
	},
	ErrTargetTypeIsRequired: {
		Title:      "Missing Target Type",
		StatusCode: http.StatusBadRequest,
		Code:       "STR0-003",
		Detail:     "Please provide the streak target type",
	},
	ErrTargetIdIsRequired: {
		Title:      "Missing Target",
		StatusCode: http.StatusBadRequest,
		Code:       "STR0-004",
		Detail:     "Please provide the streak target",
	},
	ErrCreatedByUserIdIsRequired: {
		Title:      "Missing Creator",
		StatusCode: http.StatusBadRequest,
		Code:       "STR0-005",
		Detail:     "Please provide the user creating the streak entry",
	},
	ErrInvalidPeriodType: {
		Title:      "Invalid Period Type",
		StatusCode: http.StatusBadRequest,
		Code:       "STR0-006",
		Detail:     "The specified streak period type is not supported",
	},
	ErrInvalidOccurredAt: {
		Title:      "Invalid Occurred At",
		StatusCode: http.StatusBadRequest,
		Code:       "STR0-007",
		Detail:     "The provided occurred_at value is invalid",
	},
	ErrPeriodKeyIsRequired: {
		Title:      "Missing Period Key",
		StatusCode: http.StatusBadRequest,
		Code:       "STR0-008",
		Detail:     "Please provide a period key for custom streak periods",
	},
	ErrResourceNotFound: {
		Title:      "Streak Not Found",
		StatusCode: http.StatusNotFound,
		Code:       "STR0-009",
		Detail:     "The requested streak entry could not be found",
	},
	ErrResourceConflict: {
		Title:      "Streak Conflict",
		StatusCode: http.StatusConflict,
		Code:       "STR0-010",
		Detail:     "A streak entry already exists for this period",
	},
	ErrDatabaseError: {
		Title:      "Internal Error",
		StatusCode: http.StatusInternalServerError,
		Code:       "STR0-011",
		Detail:     "Unable to complete the streak operation at this time",
	},
	ErrIdIsRequired: {
		Title:      "Missing Streak ID",
		StatusCode: http.StatusBadRequest,
		Code:       "STR0-012",
		Detail:     "Please provide a streak entry ID",
	},
	ErrNanoIdIsRequired: {
		Title:      "Missing Streak Nano ID",
		StatusCode: http.StatusBadRequest,
		Code:       "STR0-013",
		Detail:     "Please provide a streak entry nano ID",
	},
	ErrCurrentCountCannotBeZero: {
		Title:      "Invalid Current Count",
		StatusCode: http.StatusBadRequest,
		Code:       "STR0-014",
		Detail:     "The streak current count must be greater than zero",
	},
	ErrInvalidCurrentCount: {
		Title:      "Invalid Current Count",
		StatusCode: http.StatusBadRequest,
		Code:       "STR0-015",
		Detail:     "The streak current count is invalid",
	},
	ErrPreviousEntryMustBeRelated: {
		Title:      "Invalid Previous Entry",
		StatusCode: http.StatusBadRequest,
		Code:       "STR0-016",
		Detail:     "The previous streak entry must belong to the same streak scope",
	},
	ErrPeriodTypeIsRequired: {
		Title:      "Missing Period Type",
		StatusCode: http.StatusBadRequest,
		Code:       "STR0-017",
		Detail:     "Please provide the streak period type",
	},
}
