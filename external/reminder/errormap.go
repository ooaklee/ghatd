package reminder

import (
	"net/http"

	"github.com/ooaklee/reply/v2"
)

// ReminderErrorMap maps reminder sentinel errors to API response metadata.
var ReminderErrorMap reply.ErrorManifest = reply.ErrorManifest{
	ErrUserIDIsRequired: {
		Title:      "Missing User",
		StatusCode: http.StatusBadRequest,
		Code:       "REM0-001",
		Detail:     "Please provide a user ID for the reminder",
	},
	ErrTargetTypeIsRequired: {
		Title:      "Missing Target Type",
		StatusCode: http.StatusBadRequest,
		Code:       "REM0-013",
		Detail:     "Please provide a target type for the reminder lookup",
	},
	ErrTitleIsRequired: {
		Title:      "Missing Title",
		StatusCode: http.StatusBadRequest,
		Code:       "REM0-002",
		Detail:     "Please provide a title for the reminder",
	},
	ErrTargetTimeIsRequired: {
		Title:      "Missing Target Time",
		StatusCode: http.StatusBadRequest,
		Code:       "REM0-003",
		Detail:     "Please provide a target/scheduled time for the reminder",
	},
	ErrInvalidTargetTime: {
		Title:      "Invalid Target Time",
		StatusCode: http.StatusBadRequest,
		Code:       "REM0-004",
		Detail:     "The provided target time is invalid",
	},
	ErrInvalidStatus: {
		Title:      "Invalid Status",
		StatusCode: http.StatusBadRequest,
		Code:       "REM0-005",
		Detail:     "The provided reminder status is not supported",
	},
	ErrResourceNotFound: {
		Title:      "Reminder Not Found",
		StatusCode: http.StatusNotFound,
		Code:       "REM0-006",
		Detail:     "The requested reminder could not be found",
	},
	ErrDatabaseError: {
		Title:      "Internal Error",
		StatusCode: http.StatusInternalServerError,
		Code:       "REM0-007",
		Detail:     "Unable to complete the reminder operation at this time",
	},
	ErrIdIsRequired: {
		Title:      "Missing Reminder ID",
		StatusCode: http.StatusBadRequest,
		Code:       "REM0-008",
		Detail:     "Please provide a reminder ID",
	},
	ErrNanoIdIsRequired: {
		Title:      "Missing Reminder Nano ID",
		StatusCode: http.StatusBadRequest,
		Code:       "REM0-009",
		Detail:     "Please provide a reminder nano ID",
	},
	ErrNotAuthorized: {
		Title:      "Not Authorized",
		StatusCode: http.StatusForbidden,
		Code:       "REM0-010",
		Detail:     "You do not have permission to access this reminder",
	},
	ErrInvalidReminderStatus: {
		Title:      "Invalid Status Transition",
		StatusCode: http.StatusBadRequest,
		Code:       "REM0-011",
		Detail:     "The requested status change is not valid for this reminder",
	},
	ErrInvalidExecutionStatus: {
		Title:      "Invalid Execution Status",
		StatusCode: http.StatusBadRequest,
		Code:       "REM0-014",
		Detail:     "The provided reminder execution status is not supported",
	},
	ErrInvalidPaginationParameter: {
		Title:      "Invalid Pagination",
		StatusCode: http.StatusBadRequest,
		Code:       "REM0-012",
		Detail:     "Invalid pagination parameters provided",
	},
}
