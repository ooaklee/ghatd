package examples

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/ooaklee/ghatd/external/emailtemplater"
	"github.com/ooaklee/ghatd/external/emailtemplater/templates"
)

// Example 1: Basic templater usage with default example config
func ExampleTemplaterBasicUsage() {
	config := emailtemplater.ExampleConfig()
	t, _ := emailtemplater.NewEmailTemplater(config)

	// Generate a verification email
	rendered, err := t.GenerateVerificationEmail(context.Background(), &emailtemplater.GenerateVerificationEmailRequest{
		FirstName:          "John",
		LastName:           "Doe",
		Email:              "john@example.com",
		Token:              "verification-token-123",
		IsDashboardRequest: false,
		RequestUrl:         "",
	})

	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Generated email to: %s\n", rendered.To)
	fmt.Printf("Subject: %s\n", rendered.Subject)
	fmt.Printf("HTML Body length: %d bytes\n", len(rendered.HTMLBody))
}

// Example 2: Templater with production config
func ExampleTemplaterProductionConfig() {
	config := &emailtemplater.Config{
		FrontEndDomainName:            "https://app.example.com",
		DashboardDomainName:           "https://dashboard.example.com",
		EmailVerificationFullEndpoint: "https://app.example.com/v0/auth/verify",
		DashboardVerificationURIPath:  "/v0/auth/verify",
		Environment:                   "production",
		BusinessEntityName:            "MyApp Inc.",
		BusinessEntityWebsite:         "https://example.com",
		WelcomeEmailSubject:           "Welcome to MyApp!",
		LoginEmailSubject:             "Your MyApp Login Link",
		FromEmailAddress:              "noreply@example.com",
		NoReplyEmailAddress:           "noreply@example.com",
		Templates: map[emailtemplater.EmailTemplateType]string{
			// Using provided login template, could also make your own
			emailtemplater.EmailTemplateTypeLogin: templates.NewLoginEmailTemplate(
				time.Now().Year(),
				"MyApp Inc.",
				"https://example.com",
			),
		},
	}

	t, _ := emailtemplater.NewEmailTemplater(config)

	// Generate a login email
	rendered, err := t.GenerateLoginEmail(context.Background(), &emailtemplater.GenerateLoginEmailRequest{
		Email:              "user@example.com",
		Token:              "login-token-456",
		IsDashboardRequest: false,
		RequestUrl:         "/dashboard",
	})

	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Login email generated for: %s\n", rendered.To)
	fmt.Printf("From: %s\n", rendered.From)
	fmt.Printf("Subject: %s\n", rendered.Subject)
}

// Example 3: Generating custom emails from base template
func ExampleTemplaterCustomEmail() {
	config := &emailtemplater.Config{
		FrontEndDomainName:            "https://app.customapp.com",
		DashboardDomainName:           "https://dashboard.customapp.com",
		EmailVerificationFullEndpoint: "https://app.example.com/v0/auth/verify",
		DashboardVerificationURIPath:  "/v0/auth/verify",
		Environment:                   "production",
		BusinessEntityName:            "CustomApp Inc.",
		BusinessEntityWebsite:         "https://customapp.com",
		WelcomeEmailSubject:           "Welcome to CustomApp!",
		LoginEmailSubject:             "Your CustomApp Login Link",
		FromEmailAddress:              "noreply@customapp.com",
		NoReplyEmailAddress:           "noreply@customapp.com",
		DynamicTemplates: map[emailtemplater.EmailTemplateType]func(emailPreview string, emailSubject string, emailMainContent string, footerEnabled bool, footerYear int, footerEntityName string, footerEntityUrl string) string{
			emailtemplater.EmailTemplateTypeBase: templates.NewBaseHtmlEmailTemplate,
		},
	}

	t, _ := emailtemplater.NewEmailTemplater(config)

	// Custom email body (must be wrapped in <td> tags)
	customBody := `
<td style="font-family: sans-serif; font-size: 14px; vertical-align: top;">
	<p style="font-family: sans-serif; font-size: 14px; font-weight: normal; margin: 0; Margin-bottom: 15px;">
		Hello there!
	</p>
	<p style="font-family: sans-serif; font-size: 14px; font-weight: normal; margin: 0; Margin-bottom: 15px;">
		This is a custom notification email.
	</p>
	<table border="0" cellpadding="0" cellspacing="0" class="btn btn-primary" style="border-collapse: separate; mso-table-lspace: 0pt; mso-table-rspace: 0pt; width: 100%; box-sizing: border-box;">
		<tbody>
			<tr>
				<td align="left" style="font-family: sans-serif; font-size: 14px; vertical-align: top; padding-bottom: 15px;">
					<table border="0" cellpadding="0" cellspacing="0" style="border-collapse: separate; mso-table-lspace: 0pt; mso-table-rspace: 0pt; width: auto;">
						<tbody>
							<tr>
								<td style="font-family: sans-serif; font-size: 14px; vertical-align: top; background-color: #000000; border-radius: 5px; text-align: center;">
									<a href="https://customapp.com/action" title="Take Action" target="_blank" style="display: inline-block; color: #ffffff; background-color: #000000; border: solid 1px #000000; border-radius: 5px; box-sizing: border-box; cursor: pointer; text-decoration: none; font-size: 14px; font-weight: bold; margin: 0; padding: 12px 25px; text-transform: capitalize; border-color: #000000;">Take Action</a>
								</td>
							</tr>
						</tbody>
					</table>
				</td>
			</tr>
		</tbody>
	</table>
</td>`

	rendered, err := t.GenerateFromBaseTemplate(context.Background(), &emailtemplater.GenerateFromBaseTemplateRequest{
		EmailSubject:         "Important Notification",
		EmailPreview:         "You have a new notification",
		EmailBody:            customBody,
		EmailTo:              "user@example.com",
		OverrideEmailFrom:    "", // Use default
		OverrideEmailReplyTo: "", // Use default
		WithFooter:           true,
	})

	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Custom email generated\n")
	fmt.Printf("To: %s\n", rendered.To)
	fmt.Printf("Subject: %s\n", rendered.Subject)
}

