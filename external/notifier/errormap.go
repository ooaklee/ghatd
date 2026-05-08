package notifier

import (
	"net/http"

	"github.com/ooaklee/reply/v2"
)

// NotifierErrorMap is the public error manifest that teaches GHATD's API
// layer how to turn notifier errors into clean HTTP responses.
//
// Each entry maps a sentinel error (from errors.go) to:
//
//   - Title – a short human-readable label for the problem.
//   - Detail – a sentence explaining what happened, safe to show to the user.
//   - StatusCode – the HTTP status code (400, 404, 500, 503, etc.).
//   - Code – a stable error code (NTF00-XXX) that clients and support teams
//     can rely on even when the error message text changes.
//
// The error manifest is plugged into the GHATD error middleware at startup,
// so any notifier error returned from a handler is automatically converted
// to the right API response.
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
