package contentmanager

import "github.com/ooaklee/reply/v2"

// ContentManagerErrorMap holds Error keys, their corresponding human-friendly message, and response status code
// nolint will be used later
var ContentManagerErrorMap reply.ErrorManifest = reply.ErrorManifest{
	ErrUnauthorisedCMUser: {Title: "Forbidden", Detail: "User is not authorised to carry out this action", StatusCode: 403, Code: "CM00-01"},
}
