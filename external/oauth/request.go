package oauth

import "net/http"

// NewGoogleProviderRequest holds needed to create
// a google oauth provider
type NewGoogleProviderRequest struct {

	// RedirectURL the url the user should be redirected to when verified
	RedirectURL string

	// ClientID our google credentials Id
	ClientID string

	// ClientSecret our google credentials secrets
	ClientSecret string

	// HTTPClient carries timeouts, trace propagation, and transport policy for
	// both the token exchange and user-info request.
	HTTPClient *http.Client
}
