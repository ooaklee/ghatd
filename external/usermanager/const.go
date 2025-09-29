package usermanager

const UserManagerURIVariableID = "blankpackagID"

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
)
