package examples

import (
	"context"
	"errors"
	"fmt"
	"log"

	sp "github.com/SparkPost/gosparkpost"
	"github.com/ooaklee/ghatd/external/emailprovider"
)

// Example 1: Using SparkPost provider
func ExampleSparkPostProvider() {
	// Initialise SparkPost client
	sparkpostClient := &sp.Client{
		Config: &sp.Config{
			BaseUrl:    "https://api.sparkpost.com",
			ApiKey:     "your-sparkpost-api-key",
			ApiVersion: 1,
		},
	}

	provider := emailprovider.NewSparkPostEmailProvider(sparkpostClient)

	// Create email
	email := &emailprovider.Email{
		To:       "user@example.com",
		From:     "noreply@example.com",
		ReplyTo:  "support@example.com",
		Subject:  "Welcome to Our Service",
		HTMLBody: "<html><body><h1>Welcome!</h1><p>Thanks for signing up.</p></body></html>",
	}

	// Send email
	ctx := context.Background()
	result, err := provider.Send(ctx, email)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Email sent via %s\n", result.Provider)
	fmt.Printf("Message ID: %s\n", result.MessageID)
	fmt.Printf("Success: %v\n", result.Success)
}

// Example 2: Using Logging provider for development
func ExampleLoggingProvider() {
	provider := emailprovider.NewLoggingEmailProvider(nil)

	email := &emailprovider.Email{
		To:       "dev@example.com",
		From:     "noreply@example.com",
		ReplyTo:  "noreply@example.com",
		Subject:  "Development Test Email",
		HTMLBody: "<html><body>This email is logged, not sent</body></html>",
	}

	ctx := context.Background()
	result, err := provider.Send(ctx, email)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Email logged via %s\n", result.Provider)
	fmt.Printf("Message ID: %s\n", result.MessageID)
}

// Example 3: Provider health check
func ExampleProviderHealthCheck() {
	sparkpostClient := &sp.Client{ /* ... */ }
	provider := emailprovider.NewSparkPostEmailProvider(sparkpostClient)

	ctx := context.Background()
	if provider.IsHealthy(ctx) {
		fmt.Printf("Provider %s is healthy\n", provider.Name())
	} else {
		fmt.Printf("Provider %s is not healthy\n", provider.Name())
	}
}

// Example 4: Configuration-based provider selection
func ExampleProviderSelection() {
	config := emailprovider.DefaultConfig()
	config.Environment = "production"
	config.EnableSendingEmail = true

	var provider emailprovider.EmailProvider

	if config.ShouldSendEmail() {
		// Use real email provider in production
		sparkpostClient := &sp.Client{ /* ... */ }
		provider = emailprovider.NewSparkPostEmailProvider(sparkpostClient)
		fmt.Println("Using SparkPost provider")
	} else {
		// Use logging provider in development
		provider = emailprovider.NewLoggingEmailProvider(nil)
		fmt.Println("Using Logging provider")
	}

	fmt.Printf("Selected provider: %s\n", provider.Name())
}

// Example 5: Error handling
func ExampleProviderErrorHandling() {
	provider := emailprovider.NewLoggingEmailProvider(nil)

	// Invalid email (missing recipient)
	invalidEmail := &emailprovider.Email{
		To:       "", // Missing!
		From:     "noreply@example.com",
		Subject:  "Test",
		HTMLBody: "<html><body>Test</body></html>",
	}

	ctx := context.Background()
	result, err := provider.Send(ctx, invalidEmail)

	if err != nil {
		fmt.Printf("Error occurred: %v\n", err)
		fmt.Printf("Result success: %v\n", result.Success)
		fmt.Printf("Result error: %v\n", result.Error)
	}
}

// Example 6: Custom provider implementation
type CustomEmailProvider struct {
	apiKey string
}

func NewCustomEmailProvider(apiKey string) *CustomEmailProvider {
	return &CustomEmailProvider{apiKey: apiKey}
}

func (p *CustomEmailProvider) Send(ctx context.Context, email *emailprovider.Email) (*emailprovider.SendResult, error) {
	// Validate email first
	if email.To == "" {
		return &emailprovider.SendResult{
			Provider: p.Name(),
			Success:  false,
			Error:    errors.New(emailprovider.ErrKeyEmailProviderMissingRecipient),
		}, errors.New(emailprovider.ErrKeyEmailProviderMissingRecipient)
	}

	// Your custom sending logic here
	fmt.Printf("Sending email via custom provider to: %s\n", email.To)

	// Simulate successful send
	return &emailprovider.SendResult{
		MessageID: "custom-msg-123",
		Provider:  p.Name(),
		Success:   true,
		Error:     nil,
	}, nil
}

func (p *CustomEmailProvider) Name() string {
	return "CUSTOM"
}

func (p *CustomEmailProvider) IsHealthy(ctx context.Context) bool {
	// Your health check logic
	return p.apiKey != ""
}

func ExampleCustomProvider() {
	provider := NewCustomEmailProvider("api-key-123")

	email := &emailprovider.Email{
		To:       "user@example.com",
		From:     "noreply@example.com",
		ReplyTo:  "noreply@example.com",
		Subject:  "Test from Custom Provider",
		HTMLBody: "<html><body>Hello!</body></html>",
	}

	ctx := context.Background()
	result, err := provider.Send(ctx, email)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Sent via custom provider: %s\n", result.Provider)
}

// Example 7: Environment-specific configuration
func ExampleEnvironmentSpecificProvider() {
	type EnvironmentConfig struct {
		Name               string
		EnableSendingEmail bool
	}

	environments := map[string]EnvironmentConfig{
		"development": {"development", false},
		"staging":     {"staging", true},
		"production":  {"production", true},
	}

	currentEnv := "staging"
	envConfig := environments[currentEnv]

	var provider emailprovider.EmailProvider

	if envConfig.EnableSendingEmail {
		sparkpostClient := &sp.Client{ /* ... */ }
		provider = emailprovider.NewSparkPostEmailProvider(sparkpostClient)
	} else {
		provider = emailprovider.NewLoggingEmailProvider(nil)
	}

	fmt.Printf("Environment: %s\n", currentEnv)
	fmt.Printf("Provider: %s\n", provider.Name())
	fmt.Printf("Will send emails: %v\n", envConfig.EnableSendingEmail)
}

// Example 8: Batch email sending
func ExampleBatchEmailSending() {
	provider := emailprovider.NewLoggingEmailProvider(nil)
	ctx := context.Background()

	recipients := []string{
		"user1@example.com",
		"user2@example.com",
		"user3@example.com",
	}

	baseEmail := &emailprovider.Email{
		From:     "noreply@example.com",
		ReplyTo:  "noreply@example.com",
		Subject:  "Batch Notification",
		HTMLBody: "<html><body>This is a batch email</body></html>",
	}

	successCount := 0
	failureCount := 0

	for _, recipient := range recipients {
		email := *baseEmail // Copy
		email.To = recipient

		result, err := provider.Send(ctx, &email)
		if err != nil {
			fmt.Printf("Failed to send to %s: %v\n", recipient, err)
			failureCount++
		} else {
			fmt.Printf("Sent to %s (ID: %s)\n", recipient, result.MessageID)
			successCount++
		}
	}

	fmt.Printf("\nBatch complete: %d succeeded, %d failed\n", successCount, failureCount)
}
