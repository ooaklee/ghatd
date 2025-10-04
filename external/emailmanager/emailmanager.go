package emailmanager

import (
	"context"
	"errors"

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
}

// Config holds configuration for the email manager
type Config struct {
	// ShouldSendEmail determines if emails should actually be sent or just logged
	ShouldSendEmail bool

	// EnableAuditLogging determines if audit events should be logged
	EnableAuditLogging bool
}

// DefaultConfig returns a config with sensible defaults
func DefaultConfig() *Config {
	return &Config{
		ShouldSendEmail:    true,
		EnableAuditLogging: true,
	}
}

// NewEmailManager creates a new email manager
func NewEmailManager(templater emailTemplater, provider emailprovider.EmailProvider, auditService AuditService, config *Config) *EmailManager {
	if config == nil {
		config = DefaultConfig()
	}

	return &EmailManager{
		templater:    templater,
		provider:     provider,
		auditService: auditService,
		config:       config,
	}
}

// SendVerificationEmail sends a verification email
func (m *EmailManager) SendVerificationEmail(ctx context.Context, req *SendVerificationEmailRequest) error {
	// Generate template
	templateReq := &emailtemplater.GenerateVerificationEmailRequest{
		FirstName:          req.FirstName,
		LastName:           req.LastName,
		Email:              req.Email,
		Token:              req.Token,
		IsDashboardRequest: req.IsDashboardRequest,
		RequestUrl:         req.RequestUrl,
	}

	rendered, err := m.templater.GenerateVerificationEmail(ctx, templateReq)
	if err != nil {
		return errors.New(ErrKeyEmailMailerTemplateGenerationFailed)
	}

	// Send email
	emailInfo := &EmailInfo{
		To:            rendered.To,
		From:          rendered.From,
		Subject:       rendered.Subject,
		EmailProvider: m.provider.Name(),
		UserId:        req.UserId,
		RecipientType: string(audit.User),
	}

	return m.sendEmail(ctx, rendered, emailInfo)
}

// SendLoginEmail sends a login email
func (m *EmailManager) SendLoginEmail(ctx context.Context, req *SendLoginEmailRequest) error {
	// Generate template
	templateReq := &emailtemplater.GenerateLoginEmailRequest{
		Email:              req.Email,
		Token:              req.Token,
		IsDashboardRequest: req.IsDashboardRequest,
		RequestUrl:         req.RequestUrl,
	}

	rendered, err := m.templater.GenerateLoginEmail(ctx, templateReq)
	if err != nil {
		return errors.New(ErrKeyEmailMailerTemplateGenerationFailed)
	}

	// Send email
	emailInfo := &EmailInfo{
		To:            rendered.To,
		From:          rendered.From,
		Subject:       rendered.Subject,
		EmailProvider: m.provider.Name(),
		UserId:        req.UserId,
		RecipientType: string(audit.User),
	}

	return m.sendEmail(ctx, rendered, emailInfo)
}

// SendCustomEmail sends a custom email from the base template
func (m *EmailManager) SendCustomEmail(ctx context.Context, req *SendCustomEmailRequest) error {
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
		return errors.New(ErrKeyEmailMailerTemplateGenerationFailed)
	}

	// Send email
	emailInfo := &EmailInfo{
		To:            rendered.To,
		From:          rendered.From,
		Subject:       rendered.Subject,
		EmailProvider: m.provider.Name(),
		UserId:        req.UserId,
		RecipientType: req.RecipientType,
	}

	return m.sendEmail(ctx, rendered, emailInfo)
}

// SendEmail sends a pre-rendered email
func (m *EmailManager) SendEmail(ctx context.Context, req *SendEmailRequest) error {
	// Create email from request
	email := &emailprovider.Email{
		To:       req.To,
		From:     req.From,
		ReplyTo:  req.ReplyTo,
		Subject:  req.Subject,
		HTMLBody: req.HTMLBody,
	}

	// Send via provider
	result, err := m.provider.Send(ctx, email)
	if err != nil {
		return errors.New(ErrKeyEmailMailerSendFailed)
	}

	// Log audit event if enabled
	if m.config.EnableAuditLogging && m.auditService != nil {
		emailInfo := &EmailInfo{
			To:            req.To,
			From:          req.From,
			Subject:       req.Subject,
			EmailProvider: result.Provider,
			UserId:        req.UserId,
			RecipientType: req.RecipientType,
		}
		m.logAuditEvent(ctx, emailInfo)
	}

	return nil
}

// sendEmail is the internal method that handles sending and audit logging
func (m *EmailManager) sendEmail(ctx context.Context, rendered *emailtemplater.RenderedEmail, emailInfo *EmailInfo) error {
	log := logger.AcquireFrom(ctx)

	// Check if we should actually send or just log
	if !m.config.ShouldSendEmail {
		log.Info("email-outputted-locally-not-sent-disabled-by-config",
			zap.String("to", rendered.To),
			zap.String("from", rendered.From),
			zap.String("subject", rendered.Subject),
			zap.String("provider", emailInfo.EmailProvider),
		)

		// Still log audit event even if not sending
		if m.config.EnableAuditLogging && m.auditService != nil {
			m.logAuditEvent(ctx, emailInfo)
		}

		return nil
	}

	// Check if provider is healthy
	if !m.provider.IsHealthy(ctx) {
		log.Error("email-provider-is-not-healthy",
			zap.String("provider", m.provider.Name()),
		)
		return errors.New(ErrKeyEmailMailerProviderUnavailable)
	}

	// Create email object
	email := &emailprovider.Email{
		To:       rendered.To,
		From:     rendered.From,
		ReplyTo:  rendered.ReplyTo,
		Subject:  rendered.Subject,
		HTMLBody: rendered.HTMLBody,
	}

	// Send via provider
	result, err := m.provider.Send(ctx, email)
	if err != nil {
		log.Error("failed-to-send-email",
			zap.String("provider", m.provider.Name()),
			zap.String("to", rendered.To),
			zap.Error(err),
		)
		return errors.New(ErrKeyEmailMailerSendFailed)
	}

	log.Info("email-sent-successfully",
		zap.String("provider", result.Provider),
		zap.String("message_id", result.MessageID),
		zap.String("to", rendered.To),
		zap.String("subject", rendered.Subject),
	)

	// Log audit event if enabled
	if m.config.EnableAuditLogging && m.auditService != nil {
		m.logAuditEvent(ctx, emailInfo)
	}

	return nil
}

// logAuditEvent logs an audit event for the sent email
func (m *EmailManager) logAuditEvent(ctx context.Context, emailInfo *EmailInfo) {
	log := logger.AcquireFrom(ctx)

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
			SentAt:        toolbox.TimeNowUTC(),
			EmailProvider: emailInfo.EmailProvider,
			EmailType:     audit.Security,
		},
	})

	if err != nil {
		log.Warn("failed-to-log-audit-event",
			zap.String("actor-id", audit.AuditActorIdSystem),
			zap.String("user-id", emailInfo.UserId),
			zap.String("event-type", string(audit.UserEmailOutbound)),
			zap.String("subject", emailInfo.Subject),
			zap.Error(err),
		)
	}
}
