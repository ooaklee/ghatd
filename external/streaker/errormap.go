package streaker

import (
	"net/http"

	"github.com/ooaklee/reply/v2"
)

// StreakErrorMap holds error keys, human-friendly messages, and response status codes.
var StreakErrorMap reply.ErrorManifest = reply.ErrorManifest{
	ErrStreakTypeIsRequired: {
		StatusCode: http.StatusBadRequest,
		Code:       "STR0-001",
		Detail:     "Please provide a streak type",
	},
	ErrOwnerIdIsRequired: {
		StatusCode: http.StatusBadRequest,
		Code:       "STR0-002",
		Detail:     "Please provide the streak owner",
	},
	ErrTargetTypeIsRequired: {
		StatusCode: http.StatusBadRequest,
		Code:       "STR0-003",
		Detail:     "Please provide the streak target type",
	},
	ErrTargetIdIsRequired: {
		StatusCode: http.StatusBadRequest,
		Code:       "STR0-004",
		Detail:     "Please provide the streak target",
	},
	ErrCreatedByUserIdIsRequired: {
		StatusCode: http.StatusBadRequest,
		Code:       "STR0-005",
		Detail:     "Please provide the user creating the streak entry",
	},
	ErrInvalidPeriodType: {
		StatusCode: http.StatusBadRequest,
		Code:       "STR0-006",
		Detail:     "The specified streak period type is not supported",
	},
	ErrInvalidOccurredAt: {
		StatusCode: http.StatusBadRequest,
		Code:       "STR0-007",
		Detail:     "The provided occurred_at value is invalid",
	},
	ErrPeriodKeyIsRequired: {
		StatusCode: http.StatusBadRequest,
		Code:       "STR0-008",
		Detail:     "Please provide a period key for custom streak periods",
	},
	ErrResourceNotFound: {
		StatusCode: http.StatusNotFound,
		Code:       "STR0-009",
		Detail:     "The requested streak entry could not be found",
	},
	ErrResourceConflict: {
		StatusCode: http.StatusConflict,
		Code:       "STR0-010",
		Detail:     "A streak entry already exists for this period",
	},
	ErrDatabaseError: {
		StatusCode: http.StatusInternalServerError,
		Code:       "STR0-011",
		Detail:     "Unable to complete the streak operation at this time",
	},
	ErrIdIsRequired: {
		StatusCode: http.StatusBadRequest,
		Code:       "STR0-012",
		Detail:     "Please provide a streak entry ID",
	},
	ErrNanoIdIsRequired: {
		StatusCode: http.StatusBadRequest,
		Code:       "STR0-013",
		Detail:     "Please provide a streak entry nano ID",
	},
	ErrCurrentCountCannotBeZero: {
		StatusCode: http.StatusBadRequest,
		Code:       "STR0-014",
		Detail:     "The streak current count must be greater than zero",
	},
	ErrInvalidCurrentCount: {
		StatusCode: http.StatusBadRequest,
		Code:       "STR0-015",
		Detail:     "The streak current count is invalid",
	},
	ErrPreviousEntryMustBeRelated: {
		StatusCode: http.StatusBadRequest,
		Code:       "STR0-016",
		Detail:     "The previous streak entry must belong to the same streak scope",
	},
	ErrPeriodTypeIsRequired: {
		StatusCode: http.StatusBadRequest,
		Code:       "STR0-017",
		Detail:     "Please provide the streak period type",
	},
}
