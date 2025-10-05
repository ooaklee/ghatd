package examples

import (
	"context"
	"fmt"
	"log"

	sp "github.com/SparkPost/gosparkpost"
	"github.com/ooaklee/ghatd/external/audit"
	"github.com/ooaklee/ghatd/external/emailmanager"
	"github.com/ooaklee/ghatd/external/emailprovider"
	"github.com/ooaklee/ghatd/external/emailtemplater"
)

// MockAuditService for examples
type MockAuditService struct{}

func (m *MockAuditService) LogAuditEvent(ctx context.Context, r *audit.LogAuditEventRequest) error {
	fmt.Printf("Audit: %s - %s (User: %s)\n", r.Action, r.Domain, r.TargetId)
	return nil
}

// Example 1: Basic email manager setup
func ExampleBasicSetup() {
	// Create templater
	templaterConfig := &emailtemplater.Config{
		FrontEndDomainName:            "https://app.example.com",
		DashboardDomainName:           "https://dashboard.example.com",
		Environment:                   "production",
		EmailVerificationFullEndpoint: "https://app.example.com/auth/verify",
		BusinessEntityName:            "MyApp",
		BusinessEntityWebsite:         "https://example.com",
		WelcomeEmailSubject:           "Welcome to MyApp!",
		LoginEmailSubject:             "Login to MyApp",
		FromEmailAddress:              "noreply@example.com",
		NoReplyEmailAddress:           "noreply@example.com",
	}
	tmplr, _ := emailtemplater.NewEmailTemplater(templaterConfig)

	// Create provider
	sparkpostClient := &sp.Client{
		Config: &sp.Config{
			BaseUrl:    "https://api.sparkpost.com",
			ApiKey:     "your-api-key",
			ApiVersion: 1,
		},
	}
	provider := emailprovider.NewSparkPostEmailProvider(sparkpostClient)

	// Create audit service
	auditService := &MockAuditService{}

	// Create manager
	managerConfig := &emailmanager.Config{
		ShouldSendEmail:    true,
		EnableAuditLogging: true,
	}
	manager := emailmanager.NewEmailManager(tmplr, provider, auditService, managerConfig)

	_ = manager // Manager created for demonstration
	fmt.Printf("Email manager created successfully\n")
	fmt.Printf("Templater configured for environment: %s\n", templaterConfig.Environment)
	fmt.Printf("Provider: SparkPost\n")
}

// Example 2: Sending verification email
func ExampleSendVerificationEmail() {
	manager := createExampleManager()

	ctx := context.Background()
	err := manager.SendVerificationEmail(ctx, &emailmanager.SendVerificationEmailRequest{
		FirstName:          "John",
		LastName:           "Doe",
		Email:              "john@example.com",
		Token:              "verification-token-123",
		IsDashboardRequest: false,
		RequestUrl:         "/dashboard",
		UserId:             "user-123",
	})

	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("Verification email sent successfully")
}

// Example 3: Sending login email
func ExampleSendLoginEmail() {
	manager := createExampleManager()

	ctx := context.Background()
	err := manager.SendLoginEmail(ctx, &emailmanager.SendLoginEmailRequest{
		Email:              "user@example.com",
		Token:              "login-token-456",
		IsDashboardRequest: false,
		RequestUrl:         "/dashboard",
		UserId:             "user-456",
	})

	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("Login email sent successfully")
}

// Example 4: Sending custom email
func ExampleSendCustomEmail() {
	manager := createExampleManager()

	customBody := `
<td style="font-family: sans-serif; font-size: 14px; vertical-align: top;">
	<p style="font-family: sans-serif; font-size: 14px; font-weight: normal; margin: 0; Margin-bottom: 15px;">
		Hello! You have a new notification.
	</p>
	<p style="font-family: sans-serif; font-size: 14px; font-weight: normal; margin: 0; Margin-bottom: 15px;">
		Please check your account for more details.
	</p>
</td>`

	ctx := context.Background()
	err := manager.SendCustomEmail(ctx, &emailmanager.SendCustomEmailRequest{
		EmailSubject:         "New Notification",
		EmailPreview:         "You have a new notification",
		EmailBody:            customBody,
		EmailTo:              "user@example.com",
		OverrideEmailFrom:    "",
		OverrideEmailReplyTo: "",
		WithFooter:           true,
		UserId:               "user-789",
		RecipientType:        string(audit.User),
	})

	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("Custom email sent successfully")
}

