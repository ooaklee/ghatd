package router

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/ooaklee/ghatd/external/common"
)

// NewAuthVerifyHandler returns a function that handles authentication verification requests.
// The returned function takes an http.ResponseWriter and an *http.Request as arguments,
// and performs the following actions:
//
// 1. Extracts the authentication verification request parameters from the URL query string.
// 2. Checks if the verification token and email type are present in the request.
// 3. Redirects the user to the frontend login URL if the verification token or email type is missing.
// 4. Determines the next step URL based on the requested URL or the frontend login URL.
// 5. Redirects the user to the appropriate API endpoint (login or email verification) based on the email type.
func NewAuthVerifyHandler(apiVerifyEndpoint, apiLoginEndpoint, frontendLoginUrl, frontendAppUrl string) func(http.ResponseWriter, *http.Request) {

	return func(w http.ResponseWriter, r *http.Request) {

		nextStepParam := fmt.Sprintf("&%s=", common.WebNextStepsHttpQueryParam)

		authVerifyRequest := getAuthVerifyRequest(r.URL.RawQuery)

		if authVerifyRequest.VerificationToken == "" || authVerifyRequest.VerificationEmailType == "" {
			http.Redirect(w, r, frontendLoginUrl, http.StatusTemporaryRedirect)
			return
		}

		nextStepParamValue := resolveFrontendNextStep(authVerifyRequest.RequestedUrl, frontendAppUrl)
		encodedNextStepParamValue := url.QueryEscape(nextStepParamValue)

		switch authVerifyRequest.VerificationEmailType {
		// loginVerification
		case "1":
			http.Redirect(w, r, fmt.Sprintf(apiLoginEndpoint, authVerifyRequest.VerificationToken)+nextStepParam+encodedNextStepParamValue, http.StatusTemporaryRedirect)
			return

		// emailVerification
		case "2":
			http.Redirect(w, r, fmt.Sprintf(apiVerifyEndpoint, authVerifyRequest.VerificationToken)+nextStepParam+encodedNextStepParamValue, http.StatusTemporaryRedirect)
			return

		default:
			// Fix: previously called WriteHeader before setting the Location
			// header, causing the redirect target to be silently dropped.
			http.Redirect(w, r, frontendLoginUrl, http.StatusTemporaryRedirect)
			return
		}
	}
}

// resolveFrontendNextStep normalises the optional request_url carried by an auth
// verification link into an absolute frontend URL for the downstream next_step
// redirect. This keeps magic-link redirects on the web app host in local dev,
// where /v0/auth/verify may be served from a separate backend origin, and avoids
// forwarding malformed or external redirect targets.
func resolveFrontendNextStep(requestedUrl string, frontendAppUrl string) string {
	requestedUrl = strings.TrimSpace(requestedUrl)
	if requestedUrl == "" {
		return frontendAppUrl
	}

	frontendURL, err := url.Parse(frontendAppUrl)
	if err != nil || frontendURL.Scheme == "" || frontendURL.Host == "" {
		return frontendAppUrl
	}

	frontendOrigin := &url.URL{
		Scheme: frontendURL.Scheme,
		Host:   frontendURL.Host,
	}

	if strings.HasPrefix(requestedUrl, "/") && !strings.HasPrefix(requestedUrl, "//") {
		requestURL, err := url.Parse(requestedUrl)
		if err != nil {
			return frontendAppUrl
		}

		return frontendOrigin.ResolveReference(requestURL).String()
	}

	requestURL, err := url.Parse(requestedUrl)
	if err != nil || requestURL.Scheme == "" || requestURL.Host == "" {
		return frontendAppUrl
	}

	if requestURL.Scheme != frontendURL.Scheme || requestURL.Host != frontendURL.Host {
		return frontendAppUrl
	}

	return requestURL.String()
}

// authVerifyRequest represents the parameters extracted from the URL query string
// for an authentication verification request.
type authVerifyRequest struct {
	// VerificationToken holds the token used to verify the authentication.
	VerificationToken string

	// RequestedUrl holds the URL that the user originally requested, before being redirected.
	RequestedUrl string

	// VerificationEmailType holds the type of email verification being performed (login or email).
	VerificationEmailType string
}

// getAuthVerifyRequest parses the URL query string and extracts the parameters
// required for an authentication verification request. It returns a pointer to
// an authVerifyRequest struct containing the parsed parameters.
//
// HTML-escaped query separators ("&amp;") are normalised to "&" before parsing.
// Values are URL-decoded by net/url, so encoded characters such as "%2F" are
// returned in their decoded form. Values containing "=" are preserved in full.
func getAuthVerifyRequest(urlRawQuery string) *authVerifyRequest {
	var parsed authVerifyRequest

	// Normalise any HTML-escaped separators so url.ParseQuery can split correctly.
	normalised := strings.ReplaceAll(urlRawQuery, "&amp;", "&")

	values, err := url.ParseQuery(normalised)
	if err != nil {
		return &parsed
	}

	parsed.VerificationToken = values.Get("__t")
	parsed.VerificationEmailType = values.Get("type")
	parsed.RequestedUrl = values.Get("request_url")

	return &parsed
}
