package starter

import (
	"fmt"
	"strings"

	"github.com/ooaklee/ghatd/external/accessmanager"
	"github.com/ooaklee/ghatd/external/apitoken"
	"github.com/ooaklee/ghatd/external/audit"
	"github.com/ooaklee/ghatd/external/auth"
	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/billingmanager"
	"github.com/ooaklee/ghatd/external/contacter"
	"github.com/ooaklee/ghatd/external/contentmanager"
	"github.com/ooaklee/ghatd/external/group"
	"github.com/ooaklee/ghatd/external/notifier"
	"github.com/ooaklee/ghatd/external/paymentprovider"
	"github.com/ooaklee/ghatd/external/policy"
	"github.com/ooaklee/ghatd/external/post"
	"github.com/ooaklee/ghatd/external/pricer"
	"github.com/ooaklee/ghatd/external/reminder"
	"github.com/ooaklee/ghatd/external/streaker"
	userv2 "github.com/ooaklee/ghatd/external/user/v2"
	"github.com/ooaklee/ghatd/external/usermanager"
)

// Services groups the standard GHATD business services and manager services.
type Services struct {
	AccessManager           *accessmanager.Service
	APIToken                *apitoken.Service
	Audit                   *audit.Service
	Auth                    *auth.Service
	Billing                 *billing.Service
	BillingManager          *billingmanager.Service
	Contacter               *contacter.Service
	ContentManager          *contentmanager.Service
	Group                   *group.Service
	Notifier                *notifier.Service
	Policy                  *policy.Service
	Post                    *post.Service
	Pricer                  *pricer.Service
	Reminder                *reminder.Service
	Streaker                *streaker.Service
	User                    *userv2.Service
	UserManager             *usermanager.Service
	EphemeralStore          accessmanager.EphemeralStore
	EmailManager            accessmanager.EmailManager
	PaymentProviderRegistry billingmanager.ProviderRegistry
}

// PolicyConfig holds the business metadata needed to create a policy.Store.
type PolicyConfig struct {
	BusinessEntityName      string
	BusinessEntityEmail     string
	BusinessEntityWebsite   string
	LegalBusinessEntityName string
	GenerateStaticPolicies  bool
}

// NewServicesRequest holds service construction inputs. External clients such
// as Redis, email providers, OAuth providers, and payment providers remain
// explicit so starter/v0 does not hide app-specific decisions.
type NewServicesRequest struct {
	Repositories *Repositories

	EphemeralStore accessmanager.EphemeralStore
	EmailManager   accessmanager.EmailManager
	OAuthServices  []accessmanager.OauthService

	AccessTokenSecret     string
	RefreshTokenSecret    string
	StaticPlaceholderUUID string

	AuditService               *audit.Service
	AutoAdminEmailAddressRegex string
	UserConfig                 *userv2.UserConfig
	UserConfigs                []*userv2.UserConfig
	GroupConfig                *group.GroupConfig
	// ValidPostTags uses post.DefaultValidPostTags when nil. Pass an empty
	// slice to intentionally disable starter's default changelog tag set.
	ValidPostTags []string

	NotifierSenders []notifier.ChannelSender
	// ReminderService overrides the reminder service attached to UserManager.
	// When nil, starter attaches the Reminder service it creates from repositories.
	ReminderService usermanager.ReminderService

	PolicyStore  policy.PolicyStore
	PolicyConfig *PolicyConfig

	// PaymentProviderRegistry and PaymentProviders are mutually exclusive. When
	// no registry is supplied, starter creates one and registers PaymentProviders.
	PaymentProviderRegistry billingmanager.ProviderRegistry
	PaymentProviders        []paymentprovider.Provider
}

