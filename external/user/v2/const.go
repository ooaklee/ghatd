package user

const (
	// Account Status Keys
	AccountStatusKeyProvisioned = "PROVISIONED"
	AccountStatusKeyActive      = "ACTIVE"
	AccountStatusKeyDeactivated = "DEACTIVATED"
	AccountStatusKeyLockedOut   = "LOCKED_OUT"
	AccountStatusKeyRecovery    = "RECOVERY"
	AccountStatusKeySuspended   = "SUSPENDED"

	// Account Status Valid Origin Keys
	AccountStatusValidOriginKeyReactivate  = "REACTIVATE"
	AccountStatusValidOriginKeyUnsuspend   = "UNSUSPEND"
	AccountStatusValidOriginKeyUnlock      = "UNLOCK"
	AccountStatusValidOriginKeyEmailChange = "EMAIL_CHANGE"
	AccountStatusValidOriginKeyVerifyEmail = "VERIFY_EMAIL"
)

const (
	ErrKeyUserConfigNotSet                  = "UserConfigNotSet"
	ErrKeyUserInvalidTargetStatus           = "UserInvalidTargetStatus"
	ErrKeyUserInvalidStatusTransition       = "UserInvalidStatusTransition"
	ErrKeyUserRequiredFieldMissingEmail     = "UserRequiredFieldMissingEmail"
	ErrKeyUserRequiredFieldMissingFirstName = "UserRequiredFieldMissingFirstName"
	ErrKeyUserRequiredFieldMissingLastName  = "UserRequiredFieldMissingLastName"
	ErrKeyUserInvalidStatus                 = "UserInvalidStatus"
	ErrKeyUserInvalidRole                   = "UserInvalidRole"
	ErrKeyUserNeverActivated                = "UserNeverActivated"
	ErrKeyInvalidUserOriginStatus           = "InvalidUserOriginStatus"
	ErrKeyInvalidUserBody                   = "InvalidCreateUserBody"
	ErrKeyResourceConflict                  = "UserResourceConflict"
	ErrKeyInvalidQueryParam                 = "InvalidQueryParam"
	ErrKeyPageOutOfRange                    = "PageOutOfRange"
	ErrKeyInvalidUserID                     = "KeyInvalidUserID"
	ErrKeyResourceNotFound                  = "UserResourceNotFound"
	ErrKeyNoChangesDetected                 = "UserNoChangesDetected"
	ErrKeyInvalidEmail                      = "UserInvalidEmail"
	ErrKeyEmailAlreadyExists                = "UserEmailAlreadyExists"
	ErrKeyUserNotFound                      = "UserNotFound"
	ErrKeyUnauthorisedAccess                = "UserUnauthorisedAccess"
	ErrKeyExtensionNotFound                 = "UserExtensionNotFound"
	ErrKeyValidationFailed                  = "UserValidationFailed"
	ErrKeyDatabaseError                     = "UserDatabaseError"
	ErrKeyInvalidNanoID                     = "UserInvalidNanoID"
)

const (
	// User Order Constants
	GetUserOrderCreatedAtDesc       = "created_at_desc"
	GetUserOrderCreatedAtAsc        = "created_at_asc"
	GetUserOrderUpdatedAtDesc       = "updated_at_desc"
	GetUserOrderUpdatedAtAsc        = "updated_at_asc"
	GetUserOrderLastLoginAtDesc     = "last_login_at_desc"
	GetUserOrderLastLoginAtAsc      = "last_login_at_asc"
	GetUserOrderActivatedAtDesc     = "activated_at_desc"
	GetUserOrderActivatedAtAsc      = "activated_at_asc"
	GetUserOrderStatusChangedAtDesc = "status_changed_at_desc"
	GetUserOrderStatusChangedAtAsc  = "status_changed_at_asc"
	GetUserOrderEmailVerifiedAtDesc = "email_verified_at_desc"
	GetUserOrderEmailVerifiedAtAsc  = "email_verified_at_asc"
)

const (
	// URI Variables
	UserURIVariableID           = "userID"
	UserURIVariableNanoID       = "nanoID"
	UserURIVariableExtensionKey = "extensionKey"

	// User Roles
	UserRoleAdmin = "ADMIN"
	UserRoleUser  = "USER"
)
