package oauth

// OauthUserInfo is an interface that holds
// all the methods of a valid oauth provider user info
type OauthUserInfo interface {
	GetUserEmail() string
	GetUserFirstName() string
	GetUserLastName() string
	IsUserEmailVerifiedByProvider() bool
}
