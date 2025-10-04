package emailtemplater

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/mailgun/raymond/v2"
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
		return nil, errors.New(ErrKeyEmailTemplaterNoConfigProvided)
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
	if err := req.Validate(); err != nil {
		return nil, err
	}

	if t.dynamicTemplates == nil || t.dynamicTemplates[EmailTemplateTypeBase] == nil {
		return nil, errors.New(ErrKeyEmailTemplaterDynamicTemplateNotFound)
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
	if err := req.Validate(); err != nil {
		return nil, err
	}

	if t.templates == nil || t.templates[EmailTemplateTypeVerification] == "" {
		return nil, errors.New(ErrKeyEmailTemplaterTemplateNotFound)
	}

	// Generate substitutes
	substitutes := t.generateVerificationEmailSubstitutes(
		req.FirstName,
		req.LastName,
		req.Token,
		req.IsDashboardRequest,
		req.RequestUrl,
	)

	// Render template with substitutes
	renderedHTML, err := t.renderTemplate(t.templates[EmailTemplateTypeVerification], substitutes)
	if err != nil {
		return nil, errors.New(ErrKeyEmailTemplaterTemplateRenderingFailed)
	}

	// Adjust subject for environment
	emailSubject := t.config.AdjustSubjectForEnvironment(t.config.WelcomeEmailSubject)

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
	if err := req.Validate(); err != nil {
		return nil, err
	}

	if t.templates == nil || t.templates[EmailTemplateTypeLogin] == "" {
		return nil, errors.New(ErrKeyEmailTemplaterTemplateNotFound)
	}

	// Generate substitutes
	substitutes := t.generateLoginEmailSubstitutes(
		req.Token,
		req.IsDashboardRequest,
		req.RequestUrl,
	)

	// Render template with substitutes
	renderedHTML, err := t.renderTemplate(t.templates[EmailTemplateTypeLogin], substitutes)
	if err != nil {
		return nil, errors.New(ErrKeyEmailTemplaterTemplateRenderingFailed)
	}

	// Adjust subject for environment
	emailSubject := t.config.AdjustSubjectForEnvironment(t.config.LoginEmailSubject)

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
func (t *EmailTemplater) generateVerificationEmailSubstitutes(firstName, lastName, token string, isDashboardRequest bool, requestUrl string) *verificationEmailSubstitutes {
	const requestUrlPrefix string = "&request_url="

	var loginUrl string
	if isDashboardRequest {
		loginUrl = fmt.Sprintf("%s/auth/login", t.config.DashboardDomainName)
	} else {
		loginUrl = fmt.Sprintf("%s/auth/login", t.config.FrontEndDomainName)
	}

	if requestUrl != "" {
		loginUrl += (requestUrlPrefix + requestUrl)
	}

	return &verificationEmailSubstitutes{
		FullName:        strings.Title(fmt.Sprintf("%v %v", firstName, lastName)),
		VerificationURL: t.generateEmailVerificationURL(token, isDashboardRequest, requestUrl),
		LoginURL:        loginUrl,
	}
}

// generateLoginEmailSubstitutes prepares substitutes for login email
func (t *EmailTemplater) generateLoginEmailSubstitutes(token string, isDashboardRequest bool, requestUrl string) *loginEmailSubstitutes {
	return &loginEmailSubstitutes{
		LoginURL: t.generateLoginURL(token, isDashboardRequest, requestUrl),
	}
}

// generateEmailVerificationURL generates a URL used for email verification
func (t *EmailTemplater) generateEmailVerificationURL(token string, isDashboardRequest bool, requestUrl string) string {
	const requestUrlPrefix string = "&request_url="

	verificationUrl := fmt.Sprintf(t.generateActionPath(2, isDashboardRequest), token)

	if requestUrl != "" {
		if isDashboardRequest && strings.Contains(requestUrl, t.config.DashboardDomainName) {
			verificationUrl += (requestUrlPrefix + requestUrl)
		} else if !isDashboardRequest && strings.Contains(requestUrl, t.config.FrontEndDomainName) {
			verificationUrl += (requestUrlPrefix + requestUrl)
		}
	}

	return verificationUrl
}

// generateLoginURL generates a URL used for logging into platform
func (t *EmailTemplater) generateLoginURL(token string, isDashboardRequest bool, requestUrl string) string {
	const requestUrlPrefix string = "&request_url="

	loginUrl := fmt.Sprintf(t.generateActionPath(1, isDashboardRequest), token)

	if requestUrl != "" {
		if isDashboardRequest && strings.Contains(requestUrl, t.config.DashboardDomainName) {
			loginUrl += (requestUrlPrefix + requestUrl)
		} else if !isDashboardRequest && strings.Contains(requestUrl, t.config.FrontEndDomainName) {
			loginUrl += (requestUrlPrefix + requestUrl)
		}
	}

	return loginUrl
}

// generateActionPath returns the correctly formatted front end path that will deal with passing
// respective tokens back to the correct endpoint(s).
//
// [Type 1]: handles login actions
// [Type 2]: handles verification actions
func (t *EmailTemplater) generateActionPath(verificationType int, isDashboardRequest bool) string {
	return t.config.EmailVerificationFullEndpoint + "?type=" + fmt.Sprintf("%d", verificationType) + "&__t=%v"
}
