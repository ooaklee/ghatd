package usermanager

import (
	"github.com/ooaklee/reply"
)

// usermanagerErrorMap holds Error keys, their corresponding human-friendly message, and response status code
// TODO: remove nolint
// nolint will be used later
var usermanagerErrorMap = map[string]reply.ErrorManifestItem{
	ErrKeyUserManagerError:        {Title: "Bad Request", Detail: "Some user manager related error.", StatusCode: 400, Code: "USM00-001"},
	ErrKeyUnableToIdentifyUser:    {Title: "Unauthorized", Detail: "Please contact support.", StatusCode: 401, Code: "USM00-002"},
	ErrKeyInvalidUserBody:         {Title: "Bad Request", Detail: "Check your submitted user information.", StatusCode: 400, Code: "USM00-003"},
	ErrKeyRequestFailedValidation: {Title: "Bad Request", Detail: "Request failed validation, please check provided data", StatusCode: 400, Code: "USM00-004"},
}
