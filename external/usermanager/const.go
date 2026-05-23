package usermanager

const (
	// UserManagerURIVariableID placeholder for URI variable ID
	UserManagerURIVariableID = "blankpackagID"

	// UserManagerURIVariableGroupID is the URI variable for group ID
	UserManagerURIVariableGroupID = "groupID"

	// UserManagerURIVariableGroupType is the URI variable for group type
	UserManagerURIVariableGroupType = "groupType"

	// UserManagerURIVariableMemberID is the URI variable for member ID
	UserManagerURIVariableMemberID = "memberID"
)

const (

	// ErrKeyRequestFailedValidation is the error key for when the request fails validation
	ErrKeyRequestFailedValidation = "RequestFailedValidation"

	// ErrKeyUserManagerError error key placeholder
	ErrKeyUserManagerError string = "UserManagerError"

	// ErrKeyUnableToIdentifyUser returned when unable to pull user's ID from context
	ErrKeyUnableToIdentifyUser = "UnableToIdentifyUser"

	// ErrKeyInvalidUserBody returned when a request that is request body dependent fails
	// validation
	ErrKeyInvalidUserBody = "UserManagerInvalidUserBody"

	// Group/Team related errors

	// ErrKeyGroupNotFound returned when the requested group cannot be found
	ErrKeyGroupNotFound = "GroupNotFound"

	// ErrKeyUserNotFound returned when the requested user cannot be found
	ErrKeyUserNotFound = "UserNotFound"

	// ErrKeyUserAlreadyMemberOfGroup returned when user is already a member of the group
	ErrKeyUserAlreadyMemberOfGroup = "UserAlreadyMemberOfGroup"

	// ErrKeyFailedToAddUserToGroup returned when adding user to group fails
	ErrKeyFailedToAddUserToGroup = "FailedToAddUserToGroup"

	// ErrKeyFailedToRemoveUserFromGroup returned when removing user from group fails
	ErrKeyFailedToRemoveUserFromGroup = "FailedToRemoveUserFromGroup"

	// ErrKeyFailedToUpdateGroupMember returned when updating a group member fails
	ErrKeyFailedToUpdateGroupMember = "FailedToUpdateGroupMember"

	// ErrKeyInvalidGroupType returned when an invalid group type is provided
	ErrKeyInvalidGroupType = "InvalidGroupType"

	// ErrKeyNoGroupsFound returned when no groups match the search criteria
	ErrKeyNoGroupsFound = "NoGroupsFound"

	// ErrKeyBulkOperationPartialFailure returned when bulk operation has some failures
	ErrKeyBulkOperationPartialFailure = "BulkOperationPartialFailure"

	// ErrKeyGroupServiceNotEnabled is returned when group features are requested but GroupService is not configured
	ErrKeyGroupServiceNotEnabled = "GroupServiceNotEnabled"

	// ErrKeyNotifierServiceNotEnabled is returned when notification features are requested but NotifierService is not configured
	ErrKeyNotifierServiceNotEnabled = "NotifierServiceNotEnabled"

	// ErrKeyFailedToUpdateGroupOwner returned when updating group owner fails
	ErrKeyFailedToUpdateGroupOwner = "FailedToUpdateGroupOwner"

	// ErrKeyFailedToResolveGroupAccessMap returned when group access map resolution fails
	ErrKeyFailedToResolveGroupAccessMap = "FailedToResolveGroupAccessMap"

	// ErrKeyInvalidMemberID returned when the provided member ID is invalid or empty
	ErrKeyInvalidMemberID = "InvalidMemberID"

	// UserManagerURIVariableAddressID is the URI variable for notification address ID
	UserManagerURIVariableAddressID = "addressID"

	// UserManagerURIVariableReminderID is the URI variable for reminder ID
	UserManagerURIVariableReminderID = "reminderID"
)

const (
	// ErrKeyReminderServiceNotEnabled is returned when reminder features are requested but ReminderService is not configured
	ErrKeyReminderServiceNotEnabled = "ReminderServiceNotEnabled"

	// ErrKeyStreakServiceNotEnabled is returned when streak features are requested but StreakService is not configured
	ErrKeyStreakServiceNotEnabled = "StreakServiceNotEnabled"
)
