package emailtemplater

import (
	"time"

	"github.com/ooaklee/ghatd/external/emailtemplater/templates"
)

// Config holds configuration for email template generation
type Config struct {
	// FrontEndDomainName the frontend domain being used for current environment
	FrontEndDomainName string

	// DashboardDomainName the dashboard domain being used for current environment
	DashboardDomainName string

	// Environment the environment API is running in (e.g., "production", "staging", "development")
	Environment string

	// EmailVerificationFullEndpoint the full endpoint that will catch and handle token
	// verify actions
	EmailVerificationFullEndpoint string

	// DashboardVerificationURIPath the path on the front end that will catch and handle token
	// verify actions for admins/dashboard access
	DashboardVerificationURIPath string

	// BusinessEntityName the name of this entity, whether an application, a person, business etc.
	BusinessEntityName string

	// BusinessEntityWebsite the website to reach the entity outlined in BusinessEntityName
	BusinessEntityWebsite string

	// WelcomeEmailSubject the subject for verification email
	WelcomeEmailSubject string

	// LoginEmailSubject the subject for login request email
	LoginEmailSubject string

	// FromEmailAddress the email address that should be in "From" field
	FromEmailAddress string

	// NoReplyEmailAddress the email address that should be in reply-to field
	NoReplyEmailAddress string

	// TimeProvider optional function to get current time (useful for testing)
	// If nil, time.Now() will be used
	TimeProvider func() time.Time

	// Templates optional map of template names to template strings
	Templates map[EmailTemplateType]string

	// DynamicTemplates optional map of template names to functions that generate template strings
	DynamicTemplates map[EmailTemplateType]func(emailPreview string, emailSubject string, emailMainContent string, footerEnabled bool, footerYear int, footerEntityName string, footerEntityUrl string) string
}

// ExampleConfig returns a config with sensible example defaults
func ExampleConfig() *Config {
	return &Config{
		Environment:                   "development",
		WelcomeEmailSubject:           "Welcome! Please verify your email",
		LoginEmailSubject:             "Your login link",
		FromEmailAddress:              "noreply@example.com",
		NoReplyEmailAddress:           "noreply@example.com",
		BusinessEntityName:            "Example Inc.",
		BusinessEntityWebsite:         "https://example.com",
		FrontEndDomainName:            "https://app.example.com",
		EmailVerificationFullEndpoint: "https://app.example.com/v0/auth/verify",
		DashboardDomainName:           "https://app.example.com",
		DashboardVerificationURIPath:  "/v0/auth/verify",
		TimeProvider:                  time.Now,
		Templates: map[EmailTemplateType]string{
			EmailTemplateTypeLogin:        templates.NewLoginEmailTemplate(time.Now().Year(), "Example Inc.", "https://example.com"),
			EmailTemplateTypeVerification: templates.NewVerificationEmailTemplate(time.Now().Year(), "Example Inc.", "https://example.com"),
		},
		DynamicTemplates: map[EmailTemplateType]func(emailPreview string, emailSubject string, emailMainContent string, footerEnabled bool, footerYear int, footerEntityName string, footerEntityUrl string) string{
			EmailTemplateTypeBase: templates.NewBaseHtmlEmailTemplate,
		},
	}
}

// GetCurrentYear returns the current year based on the configured time provider
func (c *Config) GetCurrentYear() int {
	if c.TimeProvider == nil {
		return time.Now().Year()
	}
	return c.TimeProvider().Year()
}

// AdjustSubjectForEnvironment adds environment tag to subject if not in production
func (c *Config) AdjustSubjectForEnvironment(subject string) string {
	if c.Environment != "production" {
		return subject + " [" + c.Environment + "]"
	}
	return subject
}
