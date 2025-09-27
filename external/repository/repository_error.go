package repository

import (
	"fmt"
)

// RepositoryError represents a repository-level error with error codes
type RepositoryError struct {
	Code    string
	Message string
	Cause   error
}

// NewRepositoryError creates a new repository error
func NewRepositoryError(code, message string) *RepositoryError {
	return &RepositoryError{
		Code:    code,
		Message: message,
	}
}

// NewRepositoryErrorWithCause creates a new repository error with a cause
func NewRepositoryErrorWithCause(code, message string, cause error) *RepositoryError {
	return &RepositoryError{
		Code:    code,
		Message: message,
		Cause:   cause,
	}
}

// Error implements the error interface
func (e *RepositoryError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("[%s] %s: %v", e.Code, e.Message, e.Cause)
	}
	return fmt.Sprintf("[%s] %s", e.Code, e.Message)
}

// Unwrap returns the underlying cause
func (e *RepositoryError) Unwrap() error {
	return e.Cause
}

// Is checks if the error matches a specific error code
func (e *RepositoryError) Is(target error) bool {
	if re, ok := target.(*RepositoryError); ok {
		return e.Code == re.Code
	}
	return false
}
