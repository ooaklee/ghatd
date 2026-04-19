package contentmanager

import "github.com/ooaklee/reply"

// ContentManagerErrorMap holds Error keys, their corresponding human-friendly message, and response status code
// nolint will be used later
var ContentManagerErrorMap reply.ErrorManifest = map[string]reply.ErrorManifestItem{
	ErrKeyUnauthorisedCMUser: {Title: "Forbidden", Detail: "User is not authorised to carry out this action", StatusCode: 403, Code: "CM00-01"},
}
