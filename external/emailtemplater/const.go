package emailtemplater

type EmailTemplateType string

const (
	// EmailTemplateTypeLogin is the template type for login emails
	EmailTemplateTypeLogin EmailTemplateType = "login"

	// EmailTemplateTypeVerification is the template type for verification emails
	EmailTemplateTypeVerification EmailTemplateType = "verification"

	// EmailTemplateTypeCustom is the template type for custom emails based on base template
	EmailTemplateTypeCustom EmailTemplateType = "custom"

	// EmailTemplateTypeBase is the template type for the base email template
	EmailTemplateTypeBase EmailTemplateType = "base"
)

const (
	// ErrKeyEmailTemplaterMissingRecipient indicates that the recipient email is missing
	ErrKeyEmailTemplaterMissingRecipient = "EmailTemplaterMissingRecipient"

	// ErrKeyEmailTemplaterMissingSubject indicates that the email subject is missing
	ErrKeyEmailTemplaterMissingSubject = "EmailTemplaterMissingSubject"

	// ErrKeyEmailTemplaterMissingBody indicates that the email body is missing
	ErrKeyEmailTemplaterMissingBody = "EmailTemplaterMissingBody"

	// ErrKeyEmailTemplaterMissingToken indicates that the authentication token is missing
	ErrKeyEmailTemplaterMissingToken = "EmailTemplaterMissingToken"

	// ErrKeyEmailTemplaterMissingPersonalInfo indicates that personal information (first/last name) is missing
	ErrKeyEmailTemplaterMissingPersonalInfo = "EmailTemplaterMissingPersonalInfo"

	// ErrKeyEmailTemplaterTemplateRenderingFailed indicates that template rendering failed
	ErrKeyEmailTemplaterTemplateRenderingFailed = "EmailTemplaterTemplateRenderingFailed"

	// ErrKeyEmailTemplaterTemplateNotFound indicates that the specified template was not found
	ErrKeyEmailTemplaterTemplateNotFound = "EmailTemplaterTemplateNotFound"

	// ErrKeyEmailTemplaterDynamicTemplateNotFound indicates that the specified dynamic template was not found
	ErrKeyEmailTemplaterDynamicTemplateNotFound = "EmailTemplaterDynamicTemplateNotFound"

	// ErrKeyEmailTemplaterNoConfigProvided indicates that no configuration was provided when creating the templater
	ErrKeyEmailTemplaterNoConfigProvided = "EmailTemplaterNoConfigProvided"
)