// Example 4: Dashboard verification email
func ExampleTemplaterDashboardEmail() {
	config := &emailtemplater.Config{
		FrontEndDomainName:            "https://app.example.com",
		DashboardDomainName:           "https://dashboard.example.com",
		EmailVerificationFullEndpoint: "https://app.example.com/v0/auth/verify",
		DashboardVerificationURIPath:  "/v0/auth/verify",
		Environment:                   "staging",
		BusinessEntityName:            "Admin Portal",
		BusinessEntityWebsite:         "https://example.com",
		WelcomeEmailSubject:           "Verify Your Admin Account",
		FromEmailAddress:              "admin@example.com",
		NoReplyEmailAddress:           "noreply@example.com",
		Templates: map[emailtemplater.EmailTemplateType]string{
			// Using provided verification template, or you could also make your own
			emailtemplater.EmailTemplateTypeVerification: templates.NewVerificationEmailTemplate(
				time.Now().Year(),
				"Admin Portal",
				"https://example.com",
			),
		},
	}

	t, _ := emailtemplater.NewEmailTemplater(config)

	rendered, err := t.GenerateVerificationEmail(context.Background(), &emailtemplater.GenerateVerificationEmailRequest{
		FirstName:          "Admin",
		LastName:           "User",
		Email:              "admin@example.com",
		Token:              "admin-verification-token",
		IsDashboardRequest: true, // Important: set to true for dashboard
		RequestUrl:         "/admin/dashboard",
	})

	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Dashboard verification email generated\n")
	fmt.Printf("Subject includes environment tag: %s\n", rendered.Subject)
}

// Example 5: Testing template generation
func ExampleTemplaterTesting() {
	config := emailtemplater.ExampleConfig()
	config.Environment = "test"

	t, _ := emailtemplater.NewEmailTemplater(config)

	// Generate multiple emails for testing
	testCases := []struct {
		name      string
		firstName string
		lastName  string
		email     string
	}{
		{"Standard User", "John", "Doe", "john@example.com"},
		{"User with Long Name", "Christopher", "Montgomery", "chris@example.com"},
		{"User with Special Chars", "François", "O'Brien", "francois@example.com"},
	}

	for _, tc := range testCases {
		rendered, err := t.GenerateVerificationEmail(context.Background(), &emailtemplater.GenerateVerificationEmailRequest{
			FirstName:          tc.firstName,
			LastName:           tc.lastName,
			Email:              tc.email,
			Token:              "test-token-123",
			IsDashboardRequest: false,
		})

		if err != nil {
			log.Printf("Failed to generate email for %s: %v\n", tc.name, err)
			continue
		}

		fmt.Printf("✓ Generated email for %s (%s)\n", tc.name, rendered.To)
		fmt.Printf("  Subject: %s\n", rendered.Subject)
	}
}

// Example 6: Validation errors
func ExampleTemplaterValidation() {
	config := emailtemplater.ExampleConfig()
	t, _ := emailtemplater.NewEmailTemplater(config)

	// This will fail validation - missing required fields
	_, err := t.GenerateVerificationEmail(context.Background(), &emailtemplater.GenerateVerificationEmailRequest{
		FirstName: "John",
		// Missing LastName
		Email: "john@example.com",
		Token: "token-123",
	})

	if err != nil {
		fmt.Printf("Validation error (expected): %v\n", err)
	}

	// This will also fail - missing email
	_, err = t.GenerateLoginEmail(context.Background(), &emailtemplater.GenerateLoginEmailRequest{
		// Missing Email
		Token:              "token-456",
		IsDashboardRequest: false,
	})

	if err != nil {
		fmt.Printf("Validation error (expected): %v\n", err)
	}
}

// Example 7: Custom time provider for testing
func ExampleTemplaterCustomTimeProvider() {
	config := emailtemplater.ExampleConfig()

	// Override time provider for deterministic testing
	config.TimeProvider = func() time.Time {
		return time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	}

	t, _ := emailtemplater.NewEmailTemplater(config)

	// Year in footer will be 2025
	rendered, err := t.GenerateFromBaseTemplate(context.Background(), &emailtemplater.GenerateFromBaseTemplateRequest{
		EmailSubject: "Test Email",
		EmailPreview: "Testing custom time",
		EmailBody:    `<td>Test content</td>`,
		EmailTo:      "test@example.com",
		WithFooter:   true,
	})

	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Generated with custom time provider\n")
	fmt.Printf("Year used: %d\n", config.GetCurrentYear())
	fmt.Printf("Email to: %s\n", rendered.To)
}