// Example 5: Development setup with logging provider
func ExampleDevelopmentSetup() {
	// Create templater
	templaterConfig := emailtemplater.ExampleConfig()
	templaterConfig.Environment = "development"
	tmplr, _ := emailtemplater.NewEmailTemplater(templaterConfig)

	// Use logging provider for development
	provider := emailprovider.NewLoggingEmailProvider(nil)

	// Create audit service
	auditService := &MockAuditService{}

	// Create manager with sending enabled (but will log instead of send)
	managerConfig := &emailmanager.Config{
		ShouldSendEmail:    true,
		EnableAuditLogging: true,
	}
	manager := emailmanager.NewEmailManager(tmplr, provider, auditService, managerConfig)

	// Send a test email
	ctx := context.Background()
	err := manager.SendLoginEmail(ctx, &emailmanager.SendLoginEmailRequest{
		Email:              "dev@example.com",
		Token:              "dev-token",
		IsDashboardRequest: false,
		UserId:             "dev-user-123",
	})

	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("Development email logged (not actually sent)")
}

// Example 6: Sending email without audit logging
func ExampleWithoutAuditLogging() {
	templaterConfig := emailtemplater.ExampleConfig()
	tmplr, _ := emailtemplater.NewEmailTemplater(templaterConfig)
	provider := emailprovider.NewLoggingEmailProvider(nil)

	// Disable audit logging
	managerConfig := &emailmanager.Config{
		ShouldSendEmail:    true,
		EnableAuditLogging: false, // Disabled
	}
	manager := emailmanager.NewEmailManager(tmplr, provider, nil, managerConfig)

	ctx := context.Background()
	err := manager.SendLoginEmail(ctx, &emailmanager.SendLoginEmailRequest{
		Email:  "user@example.com",
		Token:  "token-123",
		UserId: "user-123",
	})

	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("Email sent without audit logging")
}

// Example 7: Error handling
func ExampleErrorHandling() {
	manager := createExampleManager()

	ctx := context.Background()

	// Invalid request - missing required fields
	err := manager.SendVerificationEmail(ctx, &emailmanager.SendVerificationEmailRequest{
		FirstName: "John",
		// Missing LastName
		Email: "john@example.com",
		Token: "token",
	})

	if err != nil {
		fmt.Printf("Error (expected): %v\n", err)
		// Handle error appropriately
		return
	}

	fmt.Println("This shouldn't print")
}

// Example 8: Dashboard/Admin emails
func ExampleDashboardEmails() {
	manager := createExampleManager()

	ctx := context.Background()

	// Send verification email for dashboard access
	err := manager.SendVerificationEmail(ctx, &emailmanager.SendVerificationEmailRequest{
		FirstName:          "Admin",
		LastName:           "User",
		Email:              "admin@example.com",
		Token:              "admin-verification-token",
		IsDashboardRequest: true, // Important for dashboard
		RequestUrl:         "/admin",
		UserId:             "admin-123",
	})

	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("Dashboard verification email sent")

	// Send login email for dashboard
	err = manager.SendLoginEmail(ctx, &emailmanager.SendLoginEmailRequest{
		Email:              "admin@example.com",
		Token:              "admin-login-token",
		IsDashboardRequest: true, // Important for dashboard
		RequestUrl:         "/admin/dashboard",
		UserId:             "admin-123",
	})

	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("Dashboard login email sent")
}

