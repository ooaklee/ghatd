package usermanager

import (
	"github.com/ooaklee/reply/v2"
)

// UsermanagerErrorMap holds Error keys, their corresponding human-friendly message, and response status code
var UsermanagerErrorMap reply.ErrorManifest = reply.ErrorManifest{
	// General errors
	ErrUserManagerError:        {Title: "Bad Request", Detail: "Some user manager related error.", StatusCode: 400, Code: "USM00-001"},
	ErrUnableToIdentifyUser:    {Title: "Unauthorised", Detail: "Please contact support.", StatusCode: 401, Code: "USM00-002"},
	ErrInvalidUserBody:         {Title: "Bad Request", Detail: "Check your submitted user information.", StatusCode: 400, Code: "USM00-003"},
	ErrRequestFailedValidation: {Title: "Bad Request", Detail: "Request failed validation, please check provided data.", StatusCode: 400, Code: "USM00-004"},

	// Group/Team related errors
	ErrGroupNotFound:               {Title: "Not Found", Detail: "The requested group could not be found.", StatusCode: 404, Code: "USM00-005"},
	ErrUserNotFound:                {Title: "Not Found", Detail: "The requested user could not be found.", StatusCode: 404, Code: "USM00-006"},
	ErrUserAlreadyMemberOfGroup:    {Title: "Conflict", Detail: "User is already a member of this group.", StatusCode: 409, Code: "USM00-007"},
	ErrFailedToAddUserToGroup:      {Title: "Internal Error", Detail: "Failed to add user to the group. Please try again.", StatusCode: 500, Code: "USM00-008"},
	ErrFailedToRemoveUserFromGroup: {Title: "Internal Error", Detail: "Failed to remove user from the group. Please try again.", StatusCode: 500, Code: "USM00-009"},
	ErrFailedToUpdateGroupMember:   {Title: "Internal Error", Detail: "Failed to update group member. Please try again.", StatusCode: 500, Code: "USM00-016"},
	ErrInvalidGroupType:            {Title: "Bad Request", Detail: "The provided group type is invalid.", StatusCode: 400, Code: "USM00-010"},
	ErrNoGroupsFound:               {Title: "Not Found", Detail: "No groups match the search criteria.", StatusCode: 404, Code: "USM00-011"},
	ErrBulkOperationPartialFailure: {Title: "Partial Success", Detail: "Some operations in the bulk update failed. Check response details.", StatusCode: 207, Code: "USM00-012"},
	ErrGroupServiceNotEnabled:      {Title: "Service Unavailable", Detail: "Group management features have not been enabled for this service.", StatusCode: 503, Code: "USM00-013"},
	ErrFailedToUpdateGroupOwner:    {Title: "Internal Error", Detail: "Failed to update group owner. Please try again.", StatusCode: 500, Code: "USM00-014"},
	ErrInvalidMemberID:             {Title: "Bad Request", Detail: "A valid member ID must be provided.", StatusCode: 400, Code: "USM00-015"},
	ErrNotifierServiceNotEnabled:   {Title: "Service Unavailable", Detail: "Notification features have not been enabled for this service.", StatusCode: 503, Code: "USM00-018"},
	ErrReminderServiceNotEnabled:   {Title: "Service Unavailable", Detail: "Reminder features have not been enabled for this service.", StatusCode: 503, Code: "USM00-019"},
	ErrStreakServiceNotEnabled:     {Title: "Service Unavailable", Detail: "Streak features have not been enabled for this service.", StatusCode: 503, Code: "USM00-020"},
	ErrVisionServiceNotEnabled:     {Title: "Service Unavailable", Detail: "Vision features have not been enabled for this service.", StatusCode: 503, Code: "USM00-021"},
	ErrVisionEditForbidden:         {Title: "Forbidden", Detail: "Only the feedback owner or a platform administrator can edit this item.", StatusCode: 403, Code: "USM00-022"},
	ErrVisionDeleteForbidden:       {Title: "Forbidden", Detail: "Only the feedback owner or a platform administrator can delete this item.", StatusCode: 403, Code: "USM00-023"},
	ErrFailedToResolveGroupAccessMap: {
		Title:      "Internal Error",
		Detail:     "Failed to resolve user access for the requested group. Please try again.",
		StatusCode: 500,
		Code:       "USM00-017",
	},
}
