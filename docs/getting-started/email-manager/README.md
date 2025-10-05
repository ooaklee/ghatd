# Getting Started With Email Manager

The recommended email functionality comes in the form of three independent, composable packages: [**emailtemplater**](../../../external/emailtemplater/), [**emailprovider**](../../../external/emailprovider/), and [**emailmanager](../../../external/emailmanager/). For most application features, you should use the high-level `emailmanager` package, which handles both templating and sending with integrated audit logging. 

## Core Packages Overview

See below an overview of the core packages mentioned above


|Package |	Purpose	Use Case (Recommended) | Examples Location |
|**`emailtemplater`** |	Generates HTML email templates (e.g., login, verification, and custom) with variable substitution.	Generating email previews or testing template rendering.	| [external/emailtemplater/examples](../../../external/emailtemplater/examples/examples.go) |
|**`emailprovider`**	| Abstracts the logic for sending an email through a service (e.g., SparkPost).	Sending pre-rendered HTML or custom email workflows.	| [external/emailprovider/examples](../../../external/emailprovider/examples/examples.go) |
|**`emailmanager`**	| Orchestrates the templater and emailprovider with high-level API methods.	Building application features (Standard)—provides the full workflow and audit logging.	| [external/emailmanager/examples](../../../external/emailmanager/examples/examples.go) |

### Usage Overview

