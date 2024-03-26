package emailer

// SendVerificationEmailRequest holds information needed to
// send user the email verification email
type SendVerificationEmailRequest struct {

	// Email the targetted user's first name
	FirstName string

	// Email the targetted user's last name
	LastName string

	// Email the targetted user's email
	Email string

	// Token the token that will be used to authorise email verification
	// when clicked
	Token string

	// IsDashboardRequest whether the request originates from
	// the dashboard portal
	IsDashboardRequest bool

	// RequestUrl where the user should be redirected to once
	// signed in
	RequestUrl string `json:"redirect_url"`
}

// SendVerificationEmaiRequestSubstitutes holds the variables to replace in templates when
// sending verification email
type SendVerificationEmaiRequestSubstitutes struct {

	// FullName holds the combination of the user's first and last name
	FullName string `handlebars:"FullName"`

	// Holds the FE url (containing verification) for verifying email
	VerificationURL string `handlebars:"VerificationURL"`

	// LoginURL the Url used for signing in
	LoginURL string `handlebars:"LoginURL"`
}

// SendLoginEmaiRequestSubstitutes holds the variables to replace in templates when
// sending login email
type SendLoginEmaiRequestSubstitutes struct {

	// Holds the FE url (containing login token) for signing user in
	LoginURL string `handlebars:"LoginURL"`
}

// SendLoginEmailRequest holds information needed to
// send user the email log in email
type SendLoginEmailRequest struct {

	// Email the targetted user's email
	Email string

	// Token the token that will be used to confirm user is
	// authorised and authenticated to login in when clicked
	Token string

	// IsDashboardRequest whether the request originates from
	// our dashboard portal
	IsDashboardRequest bool

	// RequestUrl where the user should be redirected to once
	// signed in
	RequestUrl string `json:"redirect_url"`
}

// EmailInfo holds the information about the email generated
type EmailInfo struct {
	To            string
	From          string
	Subject       string
	EmailProvider string
	UserId        string
	RecipientType string
}
