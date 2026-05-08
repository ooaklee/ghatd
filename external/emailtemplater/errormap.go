package emailtemplater

import "github.com/ooaklee/reply/v2"

// EmailTemplaterErrorMap holds Error keys, their corresponding human-friendly message, and response status code
// nolint will be used later
var EmailTemplaterErrorMap reply.ErrorManifest = reply.ErrorManifest{
	ErrEmailTemplaterNoConfigProvided: {Title: "Internal Server Error", Detail: "Unable to process email request", StatusCode: 500, Code: "ET0-001"},
	ErrEmailTemplaterTemplateNotFound: {Title: "Internal Server Error", Detail: "Unable to process email request", StatusCode: 500, Code: "ET0-002"},
	ErrEmailTemplaterDynamicTemplateNotFound: {
		Title:      "Internal Server Error",
		Detail:     "Unable to process email request",
		StatusCode: 500,
		Code:       "ET0-003",
	},
	ErrEmailTemplaterMissingRecipient:        {Title: "Bad Request", Detail: "Recipient email is required", StatusCode: 400, Code: "ET0-004"},
	ErrEmailTemplaterMissingSubject:          {Title: "Bad Request", Detail: "Email subject is required", StatusCode: 400, Code: "ET0-005"},
	ErrEmailTemplaterMissingBody:             {Title: "Bad Request", Detail: "Email body is required", StatusCode: 400, Code: "ET0-006"},
	ErrEmailTemplaterMissingToken:            {Title: "Bad Request", Detail: "Authentication token is required", StatusCode: 400, Code: "ET0-007"},
	ErrEmailTemplaterMissingPersonalInfo:     {Title: "Bad Request", Detail: "First name and last name are required", StatusCode: 400, Code: "ET0-008"},
	ErrEmailTemplaterTemplateRenderingFailed: {Title: "Internal Server Error", Detail: "Unable to process email request", StatusCode: 500, Code: "ET0-009"},
}
