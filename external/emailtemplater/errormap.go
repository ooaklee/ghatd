package emailtemplater

import "github.com/ooaklee/reply"

// EmailTemplaterErrorMap holds Error keys, their corresponding human-friendly message, and response status code
// nolint will be used later
var EmailTemplaterErrorMap reply.ErrorManifest = map[string]reply.ErrorManifestItem{
	ErrKeyEmailTemplaterNoConfigProvided: {Title: "Internal Server Error", Detail: "No configuration provided for email templater", StatusCode: 500, Code: "ET0-001"},
	ErrKeyEmailTemplaterTemplateNotFound: {Title: "Internal Server Error", Detail: "Email template not found", StatusCode: 500, Code: "ET0-002"},
	ErrKeyEmailTemplaterDynamicTemplateNotFound: {
		Title:      "Internal Server Error",
		Detail:     "Dynamic email template function not found",
		StatusCode: 500,
		Code:       "ET0-003",
	},
	ErrKeyEmailTemplaterMissingRecipient:    {Title: "Bad Request", Detail: "Recipient email is required", StatusCode: 400, Code: "ET0-004"},
	ErrKeyEmailTemplaterMissingSubject:      {Title: "Bad Request", Detail: "Email subject is required", StatusCode: 400, Code: "ET0-005"},
	ErrKeyEmailTemplaterMissingBody:         {Title: "Bad Request", Detail: "Email body is required", StatusCode: 400, Code: "ET0-006"},
	ErrKeyEmailTemplaterMissingToken:        {Title: "Bad Request", Detail: "Authentication token is required", StatusCode: 400, Code: "ET0-007"},
	ErrKeyEmailTemplaterMissingPersonalInfo: {Title: "Bad Request", Detail: "First name and last name are required", StatusCode: 400, Code: "ET0-008"},
}
