package emailprovider

import (
	"context"
	"fmt"

	"github.com/ooaklee/ghatd/external/logger"
	"go.uber.org/zap"
)

// LoggingEmailProvider is an email provider that logs emails instead of sending them
// This is useful for development and testing environments
type LoggingEmailProvider struct {
	name string
}

// NewLoggingEmailProvider creates a new logging email provider
func NewLoggingEmailProvider() *LoggingEmailProvider {
	return &LoggingEmailProvider{
		name: "LOCAL",
	}
}

// Send handles logging the email instead of sending it
func (p *LoggingEmailProvider) Send(ctx context.Context, email *Email) (*SendResult, error) {
	// Validate email
	if err := validateEmail(email); err != nil {
		return &SendResult{
			Provider: p.Name(),
			Success:  false,
			Error:    err,
		}, err
	}

	// Get logger from context
	log := logger.AcquireFrom(ctx)

	// Log the email details
	log.Info("email-outputted-locally--not-sent",
		zap.String("provider", p.Name()),
		zap.String("to", email.To),
		zap.String("from", email.From),
		zap.String("subject", email.Subject),
		zap.String("html_body_preview", truncateString(email.HTMLBody, 200)),
	)

	// Generate a fake message ID for consistency
	messageID := fmt.Sprintf("local-%d", generateRandomID())

	return &SendResult{
		MessageID: messageID,
		Provider:  p.Name(),
		Success:   true,
		Error:     nil,
	}, nil
}

// Name returns the name of the provider
func (p *LoggingEmailProvider) Name() string {
	return p.name
}

// IsHealthy returns whether the provider is healthy
// The Logging email provider is always healthy
func (p *LoggingEmailProvider) IsHealthy(ctx context.Context) bool {
	return true
}

// truncateString truncates a string to the specified length
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// generateRandomID generates a simple random ID for local message IDs
func generateRandomID() int64 {
	// Simple implementation - in production you might want something more sophisticated
	return int64(len("local")) * 1000
}
