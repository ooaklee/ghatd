package user

import "github.com/ooaklee/reply/v2"

// UserErrorMap holds Error keys, their corresponding human-friendly message, and response status code
var UserErrorMap reply.ErrorManifest = reply.ErrorManifest{
	// Model/Validation Errors
	ErrUserConfigNotSet: {
		Title:      "Internal Server Error",
		Detail:     "User configuration not set",
		StatusCode: 500,
		Code:       "USV2-001",
	},
	ErrUserInvalidTargetStatus: {
		Title:      "Bad Request",
		Detail:     "Invalid target status provided for user",
		StatusCode: 400,
		Code:       "USV2-002",
	},
	ErrUserInvalidStatusTransition: {
		Title:      "Bad Request",
		Detail:     "User unable to transition to requested status",
		StatusCode: 400,
		Code:       "USV2-003",
	},
	ErrUserRequiredFieldMissingEmail: {
		Title:      "Bad Request",
		Detail:     "Required value for email is missing",
		StatusCode: 400,
		Code:       "USV2-004",
	},
	ErrUserRequiredFieldMissingFirstName: {
		Title:      "Bad Request",
		Detail:     "Required value for first name is missing",
		StatusCode: 400,
		Code:       "USV2-005",
	},
	ErrUserRequiredFieldMissingLastName: {
		Title:      "Bad Request",
		Detail:     "Required value for last name is missing",
		StatusCode: 400,
		Code:       "USV2-006",
	},
	ErrUserInvalidStatus: {
		Title:      "Bad Request",
		Detail:     "User has an invalid status assigned",
		StatusCode: 400,
		Code:       "USV2-007",
	},
	ErrUserInvalidRole: {
		Title:      "Bad Request",
		Detail:     "User has an invalid role assigned",
		StatusCode: 400,
		Code:       "USV2-008",
	},

	// Service/Repository Errors
	ErrUserNeverActivated: {
		Title:      "Conflict",
		Detail:     "User was never activated",
		StatusCode: 409,
		Code:       "USV2-009",
	},
	ErrInvalidUserOriginStatus: {
		Title:      "Conflict",
		Detail:     "Invalid user origin status for requested operation",
		StatusCode: 409,
		Code:       "USV2-010",
	},
	ErrInvalidUserBody: {
		Title:      "Bad Request",
		Detail:     "Invalid user request body",
		StatusCode: 400,
		Code:       "USV2-011",
	},
	ErrResourceConflict: {
		Title:      "Conflict",
		Detail:     "User resource already exists",
		StatusCode: 409,
		Code:       "USV2-012",
	},
	ErrInvalidQueryParam: {
		Title:      "Bad Request",
		Detail:     "Invalid query parameter",
		StatusCode: 400,
		Code:       "USV2-013",
	},
	ErrPageOutOfRange: {
		Title:      "Bad Request",
		Detail:     "Requested page is out of range",
		StatusCode: 400,
		Code:       "USV2-014",
	},
	ErrInvalidUserID: {
		Title:      "Bad Request",
		Detail:     "Invalid or missing user ID",
		StatusCode: 400,
		Code:       "USV2-015",
	},
	ErrResourceNotFound: {
		Title:      "Not Found",
		Detail:     "User resource not found",
		StatusCode: 404,
		Code:       "USV2-016",
	},
	ErrNoChangesDetected: {
		Title:      "Conflict",
		Detail:     "No changes detected",
		StatusCode: 409,
		Code:       "USV2-017",
	},
	ErrInvalidEmail: {
		Title:      "Bad Request",
		Detail:     "Invalid email address",
		StatusCode: 400,
		Code:       "USV2-018",
	},
	ErrEmailAlreadyExists: {
		Title:      "Conflict",
		Detail:     "Email address already exists",
		StatusCode: 409,
		Code:       "USV2-019",
	},
	ErrUserNotFound: {
		Title:      "Not Found",
		Detail:     "User not found",
		StatusCode: 404,
		Code:       "USV2-020",
	},
	ErrUnauthorisedAccess: {
		Title:      "Unauthorized",
		Detail:     "Unauthorized access to user resource",
		StatusCode: 401,
		Code:       "USV2-021",
	},
	ErrInvalidNanoID: {
		Title:      "Bad Request",
		Detail:     "Invalid or missing nano ID",
		StatusCode: 400,
		Code:       "USV2-022",
	},
	ErrDatabaseError: {
		Title:      "Internal Server Error",
		Detail:     "Database operation failed",
		StatusCode: 500,
		Code:       "USV2-023",
	},
	ErrValidationFailed: {
		Title:      "Bad Request",
		Detail:     "User validation failed",
		StatusCode: 400,
		Code:       "USV2-024",
	},
	ErrInvalidUserConfigType: {
		Title:      "Bad Request",
		Detail:     "Invalid user config type",
		StatusCode: 400,
		Code:       "USV2-025",
	},
}
