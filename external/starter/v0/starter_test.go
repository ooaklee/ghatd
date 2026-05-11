package starter

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/audit"
	"github.com/ooaklee/ghatd/external/billingmanager"
	"github.com/ooaklee/ghatd/external/emailmanager"
	"github.com/ooaklee/ghatd/external/ephemeral"
	"github.com/ooaklee/ghatd/external/paymentprovider"
	"github.com/ooaklee/ghatd/external/policy"
	"github.com/ooaklee/ghatd/external/post"
	"github.com/ooaklee/ghatd/external/repository"
	"github.com/ooaklee/reply/v2"
)

func TestConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		wantErr string
	}{
		{
			name: "Success - local environment with debug logging",
			config: Config{
				Port:        4000,
				Environment: "local",
				LogLevel:    "debug",
			},
		},
		{
			name: "Success - development environment with typical port",
			config: Config{
				Port:        8080,
				Environment: "development",
				LogLevel:    "info",
			},
		},
		{
			name: "Success - production environment with TLS port",
			config: Config{
				Port:        443,
				Environment: "production",
				LogLevel:    "warn",
			},
		},
		{
			name: "Success - trims and normalises enum-like values",
			config: Config{
				Port:        3000,
				Environment: " STAGING ",
				LogLevel:    " ERROR ",
			},
		},
		{
			name: "Failure - port is zero",
			config: Config{
				Port:        0,
				Environment: "local",
				LogLevel:    "debug",
			},
			wantErr: "starter/config-invalid-port",
		},
		{
			name: "Failure - port exceeds max",
			config: Config{
				Port:        65536,
				Environment: "production",
				LogLevel:    "info",
			},
			wantErr: "starter/config-invalid-port",
		},
		{
			name: "Failure - empty environment",
			config: Config{
				Port:     4000,
				LogLevel: "debug",
			},
			wantErr: "starter/config-invalid-environment",
		},
		{
			name: "Failure - invalid environment",
			config: Config{
				Port:        4000,
				Environment: "prod",
				LogLevel:    "info",
			},
			wantErr: "starter/config-invalid-environment",
		},
		{
			name: "Failure - empty log level",
			config: Config{
				Port:        4000,
				Environment: "local",
			},
			wantErr: "starter/config-invalid-log-level",
		},
		{
			name: "Failure - invalid log level",
			config: Config{
				Port:        4000,
				Environment: "production",
				LogLevel:    "verbose",
			},
			wantErr: "starter/config-invalid-log-level",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("expected no error, got %v", err)
				}
				return
			}

			if err == nil {
				t.Fatalf("expected error containing %q", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("expected error containing %q, got %q", tt.wantErr, err.Error())
			}
		})
	}
}

