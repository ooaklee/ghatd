package partnerhttp

// Error is a safe transport outcome. Causes and dependency diagnostics never
// enter its representation. Handler admits only the documented code/status map.
type Error struct {
	Code   string
	Status int
}

// Error returns the public machine-readable code as the error message.
func (e *Error) Error() string { return e.Code }

// fail builds the transport Error from a public code and HTTP status.
func fail(code string, status int) error { return &Error{Code: code, Status: status} }
