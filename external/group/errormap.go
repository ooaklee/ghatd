package group

import (
	"net/http"

	"github.com/ooaklee/reply"
)

// GroupErrorMap holds Error keys, their corresponding human-friendly message, and response status code
// nolint will be used later
var GroupErrorMap reply.ErrorManifest = map[string]reply.ErrorManifestItem{
	ErrKeyGroupConfigNotSet: {
		StatusCode: http.StatusInternalServerError,
		Code:       "GRP0-001",
		Detail:     "An error occurred while processing your request",
	},

	// Validation errors
	ErrKeyInvalidGroupType: {
		StatusCode: http.StatusBadRequest,
		Code:       "GRP0-002",
		Detail:     "The specified group type is not supported",
	},
	ErrKeyInvalidGroupStatus: {
		StatusCode: http.StatusBadRequest,
		Code:       "GRP0-003",
		Detail:     "The specified group status is not valid",
	},
	ErrKeyInvalidStatusTransition: {
		StatusCode: http.StatusBadRequest,
		Code:       "GRP0-004",
		Detail:     "The requested status change is not allowed",
	},
	ErrKeyRequiredFieldMissingName: {
		StatusCode: http.StatusBadRequest,
		Code:       "GRP0-005",
		Detail:     "Please provide a name for the group",
	},
	ErrKeyRequiredFieldMissingType: {
		StatusCode: http.StatusBadRequest,
		Code:       "GRP0-006",
		Detail:     "Please specify a type for the group",
	},
	ErrKeyValidationFailed: {
		StatusCode: http.StatusBadRequest,
		Code:       "GRP0-007",
		Detail:     "The provided group data failed validation",
	},
	ErrKeyInvalidNanoID: {
		StatusCode: http.StatusBadRequest,
		Code:       "GRP0-008",
		Detail:     "The provided ID format is invalid",
	},
	ErrKeyInvalidGroupID: {
		StatusCode: http.StatusBadRequest,
		Code:       "GRP0-009",
		Detail:     "The provided group ID is invalid",
	},
	ErrKeyInvalidGroupBody: {
		StatusCode: http.StatusBadRequest,
		Code:       "GRP0-010",
		Detail:     "The request body contains invalid data",
	},

	// Resource errors
	ErrKeyResourceNotFound: {
		StatusCode: http.StatusNotFound,
		Code:       "GRP0-011",
		Detail:     "The requested group could not be found",
	},
	ErrKeyResourceConflict: {
		StatusCode: http.StatusConflict,
		Code:       "GRP0-012",
		Detail:     "A group with this identifier already exists",
	},
	ErrKeyNameAlreadyExists: {
		StatusCode: http.StatusConflict,
		Code:       "GRP0-013",
		Detail:     "A group with this name is already registered",
	},
	ErrKeyUnableToFindGroupWithName: {
		StatusCode: http.StatusNotFound,
		Code:       "GRP0-014",
		Detail:     "No group found matching the provided name",
	},

	// Member errors
	ErrKeyInvalidMemberType: {
		StatusCode: http.StatusBadRequest,
		Code:       "GRP0-015",
		Detail:     "The specified member type is not valid",
	},
	ErrKeyInvalidMemberRole: {
		StatusCode: http.StatusBadRequest,
		Code:       "GRP0-028",
		Detail:     "The specified member role is not valid",
	},
	ErrKeyMemberNotFound: {
		StatusCode: http.StatusNotFound,
		Code:       "GRP0-016",
		Detail:     "The specified member is not part of this group",
	},
	ErrKeyMemberAlreadyExists: {
		StatusCode: http.StatusConflict,
		Code:       "GRP0-017",
		Detail:     "This member is already part of the group",
	},
	ErrKeyInsufficientPermissions: {
		StatusCode: http.StatusForbidden,
		Code:       "GRP0-018",
		Detail:     "You do not have permission to perform this action",
	},
	ErrKeyInvalidMemberID: {
		StatusCode: http.StatusBadRequest,
		Code:       "GRP0-019",
		Detail:     "The provided member ID is invalid",
	},

	// Hierarchy errors
	ErrKeyCircularReferenceDetected: {
		StatusCode: http.StatusBadRequest,
		Code:       "GRP0-020",
		Detail:     "The operation would create a circular reference in the group structure",
	},
	ErrKeyMaxDepthExceeded: {
		StatusCode: http.StatusBadRequest,
		Code:       "GRP0-021",
		Detail:     "The maximum allowed nesting depth has been exceeded",
	},

	// Database errors
	ErrKeyDatabaseError: {
		StatusCode: http.StatusInternalServerError,
		Code:       "GRP0-022",
		Detail:     "Unable to complete the operation at this time",
	},
	ErrKeyNoChangesDetected: {
		StatusCode: http.StatusBadRequest,
		Code:       "GRP0-023",
		Detail:     "No modifications were detected in the request",
	},

	// Query errors
	ErrKeyInvalidQueryParam: {
		StatusCode: http.StatusBadRequest,
		Code:       "GRP0-024",
		Detail:     "One or more query parameters are invalid",
	},
	ErrKeyInvalidGroupHierarchyTree: {
		StatusCode: http.StatusBadRequest,
		Code:       "GRP0-025",
		Detail:     "The configured group hierarchy tree is invalid",
	},
	ErrKeyInvalidParentChildRelation: {
		StatusCode: http.StatusBadRequest,
		Code:       "GRP0-026",
		Detail:     "The group type cannot be created under the specified parent group",
	},
	ErrKeyGroupDependedOnByOtherGroups: {
		StatusCode: http.StatusConflict,
		Code:       "GRP0-027",
		Detail:     "The group is depended on by other group(s)",
	},
}
