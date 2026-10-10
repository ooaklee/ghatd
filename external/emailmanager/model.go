package emailmanager

import (
	"context"

	"github.com/ooaklee/ghatd/external/audit"
)

// AuditService defines the interface for audit logging
type AuditService interface {
	// LogAuditEvent records the audit event described by r through the
	// AuditService, returning an error if logging fails.
	LogAuditEvent(ctx context.Context, r *audit.LogAuditEventRequest) error
}

// EmailInfo holds information about an email for audit logging
type EmailInfo struct {
	// MailType, ProviderID and State preserve trusted route/outcome attribution in the audit.
	MailType   string
	ProviderID string
	State      string
	// To is the recipient email address
	To string

	// From is the sender email address
	From string

	// Subject is the email subject
	Subject string

	// EmailProvider is the name of the provider used to send the email
	EmailProvider string

	// UserId is the ID of the user this email is sent to (for audit logging)
	UserId string

	// RecipientType is the type of recipient (e.g., "USER", "ADMIN")
	RecipientType string
}
