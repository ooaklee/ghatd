// Package validator provides input validation functionality for request
// data validation across the application.
//
// It wraps the go-playground/validator package with convenience methods
// and custom validation rules.
package validator

import (
	validator "github.com/go-playground/validator/v10"
)

// Validator defines custom validator
type Validator struct {
	validator *validator.Validate
}

// New creates a new validator
func NewValidator() *Validator {
	return &Validator{
		validator: createValidator(),
	}

}

// validate makes sure passed struct meets validation rules
func (v *Validator) Validate(s interface{}) error {
	return v.validator.Struct(s)
}

func createValidator() *validator.Validate {

	v := validator.New(validator.WithRequiredStructEnabled())

	return v
}