func TestNewStack(t *testing.T) {
	tests := []struct {
		name          string
		request       *NewStackRequest
		wantErr       error
		wantErrString string
	}{
		{
			name:    "Failure - nil request",
			request: nil,
			wantErr: ErrNilStackRequest,
		},
		{
			name: "Failure - invalid config",
			request: &NewStackRequest{
				Config: Config{
					Port:        0,
					Environment: "local",
					LogLevel:    "debug",
				},
			},
			wantErrString: "starter/config-invalid-port",
		},
		{
			name: "Success - valid config with no layers yet",
			request: &NewStackRequest{
				Config: Config{
					Port:        8080,
					Environment: "local",
					LogLevel:    "debug",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stack, err := NewStack(tt.request)
			if strings.HasPrefix(tt.name, "Success") {
				if err != nil {
					t.Fatalf("expected no error, got %v", err)
				}
				if stack == nil {
					t.Fatal("expected stack")
				}
				return
			}

			if err == nil {
				t.Fatal("expected error")
			}
			if tt.wantErr != nil && !errors.Is(err, tt.wantErr) {
				t.Fatalf("expected %v, got %v", tt.wantErr, err)
			}
			if tt.wantErrString != "" && !strings.Contains(err.Error(), tt.wantErrString) {
				t.Fatalf("expected error containing %q, got %v", tt.wantErrString, err)
			}
		})
	}
}

func TestNewRepositories(t *testing.T) {
	core := &repository.MongoDbRepository{}
	overrideAudit := audit.NewRepository(core)

	tests := []struct {
		name    string
		request *NewRepositoriesRequest
		wantErr error
		assert  func(t *testing.T, got *Repositories)
	}{
		{
			name:    "Failure - nil request",
			request: nil,
			wantErr: ErrNilRepositoriesRequest,
		},
		{
			name:    "Failure - nil core repository",
			request: &NewRepositoriesRequest{},
			wantErr: ErrNilCoreRepository,
		},
		{
			name: "Success - core repository creates default package repositories",
			request: &NewRepositoriesRequest{
				Core: core,
			},
			assert: func(t *testing.T, got *Repositories) {
				t.Helper()
				if got.Core != core {
					t.Fatalf("expected core repository to be preserved")
				}
				if got.APIToken == nil || got.Audit == nil || got.Billing == nil ||
					got.Contacter == nil || got.Group == nil || got.Notifier == nil ||
					got.Post == nil || got.Pricer == nil || got.User == nil {
					t.Fatalf("expected all repositories to be populated: %#v", got)
				}
			},
		},
		{
			name: "Success - explicit repository override is preserved",
			request: &NewRepositoriesRequest{
				Core:  core,
				Audit: overrideAudit,
			},
			assert: func(t *testing.T, got *Repositories) {
				t.Helper()
				if got.Audit != overrideAudit {
					t.Fatalf("expected audit override to be preserved")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NewRepositories(tt.request)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("expected %v, got %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
			if got == nil {
				t.Fatal("expected repositories")
			}
			if tt.assert != nil {
				tt.assert(t, got)
			}
		})
	}
}

func TestNewServices(t *testing.T) {
	customPaymentProviderRegistry := paymentprovider.NewProviderRegistry()
	customAuditService := audit.NewService(validRepositories(t).Audit)

	tests := []struct {
		name    string
		request func(t *testing.T) *NewServicesRequest
		wantErr error
		assert  func(t *testing.T, got *Services)
	}{
		{
			name:    "Failure - nil request",
			request: func(t *testing.T) *NewServicesRequest { return nil },
			wantErr: ErrNilServicesRequest,
		},
		{
			name: "Failure - nil repositories",
			request: func(t *testing.T) *NewServicesRequest {
				req := validServicesRequest(t)
				req.Repositories = nil
				return req
			},
			wantErr: ErrNilRepositories,
		},
		{
			name: "Failure - missing ephemeral store",
			request: func(t *testing.T) *NewServicesRequest {
				req := validServicesRequest(t)
				req.EphemeralStore = nil
				return req
			},
			wantErr: ErrNilEphemeralStore,
		},
		{
			name: "Failure - missing email manager",
			request: func(t *testing.T) *NewServicesRequest {
				req := validServicesRequest(t)
				req.EmailManager = nil
				return req
			},
			wantErr: ErrNilEmailManager,
		},
		{
			name: "Failure - missing access token secret",
			request: func(t *testing.T) *NewServicesRequest {
				req := validServicesRequest(t)
				req.AccessTokenSecret = " "
				return req
			},
			wantErr: ErrMissingAccessTokenSecret,
		},
		{
			name: "Failure - missing refresh token secret",
			request: func(t *testing.T) *NewServicesRequest {
				req := validServicesRequest(t)
				req.RefreshTokenSecret = ""
				return req
			},
			wantErr: ErrMissingRefreshTokenSecret,
		},
		{
			name: "Failure - missing policy store or config",
			request: func(t *testing.T) *NewServicesRequest {
				req := validServicesRequest(t)
				req.PolicyStore = nil
				req.PolicyConfig = nil
				return req
			},
			wantErr: ErrNilPolicyConfig,
		},
		{
			name: "Failure - invalid generated policy config",
			request: func(t *testing.T) *NewServicesRequest {
				req := validServicesRequest(t)
				req.PolicyStore = nil
				req.PolicyConfig = &PolicyConfig{
					BusinessEntityName:      "Example",
					BusinessEntityEmail:     "hello@example.test",
					BusinessEntityWebsite:   "example.test",
					LegalBusinessEntityName: "Example Ltd",
					GenerateStaticPolicies:  true,
				}
				return req
			},
			wantErr: ErrInvalidPolicyConfig,
		},
		{
			name: "Failure - nil payment provider",
			request: func(t *testing.T) *NewServicesRequest {
				req := validServicesRequest(t)
				req.PaymentProviders = []paymentprovider.Provider{nil}
				return req
			},
			wantErr: ErrNilPaymentProvider,
		},
		{
			name: "Failure - custom registry and providers conflict",
			request: func(t *testing.T) *NewServicesRequest {
				req := validServicesRequest(t)
				req.PaymentProviderRegistry = paymentprovider.NewProviderRegistry()
				req.PaymentProviders = []paymentprovider.Provider{nil}
				return req
			},
			wantErr: ErrPaymentProviderRegistryConflict,
		},
		{
			name:    "Success - valid dependencies create concrete services",
			request: validServicesRequest,
			assert: func(t *testing.T, got *Services) {
				t.Helper()
				if got.AccessManager == nil || got.APIToken == nil || got.Audit == nil ||
					got.Auth == nil || got.Billing == nil || got.BillingManager == nil ||
					got.Contacter == nil || got.ContentManager == nil || got.Group == nil ||
					got.Notifier == nil || got.Policy == nil || got.Post == nil ||
					got.Pricer == nil || got.User == nil || got.UserManager == nil {
					t.Fatalf("expected all services to be populated: %#v", got)
				}
				if got.AccessManager.GroupService != got.Group {
					t.Fatalf("expected access manager to receive group service")
				}
				if got.AccessManager.BillingService != got.Billing {
					t.Fatalf("expected access manager to receive billing service")
				}
				if got.UserManager.GroupService != got.Group {
					t.Fatalf("expected user manager to receive group service")
				}
				if got.UserManager.NotifierService != got.Notifier {
					t.Fatalf("expected user manager to receive notifier service")
				}
			},
		},
		{
			name: "Success - policy config can generate a policy store",
			request: func(t *testing.T) *NewServicesRequest {
				req := validServicesRequest(t)
				req.PolicyStore = nil
				req.PolicyConfig = &PolicyConfig{
					BusinessEntityName:      "Example",
					BusinessEntityEmail:     "hello@example.test",
					BusinessEntityWebsite:   "https://example.test",
					LegalBusinessEntityName: "Example Ltd",
					GenerateStaticPolicies:  true,
				}
				return req
			},
			assert: func(t *testing.T, got *Services) {
				t.Helper()
				store, ok := got.Policy.Store.(*policy.Store)
				if !ok {
					t.Fatalf("expected concrete policy store, got %T", got.Policy.Store)
				}
				if len(store.Policies) == 0 {
					t.Fatalf("expected generated static policies")
				}
			},
		},
		{
			name: "Success - custom payment provider registry is preserved",
			request: func(t *testing.T) *NewServicesRequest {
				req := validServicesRequest(t)
				req.PaymentProviderRegistry = customPaymentProviderRegistry
				return req
			},
			assert: func(t *testing.T, got *Services) {
				t.Helper()
				if got.PaymentProviderRegistry != customPaymentProviderRegistry {
					t.Fatalf("expected custom payment provider registry to be preserved")
				}
			},
		},
		{
			name: "Success - custom audit service is preserved",
			request: func(t *testing.T) *NewServicesRequest {
				req := validServicesRequest(t)
				req.AuditService = customAuditService
				return req
			},
			assert: func(t *testing.T, got *Services) {
				t.Helper()
				if got.Audit != customAuditService {
					t.Fatalf("expected custom audit service to be preserved")
				}
				if got.AccessManager.AuditService != customAuditService {
					t.Fatalf("expected access manager to receive custom audit service")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NewServices(tt.request(t))
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("expected %v, got %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
			if got == nil {
				t.Fatal("expected services")
			}
			if tt.assert != nil {
				tt.assert(t, got)
			}
		})
	}
}

func TestResolvePostTags(t *testing.T) {
	tests := []struct {
		name     string
		tags     []string
		wantLen  int
		wantSame bool
	}{
		{
			name:    "Success - nil tags use starter defaults",
			tags:    nil,
			wantLen: len(post.DefaultValidPostTags),
		},
		{
			name:     "Success - empty tags intentionally clear defaults",
			tags:     []string{},
			wantLen:  0,
			wantSame: true,
		},
		{
			name:     "Success - custom tags are passed through",
			tags:     []string{"release"},
			wantLen:  1,
			wantSame: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolvePostTags(tt.tags)
			if len(got) != tt.wantLen {
				t.Fatalf("expected %d tags, got %d", tt.wantLen, len(got))
			}
			if tt.wantSame && len(tt.tags) > 0 && &got[0] != &tt.tags[0] {
				t.Fatalf("expected custom tags to be passed through")
			}
			if tt.tags == nil && len(got) > 0 {
				got[0] = "mutated"
				second := resolvePostTags(nil)
				if second[0] == "mutated" {
					t.Fatalf("expected default tags to be copied")
				}
			}
		})
	}
}

func TestNewHandlers(t *testing.T) {
	tests := []struct {
		name    string
		request func(t *testing.T) *NewHandlersRequest
		wantErr error
		assert  func(t *testing.T, got *Handlers)
	}{
		{
			name:    "Failure - nil request",
			request: func(t *testing.T) *NewHandlersRequest { return nil },
			wantErr: ErrNilHandlersRequest,
		},
		{
			name: "Failure - nil services",
			request: func(t *testing.T) *NewHandlersRequest {
				req := validHandlersRequest(t)
				req.Services = nil
				return req
			},
			wantErr: ErrNilServices,
		},
		{
			name: "Failure - incomplete services",
			request: func(t *testing.T) *NewHandlersRequest {
				req := validHandlersRequest(t)
				req.Services = &Services{}
				return req
			},
			wantErr: ErrNilServices,
		},
		{
			name: "Failure - nil validator",
			request: func(t *testing.T) *NewHandlersRequest {
				req := validHandlersRequest(t)
				req.Validator = nil
				return req
			},
			wantErr: ErrNilValidator,
		},
		{
			name:    "Success - default error bundles create handlers",
			request: validHandlersRequest,
			assert: func(t *testing.T, got *Handlers) {
				t.Helper()
				if got.AccessManager == nil || got.BillingManager == nil ||
					got.ContentManager == nil || got.Group == nil || got.Policy == nil ||
					got.Pricer == nil || got.User == nil || got.UserManager == nil {
					t.Fatalf("expected all handlers to be populated: %#v", got)
				}
				if got.AccessManager.Environment != "local" {
					t.Fatalf("expected accessmanager handler environment to be set")
				}
				if got.UserManager.CookieDomain != "example.test" {
					t.Fatalf("expected usermanager handler cookie domain to be set")
				}
			},
		},
		{
			name: "Success - explicit empty error maps are accepted",
			request: func(t *testing.T) *NewHandlersRequest {
				req := validHandlersRequest(t)
				req.ErrorMaps = &HandlerErrorMaps{
					AccessManager:  []reply.ErrorManifest{},
					BillingManager: []reply.ErrorManifest{},
					ContentManager: []reply.ErrorManifest{},
					UserManager:    []reply.ErrorManifest{},
				}
				return req
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NewHandlers(tt.request(t))
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("expected %v, got %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
			if got == nil {
				t.Fatal("expected handlers")
			}
			if tt.assert != nil {
				tt.assert(t, got)
			}
		})
	}
}

func TestNewMiddleware(t *testing.T) {
	tests := []struct {
		name    string
		request func(t *testing.T) *NewMiddlewareRequest
		wantErr error
	}{
		{
			name:    "Failure - nil request",
			request: func(t *testing.T) *NewMiddlewareRequest { return nil },
			wantErr: ErrNilMiddlewareRequest,
		},
		{
			name: "Failure - nil services",
			request: func(t *testing.T) *NewMiddlewareRequest {
				req := validMiddlewareRequest(t)
				req.Services = nil
				return req
			},
			wantErr: ErrNilServices,
		},
		{
			name: "Failure - missing accessmanager service",
			request: func(t *testing.T) *NewMiddlewareRequest {
				req := validMiddlewareRequest(t)
				services := *req.Services
				services.AccessManager = nil
				req.Services = &services
				return req
			},
			wantErr: ErrNilAccessManagerService,
		},
		{
			name: "Failure - missing ephemeral store",
			request: func(t *testing.T) *NewMiddlewareRequest {
				req := validMiddlewareRequest(t)
				services := *req.Services
				services.EphemeralStore = nil
				req.Services = &services
				return req
			},
			wantErr: ErrNilEphemeralStore,
		},
		{
			name: "Failure - service ephemeral store lacks hardened methods",
			request: func(t *testing.T) *NewMiddlewareRequest {
				req := validMiddlewareRequest(t)
				services := *req.Services
				services.EphemeralStore = fakeAccessEphemeralStore{}
				req.Services = &services
				return req
			},
			wantErr: ErrInvalidEphemeralStore,
		},
		{
			name:    "Success - default middleware suite",
			request: validMiddlewareRequest,
		},
		{
			name: "Success - middleware accepts explicit hardened store override",
			request: func(t *testing.T) *NewMiddlewareRequest {
				req := validMiddlewareRequest(t)
				services := *req.Services
				services.EphemeralStore = fakeAccessEphemeralStore{}
				req.Services = &services
				req.EphemeralStore = fakeHardenedRateLimitStore{}
				return req
			},
		},
		{
			name: "Success - explicit empty error maps are accepted",
			request: func(t *testing.T) *NewMiddlewareRequest {
				req := validMiddlewareRequest(t)
				req.ErrorMaps = []reply.ErrorManifest{}
				return req
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NewMiddleware(tt.request(t))
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("expected %v, got %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
			if got == nil || got.AccessManager == nil {
				t.Fatalf("expected accessmanager middleware suite")
			}
			if got.AccessManager.AdminOnly == nil ||
				got.AccessManager.ActiveOnly == nil ||
				got.AccessManager.Authenticated == nil ||
				got.AccessManager.HardenedRateLimit == nil {
				t.Fatalf("expected core middleware functions to be populated: %#v", got.AccessManager)
			}
		})
	}
}

func TestNewStack_LayersPreserved(t *testing.T) {
	cleanupCalled := false
	cleanupFn := func(ctx context.Context) error {
		cleanupCalled = true
		return nil
	}
	repos := validRepositories(t)
	services := validServices(t)
	handlers := validHandlers(t)
	middleware := validMiddleware(t)

	stack, err := NewStack(&NewStackRequest{
		Config: Config{
			Port:        8080,
			Environment: "local",
			LogLevel:    "debug",
		},
		Repositories: repos,
		Services:     services,
		Handlers:     handlers,
		Middleware:   middleware,
		Cleanup:      cleanupFn,
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if stack.Repositories != repos {
		t.Fatalf("expected supplied repositories to be preserved")
	}
	if stack.Services != services {
		t.Fatalf("expected supplied services to be preserved")
	}
	if stack.Handlers != handlers {
		t.Fatalf("expected supplied handlers to be preserved")
	}
	if stack.Middleware != middleware {
		t.Fatalf("expected supplied middleware to be preserved")
	}
	if stack.Cleanup == nil {
		t.Fatalf("expected supplied cleanup to be preserved")
	}
	if err := stack.Cleanup(context.Background()); err != nil {
		t.Fatalf("expected cleanup to run without error, got %v", err)
	}
	if !cleanupCalled {
		t.Fatalf("expected supplied cleanup to be callable from stack")
	}
}

func TestResolvePolicyStore(t *testing.T) {
	supplied := &fakePolicyStore{}

	tests := []struct {
		name    string
		store   policy.PolicyStore
		cfg     *PolicyConfig
		wantErr error
		assert  func(t *testing.T, got policy.PolicyStore)
	}{
		{
			name:  "GOOD - caller-supplied store is returned",
			store: supplied,
			assert: func(t *testing.T, got policy.PolicyStore) {
				if got != supplied {
					t.Fatalf("expected supplied store to be preserved")
				}
			},
		},
		{
			name:    "BAD - both store and config are nil",
			wantErr: ErrNilPolicyConfig,
		},
		{
			name: "BAD - missing business entity name",
			cfg: &PolicyConfig{
				BusinessEntityEmail:     "hello@example.test",
				BusinessEntityWebsite:   "https://example.test",
				LegalBusinessEntityName: "Example Ltd",
			},
			wantErr: ErrInvalidPolicyConfig,
		},
		{
			name: "BAD - missing business entity email",
			cfg: &PolicyConfig{
				BusinessEntityName:      "Example",
				BusinessEntityWebsite:   "https://example.test",
				LegalBusinessEntityName: "Example Ltd",
			},
			wantErr: ErrInvalidPolicyConfig,
		},
		{
			name: "BAD - missing business entity website",
			cfg: &PolicyConfig{
				BusinessEntityName:      "Example",
				BusinessEntityEmail:     "hello@example.test",
				LegalBusinessEntityName: "Example Ltd",
			},
			wantErr: ErrInvalidPolicyConfig,
		},
		{
			name: "BAD - missing legal business entity name",
			cfg: &PolicyConfig{
				BusinessEntityName:    "Example",
				BusinessEntityEmail:   "hello@example.test",
				BusinessEntityWebsite: "https://example.test",
			},
			wantErr: ErrInvalidPolicyConfig,
		},
		{
			name: "BAD - website without :// with GenerateStaticPolicies",
			cfg: &PolicyConfig{
				BusinessEntityName:      "Example",
				BusinessEntityEmail:     "hello@example.test",
				BusinessEntityWebsite:   "example.test",
				LegalBusinessEntityName: "Example Ltd",
				GenerateStaticPolicies:  true,
			},
			wantErr: ErrInvalidPolicyConfig,
		},
		{
			name: "GOOD - website without :// when GenerateStaticPolicies is false",
			cfg: &PolicyConfig{
				BusinessEntityName:      "Example",
				BusinessEntityEmail:     "hello@example.test",
				BusinessEntityWebsite:   "example.test",
				LegalBusinessEntityName: "Example Ltd",
				GenerateStaticPolicies:  false,
			},
			assert: func(t *testing.T, got policy.PolicyStore) {
				if got == nil {
					t.Fatalf("expected policy store")
				}
			},
		},
		{
			name: "GOOD - valid config with GenerateStaticPolicies creates store",
			cfg: &PolicyConfig{
				BusinessEntityName:      "Example",
				BusinessEntityEmail:     "hello@example.test",
				BusinessEntityWebsite:   "https://example.test",
				LegalBusinessEntityName: "Example Ltd",
				GenerateStaticPolicies:  true,
			},
			assert: func(t *testing.T, got policy.PolicyStore) {
				if got == nil {
					t.Fatalf("expected policy store")
				}
			},
		},
		{
			name: "GOOD - valid config without GenerateStaticPolicies",
			cfg: &PolicyConfig{
				BusinessEntityName:      "Example",
				BusinessEntityEmail:     "hello@example.test",
				BusinessEntityWebsite:   "https://example.test",
				LegalBusinessEntityName: "Example Ltd",
			},
			assert: func(t *testing.T, got policy.PolicyStore) {
				if got == nil {
					t.Fatalf("expected policy store")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolvePolicyStore(tt.store, tt.cfg)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("expected %v, got %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
			if tt.assert != nil {
				tt.assert(t, got)
			}
		})
	}
}

func TestResolvePaymentProviderRegistry(t *testing.T) {
	suppliedRegistry := paymentprovider.NewProviderRegistry()

	tests := []struct {
		name      string
		registry  billingmanager.ProviderRegistry
		providers []paymentprovider.Provider
		wantErr   error
		assert    func(t *testing.T, got billingmanager.ProviderRegistry)
	}{
		{
			name:     "GOOD - supplied registry is returned when no providers",
			registry: suppliedRegistry,
			assert: func(t *testing.T, got billingmanager.ProviderRegistry) {
				if got != suppliedRegistry {
					t.Fatalf("expected supplied registry to be preserved")
				}
			},
		},
		{
			name:     "BAD - registry and providers both supplied",
			registry: suppliedRegistry,
			providers: []paymentprovider.Provider{
				fakePaymentProvider{},
			},
			wantErr: ErrPaymentProviderRegistryConflict,
		},
		{
			name:      "GOOD - empty providers create empty registry",
			providers: []paymentprovider.Provider{},
			assert: func(t *testing.T, got billingmanager.ProviderRegistry) {
				if got == nil {
					t.Fatalf("expected registry")
				}
			},
		},
		{
			name: "GOOD - providers create registry",
			providers: []paymentprovider.Provider{
				fakePaymentProvider{},
			},
			assert: func(t *testing.T, got billingmanager.ProviderRegistry) {
				if got == nil {
					t.Fatalf("expected registry")
				}
			},
		},
		{
			name: "BAD - nil provider in list",
			providers: []paymentprovider.Provider{
				fakePaymentProvider{},
				nil,
			},
			wantErr: ErrNilPaymentProvider,
		},
		{
			name: "GOOD - nil registry with nil providers creates new registry",
			assert: func(t *testing.T, got billingmanager.ProviderRegistry) {
				if got == nil {
					t.Fatalf("expected registry")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolvePaymentProviderRegistry(tt.registry, tt.providers)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("expected %v, got %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
			if tt.assert != nil {
				tt.assert(t, got)
			}
		})
	}
}

func TestResolveHandlerErrorMaps(t *testing.T) {
	defaults := []reply.ErrorManifest{
		{errors.New("default"): {Code: "default", Title: "default"}},
	}
	custom := []reply.ErrorManifest{
		{errors.New("custom"): {Code: "custom", Title: "custom"}},
	}
	empty := []reply.ErrorManifest{}

	tests := []struct {
		name    string
		custom  []reply.ErrorManifest
		wantLen int
	}{
		{
			name:    "GOOD - nil custom returns defaults",
			custom:  nil,
			wantLen: 1,
		},
		{
			name:    "GOOD - custom maps are passed through",
			custom:  custom,
			wantLen: 1,
		},
		{
			name:    "GOOD - empty allows explicit clearing",
			custom:  empty,
			wantLen: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveHandlerErrorMaps(tt.custom, defaults)
			if got == nil {
				t.Fatalf("expected non-nil, got nil")
			}
			if len(got) != tt.wantLen {
				t.Fatalf("expected %d entries, got %d", tt.wantLen, len(got))
			}
		})
	}
}

func TestValidateRepositoriesForServices(t *testing.T) {
	fullRepos := validRepositories(t)
	core := fullRepos.Core

	tests := []struct {
		name    string
		mutate  func(*Repositories)
		wantErr error
	}{
		{
			name:    "GOOD - full repositories",
			wantErr: nil,
		},
		{
			name:    "BAD - nil repositories",
			mutate:  func(r *Repositories) { *r = Repositories{} },
			wantErr: ErrNilRepositories,
		},
		{
			name:    "BAD - nil APIToken",
			mutate:  func(r *Repositories) { r.APIToken = nil },
			wantErr: ErrNilRepositories,
		},
		{
			name:    "BAD - nil Audit",
			mutate:  func(r *Repositories) { r.Audit = nil },
			wantErr: ErrNilRepositories,
		},
		{
			name:    "BAD - nil Billing",
			mutate:  func(r *Repositories) { r.Billing = nil },
			wantErr: ErrNilRepositories,
		},
		{
			name:    "BAD - nil Contacter",
			mutate:  func(r *Repositories) { r.Contacter = nil },
			wantErr: ErrNilRepositories,
		},
		{
			name:    "BAD - nil Group",
			mutate:  func(r *Repositories) { r.Group = nil },
			wantErr: ErrNilRepositories,
		},
		{
			name:    "BAD - nil Notifier",
			mutate:  func(r *Repositories) { r.Notifier = nil },
			wantErr: ErrNilRepositories,
		},
		{
			name:    "BAD - nil Post",
			mutate:  func(r *Repositories) { r.Post = nil },
			wantErr: ErrNilRepositories,
		},
		{
			name:    "BAD - nil Pricer",
			mutate:  func(r *Repositories) { r.Pricer = nil },
			wantErr: ErrNilRepositories,
		},
		{
			name:    "BAD - nil User",
			mutate:  func(r *Repositories) { r.User = nil },
			wantErr: ErrNilRepositories,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repos := &Repositories{
				Core:      core,
				APIToken:  fullRepos.APIToken,
				Audit:     fullRepos.Audit,
				Billing:   fullRepos.Billing,
				Contacter: fullRepos.Contacter,
				Group:     fullRepos.Group,
				Notifier:  fullRepos.Notifier,
				Post:      fullRepos.Post,
				Pricer:    fullRepos.Pricer,
				User:      fullRepos.User,
			}
			if tt.name == "BAD - nil repositories" {
				err := validateRepositoriesForServices(nil)
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("expected %v, got %v", tt.wantErr, err)
				}
				return
			}
			if tt.mutate != nil {
				tt.mutate(repos)
			}

			err := validateRepositoriesForServices(repos)
			if tt.wantErr == nil {
				if err != nil {
					t.Fatalf("expected no error, got %v", err)
				}
				return
			}
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("expected %v, got %v", tt.wantErr, err)
			}
		})
	}
}

func validRepositories(t *testing.T) *Repositories {
	t.Helper()

	repos, err := NewRepositories(&NewRepositoriesRequest{
		Core: &repository.MongoDbRepository{},
	})
	if err != nil {
		t.Fatalf("creating repositories: %v", err)
	}

	return repos
}

func validServicesRequest(t *testing.T) *NewServicesRequest {
	t.Helper()

	return &NewServicesRequest{
		Repositories:               validRepositories(t),
		EphemeralStore:             fakeEphemeralStore{},
		EmailManager:               fakeEmailManager{},
		AccessTokenSecret:          "access-token-secret",
		RefreshTokenSecret:         "refresh-token-secret",
		StaticPlaceholderUUID:      "00000000-0000-0000-0000-000000000000",
		PolicyStore:                &fakePolicyStore{},
		AutoAdminEmailAddressRegex: "^admin@example.test$",
	}
}

func validServices(t *testing.T) *Services {
	t.Helper()

	services, err := NewServices(validServicesRequest(t))
	if err != nil {
		t.Fatalf("creating services: %v", err)
	}

	return services
}

func validHandlersRequest(t *testing.T) *NewHandlersRequest {
	t.Helper()

	return &NewHandlersRequest{
		Services:                 validServices(t),
		Validator:                fakeValidator{},
		Environment:              "local",
		CookiePrefixAuthToken:    "auth",
		CookiePrefixRefreshToken: "refresh",
		CookieDomain:             "example.test",
	}
}

func validMiddlewareRequest(t *testing.T) *NewMiddlewareRequest {
	t.Helper()

	return &NewMiddlewareRequest{
		Services:                 validServices(t),
		Environment:              "local",
		CookiePrefixAuthToken:    "auth",
		CookiePrefixRefreshToken: "refresh",
		CookieDomain:             "example.test",
	}
}

type fakeValidator struct{}

func (fakeValidator) Validate(s interface{}) error {
	return nil
}

type fakePaymentProvider struct{}

func (fakePaymentProvider) GetProviderName() string                                    { return "fake" }
func (fakePaymentProvider) VerifyWebhook(ctx context.Context, req *http.Request) error { return nil }
func (fakePaymentProvider) ParsePayload(ctx context.Context, req *http.Request) (*paymentprovider.WebhookPayload, error) {
	return nil, nil
}
func (fakePaymentProvider) GetSubscriptionInfo(ctx context.Context, subscriptionID string) (*paymentprovider.SubscriptionInfo, error) {
	return nil, nil
}

type fakePolicyStore struct {
	policies []policy.WebAppPolicy
}

func (f *fakePolicyStore) GenerateStaticPolicies() {
	f.policies = append(f.policies, policy.WebAppPolicy{Name: "Terms and Conditions"})
}

func (f *fakePolicyStore) GetPolicies() []policy.WebAppPolicy {
	return f.policies
}

func (f *fakePolicyStore) AddPolicy(p policy.WebAppPolicy) {
	f.policies = append(f.policies, p)
}

type fakeEmailManager struct{}

func (fakeEmailManager) SendCustomEmail(ctx context.Context, req *emailmanager.SendCustomEmailRequest) error {
	return nil
}

func (fakeEmailManager) SendLoginEmail(ctx context.Context, req *emailmanager.SendLoginEmailRequest) error {
	return nil
}

func (fakeEmailManager) SendVerificationEmail(ctx context.Context, req *emailmanager.SendVerificationEmailRequest) error {
	return nil
}

type fakeEphemeralStore struct{}

func (fakeEphemeralStore) CreateAuth(ctx context.Context, userID string, tokenDetails ephemeral.TokenDetailsAuth) error {
	return nil
}

func (fakeEphemeralStore) StoreToken(ctx context.Context, accessTokenUUID string, userID string, ttl time.Duration) error {
	return nil
}

func (fakeEphemeralStore) FetchAuth(ctx context.Context, accessDetails ephemeral.TokenDetailsAccess) (string, error) {
	return "", nil
}

func (fakeEphemeralStore) DeleteAuth(ctx context.Context, tokenID string) (int64, error) {
	return 0, nil
}

func (fakeEphemeralStore) AddRequestCountEntry(ctx context.Context, clientIP string) error {
	return nil
}

func (fakeEphemeralStore) DeleteAllTokenExceptedSpecified(ctx context.Context, userID string, exemptionTokenIDs []string) error {
	return nil
}

func (fakeEphemeralStore) CodeExists(ctx context.Context, code string) (bool, error) {
	return false, nil
}

func (fakeEphemeralStore) StoreCode(ctx context.Context, code string, ttl time.Duration) error {
	return nil
}

func (fakeEphemeralStore) StoreCodeMapping(ctx context.Context, code, token string, ttl time.Duration) error {
	return nil
}

func (fakeEphemeralStore) GetCodeMapping(ctx context.Context, code string) (string, error) {
	return "", nil
}

func (fakeEphemeralStore) TrackHardenedAttempt(ctx context.Context, ip, code string, maxAttempts int, window time.Duration) error {
	return nil
}

func (fakeEphemeralStore) BlockIP(ctx context.Context, ip string, duration time.Duration) error {
	return nil
}

func (fakeEphemeralStore) IsIPBlocked(ctx context.Context, ip string) (bool, error) {
	return false, nil
}

type fakeAccessEphemeralStore struct{}

func (fakeAccessEphemeralStore) CreateAuth(ctx context.Context, userID string, tokenDetails ephemeral.TokenDetailsAuth) error {
	return nil
}

func (fakeAccessEphemeralStore) StoreToken(ctx context.Context, accessTokenUUID string, userID string, ttl time.Duration) error {
	return nil
}

func (fakeAccessEphemeralStore) FetchAuth(ctx context.Context, accessDetails ephemeral.TokenDetailsAccess) (string, error) {
	return "", nil
}

func (fakeAccessEphemeralStore) DeleteAuth(ctx context.Context, tokenID string) (int64, error) {
	return 0, nil
}

func (fakeAccessEphemeralStore) AddRequestCountEntry(ctx context.Context, clientIP string) error {
	return nil
}

func (fakeAccessEphemeralStore) DeleteAllTokenExceptedSpecified(ctx context.Context, userID string, exemptionTokenIDs []string) error {
	return nil
}

func (fakeAccessEphemeralStore) CodeExists(ctx context.Context, code string) (bool, error) {
	return false, nil
}

func (fakeAccessEphemeralStore) StoreCode(ctx context.Context, code string, ttl time.Duration) error {
	return nil
}

func (fakeAccessEphemeralStore) StoreCodeMapping(ctx context.Context, code, token string, ttl time.Duration) error {
	return nil
}

func (fakeAccessEphemeralStore) GetCodeMapping(ctx context.Context, code string) (string, error) {
	return "", nil
}

type fakeHardenedRateLimitStore struct{}

func (fakeHardenedRateLimitStore) TrackHardenedAttempt(ctx context.Context, ip, code string, maxAttempts int, window time.Duration) error {
	return nil
}

func (fakeHardenedRateLimitStore) BlockIP(ctx context.Context, ip string, duration time.Duration) error {
	return nil
}

func (fakeHardenedRateLimitStore) IsIPBlocked(ctx context.Context, ip string) (bool, error) {
	return false, nil
}
