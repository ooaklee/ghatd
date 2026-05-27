package emailtemplater

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/mailgun/raymond/v2"
	"github.com/ooaklee/ghatd/external/logger"
	"go.uber.org/zap"
)

// EmailTemplater handles generating email templates
type EmailTemplater struct {
	config           *Config
	templates        map[EmailTemplateType]string
	dynamicTemplates map[EmailTemplateType]func(emailPreview string, emailSubject string, emailMainContent string, footerEnabled bool, footerYear int, footerEntityName string, footerEntityUrl string) string
}

// NewEmailTemplater creates a new EmailTemplater with the given configuration
func NewEmailTemplater(config *Config) (*EmailTemplater, error) {
	if config == nil {
		return nil, ErrEmailTemplaterNoConfigProvided
	}

	templater := &EmailTemplater{
		config: config,
	}

	if len(config.Templates) > 0 {
		templater.templates = config.Templates
	}

	if len(config.DynamicTemplates) > 0 {
		templater.dynamicTemplates = config.DynamicTemplates
	}

	return templater, nil
}

// GenerateFromBaseTemplate generates a custom email from the base dynamic template
func (t *EmailTemplater) GenerateFromBaseTemplate(ctx context.Context, req *GenerateFromBaseTemplateRequest) (*RenderedEmail, error) {
	logger := logger.AcquireOperationFrom(ctx, "external/emailtemplater", "generate-from-base-template")
	logger.Debug("email-template-render-started", zap.Bool("footer-enabled", req.WithFooter))

	if err := req.Validate(); err != nil {
		logger.Warn("email-template-validation-failed", zap.Error(err))
		return nil, err
	}

	if t.dynamicTemplates == nil || t.dynamicTemplates[EmailTemplateTypeBase] == nil {
		logger.Error("email-template-dynamic-template-not-found", zap.String("template-type", string(EmailTemplateTypeBase)))
		return nil, ErrEmailTemplaterDynamicTemplateNotFound
	}

	// Determine email addresses
	emailFrom := t.config.FromEmailAddress
	if req.OverrideEmailFrom != "" {
		emailFrom = req.OverrideEmailFrom
	}

	emailReplyTo := t.config.NoReplyEmailAddress
	if req.OverrideEmailReplyTo != "" {
		emailReplyTo = req.OverrideEmailReplyTo
	}

	// Adjust subject for environment
	emailSubject := t.config.AdjustSubjectForEnvironment(req.EmailSubject)

	// Render the full email using the base template
	fullRenderedEmail := t.dynamicTemplates[EmailTemplateTypeBase](
		req.EmailPreview,
		req.EmailSubject,
		req.EmailBody,
		req.WithFooter,
		t.config.GetCurrentYear(),
		t.config.BusinessEntityName,
		t.config.BusinessEntityWebsite,
	)

	logger.Debug("email-template-render-completed", zap.String("template-type", string(EmailTemplateTypeBase)))

	return &RenderedEmail{
		To:       req.EmailTo,
		From:     emailFrom,
		ReplyTo:  emailReplyTo,
		Subject:  emailSubject,
		HTMLBody: fullRenderedEmail,
		Preview:  req.EmailPreview,
	}, nil
}

