// Package emailmanager provides email sending functionality with template
// support and integration with various email service providers.
//
// The package abstracts email provider details and provides a consistent
// interface for sending transactional and marketing emails.
package emailmanager

import (
	"context"

	"github.com/ooaklee/ghatd/external/audit"
	"github.com/ooaklee/ghatd/external/emailprovider"
	"github.com/ooaklee/ghatd/external/emailtemplater"
	"github.com/ooaklee/ghatd/external/logger"
	"github.com/ooaklee/ghatd/external/toolbox"
	"go.uber.org/zap"
)

// emailTemplater is the interface that represents the templater used to generate email content
type emailTemplater interface {
	GenerateVerificationEmail(ctx context.Context, req *emailtemplater.GenerateVerificationEmailRequest) (*emailtemplater.RenderedEmail, error)
	GenerateLoginEmail(ctx context.Context, req *emailtemplater.GenerateLoginEmailRequest) (*emailtemplater.RenderedEmail, error)
	GenerateFromBaseTemplate(ctx context.Context, req *emailtemplater.GenerateFromBaseTemplateRequest) (*emailtemplater.RenderedEmail, error)
}

// EmailManager orchestrates email templating and sending
type EmailManager struct {
	templater    emailTemplater
	provider     emailprovider.EmailProvider
	auditService AuditService
	config       *Config
	router       *providerRouter
}

// Config holds configuration for the email manager
type Config struct {
	// ShouldSendEmail determines if emails should actually be sent or just logged
	ShouldSendEmail bool

	// EnableAuditLogging determines if audit events should be logged
	EnableAuditLogging bool
}

type localOutputProvider interface {
	IsLocalOutputProvider() bool
}

// DefaultConfig returns a config with sensible defaults
func DefaultConfig() *Config {
	return &Config{
		ShouldSendEmail:    true,
		EnableAuditLogging: true,
	}
}

func isLocalOutputProvider(provider emailprovider.EmailProvider) bool {
	localProvider, ok := provider.(localOutputProvider)
	return ok && localProvider.IsLocalOutputProvider()
}

// NewEmailManager creates a new email manager
func NewEmailManager(templater emailTemplater, provider emailprovider.EmailProvider, auditService AuditService, config *Config) *EmailManager {
	if config == nil {
		config = DefaultConfig()
	}

	configCopy := *config
	return &EmailManager{
		templater:    templater,
		provider:     provider,
		auditService: auditService,
		config:       &configCopy,
	}
}

// SendVerificationEmail sends a verification email
func (m *EmailManager) SendVerificationEmail(ctx context.Context, req *SendVerificationEmailRequest) error {
	_, err := m.SendVerificationEmailWithResult(ctx, req)
	return err
}

// SendVerificationEmailWithResult always selects the transactional route.
func (m *EmailManager) SendVerificationEmailWithResult(ctx context.Context, req *SendVerificationEmailRequest) (*SendReceipt, error) {
	if ctx == nil || req == nil {
		return nil, ErrEmailMailerTemplateGenerationFailed
	}
	logger := logger.AcquireOperationFrom(ctx, "external/emailmanager", "send-verification-email")
	logger.Debug("handling-send-verification-email-request")

	// Generate template
	templateReq := &emailtemplater.GenerateVerificationEmailRequest{
		FirstName:          req.FirstName,
		LastName:           req.LastName,
		Email:              req.Email,
		Token:              req.Token,
		Code:               req.Code,
		IsDashboardRequest: req.IsDashboardRequest,
		RequestUrl:         req.RequestUrl,
	}

	rendered, err := m.templater.GenerateVerificationEmail(ctx, templateReq)
	if err != nil {
		return nil, ErrEmailMailerTemplateGenerationFailed
	}

	// Send email
	emailInfo := &EmailInfo{
		To:            rendered.To,
		From:          rendered.From,
		Subject:       rendered.Subject,
		EmailProvider: "",
		UserId:        req.UserId,
		RecipientType: string(audit.User),
	}

	return m.sendEmailResult(ctx, rendered, emailInfo, emailprovider.Transactional, "")
}

// SendLoginEmail sends a login email
func (m *EmailManager) SendLoginEmail(ctx context.Context, req *SendLoginEmailRequest) error {
	_, err := m.SendLoginEmailWithResult(ctx, req)
	return err
}

// SendLoginEmailWithResult always selects the transactional route.
func (m *EmailManager) SendLoginEmailWithResult(ctx context.Context, req *SendLoginEmailRequest) (*SendReceipt, error) {
	if ctx == nil || req == nil {
		return nil, ErrEmailMailerTemplateGenerationFailed
	}
	logger := logger.AcquireOperationFrom(ctx, "external/emailmanager", "send-login-email")
	logger.Debug("handling-send-login-email-request")

	// Generate template
	templateReq := &emailtemplater.GenerateLoginEmailRequest{
		Email:              req.Email,
		Token:              req.Token,
		Code:               req.Code,
		IsDashboardRequest: req.IsDashboardRequest,
		RequestUrl:         req.RequestUrl,
	}

	rendered, err := m.templater.GenerateLoginEmail(ctx, templateReq)
	if err != nil {
		return nil, ErrEmailMailerTemplateGenerationFailed
	}

	// Send email
	emailInfo := &EmailInfo{
		To:            rendered.To,
		From:          rendered.From,
		Subject:       rendered.Subject,
		EmailProvider: "",
		UserId:        req.UserId,
		RecipientType: string(audit.User),
	}

	return m.sendEmailResult(ctx, rendered, emailInfo, emailprovider.Transactional, "")
}

