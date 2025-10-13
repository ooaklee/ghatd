package user

import "github.com/ooaklee/reply"

// UserErrorMap holds Error keys, their corresponding human-friendly message, and response status code
var UserErrorMap reply.ErrorManifest = reply.ErrorManifest{
	// Model/Validation Errors
	ErrKeyUserConfigNotSet: {
		Title:      "Internal Server Error",
		Detail:     "User configuration not set",
		StatusCode: 500,
		Code:       "USV2-001",
	},
	ErrKeyUserInvalidTargetStatus: {
		Title:      "Bad Request",
		Detail:     "Invalid target status provided for user",
		StatusCode: 400,
		Code:       "USV2-002",
	},
	ErrKeyUserInvalidStatusTransition: {
		Title:      "Bad Request",
		Detail:     "User unable to transition to requested status",
		StatusCode: 400,
		Code:       "USV2-003",
	},
	ErrKeyUserRequiredFieldMissingEmail: {
		Title:      "Bad Request",
		Detail:     "Required value for email is missing",
		StatusCode: 400,
		Code:       "USV2-004",
	},
	ErrKeyUserRequiredFieldMissingFirstName: {
		Title:      "Bad Request",
		Detail:     "Required value for first name is missing",
		StatusCode: 400,
		Code:       "USV2-005",
	},
	ErrKeyUserRequiredFieldMissingLastName: {
		Title:      "Bad Request",
		Detail:     "Required value for last name is missing",
		StatusCode: 400,
		Code:       "USV2-006",
	},
	ErrKeyUserInvalidStatus: {
		Title:      "Bad Request",
		Detail:     "User has an invalid status assigned",
		StatusCode: 400,
		Code:       "USV2-007",
	},
	ErrKeyUserInvalidRole: {
		Title:      "Bad Request",
		Detail:     "User has an invalid role assigned",
		StatusCode: 400,
		Code:       "USV2-008",
	},

	// Service/Repository Errors
	ErrKeyUserNeverActivated: {
		Title:      "Conflict",
		Detail:     "User was never activated",
		StatusCode: 409,
		Code:       "USV2-009",
	},
	ErrKeyInvalidUserOriginStatus: {
		Title:      "Conflict",
		Detail:     "Invalid user origin status for requested operation",
		StatusCode: 409,
		Code:       "USV2-010",
	},
	ErrKeyInvalidUserBody: {
		Title:      "Bad Request",
		Detail:     "Invalid user request body",
		StatusCode: 400,
		Code:       "USV2-011",
	},
	ErrKeyResourceConflict: {
		Title:      "Conflict",
		Detail:     "User resource already exists",
		StatusCode: 409,
		Code:       "USV2-012",
	},
	ErrKeyInvalidQueryParam: {
		Title:      "Bad Request",
		Detail:     "Invalid query parameter",
		StatusCode: 400,
		Code:       "USV2-013",
	},
	ErrKeyPageOutOfRange: {
		Title:      "Bad Request",
		Detail:     "Requested page is out of range",
		StatusCode: 400,
		Code:       "USV2-014",
	},
	ErrKeyInvalidUserID: {
		Title:      "Bad Request",
		Detail:     "Invalid or missing user ID",
		StatusCode: 400,
		Code:       "USV2-015",
	},
	ErrKeyResourceNotFound: {
		Title:      "Not Found",
		Detail:     "User resource not found",
		StatusCode: 404,
		Code:       "USV2-016",
	},
	ErrKeyNoChangesDetected: {
		Title:      "Conflict",
		Detail:     "No changes detected",
		StatusCode: 409,
		Code:       "USV2-017",
	},
	ErrKeyInvalidEmail: {
		Title:      "Bad Request",
		Detail:     "Invalid email address",
		StatusCode: 400,
		Code:       "USV2-018",
	},
	ErrKeyEmailAlreadyExists: {
		Title:      "Conflict",
		Detail:     "Email address already exists",
		StatusCode: 409,
		Code:       "USV2-019",
	},
	ErrKeyUserNotFound: {
		Title:      "Not Found",
		Detail:     "User not found",
		StatusCode: 404,
		Code:       "USV2-020",
	},
	ErrKeyUnauthorisedAccess: {
		Title:      "Unauthorized",
		Detail:     "Unauthorized access to user resource",
		StatusCode: 401,
		Code:       "USV2-021",
	},
	ErrKeyInvalidNanoID: {
		Title:      "Bad Request",
		Detail:     "Invalid or missing nano ID",
		StatusCode: 400,
		Code:       "USV2-022",
	},
	ErrKeyDatabaseError: {
		Title:      "Internal Server Error",
		Detail:     "Database operation failed",
		StatusCode: 500,
		Code:       "USV2-023",
	},
	ErrKeyValidationFailed: {
		Title:      "Bad Request",
		Detail:     "User validation failed",
		StatusCode: 400,
		Code:       "USV2-024",
	},
}
