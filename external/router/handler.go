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

		nextStepParamValue := authVerifyRequest.RequestedUrl
		if nextStepParamValue == "" {
			nextStepParamValue = frontendAppUrl
		}

		switch authVerifyRequest.VerificationEmailType {
		// loginVerification
		case "1":
			http.Redirect(w, r, fmt.Sprintf(apiLoginEndpoint, authVerifyRequest.VerificationToken)+nextStepParam+nextStepParamValue, http.StatusTemporaryRedirect)
			return

		// emailVerification
		case "2":
			http.Redirect(w, r, fmt.Sprintf(apiVerifyEndpoint, authVerifyRequest.VerificationToken)+nextStepParam+nextStepParamValue, http.StatusTemporaryRedirect)
			return

		default:
			// Fix: previously called WriteHeader before setting the Location
			// header, causing the redirect target to be silently dropped.
			http.Redirect(w, r, frontendLoginUrl, http.StatusTemporaryRedirect)
			return
		}
	}
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
