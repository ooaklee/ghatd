package emailmanager

import (
	"context"

	"github.com/ooaklee/ghatd/external/audit"
)

// AuditService defines the interface for audit logging
type AuditService interface {
	LogAuditEvent(ctx context.Context, r *audit.LogAuditEventRequest) error
}

// EmailInfo holds information about an email for audit logging
type EmailInfo struct {
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
