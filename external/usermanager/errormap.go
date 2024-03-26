package usermanager

import (
	"github.com/ooaklee/reply"
)

// usermanagerErrorMap holds Error keys, their corresponding human-friendly message, and response status code
// TODO: remove nolint
//nolint will be used later
var usermanagerErrorMap = map[string]reply.ErrorManifestItem{
	ErrKeyUserManagerError:     {Title: "Bad Request", Detail: "Some usermanager error.", StatusCode: 400},
	ErrKeyUnableToIdentifyUser: {Title: "Unauthorized", Detail: "Please contact support.", StatusCode: 401},
	ErrKeyInvalidUserBody:      {Title: "Bad Request", Detail: "Check submitted user information.", StatusCode: 400},
}
