package emailmanager

import (
	"fmt"
	"time"

	"github.com/ooaklee/ghatd/external/emailprovider"
	"github.com/ooaklee/ghatd/external/emailtemplater"
	"github.com/ooaklee/ghatd/external/emailtemplater/templates"
)

// NewStandardEmailManagerRequest holds the common GHATD email manager setup inputs.
type NewStandardEmailManagerRequest struct {
	Provider     emailprovider.EmailProvider
	AuditService AuditService

	FrontendBaseURL               string
	EmailVerificationFullEndpoint string
	DashboardVerificationURIPath  string
	Environment                   string
	BusinessEntityName            string
	BusinessEntityWebsite         string
	WelcomeEmailSubject           string
	LoginEmailSubject             string
	FromEmailAddress              string
	NoReplyEmailAddress           string
	TimeProvider                  func() time.Time

	Config *Config
}

// NewStandardEmailManager creates a templater with GHATD's standard login,
// verification, and base templates, then returns an EmailManager using the
// provided email provider.
func NewStandardEmailManager(request *NewStandardEmailManagerRequest) (*EmailManager, error) {
	if request == nil {
		return nil, fmt.Errorf("emailmanager/standard-nil-request")
	}
	if request.Provider == nil {
		return nil, fmt.Errorf("emailmanager/standard-missing-provider")
	}

	timeProvider := request.TimeProvider
	if timeProvider == nil {
		timeProvider = time.Now
	}
	currentYear := timeProvider().Year()

	emailTemplater, err := emailtemplater.NewEmailTemplater(&emailtemplater.Config{
		FrontEndDomainName:            request.FrontendBaseURL,
		EmailVerificationFullEndpoint: request.EmailVerificationFullEndpoint,
		DashboardDomainName:           request.FrontendBaseURL,
		DashboardVerificationURIPath:  request.DashboardVerificationURIPath,
		Environment:                   request.Environment,
		BusinessEntityName:            request.BusinessEntityName,
		BusinessEntityWebsite:         request.BusinessEntityWebsite,
		WelcomeEmailSubject:           request.WelcomeEmailSubject,
		LoginEmailSubject:             request.LoginEmailSubject,
		FromEmailAddress:              request.FromEmailAddress,
		NoReplyEmailAddress:           request.NoReplyEmailAddress,
		TimeProvider:                  timeProvider,
		Templates: map[emailtemplater.EmailTemplateType]string{
			emailtemplater.EmailTemplateTypeLogin:        templates.NewLoginEmailTemplate(currentYear, request.BusinessEntityName, request.BusinessEntityWebsite),
			emailtemplater.EmailTemplateTypeVerification: templates.NewVerificationEmailTemplate(currentYear, request.BusinessEntityName, request.BusinessEntityWebsite),
		},
		DynamicTemplates: map[emailtemplater.EmailTemplateType]func(emailPreview string, emailSubject string, emailMainContent string, footerEnabled bool, footerYear int, footerEntityName string, footerEntityUrl string) string{
			emailtemplater.EmailTemplateTypeBase: templates.NewBaseHtmlEmailTemplate,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("emailmanager/standard-templater: %w", err)
	}

	return NewEmailManager(
		emailTemplater,
		request.Provider,
		request.AuditService,
		request.Config,
	), nil
}
