package user

const (
	// AccountStatusKeyProvisioned returned when user in provisioned state
	AccountStatusKeyProvisioned = "PROVISIONED"

	// AccountStatusKeyActive returned when user in Active state
	AccountStatusKeyActive = "ACTIVE"

	// AccountStatusKeyDeactivated returned when user in Deactivated state
	AccountStatusKeyDeactivated = "DEACTIVATED"

	// AccountStatusKeyLockedOut returned when user in locked out state
	AccountStatusKeyLockedOut = "LOCKED_OUT"

	// AccountStatusKeyRecovery returned when user in Recovery state
	AccountStatusKeyRecovery = "RECOVERY"

	// AccountStatusKeySuspended returned when user in Suspended state
	AccountStatusKeySuspended = "SUSPENDED"

	// AccountStatusValidOriginKeyReactivate returned for reactivate state
	AccountStatusValidOriginKeyReactivate = "REACTIVATE"

	// AccountStatusValidOriginKeyUnsuspend returned for Unsuspend state
	AccountStatusValidOriginKeyUnsuspend = "UNSUSPEND"

	// AccountStatusValidOriginKeyUnlock returned for Unlock state
	AccountStatusValidOriginKeyUnlock = "UNLOCK"

	// AccountStatusValidOriginKeyEmailChange returned for email change state
	AccountStatusValidOriginKeyEmailChange = "EMAIL_CHANGE"

	// AccountStatusValidOriginKeyVerifyEmail returned for verify email state
	AccountStatusValidOriginKeyVerifyEmail = "VERIFY_EMAIL"
)

const (
	// ErrKeyUserNeverActivated returned when user was never actived and statusis set to reactivate
	ErrKeyUserNeverActivated string = "UserNeverActivated"

	// ErrKeyInvalidUserOriginStatus returned when user is not in the correct state for the requested status change
	ErrKeyInvalidUserOriginStatus string = "InvalidUserOriginStatus"

	// ErrKeyInvalidUserBody returned when error occurs while decoding user request body
	ErrKeyInvalidUserBody string = "InvalidCreateUserBody"

	// ErrKeyResourceConflict returned when attempted resource creation clashes with an existing resource
	ErrKeyResourceConflict = "UserResourceConflict"

	// ErrKeyInvalidQueryParam returned when invalid query param(s) passed with request
	ErrKeyInvalidQueryParam string = "InvalidQueryParam"

	// ErrKeyPageOutOfRange returned when requested page is out of range
	ErrKeyPageOutOfRange string = "PageOutOfRange"

	// ErrKeyInvalidUserID returned when user ID missing or incorrectly formatted
	ErrKeyInvalidUserID string = "KeyInvalidUserID"

	// ErrKeyResourceNotFound returned when user resource not found
	ErrKeyResourceNotFound string = "UserResourceNotFound"

	// ErrKeyNoChangesDetected returned when no changes detected on persistent resource vs requested
	// changes
	ErrKeyNoChangesDetected string = "UserNoChangesDetected"
)

const (
	// ResponseMetaKeyUsersPerPage key used to outline the number of users in response requested
	// per page
	ResponseMetaKeyUsersPerPage = "resources_per_page"

	// ResponseMetaKeyTotalUsers key used to outline the number of total user matching filter
	ResponseMetaKeyTotalUsers = "total_resources"

	// ResponseMetaKeyTotalPages key used to outline the number pages user can query through
	ResponseMetaKeyTotalPages = "total_pages"

	// ResponseMetaKeyPage key used to outline the result page the client is looking at
	ResponseMetaKeyPage = "page"
)

const (
	GetUserOrderCreatedAtDesc = "created_at_desc"
	GetUserOrderCreatedAtAsc  = "created_at_asc"

	GetUserOrderLastLoginAtDesc = "last_login_at_desc"
	GetUserOrderLastLoginAtAsc  = "last_login_at_asc"

	GetUserOrderActivatedAtDesc = "activated_at_desc"
	GetUserOrderActivatedAtAsc  = "activated_at_asc"

	GetUserOrderStatusChangedAtDesc = "status_changed_at_desc"
	GetUserOrderStatusChangedAtAsc  = "status_changed_at_asc"

	GetUserOrderLastFreshLoginAtDesc = "last_fresh_login_at_desc"
	GetUserOrderLastFreshLoginAtAsc  = "last_fresh_login_at_asc"

	GetUserOrderEmailVerifiedAtDesc = "email_verified_at_desc"
	GetUserOrderEmailVerifiedAtAsc  = "email_verified_at_asc"
)

const (
	UserRespositoryFieldPathCreatedAt        = "meta.created_at"
	UserRespositoryFieldPathLastLoginAt      = "meta.last_login_at"
	UserRespositoryFieldPathActivatedAt      = "meta.activated_at"
	UserRespositoryFieldPathStatusChangedAt  = "meta.status_changed_at"
	UserRespositoryFieldPathLastFreshLoginAt = "meta.last_fresh_login_at"
	UserRespositoryFieldPathVerifiedAt       = "verified.email_verified_at"
	UserRespositoryFieldPathFirstName        = "first_name"
	UserRespositoryFieldPathLastName         = "last_name"
	UserRespositoryFieldPathStatus           = "status"
	UserRespositoryFieldPathRoles            = "roles"
	UserRespositoryFieldPathEmail            = "email"
)

const (
	// UserURIVariableID holds the identifier for the user ID in the URI
	UserURIVariableID = "userID"

	// UserRoleAdmin holds the identifier for user admin role
	UserRoleAdmin = "ADMIN"

	// UserRoleReader holds the identifier for user reader role
	UserRoleReader = "READER"
)
