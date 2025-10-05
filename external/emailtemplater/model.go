package emailtemplater

// TemplateRequest is the generic interface for all template generation requests
type TemplateRequest interface {
	GetEmailTo() string
	Validate() error
}

// RenderedEmail represents a fully rendered email ready to be sent
type RenderedEmail struct {
	// To is the recipient email address
	To string

	// From is the sender email address
	From string

	// ReplyTo is the reply-to email address
	ReplyTo string

	// Subject is the email subject line
	Subject string

	// HTMLBody is the rendered HTML content
	HTMLBody string

	// Preview is the preview text shown in email clients
	Preview string
}

// verificationEmailSubstitutes holds the variables to replace in verification email templates
type verificationEmailSubstitutes struct {
	// FullName holds the combination of the user's first and last name
	FullName string `handlebars:"FullName"`

	// VerificationURL holds the FE URL (containing verification) for verifying email
	VerificationURL string `handlebars:"VerificationURL"`

	// LoginURL the URL used for signing in
	LoginURL string `handlebars:"LoginURL"`
}

// loginEmailSubstitutes holds the variables to replace in login email templates
type loginEmailSubstitutes struct {
	// LoginURL holds the FE URL (containing login token) for signing user in
	LoginURL string `handlebars:"LoginURL"`
}
