package router

import (
	"fmt"
	"net/url"
	"strings"
)

// AttachDefaultAuthVerifyRouteRequest holds the router and base URLs needed to
// construct GHATD's default auth verification redirects.
type AttachDefaultAuthVerifyRouteRequest struct {
	Router          *Router
	BackendBaseURL  string
	FrontendBaseURL string
}

// AttachDefaultAuthVerifyRoute registers GHATD's default auth verification
// handler at AuthVerifyEndpoint. It composes the API verify endpoint, API login
// endpoint, frontend login URL, and frontend app URL from the supplied base
// URLs so host applications do not need to repeat the path suffixes.
//
// Use NewAuthVerifyHandler directly when a host application needs custom
// endpoint paths or redirect behaviour.
func AttachDefaultAuthVerifyRoute(request *AttachDefaultAuthVerifyRouteRequest) error {
	if request == nil {
		return fmt.Errorf("router/auth-verify-route-nil-request")
	}
	if request.Router == nil {
		return fmt.Errorf("router/auth-verify-route-missing-router")
	}
	if request.Router.httpRouter == nil {
		return fmt.Errorf("router/auth-verify-route-missing-http-router")
	}

	backendBaseURL, err := normaliseAbsoluteBaseURL(request.BackendBaseURL)
	if err != nil {
		return fmt.Errorf("router/auth-verify-route-invalid-backend-base-url: %w", err)
	}

	frontendBaseURL, err := normaliseAbsoluteBaseURL(request.FrontendBaseURL)
	if err != nil {
		return fmt.Errorf("router/auth-verify-route-invalid-frontend-base-url: %w", err)
	}

	apiVerifyEndpoint := backendBaseURL + "/api/v1/ams/verify/email?t=%s"
	apiLoginEndpoint := backendBaseURL + "/api/v1/ams/login?t=%s"
	frontendLoginURL := frontendBaseURL + "/auth/login"
	frontendAppURL := frontendBaseURL + "/"

	request.Router.httpRouter.HandleFunc(
		AuthVerifyEndpoint,
		NewAuthVerifyHandler(apiVerifyEndpoint, apiLoginEndpoint, frontendLoginURL, frontendAppURL),
	)

	return nil
}

func normaliseAbsoluteBaseURL(rawBaseURL string) (string, error) {
	rawBaseURL = strings.TrimSpace(rawBaseURL)
	if rawBaseURL == "" {
		return "", fmt.Errorf("missing")
	}

	parsedURL, err := url.Parse(rawBaseURL)
	if err != nil {
		return "", err
	}
	if parsedURL.Scheme == "" || parsedURL.Host == "" {
		return "", fmt.Errorf("absolute-url-required")
	}
	if parsedURL.RawQuery != "" || parsedURL.Fragment != "" {
		return "", fmt.Errorf("query-or-fragment-not-supported")
	}

	return strings.TrimRight(rawBaseURL, "/"), nil
}