// NewServices creates the standard service container from repositories and
// explicit app-level integrations.
func NewServices(r *NewServicesRequest) (*Services, error) {
	if r == nil {
		return nil, ErrNilServicesRequest
	}
	if err := validateRepositoriesForServices(r.Repositories); err != nil {
		return nil, err
	}
	if r.EphemeralStore == nil {
		return nil, ErrNilEphemeralStore
	}
	if r.EmailManager == nil {
		return nil, ErrNilEmailManager
	}
	if strings.TrimSpace(r.AccessTokenSecret) == "" {
		return nil, ErrMissingAccessTokenSecret
	}
	if strings.TrimSpace(r.RefreshTokenSecret) == "" {
		return nil, ErrMissingRefreshTokenSecret
	}

	policyStore, err := resolvePolicyStore(r.PolicyStore, r.PolicyConfig)
	if err != nil {
		return nil, err
	}
	paymentProviderRegistry, err := resolvePaymentProviderRegistry(r.PaymentProviderRegistry, r.PaymentProviders)
	if err != nil {
		return nil, err
	}

	auditService := r.AuditService
	if auditService == nil {
		auditService = audit.NewService(r.Repositories.Audit)
	}
	userToolbox := userv2.NewUniversalUserToolbox()
	userService := userv2.NewService(
		r.Repositories.User,
		auditService,
		r.UserConfig,
		userToolbox,
		userToolbox,
		userToolbox,
		r.AutoAdminEmailAddressRegex,
	)
	if len(r.UserConfigs) > 0 {
		userService.WithConfigs(r.UserConfigs...)
	}

	apiTokenService := apitoken.NewService(r.Repositories.APIToken)
	contacterService := contacter.NewService(r.Repositories.Contacter)
	postService := post.NewService(r.Repositories.Post, resolvePostTags(r.ValidPostTags))
	billingService := billing.NewService(r.Repositories.Billing, r.Repositories.Billing)
	pricerService := pricer.NewService(r.Repositories.Pricer)
	reminderService := reminder.NewService(r.Repositories.Reminder)
	streakerService := streaker.NewService(r.Repositories.Streaker)
	notifierService := notifier.NewService(&notifier.NewServiceRequest{
		Repository: r.Repositories.Notifier,
		Senders:    r.NotifierSenders,
	})
	authService := auth.NewService(&auth.NewServiceRequest{
		AccessTokenSecret:  r.AccessTokenSecret,
		RefreshTokenSecret: r.RefreshTokenSecret,
	})
	policyService := policy.NewService(policyStore)

	groupService, err := group.NewService(
		r.Repositories.Group,
		auditService,
		r.GroupConfig,
		group.NewDefaultIDGenerator(),
		group.NewDefaultTimeProvider(),
		group.NewDefaultStringUtils(),
	)
	if err != nil {
		return nil, fmt.Errorf("starter/group-service: %w", err)
	}

	userManagerService := usermanager.NewService(&usermanager.NewServiceRequest{
		UserService:      userService,
		ApiTokenService:  apiTokenService,
		AuditService:     auditService,
		ContacterService: contacterService,
	}).WithGroupService(groupService).WithNotifierService(notifierService).WithReminderService(reminderService)
	if r.ReminderService != nil {
		userManagerService.WithReminderService(r.ReminderService)
	}

	accessManagerService := accessmanager.NewService(&accessmanager.NewServiceRequest{
		EphemeralStore:        r.EphemeralStore,
		EmailManager:          r.EmailManager,
		AuthService:           authService,
		UserService:           userService,
		ApiTokenService:       apiTokenService,
		OauthServices:         r.OAuthServices,
		AuditService:          auditService,
		StaticPlaceholderUuid: r.StaticPlaceholderUUID,
	}).WithBillingService(billingService).WithGroupService(groupService)

	contentManagerService := contentmanager.NewService(postService, userService)
	billingManagerService := billingmanager.NewService(paymentProviderRegistry, billingService)
	billingManagerService.WithAuditService(auditService).WithUserService(userService).WithPricerService(pricerService)

	return &Services{
		AccessManager:           accessManagerService,
		APIToken:                apiTokenService,
		Audit:                   auditService,
		Auth:                    authService,
		Billing:                 billingService,
		BillingManager:          billingManagerService,
		Contacter:               contacterService,
		ContentManager:          contentManagerService,
		Group:                   groupService,
		Notifier:                notifierService,
		Policy:                  policyService,
		Post:                    postService,
		Pricer:                  pricerService,
		Reminder:                reminderService,
		Streaker:                streakerService,
		User:                    userService,
		UserManager:             userManagerService,
		EphemeralStore:          r.EphemeralStore,
		EmailManager:            r.EmailManager,
		PaymentProviderRegistry: paymentProviderRegistry,
	}, nil
}

// resolvePolicyStore returns the caller-supplied store when provided, or
// builds one from the policy config.
func resolvePolicyStore(policyStore policy.PolicyStore, cfg *PolicyConfig) (policy.PolicyStore, error) {
	if policyStore != nil {
		return policyStore, nil
	}
	if cfg == nil {
		return nil, ErrNilPolicyConfig
	}

	if strings.TrimSpace(cfg.BusinessEntityName) == "" ||
		strings.TrimSpace(cfg.BusinessEntityEmail) == "" ||
		strings.TrimSpace(cfg.BusinessEntityWebsite) == "" ||
		strings.TrimSpace(cfg.LegalBusinessEntityName) == "" {
		return nil, ErrInvalidPolicyConfig
	}
	if cfg.GenerateStaticPolicies && !strings.Contains(cfg.BusinessEntityWebsite, "://") {
		return nil, ErrInvalidPolicyConfig
	}

	store := policy.NewStore(
		cfg.BusinessEntityName,
		cfg.BusinessEntityEmail,
		cfg.BusinessEntityWebsite,
		cfg.LegalBusinessEntityName,
	)
	if cfg.GenerateStaticPolicies {
		store.GenerateStaticPolicies()
	}

	return store, nil
}

// resolvePaymentProviderRegistry returns the caller-supplied registry when
// provided, or creates one from the provider list.
func resolvePaymentProviderRegistry(
	registry billingmanager.ProviderRegistry,
	providers []paymentprovider.Provider,
) (billingmanager.ProviderRegistry, error) {
	if registry != nil {
		if len(providers) > 0 {
			return nil, ErrPaymentProviderRegistryConflict
		}
		return registry, nil
	}

	concreteRegistry := paymentprovider.NewProviderRegistry()
	for _, provider := range providers {
		if provider == nil {
			return nil, ErrNilPaymentProvider
		}
		concreteRegistry.Register(provider)
	}

	return concreteRegistry, nil
}

// resolvePostTags returns the caller-supplied tags when provided, or starter's
// default post tag set.
func resolvePostTags(tags []string) []string {
	if tags != nil {
		return tags
	}

	return append([]string(nil), post.DefaultValidPostTags...)
}