// GenerateVerificationEmail generates an email for email verification
func (t *EmailTemplater) GenerateVerificationEmail(ctx context.Context, req *GenerateVerificationEmailRequest) (*RenderedEmail, error) {
	logger := logger.AcquireOperationFrom(ctx, "external/emailtemplater", "generate-verification-email")
	logger.Debug("email-template-render-started", zap.String("template-type", string(EmailTemplateTypeVerification)), zap.Bool("dashboard-request", req.IsDashboardRequest))

	if err := req.Validate(); err != nil {
		logger.Warn("email-template-validation-failed", zap.Error(err))
		return nil, err
	}

	if t.templates == nil || t.templates[EmailTemplateTypeVerification] == "" {
		logger.Error("email-template-not-found", zap.String("template-type", string(EmailTemplateTypeVerification)))
		return nil, ErrEmailTemplaterTemplateNotFound
	}

	// Generate substitutes
	substitutes := t.generateVerificationEmailSubstitutes(
		req.FirstName,
		req.LastName,
		req.Token,
		req.Code,
		req.IsDashboardRequest,
		req.RequestUrl,
	)

	// Render template with substitutes
	renderedHTML, err := t.renderTemplate(t.templates[EmailTemplateTypeVerification], substitutes)
	if err != nil {
		logger.Error("email-template-render-failed", zap.String("template-type", string(EmailTemplateTypeVerification)), zap.Error(err))
		return nil, ErrEmailTemplaterTemplateRenderingFailed
	}

	// Adjust subject for environment
	emailSubject := t.config.AdjustSubjectForEnvironment(t.config.WelcomeEmailSubject)

	logger.Debug("email-template-render-completed", zap.String("template-type", string(EmailTemplateTypeVerification)))

	return &RenderedEmail{
		To:       req.Email,
		From:     t.config.FromEmailAddress,
		ReplyTo:  t.config.NoReplyEmailAddress,
		Subject:  emailSubject,
		HTMLBody: renderedHTML,
		Preview:  "Please verify your email",
	}, nil
}

// GenerateLoginEmail generates an email for login
func (t *EmailTemplater) GenerateLoginEmail(ctx context.Context, req *GenerateLoginEmailRequest) (*RenderedEmail, error) {
	logger := logger.AcquireOperationFrom(ctx, "external/emailtemplater", "generate-login-email")
	logger.Debug("email-template-render-started", zap.String("template-type", string(EmailTemplateTypeLogin)), zap.Bool("dashboard-request", req.IsDashboardRequest))

	if err := req.Validate(); err != nil {
		logger.Warn("email-template-validation-failed", zap.Error(err))
		return nil, err
	}

	if t.templates == nil || t.templates[EmailTemplateTypeLogin] == "" {
		logger.Error("email-template-not-found", zap.String("template-type", string(EmailTemplateTypeLogin)))
		return nil, ErrEmailTemplaterTemplateNotFound
	}

	// Generate substitutes
	substitutes := t.generateLoginEmailSubstitutes(
		req.Token,
		req.Code,
		req.IsDashboardRequest,
		req.RequestUrl,
	)

	// Render template with substitutes
	renderedHTML, err := t.renderTemplate(t.templates[EmailTemplateTypeLogin], substitutes)
	if err != nil {
		logger.Error("email-template-render-failed", zap.String("template-type", string(EmailTemplateTypeLogin)), zap.Error(err))
		return nil, ErrEmailTemplaterTemplateRenderingFailed
	}

	// Adjust subject for environment
	emailSubject := t.config.AdjustSubjectForEnvironment(t.config.LoginEmailSubject)

	logger.Debug("email-template-render-completed", zap.String("template-type", string(EmailTemplateTypeLogin)))

	return &RenderedEmail{
		To:       req.Email,
		From:     t.config.FromEmailAddress,
		ReplyTo:  t.config.NoReplyEmailAddress,
		Subject:  emailSubject,
		HTMLBody: renderedHTML,
		Preview:  "A request has been made to log in to your account",
	}, nil
}

// renderTemplate renders a parsed template with the given substitutes
func (t *EmailTemplater) renderTemplate(templateStr string, substitutes interface{}) (string, error) {
	loadedTemplate := raymond.MustParse(templateStr)

	parsedTemplate, err := loadedTemplate.Exec(substitutes)
	if err != nil {
		return "", err
	}

	return parsedTemplate, nil
}

