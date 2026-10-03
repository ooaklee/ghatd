package contentmanager

const (
	// ErrKeyUnauthorisedCMUser is the error key return when a user id being used for an
	// operation the user is NOT permitted to car out
	ErrKeyUnauthorisedCMUser = "UnauthorisedCMUser"
	// ErrKeyContentManagerUnavailable identifies an invalid service dependency result.
	ErrKeyContentManagerUnavailable = "contentmanager/unavailable"
)
