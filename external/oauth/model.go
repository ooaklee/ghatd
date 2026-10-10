package oauth

// OauthUserInfo is an interface that holds
// all the methods of a valid oauth provider user info
type OauthUserInfo interface {
	// GetUserEmail returns the email address the OAuth provider reported for the
	// user.
	GetUserEmail() string
	// GetUserFirstName returns the user's given name as supplied by the OAuth
	// provider.
	GetUserFirstName() string
	// GetUserLastName returns the user's family name as supplied by the OAuth
	// provider.
	GetUserLastName() string
	// IsUserEmailVerifiedByProvider reports whether the OAuth provider verified the
	// user's email address.
	IsUserEmailVerifiedByProvider() bool
}