For a high-level overview of how this might fit into your GHAT(D) project, please [**visit this section**.](#high-level-overview).

## Quick Start: Setup and Sending

In the following section we'll demonstrate how to set up the `emailmanager` and send a verification email, which is the recommended way to use the  system for standard application operations, for more examples [please check out the reference examples above](#core-packages-overview).

### 1. Import Packages and Configure

You'll need configuration for the `emailtemplater`, an `emailprovider` instance, and an [`audit` service](../../../external/audit/).

```go
import (
    "context"
    "github.com/ooaklee/ghatd/external/templater"
    "github.com/ooaklee/ghatd/external/emailprovider"
    "github.com/ooaklee/ghatd/external/emailmanager"
)

// Assume sparkpostClient and auditService are initialised dependencies

// 1. Configure templater
templaterConfig := &emailtemplater.Config{
		FrontEndDomainName:            "https://app.example.com",
		EmailVerificationFullEndpoint: "https://app.example.com/v0/auth/verify",
		DashboardDomainName:           "https://app.example.com",
		DashboardVerificationURIPath:  "/v0/auth/verify",
		Environment:                   "production",
		BusinessEntityName:            "MyApp Inc.",
		BusinessEntityWebsite:         "https://example.com",
		WelcomeEmailSubject:           "Welcome to MyApp!",
		LoginEmailSubject:             "Your MyApp Login Link",
		FromEmailAddress:              "noreply@example.com",
		NoReplyEmailAddress:           "noreply@example.com",
		TimeProvider:                  time.Now,
		Templates: map[emailtemplater.EmailTemplateType]string{
			emailtemplater.EmailTemplateTypeLogin:        templates.NewLoginEmailTemplate(time.Now().Year(), "MyApp Inc.", "https://example.com"),
			emailtemplater.EmailTemplateTypeVerification: templates.NewVerificationEmailTemplate(time.Now().Year(), "MyApp Inc.", "https://example.com"),
		},
		DynamicTemplates: map[emailtemplater.EmailTemplateType]func(emailPreview string, emailSubject string, emailMainContent string, footerEnabled bool, footerYear int, footerEntityName string, footerEntityUrl string) string{
			emailtemplater.EmailTemplateTypeBase: templates.NewBaseHtmlEmailTemplate,
		},
	}
tmpltr := templater.NewEmailTemplater(templaterConfig)

// 2. Create email provider (using SparkPost for Production)
provider := emailprovider.NewSparkPostEmailProvider(sparkpostClient)

// 3. Create email manager (Orchestration layer)
manager := emailmanager.NewEmailManager(tmpltr, provider, auditService, &emailmanager.Config{
    ShouldSendEmail:    true,         // Allows sending
    EnableAuditLogging: true,         // Logs email metadata
})
```

### 2. Send an Email

You'll be able to use the high-level methods on the `emailmanager`.

```go
// 4. Send a verification email
ctx := context.Background()
err := manager.SendVerificationEmail(ctx, &emailmanager.SendVerificationEmailRequest{
    FirstName:          "John",
    LastName:           "Doe",
    Email:              "john@example.com",
    Token:              "verification-token-xyz",
    IsDashboardRequest: false,
    RequestUrl:         "https://app.example.com/dashboard",
    UserId:             "user-123",
})

if err != nil {
    // Handle error, e.g., errors.New(emailmanager.ErrKeyEmailMailerSendFailed) or errors.New(emailmanager.ErrKeyEmailMailerTemplateGenerationFailed)
}
```

### 3. Development Environment Setup

If you don't want to use your email providers allowance when running your code locally, you can also leverage the `LoggingEmailProvider` to log the email content instead of actually sending it via an external API like `SparkPost`.


```go
// Use logging provider to see emails in logs instead of sending
provider := emailprovider.NewLoggingEmailProvider(&emailprovider.LoggingEmailProviderConfig{
    DisableFullHtmlBodyPreview: false, // The login and verification token by default contain the token/ magic link url for you to sign in
}) 

manager := emailmanager.NewEmailManager(
    templater.NewEmailTemplater(templaterConfig),
    provider,
    auditService,
    &emailmanager.Config{
        ShouldSendEmail:    true, // The LoggingProvider logs despite this being true
        EnableAuditLogging: true,
    },
)
```

> **Note on Environments:** The `emailtemplater` is also **environment-aware**; for example, setting the `Environment` config to `"staging"` will add `[staging]` to the email subject line.


## Advanced Use Cases

While `emailmanager` is recommended, the packages can be used independently for specialised needs.

### Template Only (e.g., Email Preview)

You can generate the HTML body without sending an email.


```go
// Use templater alone
rendered, err := tmpltr.GenerateVerificationEmail(&templater.GenerateVerificationEmailRequest{
    FirstName: "Test",
    // ...
})
// Use rendered.HTMLBody for preview or external systems
```

### Custom Provider

Adding a new provider (e.g., SendGrid, AWS SES) only requires implementing the `emailprovider.EmailProvider` interface and integrating it with the `emailmanager`.


```go
type MyCustomProvider struct{}

func (p *MyCustomProvider) Send(ctx context.Context, email *emailprovider.Email) (*emailprovider.SendResult, error) {
    // Custom sending logic here
    return &emailprovider.SendResult{Success: true}, nil
}

func (p *MyCustomProvider) Name() string {
    return "CUSTOM_PROVIDER"
}

func (p *MyCustomProvider) IsHealthy() bool {
    var isHealthy bool
    // Custom health check logic here and update isHealthy
    return isHealthy
}

// Use it with the manager
provider := &MyCustomProvider{}
manager := emailmanager.NewEmailManager(tmpl, provider, audit, config)
```

## High-level Overview

See below high-level overviews of this email solution (and its component packages) and a few examples of how it can be used in your GHATD application or different use-cases.

### Usage Patterns

#### Pattern 1: Full Stack (Recommended for Applications)

```
Application Code
       │
       ▼
   emailmanager ──────► Handles everything
       │
       ├──► emailtemplater ──► Generates HTML
       │
       ├──► emailprovider ──► Sends email
       │
       └──► auditService ──► Logs events
```

#### Pattern 2: Template Only (For Previews/Testing)

```
Application Code
       │
       ▼
   emailtemplater ──────► Returns HTML
       │
       └──► No sending, just HTML generation
```

#### Pattern 3: Custom Workflow

```
Application Code
       │
       ├──► emailtemplater ──────► Generate HTML
       │         │
       │         ▼
       │    [Custom Logic]
       │         │
       │         ▼
       └──► emailprovider ──► Send when ready
```


### Deployment View

```
┌──────────────────────────────────────────────────────────────┐
│                      Production                              │
│                                                              │
│  ┌─────────────┐         ┌──────────────┐                    │
│  │ Application │────────►│ emailmanager │                    │
│  └─────────────┘         └──────┬───────┘                    │
│                                 │                            │ 
│                   ┌─────────────┼──────────────┐             │
│                   │             │              │             │
│                   ▼             ▼              ▼             │
│         ┌──────────────┐  ┌──────────┐  ┌──────────┐         │
│         │    email     │  │SparkPost │  │  Audit   │         │
│         |   templater  |  │ Provider │  │ Service  │         │
│         └──────────────┘  └────┬─────┘  └────┬─────┘         │
│                                │             │               │
└────────────────────────────────┼─────────────┼───────────────┘
                                 │             │
                                 ▼             ▼
                       ┌──────────────┐  ┌──────────┐
                       │  SparkPost   │  │ MongoDB  │
                       │     API      │  │          │
                       └──────────────┘  └──────────┘
┌──────────────────────────────────────────────────────────────┐
│                      Development                             │
│                                                              │
│  ┌─────────────┐         ┌──────────────┐                    │
│  │ Application │────────►│ emailmanager │                    │
│  └─────────────┘         └──────┬───────┘                    │
│                                 │                            │
│                   ┌─────────────┼──────────────┐             │
│                   │             │              │             │
│                   ▼             ▼              ▼             │
│         ┌──────────────┐  ┌──────────┐  ┌──────────┐         │
│         │    email     │  │ Logging  │  │  Mock    │         │
│         |   templater  |  │ Provider │  │  Audit   │         │
│         └──────────────┘  └────┬─────┘  └────┬─────┘         │
│                                │             │               │
└────────────────────────────────┼─────────────┼───────────────┘
                                 │             │
                                 ▼             ▼
                         ┌──────────────┐  ┌──────────┐
                         │   Console    │  │ Console  │
                         │   Logs       │  │  Logs    │
                         └──────────────┘  └──────────┘
```
