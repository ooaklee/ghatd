package contacter

import "github.com/ooaklee/reply/v2"

// ContacterErrorMap holds Error keys, their corresponding human-friendly message, and response status code
// nolint will be used later
var ContacterErrorMap reply.ErrorManifest = reply.ErrorManifest{
	ErrInvalidCommsPayload: {Title: "Bad Request", Detail: "Invalid communication payload provided", StatusCode: 400, Code: "CT00-01"},
	ErrFullNameRequired:    {Title: "Bad Request", Detail: "Full name is required when user ID is not provided", StatusCode: 400, Code: "CT00-02"},
	ErrEmailRequired:       {Title: "Bad Request", Detail: "Email is required when user ID is not provided", StatusCode: 400, Code: "CT00-03"},
	ErrCommsIdRequired:     {Title: "Bad Request", Detail: "Communication ID is required for updates", StatusCode: 400, Code: "CT00-04"},
	ErrCommsNotFound:       {Title: "Not Found", Detail: "Communication not found", StatusCode: 404, Code: "CT00-05"},
}
