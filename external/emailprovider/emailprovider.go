package emailprovider

import "context"

// Email represents an email message to be sent
type Email struct {
	// To is the recipient email address
	To string

	// From is the sender email address
	From string

	// ReplyTo is the reply-to email address
	ReplyTo string

	// Subject is the email subject line
	Subject string

	// HTMLBody is the HTML content of the email
	HTMLBody string

	// TextBody is the plain text content of the email (optional)
	TextBody string
}

// SendResult contains information about a sent email
type SendResult struct {
	// MessageID is the unique identifier for the sent message (provider-specific)
	MessageID string

	// Provider is the name of the provider that sent the email
	Provider string

	// Success indicates whether the email was sent successfully
	Success bool

	// Error contains any error that occurred during sending
	Error error
}

// EmailProvider is the interface that email providers must implement
type EmailProvider interface {
	// Send sends an email and returns the result
	Send(ctx context.Context, email *Email) (*SendResult, error)

	// Name returns the name of the provider
	Name() string

	// IsHealthy checks if the provider is operational
	IsHealthy(ctx context.Context) bool
}

// Config holds configuration for email providers
type Config struct {
	// Environment the environment API is running in (e.g., "production", "staging", "development")
	Environment string

	// EnableSendingEmail specifies whether email should actually be sent
	// or just logged (useful for development)
	EnableSendingEmail bool

	// LocalOutputEnvironment holds the name of the environment that will make email
	// output to log instead of being sent using API
	LocalOutputEnvironment string
}

// DefaultConfig returns a config with sensible defaults
func DefaultConfig() *Config {
	return &Config{
		Environment:            "development",
		EnableSendingEmail:     false,
		LocalOutputEnvironment: "development",
	}
}

// ShouldSendEmail determines if emails should be actually sent based on configuration
func (c *Config) ShouldSendEmail() bool {
	return (c.Environment != c.LocalOutputEnvironment) || c.EnableSendingEmail
}
