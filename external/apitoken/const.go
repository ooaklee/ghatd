package apitoken

const ApiTokenURIVariableID = "apitokenID"

const (
	// UserTokenStatusKeyRevoked returned when api token in Revoked state
	UserTokenStatusKeyRevoked = "REVOKED"

	// UserTokenStatusKeyActive returned when api token in Active state
	UserTokenStatusKeyActive = "ACTIVE"
)

var (
	// validTokenStatuses holds all the statuses an user token can have
	validTokenStatuses = []string{UserTokenStatusKeyRevoked, UserTokenStatusKeyActive}
)

const (
	// ErrKeyPageOutOfRange returned when requested page is out of range
	ErrKeyPageOutOfRange string = "PageOutOfRange"

	// ErrKeyRequiredUserIDMissing returned when user ID is required but missing
	ErrKeyRequiredUserIDMissing string = "RequiredUserIDMissing"

	// ErrKeyTokenStatusInvalid returned when attempt is made to set token's status as
	// something unsupported
	ErrKeyTokenStatusInvalid string = "TokenStatusInvalid"

	// ErrKeyNoMatchingUserAPITokenFound [code: 200] returned when unable to match encoded secret to
	// any tokens in user's collections
	ErrKeyNoMatchingUserAPITokenFound string = "NoMatchingUserAPITokenFound"

	// ErrKeyUnableToValidateToken [code: 201] returned when system is unable to
	// validate token
	ErrKeyUnableToValidateUserAPIToken string = "UnableToValidateUserAPIToken"

	// ErrKeyUnableToFindRequiredHeaders [code: 202] returned when expected headers for api token are
	// not present
	ErrKeyUnableToFindRequiredHeaders string = "UnableToFindRequiredHeaders"

	// ErrKeyInvalidAPIFormatDetected [code: 203] is returned when expected header for api token is
	// not in the expected none empty format (2 sections)
	ErrKeyInvalidAPIFormatDetected string = "InvalidAPIFormatDetected"

	// ErrKeyResourceNotFound is returned when requested ApiToken resource is not found in repository
	ErrKeyResourceNotFound string = "ResourceNotFound"
)

const (
	GetAPITokenOrderCreatedAtDesc = "created_at_desc"
	GetAPITokenOrderCreatedAtAsc  = "created_at_asc"

	GetAPITokenOrderLastUsedAtDesc = "last_used_at_desc"
	GetAPITokenOrderLastUsedAtAsc  = "last_used_at_asc"

	GetAPITokenOrderUpdatedAtDesc = "updated_at_desc"
	GetAPITokenOrderUpdatedAtAsc  = "updated_at_asc"
)

const (
	APITokenRespositoryFieldPathCreatedAt   = "created_at"
	APITokenRespositoryFieldPathLastUsedAt  = "last_used_at"
	APITokenRespositoryFieldPathUpdatedAt   = "updated_at"
	APITokenRespositoryFieldPathDescription = "description"
	APITokenRespositoryFieldPathStatus      = "status"
	APITokenRespositoryFieldPathCreatedByID = "created_by_id"
)
