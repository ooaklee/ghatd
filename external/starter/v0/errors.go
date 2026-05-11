package starter

import "errors"

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
)
