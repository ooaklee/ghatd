package audit

// AuditActorIdSystem is the Id used to identify platform based events
const AuditActorIdSystem string = "SYSTEM"

// AuditAction the supported actions recordable on the platform
type AuditAction string

const (

	// UserEmailOutbound occurs when the system sends an email out
	UserEmailOutbound AuditAction = "USER_EMAIL_OUTBOUND"

	// UserLogin occurs when the user signs in to the account using magic email
	UserLogin AuditAction = "USER_LOGIN"

	// UserLoginSso occurs when the user signs in to the account using SSO
	UserLoginSso AuditAction = "USER_LOGIN_SSO"

	// UserLoginRequest occurs when the user makes a sign in request for their account
	UserLoginRequest AuditAction = "USER_LOGIN_REQUEST"

	// UserLogout occurs when the user signs out of their account
	UserLogout AuditAction = "USER_LOGOUT"

	// UserAccountNew occurs when a new user account is created
	UserAccountNew AuditAction = "USER_ACCOUNT_NEW"

	// UserAccountNewSso occurs when a new user account is created using SSO
	UserAccountNewSso AuditAction = "USER_ACCOUNT_NEW_SSO"

	// UserAccountDelete occurs when a user account is deleted
	UserAccountDelete AuditAction = "USER_ACCOUNT_DELETE"
)

// TargetType is the type of resource being acted on
type TargetType string

const (

	// User represents user or potential user resources
	User TargetType = "USER"
)

// EmailType is the type of email being actioned
type EmailType string

const (
	// Security represents all emails related to
	// login request, change email, deleting account etc.
	Security EmailType = "SECURITY"

	// Other represents all emails related to
	// more general requests, i.e. promotional, newletter, account stats, etc.
	Other EmailType = "OTHER"
)
