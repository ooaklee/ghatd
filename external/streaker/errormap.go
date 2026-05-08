package streaker

import (
	"net/http"

	"github.com/ooaklee/reply"
)

// StreakErrorMap holds error keys, human-friendly messages, and response status codes.
var StreakErrorMap reply.ErrorManifest = map[string]reply.ErrorManifestItem{
	ErrKeyStreakTypeIsRequired: {
		StatusCode: http.StatusBadRequest,
		Code:       "STR0-001",
		Detail:     "Please provide a streak type",
	},
	ErrKeyOwnerIdIsRequired: {
		StatusCode: http.StatusBadRequest,
		Code:       "STR0-002",
		Detail:     "Please provide the streak owner",
	},
	ErrKeyTargetTypeIsRequired: {
		StatusCode: http.StatusBadRequest,
		Code:       "STR0-003",
		Detail:     "Please provide the streak target type",
	},
	ErrKeyTargetIdIsRequired: {
		StatusCode: http.StatusBadRequest,
		Code:       "STR0-004",
		Detail:     "Please provide the streak target",
	},
	ErrKeyCreatedByUserIdIsRequired: {
		StatusCode: http.StatusBadRequest,
		Code:       "STR0-005",
		Detail:     "Please provide the user creating the streak entry",
	},
	ErrKeyInvalidPeriodType: {
		StatusCode: http.StatusBadRequest,
		Code:       "STR0-006",
		Detail:     "The specified streak period type is not supported",
	},
	ErrKeyInvalidOccurredAt: {
		StatusCode: http.StatusBadRequest,
		Code:       "STR0-007",
		Detail:     "The provided occurred_at value is invalid",
	},
	ErrKeyPeriodKeyIsRequired: {
		StatusCode: http.StatusBadRequest,
		Code:       "STR0-008",
		Detail:     "Please provide a period key for custom streak periods",
	},
	ErrKeyResourceNotFound: {
		StatusCode: http.StatusNotFound,
		Code:       "STR0-009",
		Detail:     "The requested streak entry could not be found",
	},
	ErrKeyResourceConflict: {
		StatusCode: http.StatusConflict,
		Code:       "STR0-010",
		Detail:     "A streak entry already exists for this period",
	},
	ErrKeyDatabaseError: {
		StatusCode: http.StatusInternalServerError,
		Code:       "STR0-011",
		Detail:     "Unable to complete the streak operation at this time",
	},
	ErrKeyIdIsRequired: {
		StatusCode: http.StatusBadRequest,
		Code:       "STR0-012",
		Detail:     "Please provide a streak entry ID",
	},
	ErrKeyNanoIdIsRequired: {
		StatusCode: http.StatusBadRequest,
		Code:       "STR0-013",
		Detail:     "Please provide a streak entry nano ID",
	},
	ErrKeyCurrentCountCannotBeZero: {
		StatusCode: http.StatusBadRequest,
		Code:       "STR0-014",
		Detail:     "The streak current count must be greater than zero",
	},
	ErrKeyInvalidCurrentCount: {
		StatusCode: http.StatusBadRequest,
		Code:       "STR0-015",
		Detail:     "The streak current count is invalid",
	},
	ErrKeyPreviousEntryMustBeRelated: {
		StatusCode: http.StatusBadRequest,
		Code:       "STR0-016",
		Detail:     "The previous streak entry must belong to the same streak scope",
	},
}
