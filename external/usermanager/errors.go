package usermanager

import "errors"

var (
	ErrBulkOperationPartialFailure   = errors.New(ErrKeyBulkOperationPartialFailure)
	ErrFailedToAddUserToGroup        = errors.New(ErrKeyFailedToAddUserToGroup)
	ErrFailedToRemoveUserFromGroup   = errors.New(ErrKeyFailedToRemoveUserFromGroup)
	ErrFailedToResolveGroupAccessMap = errors.New(ErrKeyFailedToResolveGroupAccessMap)
	ErrFailedToUpdateGroupMember     = errors.New(ErrKeyFailedToUpdateGroupMember)
	ErrFailedToUpdateGroupOwner      = errors.New(ErrKeyFailedToUpdateGroupOwner)
	ErrGroupNotFound                 = errors.New(ErrKeyGroupNotFound)
	ErrGroupServiceNotEnabled        = errors.New(ErrKeyGroupServiceNotEnabled)
	ErrInvalidGroupType              = errors.New(ErrKeyInvalidGroupType)
	ErrInvalidMemberID               = errors.New(ErrKeyInvalidMemberID)
	ErrInvalidUserBody               = errors.New(ErrKeyInvalidUserBody)
	ErrNoGroupsFound                 = errors.New(ErrKeyNoGroupsFound)
	ErrNotifierServiceNotEnabled     = errors.New(ErrKeyNotifierServiceNotEnabled)
	ErrRequestFailedValidation       = errors.New(ErrKeyRequestFailedValidation)
	ErrUnableToIdentifyUser          = errors.New(ErrKeyUnableToIdentifyUser)
	ErrUserAlreadyMemberOfGroup      = errors.New(ErrKeyUserAlreadyMemberOfGroup)
	ErrUserManagerError              = errors.New(ErrKeyUserManagerError)
	ErrUserNotFound                  = errors.New(ErrKeyUserNotFound)
)
