# Email Manager

The recommended email functionality comes in three independent, composable packages: `emailtemplater`, `emailprovider`, and `emailmanager`. For most application features, you should use the high-level `emailmanager` package, which handles both templating and sending with integrated audit logging.

## Core Packages Overview

Here's an overview of the core packages:

| Package | Purpose | Recommended Use Case | Examples |
|---|---|---|---|
| `emailtemplater` | Generates HTML email templates (e.g., login, verification) with variable substitution. | Generating email previews or testing template rendering. | [`emailtemplater/examples`](../emailtemplater/examples/examples.go) |
| `emailprovider` | Abstracts the logic for sending an email through a service (e.g., SparkPost). | Sending pre-rendered HTML or custom email workflows. | [`emailprovider/examples`](../emailprovider/examples/examples.go) |
| `emailmanager` | Orchestrates the templater and email provider with high-level API methods. | Building application features (Standard)—provides the full workflow and audit logging. | [`emailmanager/examples`](examples/examples.go) |

### Usage Overview

For a high-level overview of how this might fit into your project, please [**visit this section**](#high-level-overview).

Resolved Bird/Postmark/local account construction is optionally available in
[the composition helper](helper/README.md). Core routing and receipt ownership
remain in this package; hosts retain configuration, consent and templates.

## Quick Start: Setup and Sending

This section shows how to set up the `emailmanager` and send a verification email. This is the recommended way to use the system for standard operations. For more examples, [check out the reference examples above](#core-packages-overview).

For the standard GHATD host-application setup, prefer
`emailprovider.NewSparkPostClient` and `emailmanager.NewStandardEmailManager`.
They keep the common SparkPost client and email template wiring in the packages
that own those concepts.

```go
sparkpostClient, err := emailprovider.NewSparkPostClient(&emailprovider.NewSparkPostClientRequest{
    BaseURL:    sparkpostURL,
    APIKey:     sparkpostAPIKey,
    APIVersion: 1,
})
if err != nil {
    return err
}

provider := emailprovider.NewSparkPostEmailProvider(sparkpostClient)
manager, err := emailmanager.NewStandardEmailManager(&emailmanager.NewStandardEmailManagerRequest{
    Provider:                      provider,
    AuditService:                  auditService,
    FrontendBaseURL:               "https://app.example.com",
    EmailVerificationFullEndpoint: "https://api.example.com/v0/auth/verify",
    DashboardVerificationURIPath:  "https://api.example.com/v0/auth/verify",
    Environment:                   "production",
    BusinessEntityName:            "Example",
    BusinessEntityWebsite:         "https://example.com",
    WelcomeEmailSubject:           "Welcome",
    LoginEmailSubject:             "Your login link",
    FromEmailAddress:              "noreply@example.com",
    NoReplyEmailAddress:           "noreply@example.com",
})
if err != nil {
    return err
}
```

The lower-level setup remains available when a project needs custom templates.

### 1. Import Packages and Configure

You'll need configuration for the `emailtemplater`, an `emailprovider` instance, and an [`audit` service](../audit).

```go
import (
    "context"
    "github.com/ooaklee/ghatd/external/emailtemplater"
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
tmpltr := emailtemplater.NewEmailTemplater(templaterConfig)

// 2. Create email provider (using SparkPost for Production)
provider := emailprovider.NewSparkPostEmailProvider(sparkpostClient)

// 3. Create email manager (Orchestration layer)
manager := emailmanager.NewEmailManager(tmpltr, provider, auditService, &emailmanager.Config{
    ShouldSendEmail:    true,         // Allows sending
    EnableAuditLogging: true,         // Logs email metadata
})
```

### 2. Send an Email

You can use the high-level methods on the `emailmanager`.

```go
// 4. Send a verification email
ctx := context.Background()
err := manager.SendVerificationEmail(ctx, &emailmanager.SendVerificationEmailRequest{
    FirstName:          "John",
    LastName:           "Doe",
    Email:              "john@example.com",
    Token:              "verification-token-xyz",
    Code:               "ABC123DE", // 8-character alphanumeric code for manual entry
    IsDashboardRequest: false,
    RequestUrl:         "https://app.example.com/dashboard",
    UserId:             "user-123",
})
```

```go
// 5. Send a login email
ctx := context.Background()
err := manager.SendLoginEmail(ctx, &emailmanager.SendLoginEmailRequest{
    Email:              "john@example.com",
    Token:              "login-token-xyz",
    Code:               "XYZ789AB", // 8-character alphanumeric code for manual entry
    IsDashboardRequest: false,
    RequestUrl:         "https://app.example.com/dashboard",
    UserId:             "user-123",
})
```

> **Dual Verification Flow**: Both login and verification emails now include a magic link (token) AND an 8-character alphanumeric code. The code is displayed prominently below the main button with the note: _"Alternatively, enter this code in the app or web by clicking "I already have a session code""_. This allows users who open the email on a different device to manually enter the code instead of clicking the link.

### Template Substitution

The email templates use Handlebars (`{{FieldName}}`) for variable substitution. Available fields:

**Verification email:**
| Variable | Description |
|---|---|
| `{{FullName}}` | User's full name |
| `{{Code}}` | 8-character alphanumeric code |
| `{{VerificationURL}}` | Magic link with embedded token |
| `{{LoginURL}}` | Link to request a new verification email |

**Login email:**
| Variable | Description |
|---|---|
| `{{Code}}` | 8-character alphanumeric code |
| `{{LoginURL}}` | Magic link with embedded token |

### 3. Development Environment Setup

If you don't want to use your email provider's allowance when running your code locally, use the `LoggingEmailProvider` to capture emails in memory and attach the local inbox routes. The inbox lets you open rendered emails, click magic links, and copy login or verification codes without writing raw email HTML to structured logs.

```go
localEmailProvider := emailprovider.NewLoggingEmailProvider(&emailprovider.LoggingEmailProviderConfig{
    MaxStoredEmails: 50,
})

manager := emailmanager.NewEmailManager(
    emailtemplater.NewEmailTemplater(templaterConfig),
    localEmailProvider,
    auditService,
    &emailmanager.Config{
        ShouldSendEmail:    false,
        EnableAuditLogging: true,
    },
)

err := emailprovider.AttachLocalInboxRoutes(&emailprovider.AttachLocalInboxRoutesRequest{
    Router:   ghatdRouter,
    Provider: localEmailProvider,
})
if err != nil {
    return err
}
```

By default, the local inbox is available at `/_ghatd/local/emails` and rejects non-loopback clients. Set a custom `Prefix` or `AllowRemote` only when another trusted local proxy protects the route.

This local inbox workflow is described in [ADR014](../../docs/adr/adr014-local-email-inbox-for-development.md).

> **Note on Environments:** The `emailtemplater` is also **environment-aware**; for example, setting the `Environment` config to `"staging"` will add `[staging]` to the email subject line.

## Advanced Use Cases

While `emailmanager` is recommended, the packages can be used independently for specialised needs.

### Template Only (e.g., Email Preview)

You can generate the HTML body without sending an email.

```go
// Use templater alone
rendered, err := tmpltr.GenerateVerificationEmail(&emailtemplater.GenerateVerificationEmailRequest{
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

Here are some high-level overviews of this email solution and its packages, with examples of how it can be used in your application for different use-cases.

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

### Environment Usage & Outputs Flow

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
│                      Local Development                       │
│                                                              │
│  ┌─────────────┐         ┌──────────────┐                    │
│  │ Application │────────►│ emailmanager │                    │
│  └─────────────┘         └──────┬───────┘                    │
│                                 │                            │
│                   ┌─────────────┼──────────────┐             │
│                   │             │              │             │
│                   ▼             ▼              ▼             │
│         ┌──────────────┐  ┌──────────┐  ┌──────────┐         │
│         │    email     │  │ Logging  │  │  Audit   │         │
│         |   templater  |  │ Provider │  │ Service  │         │
│         └──────────────┘  └────┬─────┘  └────┬─────┘         │
│                                │             │               │
└────────────────────────────────┼─────────────┼───────────────┘
                                 │             │
                                 ▼             ▼
                         ┌──────────────┐  ┌──────────┐
                         │   Console    │  │ MongoDB  │
                         │   Logs       │  │          │
                         └──────────────┘  └──────────┘
```

## Purpose routing and submission receipts

`NewStandardEmailManagerRequest.Routing` opts into named provider instances.
Legacy `Provider`-only constructors and error-only methods remain supported.
Legacy generic sends default to transactional; routed generic/custom sends must
set a trusted `MailType`. Login and verification helpers always select
transactional. Purpose is never inferred from subject, recipient, request headers
or template content. Marketing consent remains the host's responsibility.

```go
bird := emailprovider.NewBirdEmailProvider(birdClient).
    WithMailTypePreference([]emailprovider.MailType{emailprovider.Transactional})
postmark := emailprovider.NewPostmarkEmailProvider(postmarkClient).
    WithMailTypePreference([]emailprovider.MailType{
        emailprovider.Marketing, emailprovider.Transactional,
    })
request.Routing = &emailmanager.RoutingConfig{
    Providers: []emailmanager.ProviderRegistration{
        {ID: "bird-primary", Provider: bird},
        {ID: "postmark-primary", Provider: postmark},
    },
}
manager, err := emailmanager.NewStandardEmailManager(request)
if err != nil { return err }
receipt, err := manager.SendCustomEmailWithResult(ctx,
    &emailmanager.SendCustomEmailRequest{
        MailType: emailprovider.Transactional,
        EmailTo: "recipient@example.test",
        EmailSubject: "Your confirmation",
        EmailBody: "<p>Your confirmation is ready.</p>",
        TextBody: "Your confirmation is ready.",
    })
```

The Postmark client in that example must configure a broadcast `MarketingStream`.
Preference never grants a capability. Bird supports transactional only. A custom
legacy provider without `MailTypeProvider` is treated as transactional-only in
routed mode. Decorate it with an explicit capability implementation to opt in
additional purposes. Configured instance IDs distinguish two accounts of the
same vendor; `Name()` is a vendor label, not an account identifier.

Selection first filters by supported purpose and operation. An explicit
`Routes[purpose]` selects exactly that configured instance. Otherwise the lowest
position of the requested purpose in each provider's preference list wins.
Capability-only providers (including those with empty preferences) rank after
providers explicitly preferring the purpose. Ties use round-robin in registration
order, with separate counters per purpose and operation. Every selection consumes
one turn, including a failed attempt; it never resubmits or changes provider
within that operation. If no candidate supports the purpose/operation, selection
fails. Duplicate/unknown preferences, unsupported preferred purposes, duplicate
IDs and invalid explicit routes fail construction.

`DefaultProviderID` is a transactional-only default when no explicit route or
matching ranked preference exists. Marketing does not silently reuse that
default. No failure, cancellation, invalid configured route or uncertain timeout
causes fallback to another account. Configuration and input slices/maps are
snapshotted at construction; provider/client objects must remain immutable.

`SendEmailWithResult`, `SendCustomEmailWithResult`, `SendLoginEmailWithResult` and
`SendVerificationEmailWithResult` expose a `SendReceipt`:

| State | Meaning |
| --- | --- |
| `skipped` | Global sending is disabled; no external submission occurred. |
| `captured` | Stored in the local inbox; MessageID is a local capture ID. |
| `accepted` | The selected provider acknowledged submission, not recipient delivery. |
| `failed` | Preflight or a known rejection prevented confirmed acceptance. |
| `uncertain` | Submission may have occurred; reconcile without blindly resending. |

An error-only method returning nil may mean captured or skipped. Use receipts
when durable delivery state needs that distinction. Legacy adapters with no
explicit state classify errors conservatively as uncertain. No adapter or
manager claims remote idempotency from an in-memory selection or local job key.
Lookup, authenticated delivery callbacks and durable outbox orchestration remain
separate concerns.

For local development set `Routing.LocalCapture` to the same
`LoggingEmailProvider` whose store is attached to `/_ghatd/local/emails`. It
intercepts every selected route before any external call, even if
`ShouldSendEmail` is true. With global sending disabled and no local capture,
external operations are skipped. Captures retain selected instance, vendor and
purpose on the inbox list, details and JSON API. This tests routing and template
behavior, not vendor authentication or delivery.

`ProviderRegistration.Campaign` is a separate optional `CampaignProvider` port.
`SubmitCampaign` selects marketing-capable audience operations rather than
calling `EmailProvider.Send`. Inline marketing capability does not imply a
campaign API. Disabled/local campaign operations never create remote audiences
or campaigns; capable operations are skipped. The host must validate consent and
audience eligibility before submission. This port has no campaign scheduler or
provider administration surface.

The selected instance ID is recorded only for configured registrations. Legacy
single-provider receipts leave `ProviderID` empty. When audit logging is enabled,
all submission outcomes record purpose, vendor, instance and state. `SentAt` is
populated only for accepted/captured submissions; it does not prove delivery.

Use `manager.ProviderForMailType(emailprovider.Marketing)` when composing an
existing consumer that takes an `EmailProvider` directly. The adapter binds a
trusted startup purpose and routes through the same manager and local inbox;
caller-supplied message purpose cannot override it.

An explicit marketing route may support inline messages without audience
operations. `SubmitCampaign` then returns `ErrCapabilityUnavailable` and does
not select another account. Invalid requests return the send-failure sentinel.
None of the built-in inline adapters implements a campaign port.
