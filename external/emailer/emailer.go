package emailer

import (
	"context"
	"fmt"
	"strings"

	"github.com/mailgun/raymond/v2"
	"github.com/ooaklee/ghatd/external/audit"
	"github.com/ooaklee/ghatd/external/logger"
	"github.com/ooaklee/ghatd/external/toolbox"
	"go.uber.org/zap"

	sp "github.com/SparkPost/gosparkpost"
)

// auditService expected methods of a valid audit service
type auditService interface {
	LogAuditEvent(ctx context.Context, r *audit.LogAuditEventRequest) error
}

// emailClient valid methods of an sparkpost email client
type emailClient interface {
	Send(t *sp.Transmission) (id string, res *sp.Response, err error)
}

// Config holds additional configurations for emailer
type Config struct {
	// FrontEndDomainName the frontend domain being used for current environment
	FrontEndDomainName string

	// DashboardDomainName the dashboard domain being used for current environment
	DashboardDomainName string

	// Environment the environment API in running in
	Environment string

	// EnableSendingEmail specifies whether email should be sent
	// using client or outputted to log
	EnableSendingEmail bool

	// LoginTemplate is the HTML template for login emails
	LoginTemplate string

	// VerificationTemplate is the HTML template for verification emails
	VerificationTemplate string

	// FromEmailAddress the email address that should be in "From" field
	FromEmailAddress string

	// NoReplyEmailAddress the email address that should be reply to field
	NoReplyEmailAddress string

	// WelcomeEmailSubject the subject for verification email
	WelcomeEmailSubject string

	// LoginEmailSubject the subject for login request email
	LoginEmailSubject string

	// ClientSendOutputEnvironment holds the name of the environment that will make email outputted
	// to long instead of being sent using API
	ClientSendOutputEnvironment string

	// FrontEndUserVerificationURIPath the path on the front end that will catch and handle token
	// verify actions
	FrontEndUserVerificationURIPath string

	// DashboardVerificationURIPath the path on the front end that will catch and handle token
	// verify actions for admins/ dashboard access
	DashboardVerificationURIPath string
}

// Client used to send out emails
type Client struct {
	client       emailClient
	config       *Config
	auditService auditService
	providerName string
}

// NewSparkPostClient creates sparkpost emailer client
func NewSparkPostClient(client emailClient, config *Config, auditService auditService) *Client {
	return &Client{
		client:       client,
		config:       config,
		auditService: auditService,
		providerName: "SPARKPOST",
	}
}

// GenerateVerificationEmail generates an email for the user to verify their email address
func (c *Client) GenerateVerificationEmail(r *SendVerificationEmailRequest) (*sp.Transmission, *EmailInfo, error) {

	var renderedEmail string
	var err error

	emailSubstitutes := c.generateVerificationEmailSubsitutes(r.FirstName, r.LastName, r.Token, r.IsDashboardRequest, r.RequestUrl)

	// Subsitute values into correct template
	renderedEmail, err = generateRenderedVerificationEmail(emailSubstitutes, c.config.VerificationTemplate)

	if err != nil {
		return nil, nil, err
	}

	adjustedEmailSubject := c.config.WelcomeEmailSubject

	if c.config.Environment != "production" {
		adjustedEmailSubject = adjustedEmailSubject + fmt.Sprintf(" [%s]", c.config.Environment)
	}

	emailTransmission := generateSparkPostTransmission(r.Email, adjustedEmailSubject, renderedEmail, c.config.FromEmailAddress, c.config.NoReplyEmailAddress)

	return emailTransmission, &EmailInfo{
		To:            r.Email,
		From:          c.config.FromEmailAddress,
		Subject:       adjustedEmailSubject,
		RecipientType: string(audit.User),
		EmailProvider: c.getEmailProviderBasedOnConfig(),
	}, nil
}

// GenerateLoginEmail generates an email for the user to log into account
func (c *Client) GenerateLoginEmail(r *SendLoginEmailRequest) (*sp.Transmission, *EmailInfo, error) {

	var renderedEmail string
	var err error

	emailSubstitutes := c.generateLoginEmailSubsitutes(r.Token, r.IsDashboardRequest, r.RequestUrl)

	// Subsitute values into correct template
	renderedEmail, err = generateRenderedLoginEmail(emailSubstitutes, c.config.LoginTemplate)

	if err != nil {
		return nil, nil, err
	}

	adjustedEmailSubject := c.config.LoginEmailSubject

	if c.config.Environment != "production" {
		adjustedEmailSubject = adjustedEmailSubject + fmt.Sprintf(" [%s]", c.config.Environment)
	}

	emailTransmission := generateSparkPostTransmission(r.Email, adjustedEmailSubject, renderedEmail, c.config.FromEmailAddress, c.config.NoReplyEmailAddress)

	return emailTransmission, &EmailInfo{
		To:            r.Email,
		From:          c.config.FromEmailAddress,
		Subject:       adjustedEmailSubject,
		RecipientType: string(audit.User),
		EmailProvider: c.getEmailProviderBasedOnConfig(),
	}, nil
}

