package starter

import (
	"github.com/ooaklee/ghatd/external/accessmanager"
	"github.com/ooaklee/ghatd/external/billingmanager"
	"github.com/ooaklee/ghatd/external/contentmanager"
	bundles "github.com/ooaklee/ghatd/external/errormanifest/bundles"
	"github.com/ooaklee/ghatd/external/group"
	"github.com/ooaklee/ghatd/external/policy"
	"github.com/ooaklee/ghatd/external/pricer"
	userv2 "github.com/ooaklee/ghatd/external/user/v2"
	"github.com/ooaklee/ghatd/external/usermanager"
	"github.com/ooaklee/ghatd/external/vision"
	"github.com/ooaklee/reply/v2"
)

// Validator is the minimal validator contract shared by GHATD handlers.
type Validator interface {
	Validate(s interface{}) error
}

// Handlers groups the standard GHATD HTTP handlers.
type Handlers struct {
	AccessManager  *accessmanager.Handler
	BillingManager *billingmanager.Handler
	ContentManager *contentmanager.Handler
	Group          *group.Handler
	Policy         *policy.Handler
	Pricer         *pricer.Handler
	User           *userv2.Handler
	UserManager    *usermanager.Handler
	Vision         *vision.Handler
}

// HandlerErrorMaps allows callers to override or clear default cross-package
// error maps. Nil uses starter defaults; an empty slice is treated as an
// explicit override.
type HandlerErrorMaps struct {
	AccessManager  []reply.ErrorManifest
	BillingManager []reply.ErrorManifest
	ContentManager []reply.ErrorManifest
	Group          []reply.ErrorManifest
	Policy         []reply.ErrorManifest
	Pricer         []reply.ErrorManifest
	User           []reply.ErrorManifest
	UserManager    []reply.ErrorManifest
	Vision         []reply.ErrorManifest
}

// NewHandlersRequest holds dependencies for handler construction.
type NewHandlersRequest struct {
	Services  *Services
	Validator Validator

	Environment              string
	CookiePrefixAuthToken    string
	CookiePrefixRefreshToken string
	CookieDomain             string

	ErrorMaps *HandlerErrorMaps
}

// NewHandlers creates the standard GHATD handler container.
func NewHandlers(r *NewHandlersRequest) (*Handlers, error) {
	if r == nil {
		return nil, ErrNilHandlersRequest
	}
	if err := validateServicesForHandlers(r.Services); err != nil {
		return nil, err
	}
	if r.Validator == nil {
		return nil, ErrNilValidator
	}

	errorMaps := r.ErrorMaps
	if errorMaps == nil {
		errorMaps = &HandlerErrorMaps{}
	}

	return &Handlers{
		AccessManager: accessmanager.NewHandler(&accessmanager.NewHandlerRequest{
			Service:                  r.Services.AccessManager,
			Validator:                r.Validator,
			ErrorMaps:                resolveHandlerErrorMaps(errorMaps.AccessManager, bundles.AccessManager()),
			Environment:              r.Environment,
			CookiePrefixAuthToken:    r.CookiePrefixAuthToken,
			CookiePrefixRefreshToken: r.CookiePrefixRefreshToken,
			CookieDomain:             r.CookieDomain,
		}),
		BillingManager: billingmanager.NewHandler(
			r.Services.BillingManager,
			r.Validator,
			resolveHandlerErrorMaps(errorMaps.BillingManager, bundles.BillingManager())...,
		),
		ContentManager: contentmanager.NewHandler(
			r.Services.ContentManager,
			r.Validator,
			resolveHandlerErrorMaps(errorMaps.ContentManager, bundles.ContentManager())...,
		),
		Group:  group.NewHandler(r.Services.Group, r.Validator, resolveHandlerErrorMaps(errorMaps.Group, nil)...),
		Policy: policy.NewHandler(r.Services.Policy, r.Validator, resolveHandlerErrorMaps(errorMaps.Policy, nil)...),
		Pricer: pricer.NewHandler(r.Services.Pricer, r.Validator, resolveHandlerErrorMaps(errorMaps.Pricer, nil)...),
		User:   userv2.NewHandler(r.Services.User, r.Validator, resolveHandlerErrorMaps(errorMaps.User, nil)...),
		UserManager: usermanager.NewHandler(&usermanager.NewHandlerRequest{
			Service:                  r.Services.UserManager,
			Validator:                r.Validator,
			ErrorMaps:                resolveHandlerErrorMaps(errorMaps.UserManager, bundles.UserManager()),
			Environment:              r.Environment,
			CookiePrefixAuthToken:    r.CookiePrefixAuthToken,
			CookiePrefixRefreshToken: r.CookiePrefixRefreshToken,
			CookieDomain:             r.CookieDomain,
		}),
		Vision: vision.NewHandler(
			r.Services.Vision,
			r.Validator,
			resolveHandlerErrorMaps(errorMaps.Vision, nil)...,
		),
	}, nil
}

// validateServicesForHandlers ensures every required service is present for handler construction.
func validateServicesForHandlers(services *Services) error {
	if services == nil {
		return ErrNilServices
	}
	if services.AccessManager == nil ||
		services.BillingManager == nil ||
		services.ContentManager == nil ||
		services.Group == nil ||
		services.Policy == nil ||
		services.Pricer == nil ||
		services.User == nil ||
		services.UserManager == nil ||
		services.Vision == nil {
		return ErrNilServices
	}

	return nil
}

// resolveHandlerErrorMaps returns custom error maps when provided, otherwise
// returns the supplied defaults.
func resolveHandlerErrorMaps(custom []reply.ErrorManifest, defaults []reply.ErrorManifest) []reply.ErrorManifest {
	if custom != nil {
		return custom
	}

	return defaults
}
