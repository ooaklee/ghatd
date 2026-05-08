package notifier

import (
	"net/http"

	"github.com/ooaklee/reply/v2"
)

// NotifierErrorMap holds user-facing API error metadata for notifier errors.
var NotifierErrorMap reply.ErrorManifest = reply.ErrorManifest{
	ErrDatabaseError: {
		Title:      "Internal Error",
		Detail:     "Unable to complete the notification operation at this time.",
		StatusCode: http.StatusInternalServerError,
		Code:       "NTF00-001",
	},
	ErrInvalidNotificationAddressBody: {
		Title:      "Bad Request",
		Detail:     "The notification address payload is invalid.",
		StatusCode: http.StatusBadRequest,
		Code:       "NTF00-002",
	},
	ErrInvalidNotificationChannel: {
		Title:      "Bad Request",
		Detail:     "The requested notification channel is not supported.",
		StatusCode: http.StatusBadRequest,
		Code:       "NTF00-003",
	},
	ErrNotificationAddressNotFound: {
		Title:      "Not Found",
		Detail:     "The requested notification address could not be found.",
		StatusCode: http.StatusNotFound,
		Code:       "NTF00-004",
	},
	ErrNotificationSenderNotEnabled: {
		Title:      "Service Unavailable",
		Detail:     "The requested notification sender is not enabled.",
		StatusCode: http.StatusServiceUnavailable,
		Code:       "NTF00-005",
	},
	ErrNotificationNoActiveAddresses: {
		Title:      "Not Found",
		Detail:     "No active notification addresses are registered for this user.",
		StatusCode: http.StatusNotFound,
		Code:       "NTF00-006",
	},
	ErrNotificationSendFailed: {
		Title:      "Internal Error",
		Detail:     "One or more notification sends failed.",
		StatusCode: http.StatusInternalServerError,
		Code:       "NTF00-007",
	},
	ErrNotificationUserIDRequired: {
		Title:      "Bad Request",
		Detail:     "A user ID is required for this notification operation.",
		StatusCode: http.StatusBadRequest,
		Code:       "NTF00-008",
	},
	ErrInvalidNotificationPreferences: {
		Title:      "Bad Request",
		Detail:     "The notification preferences payload is invalid.",
		StatusCode: http.StatusBadRequest,
		Code:       "NTF00-009",
	},
}