// SendVerificationEmail handles sending email to the user for them to verify their email. Dependent on enivronment,
// email may be outputted to standard out.
func (c *Client) SendEmail(ctx context.Context, emailInfo *EmailInfo, emailTransmission *sp.Transmission) error {

	return c.send(ctx, emailTransmission, emailInfo)
}

// generateSparkPostTransmission returns valid transmission that can be used for sending email
// on SparkPost's transmission API
func generateSparkPostTransmission(userEmail, emailSubject, emailBodyAsHTML, fromAddress, noReplyAddress string) *sp.Transmission {
	return &sp.Transmission{
		Recipients: []string{userEmail},
		Content: sp.Content{
			HTML:    emailBodyAsHTML,
			From:    fromAddress,
			ReplyTo: noReplyAddress,
			Subject: emailSubject,
		},
	}
}

// generateLoginEmailSubsitutes takes arguments needed to prepare relevant subsitutes
// for the login email
func (c *Client) generateLoginEmailSubsitutes(token string, isDashboardRequest bool, requestUrl string) *SendLoginEmaiRequestSubstitutes {
	return &SendLoginEmaiRequestSubstitutes{
		//nolint we generate the html so know it is safe
		LoginURL: string(c.generateLoginURL(token, isDashboardRequest, requestUrl)),
	}
}

// generateVerificationEmailSubsitutes takes arguments needed to prepare relevant subsitutes
// TODO: Refactor
func (c *Client) generateVerificationEmailSubsitutes(firstName, lastName, token string, isDashboardRequest bool, requestUrl string) *SendVerificationEmaiRequestSubstitutes {

	var loginUrl string
	const requestUrlPrefix string = "&request_url="

	if isDashboardRequest {
		loginUrl = fmt.Sprintf("%s/auth/login", c.config.DashboardDomainName)

	}

	// check if user portal (desktop)
	if !isDashboardRequest {
		loginUrl = fmt.Sprintf("%s/auth/login", c.config.FrontEndDomainName)

	}

	if requestUrl != "" {
		loginUrl += (requestUrlPrefix + requestUrl)
	}

	return &SendVerificationEmaiRequestSubstitutes{
		FullName: strings.Title(fmt.Sprintf("%v %v", firstName, lastName)),
		//nolint we generate the html so know it is safe
		VerificationURL: string(c.generateEmailVerificationURL(token, isDashboardRequest, requestUrl)),

		LoginURL: string(loginUrl),
	}
}

// generateRenderedLoginEmail returns rendered email template with subsitutes swapped.
// If it fails, an error is returned
func generateRenderedLoginEmail(r *SendLoginEmaiRequestSubstitutes, emailTemplate string) (string, error) {

	loadedTemplate := raymond.MustParse(emailTemplate)

	return renderParsedTemplate(loadedTemplate, r)
}

// generateRenderedVerificationEmail returns rendered email template with subsitutes swapped.
// If it fails, an error is returned
func generateRenderedVerificationEmail(r *SendVerificationEmaiRequestSubstitutes, emailTemplate string) (string, error) {

	loadedTemplate := raymond.MustParse(emailTemplate)

	return renderParsedTemplate(loadedTemplate, r)
}

// renderParsedTemplate renders passed subsitutes to passed email template, returns errors on failure
func renderParsedTemplate(emailTemplate *raymond.Template, subsitutes interface{}) (string, error) {

	// Create empty buffer to hold template with variables
	var parsedTemplate string
	var err error

	// Generate email body using Template + variables
	parsedTemplate, err = emailTemplate.Exec(subsitutes)
	if err != nil {
		return parsedTemplate, err
	}

	return parsedTemplate, nil
}

// generateEmailVerificationURL generates an URL used for email verification
func (c *Client) generateEmailVerificationURL(token string, isDashboardRequest bool, requestUrl string) (url string) {

	var verificationUrl string
	const requestUrlPrefix string = "&request_url="

	if isDashboardRequest {
		verificationUrl = fmt.Sprintf(c.generateActionPath(2, isDashboardRequest), c.config.DashboardDomainName, token)

		if requestUrl != "" && strings.Contains(requestUrl, c.config.DashboardDomainName) {
			verificationUrl += (requestUrlPrefix + requestUrl)
		}

		return verificationUrl
	}

	verificationUrl = fmt.Sprintf(c.generateActionPath(2, isDashboardRequest), c.config.FrontEndDomainName, token)

	if requestUrl != "" && strings.Contains(requestUrl, c.config.FrontEndDomainName) {
		verificationUrl += (requestUrlPrefix + requestUrl)
	}

	return verificationUrl

}

