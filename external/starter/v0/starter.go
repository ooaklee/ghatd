// Package starter provides an ejectable Lazy composition layer over modular
// GHATD packages.
//
// It defines the top-level types that wire together repositories, services,
// handlers, and middleware without committing to a specific runtime wiring.
// Consumers can embed, extend, or eject these types into their own main package
// when they need full control.
package starter

import (
	"context"
	"fmt"
	"strings"
)

// Config holds the minimal runtime parameters required to bootstrap the
// application.
type Config struct {
	Port        int
	Environment string
	LogLevel    string
}

// Validate checks every Config field and returns an error if any constraint
// is violated.
func (c Config) Validate() error {
	if c.Port < 1 || c.Port > 65535 {
		return fmt.Errorf("starter/config-invalid-port")
	}
	if !isValidEnvironment(c.Environment) {
		return fmt.Errorf("starter/config-invalid-environment")
	}
	if !isValidLogLevel(c.LogLevel) {
		return fmt.Errorf("starter/config-invalid-log-level")
	}

	return nil
}

// Cleanup is a function that gracefully releases shared resources (database
// connections, background goroutines, etc.) when called.  It should be
// deferred in main.
type Cleanup func(ctx context.Context) error

// Repositories groups every data-layer dependency the application needs.
// Fields are left empty because starter/v0 does not own the concrete
// implementations; it only reserves the shape.
type Repositories struct{}

// Services groups every business-logic dependency the application needs.
type Services struct{}

// Handlers groups every HTTP handler dependency the application needs.
type Handlers struct{}

// Middleware groups every HTTP middleware constructor the application needs.
type Middleware struct{}

// Stack is the top-level composition that holds every layer of the
// application.  Populate its fields in main and pass the resulting value
// to a runner (e.g. a Server) when wiring is ready to be performed.
type Stack struct {
	Config       Config
	Repositories Repositories
	Services     Services
	Handlers     Handlers
	Middleware   Middleware
	Cleanup      Cleanup
}

// isValidEnvironment reports whether environment names a supported runtime environment.
func isValidEnvironment(environment string) bool {
	switch strings.ToLower(strings.TrimSpace(environment)) {
	case "local", "development", "staging", "production":
		return true
	default:
		return false
	}
}

// isValidLogLevel reports whether logLevel names a supported logger severity.
func isValidLogLevel(logLevel string) bool {
	switch strings.ToLower(strings.TrimSpace(logLevel)) {
	case "debug", "info", "warn", "error":
		return true
	default:
		return false
	}
}
