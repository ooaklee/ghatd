package emailmanager

// SendEmailRequest holds all information needed to send an email
type SendEmailRequest struct {
	// To is the recipient email address
	To string

	// From is the sender email address
	From string

	// ReplyTo is the reply-to email address
	ReplyTo string

	// Subject is the email subject
	Subject string

	// HTMLBody is the HTML body of the email
	HTMLBody string

	// UserId is the ID of the user (for audit logging)
	UserId string

	// RecipientType is the type of recipient (for audit logging)
	RecipientType string
}

// SendVerificationEmailRequest holds information for sending a verification email
type SendVerificationEmailRequest struct {
	// FirstName is the recipient's first name
	FirstName string

	// LastName is the recipient's last name
	LastName string

	// Email is the recipient's email
	Email string

	// Token is the verification token
	Token string

	// IsDashboardRequest indicates if this is for dashboard access
	IsDashboardRequest bool

	// RequestUrl is the redirect URL after verification
	RequestUrl string

	// UserId is the user ID (for audit logging)
	UserId string
}

// SendLoginEmailRequest holds information for sending a login email
type SendLoginEmailRequest struct {
	// Email is the recipient's email
	Email string

	// Token is the login token
	Token string

	// IsDashboardRequest indicates if this is for dashboard access
	IsDashboardRequest bool

	// RequestUrl is the redirect URL after login
	RequestUrl string

	// UserId is the user ID (for audit logging)
	UserId string
}

// SendCustomEmailRequest holds information for sending a custom email from base template
type SendCustomEmailRequest struct {
	// EmailSubject is the email subject
	EmailSubject string

	// EmailPreview is the preview text
	EmailPreview string

	// EmailBody is the HTML body content (wrapped in <td> tags)
	EmailBody string

	// EmailTo is the recipient email
	EmailTo string

	// OverrideEmailFrom optionally overrides the default from address
	OverrideEmailFrom string

	// OverrideEmailReplyTo optionally overrides the default reply-to address
	OverrideEmailReplyTo string

	// WithFooter indicates whether to include a footer
	WithFooter bool

	// UserId is the user ID (for audit logging)
	UserId string

	// RecipientType is the type of recipient (for audit logging)
	RecipientType string
}
