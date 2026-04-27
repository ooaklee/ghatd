package usermanager

import (
	"github.com/ooaklee/reply"
)

// UsermanagerErrorMap holds Error keys, their corresponding human-friendly message, and response status code
var UsermanagerErrorMap reply.ErrorManifest = map[string]reply.ErrorManifestItem{
	// General errors
	ErrKeyUserManagerError:        {Title: "Bad Request", Detail: "Some user manager related error.", StatusCode: 400, Code: "USM00-001"},
	ErrKeyUnableToIdentifyUser:    {Title: "Unauthorised", Detail: "Please contact support.", StatusCode: 401, Code: "USM00-002"},
	ErrKeyInvalidUserBody:         {Title: "Bad Request", Detail: "Check your submitted user information.", StatusCode: 400, Code: "USM00-003"},
	ErrKeyRequestFailedValidation: {Title: "Bad Request", Detail: "Request failed validation, please check provided data.", StatusCode: 400, Code: "USM00-004"},

	// Group/Team related errors
	ErrKeyGroupNotFound:               {Title: "Not Found", Detail: "The requested group could not be found.", StatusCode: 404, Code: "USM00-005"},
	ErrKeyUserNotFound:                {Title: "Not Found", Detail: "The requested user could not be found.", StatusCode: 404, Code: "USM00-006"},
	ErrKeyUserAlreadyMemberOfGroup:    {Title: "Conflict", Detail: "User is already a member of this group.", StatusCode: 409, Code: "USM00-007"},
	ErrKeyFailedToAddUserToGroup:      {Title: "Internal Error", Detail: "Failed to add user to the group. Please try again.", StatusCode: 500, Code: "USM00-008"},
	ErrKeyFailedToRemoveUserFromGroup: {Title: "Internal Error", Detail: "Failed to remove user from the group. Please try again.", StatusCode: 500, Code: "USM00-009"},
	ErrKeyInvalidGroupType:            {Title: "Bad Request", Detail: "The provided group type is invalid.", StatusCode: 400, Code: "USM00-010"},
	ErrKeyNoGroupsFound:               {Title: "Not Found", Detail: "No groups match the search criteria.", StatusCode: 404, Code: "USM00-011"},
	ErrKeyBulkOperationPartialFailure: {Title: "Partial Success", Detail: "Some operations in the bulk update failed. Check response details.", StatusCode: 207, Code: "USM00-012"},
	ErrKeyGroupServiceNotEnabled:      {Title: "Service Unavailable", Detail: "Group management features have not been enabled for this service.", StatusCode: 503, Code: "USM00-013"},
	ErrKeyFailedToUpdateGroupOwner:    {Title: "Internal Error", Detail: "Failed to update group owner. Please try again.", StatusCode: 500, Code: "USM00-014"},
	ErrKeyInvalidMemberID:             {Title: "Bad Request", Detail: "A valid member ID must be provided.", StatusCode: 400, Code: "USM00-015"},
	ErrKeyFailedToResolveGroupAccessMap: {
		Title:      "Internal Error",
		Detail:     "Failed to resolve user access for the requested group. Please try again.",
		StatusCode: 500,
		Code:       "USM00-017",
	},
}
