package blueprint

import (
	"net/http"

	"github.com/ooaklee/reply/v2"
)

// blueprintErrorMap holds Error keys, their corresponding human-friendly message, and response status code.
var blueprintErrorMap = reply.ErrorManifest{
	ErrBlueprintError: {
		Title:      "Bad Request",
		Detail:     "Some blueprint error",
		StatusCode: http.StatusBadRequest,
		Code:       "BLP0-001",
	},
	ErrBlueprintNameIsRequired: {
		Title:      "Missing Blueprint Name",
		Detail:     "Please provide a blueprint name",
		StatusCode: http.StatusBadRequest,
		Code:       "BLP0-002",
	},
	ErrBlueprintKindIsRequired: {
		Title:      "Missing Blueprint Kind",
		Detail:     "Please provide a blueprint kind",
		StatusCode: http.StatusBadRequest,
		Code:       "BLP0-003",
	},
	ErrBlueprintIDIsRequired: {
		Title:      "Missing Blueprint ID",
		Detail:     "Please provide a blueprint ID",
		StatusCode: http.StatusBadRequest,
		Code:       "BLP0-004",
	},
	ErrBlueprintResourceNotFound: {
		Title:      "Blueprint Not Found",
		Detail:     "The requested blueprint could not be found",
		StatusCode: http.StatusNotFound,
		Code:       "BLP0-005",
	},
	ErrBlueprintUserIDIsRequired: {
		Title:      "Missing User ID",
		Detail:     "Please authenticate before requesting this blueprint",
		StatusCode: http.StatusUnauthorized,
		Code:       "BLP0-006",
	},
	ErrBlueprintInvalidPayload: {
		Title:      "Invalid Blueprint Payload",
		Detail:     "Please provide a valid blueprint payload",
		StatusCode: http.StatusBadRequest,
		Code:       "BLP0-007",
	},
	ErrBlueprintInvalidQueryParam: {
		Title:      "Invalid Blueprint Query",
		Detail:     "Please provide valid blueprint query parameters",
		StatusCode: http.StatusBadRequest,
		Code:       "BLP0-008",
	},
	ErrBlueprintDatabaseError: {
		Title:      "Internal Error",
		Detail:     "Unable to complete the blueprint operation at this time",
		StatusCode: http.StatusInternalServerError,
		Code:       "BLP0-009",
	},
	ErrBlueprintRegistrationKeyMissing: {
		Title:      "Missing Registration Key",
		Detail:     "Please provide a blueprint registration key",
		StatusCode: http.StatusBadRequest,
		Code:       "BLP0-010",
	},
	ErrBlueprintRegistrationConflict: {
		Title:      "Blueprint Registration Conflict",
		Detail:     "A blueprint registration already exists for this key",
		StatusCode: http.StatusConflict,
		Code:       "BLP0-011",
	},
	ErrBlueprintRegistrationNotFound: {
		Title:      "Blueprint Registration Not Found",
		Detail:     "The requested blueprint registration could not be found",
		StatusCode: http.StatusNotFound,
		Code:       "BLP0-012",
	},
}
