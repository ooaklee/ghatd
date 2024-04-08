package router

import (
	"fmt"
	"net/http"
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

		var nextStepParam = fmt.Sprintf("&%s=", common.WebNextStepsHttpQueryParam)
		var nextStepParamValue string

		authVerifyRequest := getAuthVerifyRequest(r.URL.RawQuery)

		if authVerifyRequest.VerificationToken == "" {
			http.Redirect(w, r, frontendLoginUrl, http.StatusTemporaryRedirect)
			return
		}

		if authVerifyRequest.VerificationEmailType == "" {
			http.Redirect(w, r, frontendLoginUrl, http.StatusTemporaryRedirect)
			return
		}

		if authVerifyRequest.RequestedUrl != "" {
			nextStepParamValue = authVerifyRequest.RequestedUrl
		} else {
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
			w.WriteHeader(http.StatusTemporaryRedirect)
			w.Header().Add("Location", frontendLoginUrl)
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
func getAuthVerifyRequest(urlRawQuery string) *authVerifyRequest {
	var parsedVerifyParams authVerifyRequest
	var requestParms []string

	if strings.Contains(urlRawQuery, "&amp;") {
		requestParms = strings.Split(urlRawQuery, "&amp;")
	} else if strings.Contains(urlRawQuery, "&") {
		requestParms = strings.Split(urlRawQuery, "&")
	}

	for _, param := range requestParms {

		if strings.HasPrefix(param, "type=") {
			parsedVerifyParams.VerificationEmailType = strings.Split(param, "=")[1]
			continue
		}

		if strings.HasPrefix(param, "__t=") {
			parsedVerifyParams.VerificationToken = strings.Split(param, "=")[1]
			continue
		}

		if strings.HasPrefix(param, "request_url=") {
			parsedVerifyParams.RequestedUrl = strings.Split(param, "=")[1]
			continue
		}
	}

	return &parsedVerifyParams
}