// Example 9: Batch email sending
func ExampleBatchEmailSending() {
	manager := createExampleManager()
	ctx := context.Background()

	users := []struct {
		firstName string
		lastName  string
		email     string
		userId    string
	}{
		{"John", "Doe", "john@example.com", "user-1"},
		{"Jane", "Smith", "jane@example.com", "user-2"},
		{"Bob", "Johnson", "bob@example.com", "user-3"},
	}

	successCount := 0
	failureCount := 0

	for _, user := range users {
		err := manager.SendVerificationEmail(ctx, &emailmanager.SendVerificationEmailRequest{
			FirstName: user.firstName,
			LastName:  user.lastName,
			Email:     user.email,
			Token:     "token-" + user.userId,
			UserId:    user.userId,
		})

		if err != nil {
			fmt.Printf("Failed to send to %s: %v\n", user.email, err)
			failureCount++
		} else {
			fmt.Printf("Sent verification email to %s\n", user.email)
			successCount++
		}
	}

	fmt.Printf("\nBatch complete: %d succeeded, %d failed\n", successCount, failureCount)
}

// Example 10: Pre-rendered email sending
func ExampleSendPreRenderedEmail() {
	manager := createExampleManager()

	ctx := context.Background()
	err := manager.SendEmail(ctx, &emailmanager.SendEmailRequest{
		To:            "user@example.com",
		From:          "noreply@example.com",
		ReplyTo:       "support@example.com",
		Subject:       "Pre-rendered Email",
		HTMLBody:      "<html><body><h1>Hello!</h1><p>This is a pre-rendered email.</p></body></html>",
		UserId:        "user-123",
		RecipientType: string(audit.User),
	})

	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("Pre-rendered email sent successfully")
}

// Example 11: Environment-based configuration
func ExampleEnvironmentBasedConfig() {
	environment := "staging" // Could come from env var

	var provider emailprovider.EmailProvider
	var shouldSend bool

	switch environment {
	case "production":
		sparkpostClient := &sp.Client{ /* ... */ }
		provider = emailprovider.NewSparkPostEmailProvider(sparkpostClient)
		shouldSend = true
	case "staging":
		sparkpostClient := &sp.Client{ /* ... */ }
		provider = emailprovider.NewSparkPostEmailProvider(sparkpostClient)
		shouldSend = true
	case "development":
		provider = emailprovider.NewLoggingEmailProvider(nil)
		shouldSend = true // Will log instead of send
	default:
		provider = emailprovider.NewLoggingEmailProvider(nil)
		shouldSend = false
	}

	templaterConfig := emailtemplater.ExampleConfig()
	templaterConfig.Environment = environment
	tmplr, _ := emailtemplater.NewEmailTemplater(templaterConfig)

	managerConfig := &emailmanager.Config{
		ShouldSendEmail:    shouldSend,
		EnableAuditLogging: environment != "development",
	}

	manager := emailmanager.NewEmailManager(tmplr, provider, &MockAuditService{}, managerConfig)

	_ = manager // Manager created for demonstration
	fmt.Printf("Manager configured for environment: %s\n", environment)
	fmt.Printf("Provider: %s\n", provider.Name())
	fmt.Printf("Will send: %v\n", shouldSend)
}

// Helper function to create an example manager with example configuration
func createExampleManager() *emailmanager.EmailManager {
	templaterConfig := emailtemplater.ExampleConfig()
	tmplr, _ := emailtemplater.NewEmailTemplater(templaterConfig)

	provider := emailprovider.NewLoggingEmailProvider(&emailprovider.LoggingEmailProviderConfig{
		DisableFullHtmlBodyPreview: true,
	})

	auditService := &MockAuditService{}

	managerConfig := &emailmanager.Config{
		ShouldSendEmail:    true,
		EnableAuditLogging: true,
	}

	return emailmanager.NewEmailManager(tmplr, provider, auditService, managerConfig)
}
