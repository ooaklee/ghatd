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
