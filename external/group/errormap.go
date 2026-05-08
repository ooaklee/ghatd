package group

import (
	"net/http"

	"github.com/ooaklee/reply/v2"
)

// GroupErrorMap holds Error keys, their corresponding human-friendly message, and response status code
// nolint will be used later
var GroupErrorMap reply.ErrorManifest = reply.ErrorManifest{
	ErrGroupConfigNotSet: {
		StatusCode: http.StatusInternalServerError,
		Code:       "GRP0-001",
		Detail:     "An error occurred while processing your request",
	},

	// Validation errors
	ErrInvalidGroupType: {
		StatusCode: http.StatusBadRequest,
		Code:       "GRP0-002",
		Detail:     "The specified group type is not supported",
	},
	ErrInvalidGroupStatus: {
		StatusCode: http.StatusBadRequest,
		Code:       "GRP0-003",
		Detail:     "The specified group status is not valid",
	},
	ErrInvalidStatusTransition: {
		StatusCode: http.StatusBadRequest,
		Code:       "GRP0-004",
		Detail:     "The requested status change is not allowed",
	},
	ErrRequiredFieldMissingName: {
		StatusCode: http.StatusBadRequest,
		Code:       "GRP0-005",
		Detail:     "Please provide a name for the group",
	},
	ErrRequiredFieldMissingType: {
		StatusCode: http.StatusBadRequest,
		Code:       "GRP0-006",
		Detail:     "Please specify a type for the group",
	},
	ErrValidationFailed: {
		StatusCode: http.StatusBadRequest,
		Code:       "GRP0-007",
		Detail:     "The provided group data failed validation",
	},
	ErrInvalidNanoID: {
		StatusCode: http.StatusBadRequest,
		Code:       "GRP0-008",
		Detail:     "The provided ID format is invalid",
	},
	ErrInvalidGroupID: {
		StatusCode: http.StatusBadRequest,
		Code:       "GRP0-009",
		Detail:     "The provided group ID is invalid",
	},
	ErrInvalidGroupBody: {
		StatusCode: http.StatusBadRequest,
		Code:       "GRP0-010",
		Detail:     "The request body contains invalid data",
	},

	// Resource errors
	ErrResourceNotFound: {
		StatusCode: http.StatusNotFound,
		Code:       "GRP0-011",
		Detail:     "The requested group could not be found",
	},
	ErrResourceConflict: {
		StatusCode: http.StatusConflict,
		Code:       "GRP0-012",
		Detail:     "A group with this identifier already exists",
	},
	ErrNameAlreadyExists: {
		StatusCode: http.StatusConflict,
		Code:       "GRP0-013",
		Detail:     "A group with this name is already registered",
	},
	ErrUnableToFindGroupWithName: {
		StatusCode: http.StatusNotFound,
		Code:       "GRP0-014",
		Detail:     "No group found matching the provided name",
	},

	// Member errors
	ErrInvalidMemberType: {
		StatusCode: http.StatusBadRequest,
		Code:       "GRP0-015",
		Detail:     "The specified member type is not valid",
	},
	ErrInvalidMemberRole: {
		StatusCode: http.StatusBadRequest,
		Code:       "GRP0-028",
		Detail:     "The requested member role is not supported by this group type",
	},
	ErrMemberNotFound: {
		StatusCode: http.StatusNotFound,
		Code:       "GRP0-016",
		Detail:     "The specified member is not part of this group",
	},
	ErrMemberAlreadyExists: {
		StatusCode: http.StatusConflict,
		Code:       "GRP0-017",
		Detail:     "This member is already part of the group",
	},
	ErrInsufficientPermissions: {
		StatusCode: http.StatusForbidden,
		Code:       "GRP0-018",
		Detail:     "You do not have permission to perform this action",
	},
	ErrInvalidMemberID: {
		StatusCode: http.StatusBadRequest,
		Code:       "GRP0-019",
		Detail:     "The provided member ID is invalid",
	},
	ErrOwnerRemovalRequiresConfirm: {
		StatusCode: http.StatusConflict,
		Code:       "GRP0-029",
		Detail:     "Removing the current group owner requires explicit confirmation",
	},
	ErrInvalidUserIDProvided: {
		StatusCode: http.StatusBadRequest,
		Code:       "GRP0-030",
		Detail:     "The provided user ID is invalid",
	},
	ErrInvalidInviteEmail: {
		StatusCode: http.StatusBadRequest,
		Code:       "GRP0-031",
		Detail:     "The provided invite email is invalid",
	},
	ErrInvitationAlreadyExists: {
		StatusCode: http.StatusConflict,
		Code:       "GRP0-032",
		Detail:     "An invitation for this email already exists",
	},
	ErrInvitationNotFound: {
		StatusCode: http.StatusNotFound,
		Code:       "GRP0-033",
		Detail:     "No invitation found for the provided email",
	},
	ErrInvalidInvitationState: {
		StatusCode: http.StatusBadRequest,
		Code:       "GRP0-034",
		Detail:     "The invitation state is invalid for this operation",
	},
	ErrInviteRequiresTopLevelGroup: {
		StatusCode: http.StatusBadRequest,
		Code:       "GRP0-035",
		Detail:     "Invite operations are only supported on top-level groups",
	},

	// Hierarchy errors
	ErrCircularReferenceDetected: {
		StatusCode: http.StatusBadRequest,
		Code:       "GRP0-020",
		Detail:     "The operation would create a circular reference in the group structure",
	},
	ErrMaxDepthExceeded: {
		StatusCode: http.StatusBadRequest,
		Code:       "GRP0-021",
		Detail:     "The maximum allowed nesting depth has been exceeded",
	},

	// Database errors
	ErrDatabaseError: {
		StatusCode: http.StatusInternalServerError,
		Code:       "GRP0-022",
		Detail:     "Unable to complete the operation at this time",
	},
	ErrNoChangesDetected: {
		StatusCode: http.StatusBadRequest,
		Code:       "GRP0-023",
		Detail:     "No modifications were detected in the request",
	},

	// Query errors
	ErrInvalidQueryParam: {
		StatusCode: http.StatusBadRequest,
		Code:       "GRP0-024",
		Detail:     "One or more query parameters are invalid",
	},
	ErrInvalidGroupHierarchyTree: {
		StatusCode: http.StatusBadRequest,
		Code:       "GRP0-025",
		Detail:     "The configured group hierarchy tree is invalid",
	},
	ErrInvalidParentChildRelation: {
		StatusCode: http.StatusBadRequest,
		Code:       "GRP0-026",
		Detail:     "The group type cannot be created under the specified parent group",
	},
	ErrGroupDependedOnByOtherGroups: {
		StatusCode: http.StatusConflict,
		Code:       "GRP0-027",
		Detail:     "The group is depended on by other group(s)",
	},
	ErrUnableToIdentifyUser: {
		StatusCode: http.StatusBadRequest,
		Code:       "GRP0-036",
		Detail:     "Unable to identify the user making the request",
	},
	ErrGroupInvalidEmailFormat: {
		StatusCode: http.StatusBadRequest,
		Code:       "GRP0-037",
		Detail:     "The provided email format is invalid",
	},
	ErrGroupEmailIsRequired: {
		StatusCode: http.StatusBadRequest,
		Code:       "GRP0-038",
		Detail:     "An email is required to process this request",
	},
}
