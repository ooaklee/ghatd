package starter

import (
	"errors"
	"fmt"
)

var (
	// ErrNilStackRequest is returned when NewStack is called without a request.
	ErrNilStackRequest = errors.New("starter/stack-request-required")

	// ErrNilRepositoriesRequest is returned when NewRepositories is called without a request.
	ErrNilRepositoriesRequest = errors.New("starter/repositories-request-required")

	// ErrNilCoreRepository is returned when repository construction has no core Mongo repository.
	ErrNilCoreRepository = errors.New("starter/core-repository-required")

	// ErrNilServicesRequest is returned when NewServices is called without a request.
	ErrNilServicesRequest = errors.New("starter/services-request-required")

	// ErrNilRepositories is returned when service construction has no repository container.
	ErrNilRepositories = errors.New("starter/repositories-required")

	// ErrNilEphemeralStore is returned when access-related services or middleware lack a store.
	ErrNilEphemeralStore = errors.New("starter/ephemeral-store-required")

	// ErrInvalidEphemeralStore is returned when middleware storage lacks hardened rate-limit methods.
	ErrInvalidEphemeralStore = errors.New("starter/ephemeral-store-invalid")

	// ErrNilEmailManager is returned when accessmanager service construction lacks an email manager.
	ErrNilEmailManager = errors.New("starter/email-manager-required")

	// ErrMissingAccessTokenSecret is returned when auth service construction lacks an access secret.
	ErrMissingAccessTokenSecret = errors.New("starter/access-token-secret-required")

	// ErrMissingRefreshTokenSecret is returned when auth service construction lacks a refresh secret.
	ErrMissingRefreshTokenSecret = errors.New("starter/refresh-token-secret-required")

	// ErrNilPolicyConfig is returned when policy service construction lacks a store or config.
	ErrNilPolicyConfig = errors.New("starter/policy-config-required")

	// ErrInvalidPolicyConfig is returned when a policy config cannot safely create static policies.
	ErrInvalidPolicyConfig = errors.New("starter/policy-config-invalid")

	// ErrNilPaymentProvider is returned when a nil payment provider is registered through starter.
	ErrNilPaymentProvider = errors.New("starter/payment-provider-required")

	// ErrPaymentProviderRegistryConflict is returned when a custom registry and providers are both supplied.
	ErrPaymentProviderRegistryConflict = errors.New("starter/payment-provider-registry-conflict")

	// ErrNilHandlersRequest is returned when NewHandlers is called without a request.
	ErrNilHandlersRequest = errors.New("starter/handlers-request-required")

	// ErrNilServices is returned when handler or middleware construction has no service container.
	ErrNilServices = errors.New("starter/services-required")

	// ErrNilValidator is returned when handler construction has no request validator.
	ErrNilValidator = errors.New("starter/validator-required")

	// ErrNilMiddlewareRequest is returned when NewMiddleware is called without a request.
	ErrNilMiddlewareRequest = errors.New("starter/middleware-request-required")

	// ErrNilAccessManagerService is returned when middleware construction has no accessmanager service.
	ErrNilAccessManagerService = errors.New("starter/accessmanager-service-required")

	// ErrNilMiddleware is returned when AttachDefaultRoutesRequest.Stack.Middleware is nil.
	ErrNilMiddleware = errors.New("starter/middleware-required")

	// ErrNilMiddlewareSuite is returned when Stack.Middleware.AccessManager is nil.
	ErrNilMiddlewareSuite = errors.New("starter/middleware-suite-required")

	// ErrNilAttachDefaultRoutesRequest is returned when AttachDefaultRoutes is called without a request.
	ErrNilAttachDefaultRoutesRequest = errors.New("starter/attach-default-routes-request-required")

	// ErrUnknownRouteGroup is returned when AttachDefaultRoutesRequest.Skip has an unknown group.
	ErrUnknownRouteGroup = errors.New("starter/route-group-unknown")

	// ErrNilRouter is returned when AttachDefaultRoutesRequest.Router is nil.
	ErrNilRouter = errors.New("starter/router-required")

	// ErrNilStack is returned when AttachDefaultRoutesRequest.Stack is nil.
	ErrNilStack = errors.New("starter/stack-required")

	// ErrNilHandlers is returned when AttachDefaultRoutesRequest.Stack.Handlers is nil.
	ErrNilHandlers = errors.New("starter/handlers-required")

	// ErrMissingPricerHandler is returned when the pricer route group is not skipped
	// but Stack.Handlers.Pricer is nil.
	ErrMissingPricerHandler = errors.New("starter/pricer-handler-required")

	// ErrMissingPolicyHandler is returned when the policy route group is not skipped
	// but Stack.Handlers.Policy is nil.
	ErrMissingPolicyHandler = errors.New("starter/policy-handler-required")

	// ErrMissingUserHandler is returned when the user route group is not skipped
	// but Stack.Handlers.User is nil.
	ErrMissingUserHandler = errors.New("starter/user-handler-required")

	// ErrMissingGroupHandler is returned when the group route group is not skipped
	// but Stack.Handlers.Group is nil.
	ErrMissingGroupHandler = errors.New("starter/group-handler-required")

	// ErrMissingAccessManagerHandler is returned when the accessmanager route
	// group is not skipped but Stack.Handlers.AccessManager is nil.
	ErrMissingAccessManagerHandler = errors.New("starter/accessmanager-handler-required")

	// ErrMissingUserManagerHandler is returned when the usermanager route group
	// is not skipped but Stack.Handlers.UserManager is nil.
	ErrMissingUserManagerHandler = errors.New("starter/usermanager-handler-required")

	// ErrMissingContentManagerHandler is returned when the contentmanager route
	// group is not skipped but Stack.Handlers.ContentManager is nil.
	ErrMissingContentManagerHandler = errors.New("starter/contentmanager-handler-required")

	// ErrMissingBillingManagerHandler is returned when the billingmanager route
	// group is not skipped but Stack.Handlers.BillingManager is nil.
	ErrMissingBillingManagerHandler = errors.New("starter/billingmanager-handler-required")
)

// newErrMissingMiddleware returns a descriptive error for a nil middleware field
// that is required by at least one non-skipped route group.
func newErrMissingMiddleware(name string) error {
	return fmt.Errorf("starter/middleware-%s-required", name)
}

// newErrUnknownRouteGroup returns a descriptive error for an unknown route
// group in AttachDefaultRoutesRequest.Skip.
func newErrUnknownRouteGroup(group RouteGroup) error {
	return fmt.Errorf("%w: %s", ErrUnknownRouteGroup, group)
}
