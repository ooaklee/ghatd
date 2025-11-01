package group

import (
	"net/http"
)

// ResponseSchema defines the structure for error responses
type ResponseSchema struct {
	HttpStatusCode int    `json:"http_status_code"`
	ErrCode        string `json:"err_code"`
	Message        string `json:"message"`
}

// DefaultGroupErrorResponseMap returns the default error response mapping
func DefaultGroupErrorResponseMap() map[string]ResponseSchema {
	return map[string]ResponseSchema{
		// Configuration errors
		ErrKeyGroupConfigNotSet: {
			HttpStatusCode: http.StatusInternalServerError,
			ErrCode:        ErrKeyGroupConfigNotSet,
			Message:        "Group configuration not set",
		},

		// Validation errors
		ErrKeyInvalidGroupType: {
			HttpStatusCode: http.StatusBadRequest,
			ErrCode:        ErrKeyInvalidGroupType,
			Message:        "Invalid group type provided",
		},
		ErrKeyInvalidGroupStatus: {
			HttpStatusCode: http.StatusBadRequest,
			ErrCode:        ErrKeyInvalidGroupStatus,
			Message:        "Invalid group status provided",
		},
		ErrKeyInvalidStatusTransition: {
			HttpStatusCode: http.StatusBadRequest,
			ErrCode:        ErrKeyInvalidStatusTransition,
			Message:        "Invalid status transition",
		},
		ErrKeyRequiredFieldMissingName: {
			HttpStatusCode: http.StatusBadRequest,
			ErrCode:        ErrKeyRequiredFieldMissingName,
			Message:        "Group name is required",
		},
		ErrKeyRequiredFieldMissingType: {
			HttpStatusCode: http.StatusBadRequest,
			ErrCode:        ErrKeyRequiredFieldMissingType,
			Message:        "Group type is required",
		},
		ErrKeyValidationFailed: {
			HttpStatusCode: http.StatusBadRequest,
			ErrCode:        ErrKeyValidationFailed,
			Message:        "Group validation failed",
		},
		ErrKeyInvalidNanoID: {
			HttpStatusCode: http.StatusBadRequest,
			ErrCode:        ErrKeyInvalidNanoID,
			Message:        "Invalid nano ID format",
		},

		// Resource errors
		ErrKeyResourceNotFound: {
			HttpStatusCode: http.StatusNotFound,
			ErrCode:        ErrKeyResourceNotFound,
			Message:        "Group not found",
		},
		ErrKeyResourceConflict: {
			HttpStatusCode: http.StatusConflict,
			ErrCode:        ErrKeyResourceConflict,
			Message:        "Group already exists",
		},
		ErrKeyNameAlreadyExists: {
			HttpStatusCode: http.StatusConflict,
			ErrCode:        ErrKeyNameAlreadyExists,
			Message:        "Group with this name already exists",
		},
		ErrKeyUnableToFindGroupWithName: {
			HttpStatusCode: http.StatusNotFound,
			ErrCode:        ErrKeyUnableToFindGroupWithName,
			Message:        "Unable to find group with given name",
		},

		// Member errors
		ErrKeyInvalidMemberType: {
			HttpStatusCode: http.StatusBadRequest,
			ErrCode:        ErrKeyInvalidMemberType,
			Message:        "Invalid member type provided",
		},
		ErrKeyMemberNotFound: {
			HttpStatusCode: http.StatusNotFound,
			ErrCode:        ErrKeyMemberNotFound,
			Message:        "Member not found in group",
		},
		ErrKeyMemberAlreadyExists: {
			HttpStatusCode: http.StatusConflict,
			ErrCode:        ErrKeyMemberAlreadyExists,
			Message:        "Member already exists in group",
		},
		ErrKeyInsufficientPermissions: {
			HttpStatusCode: http.StatusForbidden,
			ErrCode:        ErrKeyInsufficientPermissions,
			Message:        "Insufficient permissions to perform this action",
		},

		// Hierarchy errors
		ErrKeyCircularReferenceDetected: {
			HttpStatusCode: http.StatusBadRequest,
			ErrCode:        ErrKeyCircularReferenceDetected,
			Message:        "Circular reference detected in group hierarchy",
		},
		ErrKeyMaxDepthExceeded: {
			HttpStatusCode: http.StatusBadRequest,
			ErrCode:        ErrKeyMaxDepthExceeded,
			Message:        "Maximum nesting depth exceeded",
		},

		// Database errors
		ErrKeyDatabaseError: {
			HttpStatusCode: http.StatusInternalServerError,
			ErrCode:        ErrKeyDatabaseError,
			Message:        "Database error occurred",
		},
		ErrKeyNoChangesDetected: {
			HttpStatusCode: http.StatusBadRequest,
			ErrCode:        ErrKeyNoChangesDetected,
			Message:        "No changes detected",
		},

		// Query errors
		ErrKeyInvalidQueryParam: {
			HttpStatusCode: http.StatusBadRequest,
			ErrCode:        ErrKeyInvalidQueryParam,
			Message:        "Invalid query parameter provided",
		},
		ErrKeyPageOutOfRange: {
			HttpStatusCode: http.StatusBadRequest,
			ErrCode:        ErrKeyPageOutOfRange,
			Message:        "Page number out of range",
		},
	}
}
