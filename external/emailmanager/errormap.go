package emailmanager

import "github.com/ooaklee/reply"

// EmailManagerErrorMap holds Error keys, their corresponding human-friendly message, and response status code
// nolint will be used later
var EmailManagerErrorMap reply.ErrorManifest = map[string]reply.ErrorManifestItem{
	ErrKeyEmailMailerTemplateGenerationFailed: {Title: "Internal Server Error", Detail: "Failed to generate email from template", StatusCode: 500, Code: "EM0-001"},
	ErrKeyEmailMailerSendFailed:               {Title: "Internal Server Error", Detail: "Failed to send email", StatusCode: 500, Code: "EM0-002"},
	ErrKeyEmailMailerProviderUnavailable:      {Title: "Internal Server Error", Detail: "Service Unavailable: No email provider is available", StatusCode: 503, Code: "EM0-004"},
	ErrKeyEmailMailerAuditFailed:              {Title: "Internal Server Error", Detail: "Failed to log audit event", StatusCode: 500, Code: "EM0-005"},
}
