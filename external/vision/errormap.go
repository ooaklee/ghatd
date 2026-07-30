package vision

import (
	"net/http"

	"github.com/ooaklee/reply/v2"
)

// visionErrorMap holds Error keys, their corresponding human-friendly message, and response status code.
var visionErrorMap = reply.ErrorManifest{
	ErrVisionError: {
		Title:      "Bad Request",
		Detail:     "Some vision error",
		StatusCode: http.StatusBadRequest,
		Code:       "BLP0-001",
	},
	ErrVisionNameIsRequired: {
		Title:      "Missing Vision Name",
		Detail:     "Please provide a vision name",
		StatusCode: http.StatusBadRequest,
		Code:       "BLP0-002",
	},
	ErrVisionKindIsRequired: {
		Title:      "Missing Vision Kind",
		Detail:     "Please provide a vision kind",
		StatusCode: http.StatusBadRequest,
		Code:       "BLP0-003",
	},
	ErrVisionIDIsRequired: {
		Title:      "Missing Vision ID",
		Detail:     "Please provide a vision ID",
		StatusCode: http.StatusBadRequest,
		Code:       "BLP0-004",
	},
	ErrVisionResourceNotFound: {
		Title:      "Vision Not Found",
		Detail:     "The requested vision could not be found",
		StatusCode: http.StatusNotFound,
		Code:       "BLP0-005",
	},
	ErrVisionUserIDIsRequired: {
		Title:      "Missing User ID",
		Detail:     "Please authenticate before requesting this vision",
		StatusCode: http.StatusUnauthorized,
		Code:       "BLP0-006",
	},
	ErrVisionInvalidPayload: {
		Title:      "Invalid Vision Payload",
		Detail:     "Please provide a valid vision payload",
		StatusCode: http.StatusBadRequest,
		Code:       "BLP0-007",
	},
	ErrVisionInvalidQueryParam: {
		Title:      "Invalid Vision Query",
		Detail:     "Please provide valid vision query parameters",
		StatusCode: http.StatusBadRequest,
		Code:       "BLP0-008",
	},
	ErrVisionDatabaseError: {
		Title:      "Internal Error",
		Detail:     "Unable to complete the vision operation at this time",
		StatusCode: http.StatusInternalServerError,
		Code:       "BLP0-009",
	},
	ErrVisionRegistrationKeyMissing: {
		Title:      "Missing Registration Key",
		Detail:     "Please provide a vision registration key",
		StatusCode: http.StatusBadRequest,
		Code:       "BLP0-010",
	},
	ErrVisionRegistrationConflict: {
		Title:      "Vision Registration Conflict",
		Detail:     "A vision registration already exists for this key",
		StatusCode: http.StatusConflict,
		Code:       "BLP0-011",
	},
	ErrVisionRegistrationNotFound: {
		Title:      "Vision Registration Not Found",
		Detail:     "The requested vision registration could not be found",
		StatusCode: http.StatusNotFound,
		Code:       "BLP0-012",
	},
}