// generateVerificationEmailSubstitutes prepares substitutes for verification email
func (t *EmailTemplater) generateVerificationEmailSubstitutes(firstName, lastName, token, code string, isDashboardRequest bool, requestUrl string) *verificationEmailSubstitutes {
	var loginUrl string
	if isDashboardRequest {
		loginUrl = fmt.Sprintf("%s/auth/login", t.config.DashboardDomainName)
	} else {
		loginUrl = fmt.Sprintf("%s/auth/login", t.config.FrontEndDomainName)
	}

	loginUrl = appendRequestURL(loginUrl, t.allowedRedirectURL(requestUrl, isDashboardRequest))

	return &verificationEmailSubstitutes{
		FullName:        strings.Title(fmt.Sprintf("%v %v", firstName, lastName)),
		Code:            code,
		VerificationURL: t.generateEmailVerificationURL(token, isDashboardRequest, requestUrl),
		LoginURL:        loginUrl,
	}
}

// generateLoginEmailSubstitutes prepares substitutes for login email
func (t *EmailTemplater) generateLoginEmailSubstitutes(token, code string, isDashboardRequest bool, requestUrl string) *loginEmailSubstitutes {
	return &loginEmailSubstitutes{
		Code:     code,
		LoginURL: t.generateLoginURL(token, isDashboardRequest, requestUrl),
	}
}

// generateEmailVerificationURL generates a URL used for email verification
func (t *EmailTemplater) generateEmailVerificationURL(token string, isDashboardRequest bool, requestUrl string) string {
	verificationUrl := fmt.Sprintf(t.generateActionPath(2, isDashboardRequest), token)
	return appendRequestURL(verificationUrl, t.allowedRedirectURL(requestUrl, isDashboardRequest))
}

// generateLoginURL generates a URL used for logging into platform
func (t *EmailTemplater) generateLoginURL(token string, isDashboardRequest bool, requestUrl string) string {
	loginUrl := fmt.Sprintf(t.generateActionPath(1, isDashboardRequest), token)
	return appendRequestURL(loginUrl, t.allowedRedirectURL(requestUrl, isDashboardRequest))
}

// allowedRedirectURL normalises the optional request URL that is embedded in
// login and verification emails. We allow relative frontend paths because the
// web app commonly sends destinations like /app/plan?slug=pro, while rejecting
// malformed values and absolute URLs that do not match the configured frontend
// or dashboard origin to avoid open redirect links.
func (t *EmailTemplater) allowedRedirectURL(requestUrl string, isDashboardRequest bool) string {
	requestUrl = strings.TrimSpace(requestUrl)
	if requestUrl == "" {
		return ""
	}

	if strings.HasPrefix(requestUrl, "/") && !strings.HasPrefix(requestUrl, "//") {
		return requestUrl
	}

	expectedBase := t.config.FrontEndDomainName
	if isDashboardRequest {
		expectedBase = t.config.DashboardDomainName
	}

	requestURI, err := url.Parse(requestUrl)
	if err != nil || !requestURI.IsAbs() {
		return ""
	}

	expectedURI, err := url.Parse(expectedBase)
	if err != nil {
		return ""
	}

	if requestURI.Scheme != expectedURI.Scheme || requestURI.Host != expectedURI.Host {
		return ""
	}

	target := requestURI.RequestURI()
	if target == "" {
		return "/"
	}

	return target
}

// appendRequestURL adds a request_url query parameter to an email action URL.
// The value is query-escaped so nested destination query strings survive when a
// browser follows the magic link, rather than being parsed as auth-link params.
func appendRequestURL(actionURL string, requestUrl string) string {
	if requestUrl == "" {
		return actionURL
	}

	separator := "?"
	if strings.Contains(actionURL, "?") {
		separator = "&"
	}

	return actionURL + separator + "request_url=" + url.QueryEscape(requestUrl)
}

// generateActionPath returns the correctly formatted front end path that will deal with passing
// respective tokens back to the correct endpoint(s).
//
// [Type 1]: handles login actions
// [Type 2]: handles verification actions
func (t *EmailTemplater) generateActionPath(verificationType int, isDashboardRequest bool) string {
	return t.config.EmailVerificationFullEndpoint + "?type=" + fmt.Sprintf("%d", verificationType) + "&__t=%v"
}