// generateLoginURL generates an URL used for login into platform
func (c *Client) generateLoginURL(token string, isDashboardRequest bool, requestUrl string) (url string) {

	var loginUrl string
	const requestUrlPrefix string = "&request_url="

	if isDashboardRequest {

		loginUrl = fmt.Sprintf(c.generateActionPath(1, isDashboardRequest), c.config.DashboardDomainName, token)

		if requestUrl != "" && strings.Contains(requestUrl, c.config.DashboardDomainName) {
			loginUrl += (requestUrlPrefix + requestUrl)
		}

		return loginUrl
	}

	loginUrl = fmt.Sprintf(c.generateActionPath(1, isDashboardRequest), c.config.FrontEndDomainName, token)

	if requestUrl != "" && strings.Contains(requestUrl, c.config.FrontEndDomainName) {
		loginUrl += (requestUrlPrefix + requestUrl)
	}

	return loginUrl
}

// generateActionPath returns the correctly formatted front end path that will deal with passing
// respective tokens back to the correct endpoint(s).
//
// [Type 1]: handles login actions
// [Type 2]: handles verification actions
func (c *Client) generateActionPath(verificationType int, isDashboardRequest bool) string {
	if isDashboardRequest {
		return "%v" + c.config.DashboardVerificationURIPath + "?type=" + fmt.Sprintf("%d", verificationType) + "&__t=%v"
	}

	return "%v" + c.config.FrontEndUserVerificationURIPath + "?type=" + fmt.Sprintf("%d", verificationType) + "&__t=%v"
}

// send decides whether the email should be logged to standard out or sent via service
func (c *Client) send(ctx context.Context, tx *sp.Transmission, emailInfo *EmailInfo) error {

	logger := logger.AcquireFrom(ctx)

	if !(c.config.Environment == c.config.ClientSendOutputEnvironment) || (c.config.EnableSendingEmail) {
		_, _, err := c.client.Send(tx)
		if err != nil {
			logger.Warn(fmt.Sprintf("Error sending email (via. SparkPost API) to %v", emailInfo.UserId), zap.Error(err))
			return err
		}

		if err == nil {
			auditErr := c.auditService.LogAuditEvent(ctx, &audit.LogAuditEventRequest{
				ActorId:    audit.AuditActorIdSystem,
				Action:     audit.UserEmailOutbound,
				TargetId:   emailInfo.UserId,
				TargetType: audit.TargetType(emailInfo.RecipientType),
				Domain:     "emailer",
				Details: &audit.UserEmailOutboundEventDetails{
					To:            emailInfo.To,
					From:          emailInfo.From,
					Subject:       emailInfo.Subject,
					SentAt:        toolbox.TimeNowUTC(),
					EmailProvider: emailInfo.EmailProvider,
					EmailType:     audit.Security,
				}})

			if auditErr != nil {
				logger.Warn("failed-to-log-event", zap.String("actor-id", audit.AuditActorIdSystem), zap.String("user-id", emailInfo.UserId), zap.String("event-type", string(audit.UserEmailOutbound)), zap.String("subject", string(emailInfo.Subject)))
			}
		}

		return nil
	}

	logger.Info("Email outputted locally", zap.Any("email body:", tx.Content.(sp.Content).HTML))

	auditErr := c.auditService.LogAuditEvent(ctx, &audit.LogAuditEventRequest{
		ActorId:    audit.AuditActorIdSystem,
		Action:     audit.UserEmailOutbound,
		TargetId:   emailInfo.UserId,
		TargetType: audit.TargetType(emailInfo.RecipientType),
		Domain:     "emailer",
		Details: &audit.UserEmailOutboundEventDetails{
			To:            emailInfo.To,
			From:          emailInfo.From,
			Subject:       emailInfo.Subject,
			SentAt:        toolbox.TimeNowUTC(),
			EmailProvider: emailInfo.EmailProvider,
			EmailType:     audit.Security,
		}})

	if auditErr != nil {
		logger.Warn("failed-to-log-event", zap.String("actor-id", audit.AuditActorIdSystem), zap.String("user-id", emailInfo.UserId), zap.String("event-type", string(audit.UserEmailOutbound)), zap.String("subject", string(emailInfo.Subject)))
	}

	return nil
}

// getEmailProviderBasedOnConfig based on the config passed returns the
// name of the config
func (c *Client) getEmailProviderBasedOnConfig() string {
	if !(c.config.Environment == c.config.ClientSendOutputEnvironment) || (c.config.EnableSendingEmail) {
		return c.providerName
	}
	return "LOCAL"
}
