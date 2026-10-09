package partnerhttp

// Error is a safe transport outcome. Causes and dependency diagnostics never
// enter its representation. Handler admits only the documented code/status map.
type Error struct {
	Code   string
	Status int
}

func (e *Error) Error() string { return e.Code }

func fail(code string, status int) error { return &Error{Code: code, Status: status} }
