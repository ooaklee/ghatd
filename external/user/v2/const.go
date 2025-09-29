package user

const (
	// ErrKeyUserConfigNotSet is used when user configuration is missing
	ErrKeyUserConfigNotSet string = "UserConfigNotSet"

	// ErrKeyUserInvalidTargetStatus is used when the target status provided for user update is invalid
	ErrKeyUserInvalidTargetStatus string = "UserInvalidTargetStatus"

	// ErrKeyUserInvalidStatusTransition is used when the status transition is not allowed
	ErrKeyUserInvalidStatusTransition string = "UserInvalidStatusTransition"

	// ErrKeyUserRequiredFieldMissingEmail is used when the required field email is missing
	ErrKeyUserRequiredFieldMissingEmail string = "UserRequiredFieldMissingEmail"

	// ErrKeyUserRequiredFieldMissingFirstName is used when the required field first name is missing
	ErrKeyUserRequiredFieldMissingFirstName string = "UserRequiredFieldMissingFirstName"

	// ErrKeyUserRequiredFieldMissingLastName is used when the required field last name is missing
	ErrKeyUserRequiredFieldMissingLastName string = "UserRequiredFieldMissingLastName"

	// ErrKeyUserInvalidStatus is used when the user has an invalid status assigned
	ErrKeyUserInvalidStatus string = "UserInvalidStatus"

	// ErrKeyUserInvalidRole is used when the user has an invalid role assigned
	ErrKeyUserInvalidRole string = "UserInvalidRole"
)
