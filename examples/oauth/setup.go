// Package oauthsetup demonstrates composing GHATD provider sign-in in a host.
// Copy/adapt this example; applications should depend on external/oauth and
// external/accessmanager, not on this example package as a configuration API.
package oauthsetup

import (
	"fmt"
	"net/url"

	"github.com/go-redis/redis/v7"
	"github.com/ooaklee/ghatd/external/accessmanager"
	"github.com/ooaklee/ghatd/external/oauth"
	starter "github.com/ooaklee/ghatd/external/starter/v0"
)

// Credentials are supplied by the host's environment/secret-store adapter.
// A nil provider disables it; a non-nil, incomplete provider fails startup.
type Credentials struct {
	Google *oauth.NewGoogleSecureProviderRequest
	Apple  *oauth.NewAppleProviderRequest
}

// Compose adds sign-in to an otherwise complete host services/handlers request.
// Apply user/v2/migrations.InitUsersOAuthIndexesUp through the host migrator
// before enabling providers, and attach the returned handlers before serving.
// Origin is an exact origin, without a path or trailing slash. This example uses
// one public origin for the frontend, API, and every provider callback.
// Redis ownership and Apple private-key loading remain with the caller.
func Compose(
	servicesRequest starter.NewServicesRequest,
	handlersRequest starter.NewHandlersRequest,
	redisClient *redis.Client,
	namespace, origin string,
	credentials Credentials,
	mobileRedirects []string,
) (*starter.Services, *starter.Handlers, error) {
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.Opaque != "" ||
		(parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Path != "" ||
		parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" || parsed.String() != origin {
		return nil, nil, fmt.Errorf("sign-in requires an exact public origin")
	}
	if redisClient == nil || namespace == "" {
		return nil, nil, fmt.Errorf("sign-in requires the host Redis client and an app/environment namespace")
	}
	store := oauth.NewRedisTransactionStore(redisClient, namespace)
	providers := []accessmanager.OauthService{}
	if credentials.Google != nil {
		request := *credentials.Google
		request.Store = store
		request.RedirectURL = origin + "/api/v1/ams/oauth/google/callback"
		provider, err := oauth.NewGoogleSecureProvider(&request)
		if err != nil {
			return nil, nil, fmt.Errorf("google sign-in configuration is invalid")
		}
		providers = append(providers, provider)
	}
	if credentials.Apple != nil {
		request := *credentials.Apple
		request.Store = store
		request.RedirectURL = origin + "/api/v1/ams/oauth/apple/callback"
		provider, err := oauth.NewAppleProvider(&request)
		if err != nil {
			return nil, nil, fmt.Errorf("apple sign-in configuration is invalid")
		}
		providers = append(providers, provider)
	}
	servicesRequest.OAuthServices = providers
	services, err := starter.NewServices(&servicesRequest)
	if err != nil {
		return nil, nil, err
	}
	handlersRequest.Services = services
	// Origin is host policy even when both provider choices are disabled.
	handlersRequest.OAuthOrigin = origin
	handlers, err := starter.NewHandlers(&handlersRequest)
	if err != nil {
		return nil, nil, err
	}
	if err := handlers.AccessManager.ConfigureMobileOAuth(accessmanager.MobileOAuthConfig{
		Origin: origin, RedirectURIs: mobileRedirects,
		Store: accessmanager.NewRedisMobileOAuthStore(redisClient, namespace),
	}); err != nil {
		return nil, nil, err
	}
	return services, handlers, nil
}
