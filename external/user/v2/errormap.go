package user

import "github.com/ooaklee/reply"

var UserErrorMap reply.ErrorManifest = reply.ErrorManifest{
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
		Detail:     "Invalid user unable to transition to requested status",
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
}
