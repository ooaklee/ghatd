package apitoken

import "errors"

var (
	// ErrPageOutOfRange returned when requested page is out of range
	ErrPageOutOfRange = errors.New("PageOutOfRange")

	// ErrRequiredUserIDMissing returned when user ID is required but missing
	ErrRequiredUserIDMissing = errors.New("RequiredUserIDMissing")

	// ErrTokenStatusInvalid returned when attempt is made to set token's status as
	// something unsupported
	ErrTokenStatusInvalid = errors.New("TokenStatusInvalid")

	// ErrNoMatchingUserAPITokenFound returned when unable to match encoded secret to
	// any tokens in user's collections
	ErrNoMatchingUserAPITokenFound = errors.New("NoMatchingUserAPITokenFound")

	// ErrUnableToValidateToken returned when system is unable to
	// validate token
	ErrUnableToValidateUserAPIToken = errors.New("UnableToValidateUserAPIToken")

	// ErrUnableToFindRequiredHeaders returned when expected headers for api token are
	// not present
	ErrUnableToFindRequiredHeaders = errors.New("UnableToFindRequiredHeaders")

	// ErrInvalidAPIFormatDetected is returned when expected header for api token is
	// not in the expected none empty format (2 sections)
	ErrInvalidAPIFormatDetected = errors.New("InvalidAPIFormatDetected")

	// ErrResourceNotFound is returned when requested ApiToken resource is not found in repository
	ErrResourceNotFound = errors.New("ResourceNotFound")

	// ErrErrorCreatingShortLivedAccessToken is returned when a failure occurs while attempting
	// to create a shortlived token
	ErrErrorCreatingShortLivedAccessToken = errors.New("ErrorCreatingShortLivedAccessToken")
)
