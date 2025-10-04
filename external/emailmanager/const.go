package emailmanager

const (
	// ErrKeyEmailMailerTemplateGenerationFailed indicates that template generation failed
	ErrKeyEmailMailerTemplateGenerationFailed = "EmailMailerTemplateGenerationFailed"

	// ErrKeyEmailMailerSendFailed indicates that sending the email failed
	ErrKeyEmailMailerSendFailed = "EmailMailerSendFailed"

	// ErrKeyEmailMailerProviderUnavailable indicates that no email provider is available
	ErrKeyEmailMailerProviderUnavailable = "EmailMailerProviderUnavailable"

	// ErrKeyEmailMailerAuditFailed indicates that audit logging failed (non-fatal)
	ErrKeyEmailMailerAuditFailed = "EmailMailerAuditFailed"
)
