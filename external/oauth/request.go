package oauth

// NewGoogleProviderRequest holds needed to create
// a google oauth provider
type NewGoogleProviderRequest struct {

	// RedirectURL the url the user should be redirected to when verified
	RedirectURL string

	// ClientID our google credentials Id
	ClientID string

	// ClientSecret our google credentials secrets
	ClientSecret string
}
