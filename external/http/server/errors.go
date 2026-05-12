package server

import (
	"errors"
)

var (
	// ErrNilRequest is returned when a server operation receives a nil request.
	ErrNilRequest = errors.New("server/nil-request")

	// ErrMissingHandler is returned when server configuration does not include a handler.
	ErrMissingHandler = errors.New("server/handler-required")

	// ErrMissingAddressData is returned when server configuration cannot resolve a listen address.
	ErrMissingAddressData = errors.New("server/address-data-required")

	// ErrNonPositiveTimeout is returned when graceful shutdown is configured with a non-positive timeout.
	ErrNonPositiveTimeout = errors.New("server/graceful-shutdown-timeout-must-be-positive")

	// ErrStartupFailure is returned when ListenAndServe fails before shutdown.
	ErrStartupFailure = errors.New("server/startup-failure")

	// ErrShutdownFailure is returned when graceful shutdown fails.
	ErrShutdownFailure = errors.New("server/shutdown-failure")
)
