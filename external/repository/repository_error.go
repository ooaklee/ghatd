package repository

import (
	"errors"
	"fmt"
)

// RepositoryError represents a repository-level error with error codes
type RepositoryError struct {
	Code    error
	Message string
	Cause   error
}

// NewRepositoryError creates a new repository error
func NewRepositoryError(code error, message string) *RepositoryError {
	return &RepositoryError{
		Code:    code,
		Message: message,
	}
}

// NewRepositoryErrorWithCause creates a new repository error with a cause
func NewRepositoryErrorWithCause(code error, message string, cause error) *RepositoryError {
	return &RepositoryError{
		Code:    code,
		Message: message,
		Cause:   cause,
	}
}

// Error implements the error interface
func (e *RepositoryError) Error() string {
	code := ""
	if e.Code != nil {
		code = e.Code.Error()
	}

	if e.Cause != nil {
		return fmt.Sprintf("[%s] %s: %v", code, e.Message, e.Cause)
	}
	return fmt.Sprintf("[%s] %s", code, e.Message)
}

// Unwrap returns the underlying cause
func (e *RepositoryError) Unwrap() error {
	return e.Cause
}

// Is checks if the error matches a specific error code
func (e *RepositoryError) Is(target error) bool {
	if re, ok := target.(*RepositoryError); ok {
		return errors.Is(e.Code, re.Code)
	}
	return errors.Is(e.Code, target)
}
