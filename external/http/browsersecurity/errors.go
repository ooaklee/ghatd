package browsersecurity

import "errors"

// Safe sentinel outcomes contain no host credentials or dependency diagnostics.
// Transports map these to their own documented status and public error codes.
var (
	ErrConfigurationInvalid   = errors.New("browsersecurity/configuration-invalid")
	ErrAuthenticationRequired = errors.New("browsersecurity/authentication-required")
	ErrForbidden              = errors.New("browsersecurity/forbidden")
	ErrInvalidRequest         = errors.New("browsersecurity/invalid-request")
)
