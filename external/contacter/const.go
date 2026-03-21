package contacter

// Error keys
const (

	// ErrKeyInvalidCommsPayload is the error key for when the comms payload is invalid
	ErrKeyInvalidCommsPayload = "InvalidCommsPayload"

	// ErrKeyFullNameRequired is the error key for when full name is required but not provided
	ErrKeyFullNameRequired = "ContacterFullNameRequired"

	// ErrKeyEmailRequired is the error key for when email is required but not provided
	ErrKeyEmailRequired = "ContacterEmailRequired"

	// ErrKeyCommsIdRequired is the error key for when comms ID is required but not provided
	ErrKeyCommsIdRequired = "ContacterCommsIdRequired"

	// ErrKeyCommsNotFound is the error key for when comms is not found
	ErrKeyCommsNotFound = "ContacterCommsNotFound"
)