// SendCustomEmail sends a custom email from the base template
func (m *EmailManager) SendCustomEmail(ctx context.Context, req *SendCustomEmailRequest) error {
	_, err := m.SendCustomEmailWithResult(ctx, req)
	return err
}

// SendCustomEmailWithResult requires trusted purpose in routed mode.
func (m *EmailManager) SendCustomEmailWithResult(ctx context.Context, req *SendCustomEmailRequest) (*SendReceipt, error) {
	if ctx == nil || req == nil {
		return nil, ErrEmailMailerTemplateGenerationFailed
	}
	logger := logger.AcquireOperationFrom(ctx, "external/emailmanager", "send-custom-email")
	logger.Debug("handling-send-custom-email-request")

	// Generate template
	templateReq := &emailtemplater.GenerateFromBaseTemplateRequest{
		EmailSubject:         req.EmailSubject,
		EmailPreview:         req.EmailPreview,
		EmailBody:            req.EmailBody,
		EmailTo:              req.EmailTo,
		OverrideEmailFrom:    req.OverrideEmailFrom,
		OverrideEmailReplyTo: req.OverrideEmailReplyTo,
		WithFooter:           req.WithFooter,
	}

	rendered, err := m.templater.GenerateFromBaseTemplate(ctx, templateReq)
	if err != nil {
		return nil, ErrEmailMailerTemplateGenerationFailed
	}

	// Send email
	emailInfo := &EmailInfo{
		To:            rendered.To,
		From:          rendered.From,
		Subject:       rendered.Subject,
		EmailProvider: "",
		UserId:        req.UserId,
		RecipientType: req.RecipientType,
	}

	return m.sendEmailResult(ctx, rendered, emailInfo, req.MailType, req.TextBody)
}

// SendEmail preserves the legacy error-only contract.
func (m *EmailManager) SendEmail(ctx context.Context, req *SendEmailRequest) error {
	_, err := m.SendEmailWithResult(ctx, req)
	return err
}

// SendEmailWithResult sends an immutable pre-rendered snapshot and retains the receipt.
func (m *EmailManager) SendEmailWithResult(ctx context.Context, req *SendEmailRequest) (*SendReceipt, error) {
	if req == nil {
		return nil, ErrEmailMailerSendFailed
	}
	return m.sendResult(ctx, &emailprovider.Email{To: req.To, From: req.From, ReplyTo: req.ReplyTo, Subject: req.Subject, HTMLBody: req.HTMLBody, TextBody: req.TextBody, MailType: req.MailType}, &EmailInfo{To: req.To, From: req.From, Subject: req.Subject, UserId: req.UserId, RecipientType: req.RecipientType})
}
func (m *EmailManager) sendEmailResult(ctx context.Context, rendered *emailtemplater.RenderedEmail, info *EmailInfo, purpose emailprovider.MailType, text string) (*SendReceipt, error) {
	return m.sendResult(ctx, &emailprovider.Email{To: rendered.To, From: rendered.From, ReplyTo: rendered.ReplyTo, Subject: rendered.Subject, HTMLBody: rendered.HTMLBody, TextBody: text, MailType: purpose}, info)
}

// logAuditEvent records submission evidence; only accepted/captured outcomes have SentAt.
func (m *EmailManager) logAuditEvent(ctx context.Context, emailInfo *EmailInfo) {
	logger := logger.AcquirePackageFrom(ctx, "external/emailmanager")

	emailType := audit.Security
	if emailInfo.MailType == string(emailprovider.Marketing) {
		emailType = audit.Other
	}
	sentAt := ""
	if emailInfo.State == string(emailprovider.Accepted) || emailInfo.State == string(emailprovider.Captured) {
		sentAt = toolbox.TimeNowUTC()
	}
	err := m.auditService.LogAuditEvent(ctx, &audit.LogAuditEventRequest{
		ActorId:    audit.AuditActorIdSystem,
		Action:     audit.UserEmailOutbound,
		TargetId:   emailInfo.UserId,
		TargetType: audit.TargetType(emailInfo.RecipientType),
		Domain:     "emailmanager",
		Details: &audit.UserEmailOutboundEventDetails{
			To:            emailInfo.To,
			From:          emailInfo.From,
			Subject:       emailInfo.Subject,
			SentAt:        sentAt,
			EmailProvider: emailInfo.EmailProvider,
			EmailType:     emailType,
			MailType:      emailInfo.MailType, ProviderID: emailInfo.ProviderID, SendState: emailInfo.State,
		},
	})

	if err != nil {
		logger.Warn("failed-to-log-audit-event", append(
			subjectLogFields(emailInfo.Subject),
			zap.String("actor-id", audit.AuditActorIdSystem),
			zap.String("user-id", emailInfo.UserId),
			zap.String("event-type", string(audit.UserEmailOutbound)),
			zap.Error(err),
		)...)
	}
}
