package emailprovider

import "github.com/ooaklee/reply"

// EmailProviderErrorMap holds Error keys, their corresponding human-friendly message, and response status code
// nolint will be used later
var EmailProviderErrorMap reply.ErrorManifest = map[string]reply.ErrorManifestItem{
	ErrKeyEmailProviderUnavailable:      {Title: "Internal Server Error", Detail: "Service Unavailable: Email provider service is unavailable", StatusCode: 503, Code: "EP0-001"},
	ErrKeyEmailProviderSendFailed:       {Title: "Internal Server Error", Detail: "Failed to send email via provider", StatusCode: 500, Code: "EP0-002"},
	ErrKeyEmailProviderInvalidEmail:     {Title: "Bad Request", Detail: "Invalid email data provided", StatusCode: 400, Code: "EP0-003"},
	ErrKeyEmailProviderMissingRecipient: {Title: "Bad Request", Detail: "Recipient email is required", StatusCode: 400, Code: "EP0-004"},
	ErrKeyEmailProviderMissingFrom:      {Title: "Bad Request", Detail: "From email address is required", StatusCode: 400, Code: "EP0-005"},
	ErrKeyEmailProviderMissingSubject:   {Title: "Bad Request", Detail: "Email subject is required", StatusCode: 400, Code: "EP0-006"},
	ErrKeyEmailProviderMissingBody:      {Title: "Bad Request", Detail: "Email body is required", StatusCode: 400, Code: "EP0-007"},
}
