package emailprovider

const (
	// ErrKeyEmailProviderUnavailable indicates that the email provider is not available
	ErrKeyEmailProviderUnavailable = "EmailProviderUnavailable"

	// ErrKeyEmailProviderSendFailed indicates that sending the email failed
	ErrKeyEmailProviderSendFailed = "EmailProviderSendFailed"

	// ErrKeyEmailProviderInvalidEmail indicates that the email data is invalid
	ErrKeyEmailProviderInvalidEmail = "EmailProviderInvalidEmail"

	// ErrKeyEmailProviderMissingRecipient indicates that the recipient email is missing
	ErrKeyEmailProviderMissingRecipient = "EmailProviderMissingRecipient"

	// ErrKeyEmailProviderMissingFrom indicates that the from email address is missing
	ErrKeyEmailProviderMissingFrom = "EmailProviderMissingFrom"

	// ErrKeyEmailProviderMissingSubject indicates that the email subject is missing
	ErrKeyEmailProviderMissingSubject = "EmailProviderMissingSubject"

	// ErrKeyEmailProviderMissingBody indicates that the email body is missing
	ErrKeyEmailProviderMissingBody = "EmailProviderMissingBody"
)
